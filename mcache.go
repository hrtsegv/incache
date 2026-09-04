package incache

import (
	"sync"
	"sync/atomic"
	"time"
)

// MCache is a simple cache with manual/no eviction policy.
// When the cache is full and a new item needs to be added,
// it first tries to evict expired items, then evicts random items if needed.
//
// Once a cache grows large enough (see numShardsFor), it is internally
// split into shards, each with its own lock, to reduce lock contention
// under concurrent access. A single background goroutine still handles TTL
// sweeping for the whole cache (one shard at a time) rather than one
// goroutine per shard. GetAll, Keys, Count and Purge each lock one shard at
// a time rather than the whole cache, so they are not an atomic snapshot
// under concurrent writes, matching the pre-sharding behavior at the
// single-map level.
type MCache[K comparable, V any] struct {
	size         uint
	shards       []*mcacheShard[K, V]
	stopCh       chan struct{} // Channel to signal timeout goroutine to stop
	timeInterval time.Duration // Time interval to sleep the goroutine that checks for expired keys
	closed       atomic.Bool   // set once Close has run, guards against use-after-close panics
	flight       flightGroup[K, V]
}

// mcacheShard is one partition of a sharded MCache. It holds the same
// map-of-values-with-timeout a non-sharded MCache used before sharding was
// introduced; MCache just routes each key to one of these by hash.
type mcacheShard[K comparable, V any] struct {
	mu   sync.Mutex
	size uint
	m    map[K]valueWithTimeout[V] // where the key-value pairs are stored
}

type valueWithTimeout[V any] struct {
	value    V
	expireAt int64 // Unix nano timestamp, 0 means no expiration
}

// NewManual creates a new cache instance with optional configuration provided by the specified options.
// The cache starts a background goroutine to periodically check for expired keys based on the configured time interval.
// If size is 0, the cache will not store any items.
func NewManual[K comparable, V any](size uint, timeInterval time.Duration) *MCache[K, V] {
	n := numShardsFor(size)
	sizes := shardSizes(size, n)
	shards := make([]*mcacheShard[K, V], n)
	for i, sz := range sizes {
		shards[i] = newMCacheShard[K, V](sz)
	}

	c := &MCache[K, V]{
		size:         size,
		shards:       shards,
		stopCh:       make(chan struct{}),
		timeInterval: timeInterval,
	}
	if c.size > 0 && c.timeInterval > 0 {
		go c.expireKeys()
	}
	return c
}

func newMCacheShard[K comparable, V any](size uint) *mcacheShard[K, V] {
	return &mcacheShard[K, V]{
		size: size,
		m:    make(map[K]valueWithTimeout[V]),
	}
}

func (c *MCache[K, V]) shardFor(k K) *mcacheShard[K, V] {
	return c.shards[shardIndexFor(k, len(c.shards))]
}

// Set adds or updates a key-value pair in the database without setting an expiration time.
// If the key already exists, its value will be overwritten with the new value.
// This function is safe for concurrent use.
func (c *MCache[K, V]) Set(k K, v V) {
	if c.size == 0 || c.closed.Load() {
		return
	}

	c.shardFor(k).set(k, v, 0)
}

// NotFoundSet adds a key-value pair to the database if the key does not already exist or is expired, and returns true.
// Otherwise, it does nothing and returns false.
func (c *MCache[K, V]) NotFoundSet(k K, v V) bool {
	if c.size == 0 || c.closed.Load() {
		return false
	}

	return c.shardFor(k).notFoundSet(k, v, 0)
}

// SetWithTimeout adds or updates a key-value pair in the database with an expiration time.
// If the timeout duration is zero or negative, the key-value pair will not have an expiration time.
// This function is safe for concurrent use.
func (c *MCache[K, V]) SetWithTimeout(k K, v V, timeout time.Duration) {
	if c.size == 0 || c.closed.Load() {
		return
	}

	c.shardFor(k).set(k, v, timeout)
}

// NotFoundSetWithTimeout adds a key-value pair to the database with an expiration time if the key does not already exist or is expired, and returns true.
// Otherwise, it does nothing and returns false.
// If the timeout is zero or negative, the key-value pair will not have an expiration time.
func (c *MCache[K, V]) NotFoundSetWithTimeout(k K, v V, timeout time.Duration) bool {
	if c.size == 0 || c.closed.Load() {
		return false
	}

	return c.shardFor(k).notFoundSet(k, v, timeout)
}

func (s *mcacheShard[K, V]) set(k K, v V, timeout time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Close nils out the map, and may have run since the caller checked the
	// closed flag. Re-checking here, under the shard lock, is what actually
	// makes a write racing with Close a no-op instead of a panic.
	if s.m == nil {
		return
	}

	var expireAt int64
	if timeout > 0 {
		expireAt = time.Now().Add(timeout).UnixNano()
	}

	// If key exists, just update
	if _, ok := s.m[k]; ok {
		s.m[k] = valueWithTimeout[V]{
			value:    v,
			expireAt: expireAt,
		}
		return
	}

	if uint(len(s.m)) >= s.size {
		s.evict(1)
	}

	s.m[k] = valueWithTimeout[V]{
		value:    v,
		expireAt: expireAt,
	}
}

func (s *mcacheShard[K, V]) notFoundSet(k K, v V, timeout time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	// See set: guards against Close having landed since the closed check.
	if s.m == nil {
		return false
	}

	if val, ok := s.m[k]; ok {
		// Check if existing key is expired
		if val.expireAt == 0 || val.expireAt >= time.Now().UnixNano() {
			return false
		}
		// Key exists but is expired, delete it
		delete(s.m, k)
	}

	var expireAt int64
	if timeout > 0 {
		expireAt = time.Now().Add(timeout).UnixNano()
	}

	if uint(len(s.m)) >= s.size {
		s.evict(1)
	}

	s.m[k] = valueWithTimeout[V]{
		value:    v,
		expireAt: expireAt,
	}
	return true
}

// Get retrieves the value associated with the given key from the cache.
// If the key is not found or has expired, it returns (zero value of V, false).
// Otherwise, it returns (value, true).
func (c *MCache[K, V]) Get(k K) (v V, b bool) {
	return c.shardFor(k).get(k)
}

func (s *mcacheShard[K, V]) get(k K) (v V, b bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	val, ok := s.m[k]
	if !ok {
		return
	}
	if val.expireAt > 0 && val.expireAt < time.Now().UnixNano() {
		delete(s.m, k)
		return
	}
	return val.value, true
}

// GetOrSet returns the existing value for k if present and not expired.
// Otherwise it calls fn to compute a value, stores the result with Set, and
// returns it. Concurrent GetOrSet calls for the same missing key are
// coalesced so fn runs at most once at a time per key; an error from fn is
// returned to every waiter and nothing is cached.
func (c *MCache[K, V]) GetOrSet(k K, fn func() (V, error)) (V, error) {
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
func (c *MCache[K, V]) GetOrSetWithTimeout(k K, fn func() (V, error), timeout time.Duration) (V, error) {
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
func (c *MCache[K, V]) GetAll() map[K]V {
	m := make(map[K]V)
	for _, s := range c.shards {
		s.getAllInto(m)
	}
	return m
}

func (s *mcacheShard[K, V]) getAllInto(m map[K]V) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UnixNano()
	for k, v := range s.m {
		if v.expireAt == 0 || v.expireAt >= now {
			m[k] = v.value
		}
	}
}

// Delete removes the key-value pair associated with the given key from the cache.
func (c *MCache[K, V]) Delete(k K) {
	c.shardFor(k).delete(k)
}

func (s *mcacheShard[K, V]) delete(k K) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, k)
}

// TransferTo transfers all non-expired key-value pairs from the source cache to the destination cache.
// The operation is performed in a deadlock-safe manner by not holding both locks simultaneously.
func (src *MCache[K, V]) TransferTo(dst *MCache[K, V]) {
	for _, s := range src.shards {
		for k, v := range s.collectAndClear() {
			dst.Set(k, v)
		}
	}
}

// collectAndClear removes and returns all non-expired entries in the shard.
func (s *mcacheShard[K, V]) collectAndClear() map[K]V {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UnixNano()
	toTransfer := make(map[K]V)
	var keysToDelete []K

	for k, v := range s.m {
		if v.expireAt == 0 || v.expireAt >= now {
			toTransfer[k] = v.value
			keysToDelete = append(keysToDelete, k)
		}
	}

	for _, k := range keysToDelete {
		delete(s.m, k)
	}

	return toTransfer
}

// CopyTo copies all non-expired key-value pairs from the source cache to the destination cache.
// The operation is performed in a deadlock-safe manner by not holding both locks simultaneously.
func (src *MCache[K, V]) CopyTo(dst *MCache[K, V]) {
	for _, s := range src.shards {
		for k, v := range s.snapshot() {
			dst.Set(k, v)
		}
	}
}

// snapshot returns a copy of all non-expired entries in the shard.
func (s *mcacheShard[K, V]) snapshot() map[K]V {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UnixNano()
	toCopy := make(map[K]V)

	for k, v := range s.m {
		if v.expireAt == 0 || v.expireAt >= now {
			toCopy[k] = v.value
		}
	}

	return toCopy
}

// Keys returns a slice of all keys currently stored in the cache.
// The returned slice does not include expired keys.
// The order of keys in the slice is not guaranteed.
func (c *MCache[K, V]) Keys() []K {
	var keys []K
	for _, s := range c.shards {
		keys = append(keys, s.keys()...)
	}
	return keys
}

func (s *mcacheShard[K, V]) keys() []K {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UnixNano()
	keys := make([]K, 0, len(s.m))

	for k, v := range s.m {
		if v.expireAt == 0 || v.expireAt >= now {
			keys = append(keys, k)
		}
	}

	return keys
}

// expireKeys is a background goroutine that periodically checks for expired keys and removes them from the database.
// It runs until the Close method is called. It sweeps one shard at a time
// rather than holding a single cache-wide lock.
// This function is not intended to be called directly by users.
func (c *MCache[K, V]) expireKeys() {
	ticker := time.NewTicker(c.timeInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			now := time.Now().UnixNano()
			for _, s := range c.shards {
				s.expireKeys(now)
			}
		case <-c.stopCh:
			return
		}
	}
}

func (s *mcacheShard[K, V]) expireKeys(now int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for k, v := range s.m {
		if v.expireAt > 0 && v.expireAt < now {
			delete(s.m, k)
		}
	}
}

// Purge removes all key-value pairs from the cache.
// The cache can still be used after calling Purge.
func (c *MCache[K, V]) Purge() {
	if c.closed.Load() {
		return
	}

	for _, s := range c.shards {
		s.purge()
	}
}

func (s *mcacheShard[K, V]) purge() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Don't hand a closed cache a fresh map to write into.
	if s.m == nil {
		return
	}

	s.m = make(map[K]valueWithTimeout[V])
}

// Close stops the background expiration goroutine and clears the cache.
// After calling Close, the cache should not be used; further calls are
// safe no-ops. Close itself is idempotent and safe to call more than once.
func (c *MCache[K, V]) Close() {
	if !c.closed.CompareAndSwap(false, true) {
		return
	}

	// Closing (rather than sending on) stopCh both signals the expiration
	// goroutine, if one is running, and is a safe no-op if it isn't.
	close(c.stopCh)

	for _, s := range c.shards {
		s.close()
	}
}

func (s *mcacheShard[K, V]) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m = nil
}

// Count returns the number of non-expired key-value pairs in the database.
func (c *MCache[K, V]) Count() int {
	count := 0
	for _, s := range c.shards {
		count += s.count()
	}
	return count
}

func (s *mcacheShard[K, V]) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	count := 0
	now := time.Now().UnixNano()
	for _, v := range s.m {
		if v.expireAt == 0 || v.expireAt >= now {
			count++
		}
	}

	return count
}

// Len returns the total number of elements in the cache (including expired ones).
func (c *MCache[K, V]) Len() int {
	n := 0
	for _, s := range c.shards {
		n += s.len()
	}
	return n
}

func (s *mcacheShard[K, V]) len() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.m)
}

// evict removes i items from the shard.
// It first tries to evict expired items, then evicts any items if needed.
// Assumes s.mu is already held by the caller.
func (s *mcacheShard[K, V]) evict(i int) {
	now := time.Now().UnixNano()
	counter := 0

	// First pass: evict expired items
	for k, v := range s.m {
		if counter >= i {
			return
		}
		if v.expireAt > 0 && v.expireAt < now {
			delete(s.m, k)
			counter++
		}
	}

	// Second pass: evict any items if we still need to evict more
	if counter < i {
		remaining := min(i-counter, len(s.m))
		for k := range s.m {
			if remaining <= 0 {
				break
			}
			delete(s.m, k)
			remaining--
		}
	}
}
