package incache

import (
	"sync"
	"time"
)

type MCache[K comparable, V any] struct {
	mu           sync.RWMutex
	size         uint
	m            map[K]valueWithTimeout[V] // where the key-value pairs are stored
	stopCh       chan struct{}             // Channel to signal timeout goroutine to stop
	timeInterval time.Duration             // Time interval to sleep the goroutine that checks for expired keys
}

type valueWithTimeout[V any] struct {
	value    V
	expireAt *time.Time
}

// NewManual creates a new cache instance with the specified size and options.
// If a cleanup interval is provided via WithCleanupInterval, a background goroutine is started to periodically check for and remove expired keys.
func NewManual[K comparable, V any](size uint, opts ...Option) *MCache[K, V] {
	o := &Options{}
	for _, opt := range opts {
		opt(o)
	}

	c := &MCache[K, V]{
		m:            make(map[K]valueWithTimeout[V]),
		stopCh:       make(chan struct{}),
		size:         size,
		timeInterval: o.CleanupInterval,
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

	if len(c.m) == int(c.size) {
		c.evict(1)
	}

	c.m[k] = valueWithTimeout[V]{
		value:    v,
		expireAt: nil,
	}
}

// NotFoundSet adds a key-value pair to the database if the key does not already exist and returns true. Otherwise, it does nothing and returns false.
func (c *MCache[K, V]) NotFoundSet(k K, v V) bool {
	if c.size == 0 {
		return false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	_, ok := c.m[k]
	if !ok {
		if len(c.m) == int(c.size) {
			c.evict(1)
		}

		c.m[k] = valueWithTimeout[V]{
			value:    v,
			expireAt: nil,
		}
	}
	return !ok
}

// SetWithTimeout adds or updates a key-value pair in the database with an expiration time.
// If the timeout duration is zero or negative, the key-value pair will not have an expiration time.
// This function is safe for concurrent use.
func (c *MCache[K, V]) SetWithTimeout(k K, v V, timeout time.Duration) {
	if c.size == 0 {
		return
	}
	if timeout > 0 {
		c.mu.Lock()
		defer c.mu.Unlock()

		if len(c.m) == int(c.size) {
			c.evict(1)
		}

		now := time.Now().Add(timeout)
		c.m[k] = valueWithTimeout[V]{
			value:    v,
			expireAt: &now,
		}
	} else {
		c.Set(k, v)
	}
}

// NotFoundSetWithTimeout adds a key-value pair to the database with an expiration time if the key does not already exist and returns true. Otherwise, it does nothing and returns false.
// If the timeout is zero or negative, the key-value pair will not have an expiration time.
// If expiry is disabled, it behaves like NotFoundSet.
func (c *MCache[K, V]) NotFoundSetWithTimeout(k K, v V, timeout time.Duration) bool {
	if c.size == 0 {
		return false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	var ok bool
	if timeout > 0 {
		now := time.Now().Add(timeout)
		_, ok = c.m[k]
		if !ok {
			if len(c.m) == int(c.size) {
				c.evict(1)
			}

			c.m[k] = valueWithTimeout[V]{
				value:    v,
				expireAt: &now,
			}
		}
	} else {
		_, ok = c.m[k]
		if !ok {
			if len(c.m) == int(c.size) {
				c.evict(1)
			}

			c.m[k] = valueWithTimeout[V]{
				value:    v,
				expireAt: nil,
			}
		}
	}
	return !ok
}

func (c *MCache[K, V]) Get(k K) (v V, b bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	val, ok := c.m[k]
	if !ok {
		return
	}
	if val.expireAt != nil && val.expireAt.Before(time.Now()) {
		delete(c.m, k)
		return
	}
	return val.value, ok
}

func (c *MCache[K, V]) GetAll() map[K]V {
	c.mu.RLock()
	defer c.mu.RUnlock()
	m := make(map[K]V)
	for k, v := range c.m {
		if v.expireAt == nil || !v.expireAt.Before(time.Now()) {
			m[k] = v.value
		}
	}
	return m
}

func (c *MCache[K, V]) Delete(k K) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.m, k)
}

// TransferTo transfers all key-value pairs from the source cache to the provided destination cache.
//
// The source cache and the destination cache are locked during the entire operation.
// The function is safe to call concurrently with other operations on any of the source cache or destination cache.
func (src *MCache[K, V]) TransferTo(dst Cache[K, V]) {
	all := src.GetAll()
	src.mu.Lock()
	src.m = make(map[K]valueWithTimeout[V])
	src.mu.Unlock()

	for k, v := range all {
		dst.Set(k, v)
	}
}

// CopyTo copies all key-value pairs from the source cache to the provided destination cache.
//
// The source cache are the destination cache are locked during the entire operation.
// The function is safe to call concurrently with other operations on any of the source cache or Destination cache.
func (src *MCache[K, V]) CopyTo(dst Cache[K, V]) {
	all := src.GetAll()

	for k, v := range all {
		dst.Set(k, v)
	}
}

func (c *MCache[K, V]) Keys() []K {
	c.mu.RLock()
	defer c.mu.RUnlock()

	keys := make([]K, 0, len(c.m))

	for k, v := range c.m {
		if v.expireAt == nil || !v.expireAt.Before(time.Now()) {
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
			now := time.Now()
			c.mu.Lock()
			for k, v := range c.m {
				if v.expireAt != nil && v.expireAt.Before(now) {
					delete(c.m, k)
				}
			}
			c.mu.Unlock()
		case <-c.stopCh:
			return
		}
	}
}

func (c *MCache[K, V]) Purge() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m = make(map[K]valueWithTimeout[V])
}

func (c *MCache[K, V]) Close() {
	if c.timeInterval > 0 {
		c.mu.Lock()
		defer c.mu.Unlock()
		select {
		case <-c.stopCh:
			// already closed
		default:
			close(c.stopCh)
		}
	}
}

// Count returns the number of key-value pairs in the database.
func (c *MCache[K, V]) Count() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var count int
	for _, v := range c.m {
		if v.expireAt == nil || !v.expireAt.Before(time.Now()) {
			count++
		}
	}

	return count
}

func (c *MCache[K, V]) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.m)
}

func (c *MCache[K, V]) evict(i int) {
	var counter int
	for k, v := range c.m {
		if counter == i {
			break
		}
		if v.expireAt != nil && !v.expireAt.After(time.Now()) {
			delete(c.m, k)
			counter++
		}
	}
	if i > len(c.m) {
		i = len(c.m)
	}
	for ; counter < i; counter++ {
		for k := range c.m {
			delete(c.m, k)
			break
		}
	}
}
