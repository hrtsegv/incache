package incache

import (
	"container/list"
	"sync"
	"time"
)

// LFUCache implements a Least Frequently Used cache with O(1) operations.
// It uses frequency buckets to efficiently track and evict items.
//
// Once a cache grows large enough (see numShardsFor), it is internally
// split into shards, each with its own lock and frequency buckets, to
// reduce lock contention under concurrent access. This means eviction is
// only strictly by global frequency for small caches (below the sharding
// threshold); larger caches evict the least-frequently-used item per shard,
// which approximates - but does not guarantee - a single global order.
// GetAll, Keys, Count and Purge each lock one shard at a time rather than
// the whole cache, so they are not an atomic snapshot under concurrent
// writes, matching the pre-sharding behavior at the single-map level.
type LFUCache[K comparable, V any] struct {
	size   uint
	shards []*lfuShard[K, V]
	flight flightGroup[K, V]
}

// lfuShard is one partition of a sharded LFUCache. It holds the same
// frequency-bucket eviction logic a non-sharded LFUCache used before
// sharding was introduced; LFUCache just routes each key to one of these by
// hash.
type lfuShard[K comparable, V any] struct {
	mu        sync.Mutex
	size      uint
	minFreq   uint
	items     map[K]*list.Element // key → list element containing lfuItem
	freqLists map[uint]*list.List // frequency → list of items with that frequency
}

type lfuItem[K comparable, V any] struct {
	key      K
	value    V
	freq     uint
	expireAt int64 // Unix nano timestamp, 0 means no expiration
}

// NewLFU creates a new LFU cache with the specified maximum size.
// If size is 0, the cache will not store any items.
func NewLFU[K comparable, V any](size uint) *LFUCache[K, V] {
	n := numShardsFor(size)
	sizes := shardSizes(size, n)
	shards := make([]*lfuShard[K, V], n)
	for i, sz := range sizes {
		shards[i] = newLFUShard[K, V](sz)
	}
	return &LFUCache[K, V]{size: size, shards: shards}
}

func newLFUShard[K comparable, V any](size uint) *lfuShard[K, V] {
	return &lfuShard[K, V]{
		size:      size,
		items:     make(map[K]*list.Element),
		freqLists: make(map[uint]*list.List),
	}
}

func (c *LFUCache[K, V]) shardFor(k K) *lfuShard[K, V] {
	return c.shards[shardIndexFor(k, len(c.shards))]
}

// Set adds the key-value pair to the cache.
func (c *LFUCache[K, V]) Set(key K, value V) {
	s := c.shardFor(key)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.set(key, value, 0)
}

// SetWithTimeout adds the key-value pair to the cache with a specified expiration time.
func (c *LFUCache[K, V]) SetWithTimeout(key K, value V, exp time.Duration) {
	s := c.shardFor(key)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.set(key, value, exp)
}

// set assumes s.mu is already held by the caller.
func (s *lfuShard[K, V]) set(key K, value V, exp time.Duration) {
	if s.size == 0 {
		return
	}

	var expireAt int64
	if exp > 0 {
		expireAt = time.Now().Add(exp).UnixNano()
	}

	// Check if key already exists
	if elem, ok := s.items[key]; ok {
		item := elem.Value.(*lfuItem[K, V])
		item.value = value
		item.expireAt = expireAt
		s.incrementFreq(elem)
		return
	}

	// Evict if at capacity
	if uint(len(s.items)) >= s.size {
		s.evict(1)
	}

	// Create new item with frequency 1
	item := &lfuItem[K, V]{
		key:      key,
		value:    value,
		freq:     1,
		expireAt: expireAt,
	}

	// Add to frequency 1 list
	if s.freqLists[1] == nil {
		s.freqLists[1] = list.New()
	}
	elem := s.freqLists[1].PushFront(item)
	s.items[key] = elem
	s.minFreq = 1
}

// Get retrieves the value associated with the given key from the cache.
// If the key is not found or has expired, it returns (zero value of V, false).
// Otherwise, it returns (value, true).
func (c *LFUCache[K, V]) Get(key K) (v V, b bool) {
	return c.shardFor(key).get(key)
}

func (s *lfuShard[K, V]) get(key K) (v V, b bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	elem, ok := s.items[key]
	if !ok {
		return
	}

	item := elem.Value.(*lfuItem[K, V])

	// Check expiration
	if item.expireAt > 0 && item.expireAt < time.Now().UnixNano() {
		s.delete(key, elem)
		return
	}

	s.incrementFreq(elem)
	return item.value, true
}

// incrementFreq moves an item to the next frequency bucket - O(1) operation
func (s *lfuShard[K, V]) incrementFreq(elem *list.Element) {
	item := elem.Value.(*lfuItem[K, V])
	oldFreq := item.freq
	newFreq := oldFreq + 1

	// Remove from old frequency list
	oldList := s.freqLists[oldFreq]
	oldList.Remove(elem)

	// Update minFreq if necessary
	if oldFreq == s.minFreq && oldList.Len() == 0 {
		s.minFreq = newFreq
		delete(s.freqLists, oldFreq)
	}

	// Add to new frequency list
	item.freq = newFreq
	if s.freqLists[newFreq] == nil {
		s.freqLists[newFreq] = list.New()
	}
	newElem := s.freqLists[newFreq].PushFront(item)
	s.items[item.key] = newElem
}

// NotFoundSet adds the key-value pair to the cache only if the key does not exist or is expired.
// It returns true if the key was added to the cache, otherwise false.
func (c *LFUCache[K, V]) NotFoundSet(k K, v V) bool {
	return c.shardFor(k).notFoundSet(k, v, 0)
}

// NotFoundSetWithTimeout adds the key-value pair to the cache only if the key does not exist or is expired.
// It sets an expiration time for the key-value pair.
// It returns true if the key was added to the cache, otherwise false.
func (c *LFUCache[K, V]) NotFoundSetWithTimeout(k K, v V, t time.Duration) bool {
	return c.shardFor(k).notFoundSet(k, v, t)
}

func (s *lfuShard[K, V]) notFoundSet(k K, v V, t time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if elem, ok := s.items[k]; ok {
		item := elem.Value.(*lfuItem[K, V])
		// Check if existing key is expired
		if item.expireAt == 0 || item.expireAt >= time.Now().UnixNano() {
			return false
		}
		// Key exists but is expired, delete it first
		s.delete(k, elem)
	}

	s.set(k, v, t)
	return true
}

// GetOrSet returns the existing value for k if present and not expired.
// Otherwise it calls fn to compute a value, stores the result with Set, and
// returns it. Concurrent GetOrSet calls for the same missing key are
// coalesced so fn runs at most once at a time per key; an error from fn is
// returned to every waiter and nothing is cached.
func (c *LFUCache[K, V]) GetOrSet(k K, fn func() (V, error)) (V, error) {
	if v, ok := c.Get(k); ok {
		return v, nil
	}

	return c.flight.do(k, func() (V, error) {
		if v, ok := c.Get(k); ok {
			return v, nil
		}

		v, err := fn()
		if err != nil {
			return v, err
		}

		c.Set(k, v)
		return v, nil
	})
}

// GetOrSetWithTimeout is GetOrSet, but stores a successful result with
// SetWithTimeout instead of Set.
func (c *LFUCache[K, V]) GetOrSetWithTimeout(k K, fn func() (V, error), timeout time.Duration) (V, error) {
	if v, ok := c.Get(k); ok {
		return v, nil
	}

	return c.flight.do(k, func() (V, error) {
		if v, ok := c.Get(k); ok {
			return v, nil
		}

		v, err := fn()
		if err != nil {
			return v, err
		}

		c.SetWithTimeout(k, v, timeout)
		return v, nil
	})
}

// GetAll retrieves all key-value pairs from the cache.
// It returns a map containing all the key-value pairs that are not expired.
func (c *LFUCache[K, V]) GetAll() map[K]V {
	m := make(map[K]V)
	for _, s := range c.shards {
		s.getAllInto(m)
	}
	return m
}

func (s *lfuShard[K, V]) getAllInto(m map[K]V) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UnixNano()
	for k, elem := range s.items {
		item := elem.Value.(*lfuItem[K, V])
		if item.expireAt == 0 || item.expireAt >= now {
			m[k] = item.value
		}
	}
}

// TransferTo transfers all non-expired key-value pairs from the source cache to the destination cache.
// The operation is performed in a deadlock-safe manner by not holding both locks simultaneously.
func (src *LFUCache[K, V]) TransferTo(dst *LFUCache[K, V]) {
	for _, s := range src.shards {
		for k, v := range s.collectAndClear() {
			dst.Set(k, v)
		}
	}
}

// collectAndClear removes and returns all non-expired entries in the shard.
func (s *lfuShard[K, V]) collectAndClear() map[K]V {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UnixNano()
	toTransfer := make(map[K]V)
	var keysToDelete []K

	for k, elem := range s.items {
		item := elem.Value.(*lfuItem[K, V])
		if item.expireAt == 0 || item.expireAt >= now {
			toTransfer[k] = item.value
			keysToDelete = append(keysToDelete, k)
		}
	}

	for _, k := range keysToDelete {
		if elem, ok := s.items[k]; ok {
			s.delete(k, elem)
		}
	}

	return toTransfer
}

// CopyTo copies all non-expired key-value pairs from the source cache to the destination cache.
// The operation is performed in a deadlock-safe manner by not holding both locks simultaneously.
func (src *LFUCache[K, V]) CopyTo(dst *LFUCache[K, V]) {
	for _, s := range src.shards {
		for k, v := range s.snapshot() {
			dst.Set(k, v)
		}
	}
}

// snapshot returns a copy of all non-expired entries in the shard.
func (s *lfuShard[K, V]) snapshot() map[K]V {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UnixNano()
	toCopy := make(map[K]V)

	for k, elem := range s.items {
		item := elem.Value.(*lfuItem[K, V])
		if item.expireAt == 0 || item.expireAt >= now {
			toCopy[k] = item.value
		}
	}

	return toCopy
}

// Keys returns a slice of all keys currently stored in the cache.
// The returned slice does not include expired keys.
// The order of keys in the slice is not guaranteed.
func (c *LFUCache[K, V]) Keys() []K {
	var keys []K
	for _, s := range c.shards {
		keys = append(keys, s.keys()...)
	}
	return keys
}

func (s *lfuShard[K, V]) keys() []K {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UnixNano()
	keys := make([]K, 0, len(s.items))

	for k, elem := range s.items {
		item := elem.Value.(*lfuItem[K, V])
		if item.expireAt == 0 || item.expireAt >= now {
			keys = append(keys, k)
		}
	}
	return keys
}

// Purge removes all key-value pairs from the cache.
func (c *LFUCache[K, V]) Purge() {
	for _, s := range c.shards {
		s.purge()
	}
}

func (s *lfuShard[K, V]) purge() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.items = make(map[K]*list.Element)
	s.freqLists = make(map[uint]*list.List)
	s.minFreq = 0
}

// Count returns the number of non-expired key-value pairs currently stored in the cache.
func (c *LFUCache[K, V]) Count() int {
	count := 0
	for _, s := range c.shards {
		count += s.count()
	}
	return count
}

func (s *lfuShard[K, V]) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	count := 0
	now := time.Now().UnixNano()
	for _, elem := range s.items {
		item := elem.Value.(*lfuItem[K, V])
		if item.expireAt == 0 || item.expireAt >= now {
			count++
		}
	}
	return count
}

// Len returns the total number of elements in the cache (including expired ones).
func (c *LFUCache[K, V]) Len() int {
	n := 0
	for _, s := range c.shards {
		n += s.len()
	}
	return n
}

func (s *lfuShard[K, V]) len() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.items)
}

// Delete removes the key-value pair associated with the given key from the cache.
func (c *LFUCache[K, V]) Delete(k K) {
	c.shardFor(k).deleteKey(k)
}

func (s *lfuShard[K, V]) deleteKey(k K) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if elem, ok := s.items[k]; ok {
		s.delete(k, elem)
	}
}

// delete assumes s.mu is already held by the caller.
func (s *lfuShard[K, V]) delete(key K, elem *list.Element) {
	item := elem.Value.(*lfuItem[K, V])
	freq := item.freq

	// Remove from frequency list
	freqList := s.freqLists[freq]
	if freqList != nil {
		freqList.Remove(elem)
		if freqList.Len() == 0 {
			delete(s.freqLists, freq)
			// Update minFreq if necessary
			if freq == s.minFreq {
				s.updateMinFreq()
			}
		}
	}

	delete(s.items, key)
}

func (s *lfuShard[K, V]) updateMinFreq() {
	s.minFreq = 0
	for freq := range s.freqLists {
		if s.minFreq == 0 || freq < s.minFreq {
			s.minFreq = freq
		}
	}
}

// evict removes n items with the lowest frequency - O(1) per item
func (s *lfuShard[K, V]) evict(n int) {
	for i := 0; i < n && len(s.items) > 0; i++ {
		// Get the list with minimum frequency
		minList := s.freqLists[s.minFreq]
		if minList == nil || minList.Len() == 0 {
			s.updateMinFreq()
			minList = s.freqLists[s.minFreq]
			if minList == nil || minList.Len() == 0 {
				return
			}
		}

		// Remove the least recently used item from the minimum frequency list (back of list)
		elem := minList.Back()
		if elem == nil {
			return
		}

		item := elem.Value.(*lfuItem[K, V])
		s.delete(item.key, elem)
	}
}
