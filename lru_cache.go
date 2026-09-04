package incache

import (
	"sync"
	"time"
)

// lruEntry is one cached item and, at the same time, a node in its shard's
// circular doubly-linked recency list. Threading the list through the entry
// itself, rather than storing the entry inside a container/list element,
// halves the per-item allocation count (one node instead of an element plus
// a separate item) and removes both the interface boxing and the type
// assertion container/list forces on every Get.
type lruEntry[K comparable, V any] struct {
	prev, next *lruEntry[K, V]
	key        K
	value      V
	expireAt   int64 // Unix nano timestamp, 0 means no expiration
}

// LRUCache implements a Least Recently Used cache with O(1) operations.
//
// Once a cache grows large enough (see numShardsFor), it is internally
// split into shards, each with its own lock and eviction list, to reduce
// lock contention under concurrent access. This means eviction order is
// only strictly global for small caches (below the sharding threshold);
// larger caches evict the least-recently-used item per shard, which
// approximates - but does not guarantee - a single global LRU order.
// GetAll, Keys, Count and Purge each lock one shard at a time rather than
// the whole cache, so they are not an atomic snapshot under concurrent
// writes, matching the pre-sharding behavior at the single-map level.
type LRUCache[K comparable, V any] struct {
	size   uint
	shards []*lruShard[K, V]
	flight flightGroup[K, V]
}

// lruShard is one partition of a sharded LRUCache. It holds the same
// eviction logic a non-sharded LRUCache used before sharding was
// introduced; LRUCache just routes each key to one of these by hash.
type lruShard[K comparable, V any] struct {
	mu   sync.Mutex
	size uint
	m    map[K]*lruEntry[K, V]
	// head is the most recently used entry. The list is circular, so
	// head.prev is the least recently used entry - the eviction candidate -
	// and no sentinel node is needed. A nil head means the shard is empty.
	head *lruEntry[K, V]
}

// NewLRU creates a new LRU cache with the specified maximum size.
// If size is 0, the cache will not store any items.
func NewLRU[K comparable, V any](size uint) *LRUCache[K, V] {
	n := numShardsFor(size)
	sizes := shardSizes(size, n)
	shards := make([]*lruShard[K, V], n)
	for i, sz := range sizes {
		shards[i] = newLRUShard[K, V](sz)
	}
	return &LRUCache[K, V]{size: size, shards: shards}
}

func newLRUShard[K comparable, V any](size uint) *lruShard[K, V] {
	return &lruShard[K, V]{
		size: size,
		m:    make(map[K]*lruEntry[K, V]),
	}
}

func (c *LRUCache[K, V]) shardFor(k K) *lruShard[K, V] {
	return c.shards[shardIndexFor(k, len(c.shards))]
}

// pushFront links e into the recency list as the most recently used entry.
// It assumes s.mu is already held by the caller.
func (s *lruShard[K, V]) pushFront(e *lruEntry[K, V]) {
	if s.head == nil {
		e.prev, e.next = e, e
	} else {
		e.prev, e.next = s.head.prev, s.head
		s.head.prev.next = e
		s.head.prev = e
	}
	s.head = e
}

// unlink removes e from the recency list.
// It assumes s.mu is already held by the caller.
func (s *lruShard[K, V]) unlink(e *lruEntry[K, V]) {
	if e.next == e {
		s.head = nil
	} else {
		e.prev.next = e.next
		e.next.prev = e.prev
		if s.head == e {
			s.head = e.next
		}
	}
	e.prev, e.next = nil, nil
}

// moveToFront marks e, which must already be in the list, as the most
// recently used entry. This runs on every cache hit, so it relinks e in
// place instead of going through unlink followed by pushFront.
// It assumes s.mu is already held by the caller.
func (s *lruShard[K, V]) moveToFront(e *lruEntry[K, V]) {
	if s.head == e {
		return
	}
	// e is in the list and is not the head, so the list holds at least two
	// entries and every pointer touched below is non-nil.
	e.prev.next = e.next
	e.next.prev = e.prev

	h := s.head
	e.prev, e.next = h.prev, h
	h.prev.next = e
	h.prev = e
	s.head = e
}

// removeEntry drops e from both the map and the recency list.
// It assumes s.mu is already held by the caller.
func (s *lruShard[K, V]) removeEntry(e *lruEntry[K, V]) {
	delete(s.m, e.key)
	s.unlink(e)
}

// Get retrieves the value associated with the given key from the cache.
// If the key is not found or has expired, it returns (zero value of V, false).
// Otherwise, it returns (value, true).
func (c *LRUCache[K, V]) Get(k K) (V, bool) {
	return c.shardFor(k).get(k)
}

func (s *lruShard[K, V]) get(k K) (v V, b bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.m[k]
	if !ok {
		return
	}

	if e.expireAt > 0 && e.expireAt < time.Now().UnixNano() {
		s.removeEntry(e)
		return
	}

	s.moveToFront(e)

	return e.value, true
}

// GetAll retrieves all key-value pairs from the cache.
// It returns a map containing all the key-value pairs that are not expired.
func (c *LRUCache[K, V]) GetAll() map[K]V {
	m := make(map[K]V, c.Len())
	for _, s := range c.shards {
		s.getAllInto(m)
	}
	return m
}

func (s *lruShard[K, V]) getAllInto(m map[K]V) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UnixNano()
	for k, e := range s.m {
		if e.expireAt == 0 || e.expireAt >= now {
			m[k] = e.value
		}
	}
}

// Set adds the key-value pair to the cache.
func (c *LRUCache[K, V]) Set(k K, v V) {
	c.shardFor(k).setLocked(k, v, 0)
}

// SetWithTimeout adds the key-value pair to the cache with a specified expiration time.
func (c *LRUCache[K, V]) SetWithTimeout(k K, v V, t time.Duration) {
	c.shardFor(k).setLocked(k, v, t)
}

func (s *lruShard[K, V]) setLocked(k K, v V, exp time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.set(k, v, exp)
}

// NotFoundSet adds the key-value pair to the cache only if the key does not exist or is expired.
// It returns true if the key was added to the cache, otherwise false.
func (c *LRUCache[K, V]) NotFoundSet(k K, v V) bool {
	return c.shardFor(k).notFoundSet(k, v, 0)
}

// NotFoundSetWithTimeout adds the key-value pair to the cache only if the key does not exist or is expired.
// It sets an expiration time for the key-value pair.
// It returns true if the key was added to the cache, otherwise false.
func (c *LRUCache[K, V]) NotFoundSetWithTimeout(k K, v V, t time.Duration) bool {
	return c.shardFor(k).notFoundSet(k, v, t)
}

func (s *lruShard[K, V]) notFoundSet(k K, v V, t time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if e, ok := s.m[k]; ok {
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
func (c *LRUCache[K, V]) GetOrSet(k K, fn func() (V, error)) (V, error) {
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
func (c *LRUCache[K, V]) GetOrSetWithTimeout(k K, fn func() (V, error), timeout time.Duration) (V, error) {
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

// Delete removes the key-value pair associated with the given key from the cache.
func (c *LRUCache[K, V]) Delete(k K) {
	c.shardFor(k).delete(k)
}

func (s *lruShard[K, V]) delete(k K) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if e, ok := s.m[k]; ok {
		s.removeEntry(e)
	}
}

// TransferTo transfers all non-expired key-value pairs from the source cache to the destination cache.
// The operation is performed in a deadlock-safe manner by not holding both locks simultaneously.
func (src *LRUCache[K, V]) TransferTo(dst *LRUCache[K, V]) {
	for _, s := range src.shards {
		for k, v := range s.collectAndClear() {
			dst.Set(k, v)
		}
	}
}

// collectAndClear removes and returns all non-expired entries in the shard.
func (s *lruShard[K, V]) collectAndClear() map[K]V {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UnixNano()
	toTransfer := make(map[K]V, len(s.m))

	// Deleting from a map while ranging over it is defined behavior in Go,
	// so the collected entries can be removed in the same pass.
	for k, e := range s.m {
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
func (src *LRUCache[K, V]) CopyTo(dst *LRUCache[K, V]) {
	for _, s := range src.shards {
		for k, v := range s.snapshot() {
			dst.Set(k, v)
		}
	}
}

// snapshot returns a copy of all non-expired entries in the shard.
func (s *lruShard[K, V]) snapshot() map[K]V {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UnixNano()
	toCopy := make(map[K]V, len(s.m))

	for k, e := range s.m {
		if e.expireAt == 0 || e.expireAt >= now {
			toCopy[k] = e.value
		}
	}

	return toCopy
}

// Keys returns a slice of all keys currently stored in the cache.
// The returned slice does not include expired keys.
// The order of keys in the slice is not guaranteed.
func (c *LRUCache[K, V]) Keys() []K {
	keys := make([]K, 0, c.Len())
	for _, s := range c.shards {
		keys = s.appendKeys(keys)
	}
	return keys
}

// appendKeys appends the shard's non-expired keys to dst. Appending into
// the caller's slice avoids allocating, and then copying, a fresh slice per
// shard.
func (s *lruShard[K, V]) appendKeys(dst []K) []K {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UnixNano()
	for k, e := range s.m {
		if e.expireAt == 0 || e.expireAt >= now {
			dst = append(dst, k)
		}
	}

	return dst
}

// Purge removes all key-value pairs from the cache.
func (c *LRUCache[K, V]) Purge() {
	for _, s := range c.shards {
		s.purge()
	}
}

func (s *lruShard[K, V]) purge() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.m = make(map[K]*lruEntry[K, V])
	s.head = nil
}

// Count returns the number of non-expired key-value pairs currently stored in the cache.
func (c *LRUCache[K, V]) Count() int {
	count := 0
	for _, s := range c.shards {
		count += s.count()
	}
	return count
}

func (s *lruShard[K, V]) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	count := 0
	now := time.Now().UnixNano()
	for _, e := range s.m {
		if e.expireAt == 0 || e.expireAt >= now {
			count++
		}
	}

	return count
}

// Len returns the total number of elements in the cache (including expired ones).
func (c *LRUCache[K, V]) Len() int {
	n := 0
	for _, s := range c.shards {
		n += s.len()
	}
	return n
}

func (s *lruShard[K, V]) len() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.m)
}

// set assumes s.mu is already held by the caller.
func (s *lruShard[K, V]) set(k K, v V, exp time.Duration) {
	if s.size == 0 {
		return
	}

	var expireAt int64
	if exp > 0 {
		expireAt = time.Now().Add(exp).UnixNano()
	}

	if e, ok := s.m[k]; ok {
		e.value = v
		e.expireAt = expireAt
		s.moveToFront(e)
		return
	}

	if uint(len(s.m)) >= s.size {
		s.evict(1)
	}

	e := &lruEntry[K, V]{
		key:      k,
		value:    v,
		expireAt: expireAt,
	}
	s.pushFront(e)
	s.m[k] = e
}

// evict removes the i least recently used entries.
// It assumes s.mu is already held by the caller.
func (s *lruShard[K, V]) evict(i int) {
	for j := 0; j < i; j++ {
		if s.head == nil {
			return
		}
		// The list is circular, so head.prev is its tail: the least
		// recently used entry.
		s.removeEntry(s.head.prev)
	}
}
