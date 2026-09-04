package incache

import (
	"sync"
	"time"
)

// lfuEntry is one cached item and, at the same time, a node in the circular
// doubly-linked list of its frequency bucket. Threading the list through the
// entry itself, rather than storing the entry inside a container/list
// element, means a frequency increment can relink the existing node instead
// of allocating a fresh element for the new bucket - which is what makes an
// LFU Get allocation-free.
type lfuEntry[K comparable, V any] struct {
	prev, next *lfuEntry[K, V]
	key        K
	value      V
	expireAt   int64 // Unix nano timestamp, 0 means no expiration
	freq       uint
}

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
	mu    sync.Mutex
	size  uint
	items map[K]*lfuEntry[K, V]
	// freqHeads maps a frequency to the head of that frequency's circular
	// list of entries. The list is circular, so head.prev is its tail: the
	// least recently used entry at that frequency, which is the one
	// eviction takes. A frequency is present in this map only while it
	// holds at least one entry, so emptied buckets are dropped rather than
	// accumulating for the lifetime of the cache.
	freqHeads map[uint]*lfuEntry[K, V]
	// minFreq is a lower bound on the smallest frequency currently in
	// freqHeads, not necessarily an exact value: removing entries can raise
	// the true minimum without updating it. Eviction repairs it lazily via
	// updateMinFreq, which keeps Delete O(1) instead of making every delete
	// that empties a bucket scan all of them.
	minFreq uint

	_ cacheLinePad
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
		items:     make(map[K]*lfuEntry[K, V]),
		freqHeads: make(map[uint]*lfuEntry[K, V]),
	}
}

func (c *LFUCache[K, V]) shardFor(k K) *lfuShard[K, V] {
	return c.shards[shardIndexFor(k, len(c.shards))]
}

// freqPushFront links e into the front of bucket f, creating the bucket if
// it does not exist yet. It assumes s.mu is already held by the caller.
func (s *lfuShard[K, V]) freqPushFront(f uint, e *lfuEntry[K, V]) {
	head := s.freqHeads[f]
	if head == nil {
		e.prev, e.next = e, e
	} else {
		e.prev, e.next = head.prev, head
		head.prev.next = e
		head.prev = e
	}
	s.freqHeads[f] = e
}

// freqRemove unlinks e from bucket f, deleting the bucket once it is empty.
// Dropping empty buckets is what keeps freqHeads bounded by the number of
// live frequencies rather than by the number of accesses ever made.
// It assumes s.mu is already held by the caller.
func (s *lfuShard[K, V]) freqRemove(f uint, e *lfuEntry[K, V]) {
	if e.next == e {
		delete(s.freqHeads, f)
	} else {
		e.prev.next = e.next
		e.next.prev = e.prev
		if s.freqHeads[f] == e {
			s.freqHeads[f] = e.next
		}
	}
	e.prev, e.next = nil, nil
}

// incrementFreq moves an entry into the next frequency bucket - O(1), and
// with no allocation, because the entry node itself is relinked.
// It assumes s.mu is already held by the caller.
func (s *lfuShard[K, V]) incrementFreq(e *lfuEntry[K, V]) {
	old := e.freq
	s.freqRemove(old, e)

	// If the bucket just emptied and it was the minimum, nothing is left
	// below old+1, which is where this entry is about to land - so old+1 is
	// the new minimum.
	if old == s.minFreq {
		if _, ok := s.freqHeads[old]; !ok {
			s.minFreq = old + 1
		}
	}

	e.freq = old + 1
	s.freqPushFront(e.freq, e)
}

// removeEntry drops e from both the item map and its frequency bucket.
// It deliberately leaves minFreq alone: minFreq is only ever used as a
// starting point for eviction, which repairs it if it has gone stale.
// It assumes s.mu is already held by the caller.
func (s *lfuShard[K, V]) removeEntry(e *lfuEntry[K, V]) {
	s.freqRemove(e.freq, e)
	delete(s.items, e.key)
}

// updateMinFreq resets minFreq to the smallest live frequency. Because
// empty buckets are never left in freqHeads, the frequency it picks always
// has an entry to evict.
// It assumes s.mu is already held by the caller.
func (s *lfuShard[K, V]) updateMinFreq() {
	s.minFreq = 0
	for freq := range s.freqHeads {
		if s.minFreq == 0 || freq < s.minFreq {
			s.minFreq = freq
		}
	}
}

// Set adds the key-value pair to the cache.
func (c *LFUCache[K, V]) Set(key K, value V) {
	c.shardFor(key).setLocked(key, value, 0)
}

// SetWithTimeout adds the key-value pair to the cache with a specified expiration time.
func (c *LFUCache[K, V]) SetWithTimeout(key K, value V, exp time.Duration) {
	c.shardFor(key).setLocked(key, value, exp)
}

func (s *lfuShard[K, V]) setLocked(key K, value V, exp time.Duration) {
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
	if e, ok := s.items[key]; ok {
		e.value = value
		e.expireAt = expireAt
		s.incrementFreq(e)
		return
	}

	// Evict if at capacity
	if uint(len(s.items)) >= s.size {
		s.evict(1)
	}

	// Create new entry with frequency 1
	e := &lfuEntry[K, V]{
		key:      key,
		value:    value,
		freq:     1,
		expireAt: expireAt,
	}
	s.freqPushFront(1, e)
	s.items[key] = e
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

	e, ok := s.items[key]
	if !ok {
		return
	}

	// Check expiration
	if e.expireAt > 0 && e.expireAt < time.Now().UnixNano() {
		s.removeEntry(e)
		return
	}

	s.incrementFreq(e)
	return e.value, true
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

	if e, ok := s.items[k]; ok {
		// Check if existing key is expired
		if e.expireAt == 0 || e.expireAt >= time.Now().UnixNano() {
			return false
		}
		// Key exists but is expired, delete it first
		s.removeEntry(e)
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
	m := make(map[K]V, c.Len())
	for _, s := range c.shards {
		s.getAllInto(m)
	}
	return m
}

func (s *lfuShard[K, V]) getAllInto(m map[K]V) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UnixNano()
	for k, e := range s.items {
		if e.expireAt == 0 || e.expireAt >= now {
			m[k] = e.value
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
	toTransfer := make(map[K]V, len(s.items))

	// Deleting from a map while ranging over it is defined behavior in Go,
	// so the collected entries can be removed in the same pass.
	for k, e := range s.items {
		if e.expireAt != 0 && e.expireAt < now {
			continue
		}
		toTransfer[k] = e.value
		s.removeEntry(e)
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
	toCopy := make(map[K]V, len(s.items))

	for k, e := range s.items {
		if e.expireAt == 0 || e.expireAt >= now {
			toCopy[k] = e.value
		}
	}

	return toCopy
}

// Keys returns a slice of all keys currently stored in the cache.
// The returned slice does not include expired keys.
// The order of keys in the slice is not guaranteed.
func (c *LFUCache[K, V]) Keys() []K {
	keys := make([]K, 0, c.Len())
	for _, s := range c.shards {
		keys = s.appendKeys(keys)
	}
	return keys
}

// appendKeys appends the shard's non-expired keys to dst. Appending into
// the caller's slice avoids allocating, and then copying, a fresh slice per
// shard.
func (s *lfuShard[K, V]) appendKeys(dst []K) []K {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UnixNano()
	for k, e := range s.items {
		if e.expireAt == 0 || e.expireAt >= now {
			dst = append(dst, k)
		}
	}

	return dst
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

	s.items = make(map[K]*lfuEntry[K, V])
	s.freqHeads = make(map[uint]*lfuEntry[K, V])
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
	for _, e := range s.items {
		if e.expireAt == 0 || e.expireAt >= now {
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

	if e, ok := s.items[k]; ok {
		s.removeEntry(e)
	}
}

// evict removes n entries with the lowest frequency - O(1) per entry,
// apart from the rare updateMinFreq scan when minFreq has gone stale.
// It assumes s.mu is already held by the caller.
func (s *lfuShard[K, V]) evict(n int) {
	for i := 0; i < n && len(s.items) > 0; i++ {
		head, ok := s.freqHeads[s.minFreq]
		if !ok {
			// minFreq is a lower bound that deletions have left behind;
			// find the real minimum and try again.
			s.updateMinFreq()
			head, ok = s.freqHeads[s.minFreq]
			if !ok {
				return
			}
		}

		// The bucket is circular, so head.prev is its tail: the least
		// recently used entry at the minimum frequency.
		s.removeEntry(head.prev)
	}
}
