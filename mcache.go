package incache

import (
	"sync"
	"time"
)

// MCache is a simple cache with manual/no eviction policy.
// When the cache is full and a new item needs to be added,
// it first tries to evict expired items, then evicts random items if needed.
type MCache[K comparable, V any] struct {
	mu           sync.Mutex
	size         uint
	m            map[K]valueWithTimeout[V] // where the key-value pairs are stored
	stopCh       chan struct{}             // Channel to signal timeout goroutine to stop
	timeInterval time.Duration             // Time interval to sleep the goroutine that checks for expired keys
}

type valueWithTimeout[V any] struct {
	value    V
	expireAt int64 // Unix nano timestamp, 0 means no expiration
}

// NewManual creates a new cache instance with optional configuration provided by the specified options.
// The cache starts a background goroutine to periodically check for expired keys based on the configured time interval.
// If size is 0, the cache will not store any items.
func NewManual[K comparable, V any](size uint, timeInterval time.Duration) *MCache[K, V] {
	c := &MCache[K, V]{
		m:            make(map[K]valueWithTimeout[V]),
		stopCh:       make(chan struct{}),
		size:         size,
		timeInterval: timeInterval,
	}
	if c.timeInterval > 0 {
		go c.expireKeys()
	}
	return c
}

// Set adds or updates a key-value pair in the database without setting an expiration time.
// If the key already exists, its value will be overwritten with the new value.
// This function is safe for concurrent use.
func (c *MCache[K, V]) Set(k K, v V) {
	if c.size == 0 {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// If key exists, just update
	if _, ok := c.m[k]; ok {
		c.m[k] = valueWithTimeout[V]{
			value:    v,
			expireAt: 0,
		}
		return
	}

	if uint(len(c.m)) >= c.size {
		c.evict(1)
	}

	c.m[k] = valueWithTimeout[V]{
		value:    v,
		expireAt: 0,
	}
}

// NotFoundSet adds a key-value pair to the database if the key does not already exist or is expired, and returns true.
// Otherwise, it does nothing and returns false.
func (c *MCache[K, V]) NotFoundSet(k K, v V) bool {
	if c.size == 0 {
		return false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if val, ok := c.m[k]; ok {
		// Check if existing key is expired
		if val.expireAt == 0 || val.expireAt >= time.Now().UnixNano() {
			return false
		}
		// Key exists but is expired, delete it
		delete(c.m, k)
	}

	if uint(len(c.m)) >= c.size {
		c.evict(1)
	}

	c.m[k] = valueWithTimeout[V]{
		value:    v,
		expireAt: 0,
	}
	return true
}

// SetWithTimeout adds or updates a key-value pair in the database with an expiration time.
// If the timeout duration is zero or negative, the key-value pair will not have an expiration time.
// This function is safe for concurrent use.
func (c *MCache[K, V]) SetWithTimeout(k K, v V, timeout time.Duration) {
	if c.size == 0 {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	var expireAt int64
	if timeout > 0 {
		expireAt = time.Now().Add(timeout).UnixNano()
	}

	// If key exists, just update
	if _, ok := c.m[k]; ok {
		c.m[k] = valueWithTimeout[V]{
			value:    v,
			expireAt: expireAt,
		}
		return
	}

	if uint(len(c.m)) >= c.size {
		c.evict(1)
	}

	c.m[k] = valueWithTimeout[V]{
		value:    v,
		expireAt: expireAt,
	}
}

// NotFoundSetWithTimeout adds a key-value pair to the database with an expiration time if the key does not already exist or is expired, and returns true.
// Otherwise, it does nothing and returns false.
// If the timeout is zero or negative, the key-value pair will not have an expiration time.
func (c *MCache[K, V]) NotFoundSetWithTimeout(k K, v V, timeout time.Duration) bool {
	if c.size == 0 {
		return false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if val, ok := c.m[k]; ok {
		// Check if existing key is expired
		if val.expireAt == 0 || val.expireAt >= time.Now().UnixNano() {
			return false
		}
		// Key exists but is expired, delete it
		delete(c.m, k)
	}

	var expireAt int64
	if timeout > 0 {
		expireAt = time.Now().Add(timeout).UnixNano()
	}

	if uint(len(c.m)) >= c.size {
		c.evict(1)
	}

	c.m[k] = valueWithTimeout[V]{
		value:    v,
		expireAt: expireAt,
	}
	return true
}

// Get retrieves the value associated with the given key from the cache.
// If the key is not found or has expired, it returns (zero value of V, false).
// Otherwise, it returns (value, true).
func (c *MCache[K, V]) Get(k K) (v V, b bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	val, ok := c.m[k]
	if !ok {
		return
	}
	if val.expireAt > 0 && val.expireAt < time.Now().UnixNano() {
		delete(c.m, k)
		return
	}
	return val.value, true
}

// GetAll retrieves all key-value pairs from the cache.
// It returns a map containing all the key-value pairs that are not expired.
func (c *MCache[K, V]) GetAll() map[K]V {
	c.mu.Lock()
	defer c.mu.Unlock()

	m := make(map[K]V)
	now := time.Now().UnixNano()
	for k, v := range c.m {
		if v.expireAt == 0 || v.expireAt >= now {
			m[k] = v.value
		}
	}
	return m
}

// Delete removes the key-value pair associated with the given key from the cache.
func (c *MCache[K, V]) Delete(k K) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.m, k)
}

// TransferTo transfers all non-expired key-value pairs from the source cache to the destination cache.
// The operation is performed in a deadlock-safe manner by not holding both locks simultaneously.
func (src *MCache[K, V]) TransferTo(dst *MCache[K, V]) {
	// Collect data with source lock
	src.mu.Lock()
	now := time.Now().UnixNano()
	toTransfer := make(map[K]V)
	var keysToDelete []K

	for k, v := range src.m {
		if v.expireAt == 0 || v.expireAt >= now {
			toTransfer[k] = v.value
			keysToDelete = append(keysToDelete, k)
		}
	}

	// Delete transferred items from source
	for _, k := range keysToDelete {
		delete(src.m, k)
	}
	src.mu.Unlock()

	// Insert into destination with destination lock
	for k, v := range toTransfer {
		dst.Set(k, v)
	}
}

// CopyTo copies all non-expired key-value pairs from the source cache to the destination cache.
// The operation is performed in a deadlock-safe manner by not holding both locks simultaneously.
func (src *MCache[K, V]) CopyTo(dst *MCache[K, V]) {
	// Collect data with source lock
	src.mu.Lock()
	now := time.Now().UnixNano()
	toCopy := make(map[K]V)

	for k, v := range src.m {
		if v.expireAt == 0 || v.expireAt >= now {
			toCopy[k] = v.value
		}
	}
	src.mu.Unlock()

	// Insert into destination with destination lock
	for k, v := range toCopy {
		dst.Set(k, v)
	}
}

// Keys returns a slice of all keys currently stored in the cache.
// The returned slice does not include expired keys.
// The order of keys in the slice is not guaranteed.
func (c *MCache[K, V]) Keys() []K {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now().UnixNano()
	keys := make([]K, 0, len(c.m))

	for k, v := range c.m {
		if v.expireAt == 0 || v.expireAt >= now {
			keys = append(keys, k)
		}
	}

	return keys
}

// expireKeys is a background goroutine that periodically checks for expired keys and removes them from the database.
// It runs until the Close method is called.
// This function is not intended to be called directly by users.
func (c *MCache[K, V]) expireKeys() {
	ticker := time.NewTicker(c.timeInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			c.mu.Lock()
			now := time.Now().UnixNano()
			for k, v := range c.m {
				if v.expireAt > 0 && v.expireAt < now {
					delete(c.m, k)
				}
			}
			c.mu.Unlock()
		case <-c.stopCh:
			return
		}
	}
}

// Purge removes all key-value pairs from the cache.
// The cache can still be used after calling Purge.
func (c *MCache[K, V]) Purge() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.m = make(map[K]valueWithTimeout[V])
}

// Close stops the background expiration goroutine and clears the cache.
// After calling Close, the cache should not be used.
func (c *MCache[K, V]) Close() {
	if c.timeInterval > 0 {
		c.stopCh <- struct{}{} // Signal the expiration goroutine to stop
		close(c.stopCh)
	}
	c.mu.Lock()
	c.m = nil
	c.mu.Unlock()
}

// Count returns the number of non-expired key-value pairs in the database.
func (c *MCache[K, V]) Count() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	count := 0
	now := time.Now().UnixNano()
	for _, v := range c.m {
		if v.expireAt == 0 || v.expireAt >= now {
			count++
		}
	}

	return count
}

// Len returns the total number of elements in the cache (including expired ones).
func (c *MCache[K, V]) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return len(c.m)
}

// evict removes i items from the cache.
// It first tries to evict expired items, then evicts any items if needed.
func (c *MCache[K, V]) evict(i int) {
	now := time.Now().UnixNano()
	counter := 0

	// First pass: evict expired items
	for k, v := range c.m {
		if counter >= i {
			return
		}
		if v.expireAt > 0 && v.expireAt < now {
			delete(c.m, k)
			counter++
		}
	}

	// Second pass: evict any items if we still need to evict more
	if counter < i {
		remaining := i - counter
		if remaining > len(c.m) {
			remaining = len(c.m)
		}
		for k := range c.m {
			if remaining <= 0 {
				break
			}
			delete(c.m, k)
			remaining--
		}
	}
}
