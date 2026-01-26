package incache

import (
	"container/list"
	"sync"
	"time"
)

type lruItem[K comparable, V any] struct {
	key      K
	value    V
	expireAt int64 // Unix nano timestamp, 0 means no expiration
}

// LRUCache implements a Least Recently Used cache with O(1) operations.
type LRUCache[K comparable, V any] struct {
	mu           sync.Mutex
	size         uint
	m            map[K]*list.Element // where the key-value pairs are stored
	evictionList *list.List
}

// NewLRU creates a new LRU cache with the specified maximum size.
// If size is 0, the cache will not store any items.
func NewLRU[K comparable, V any](size uint) *LRUCache[K, V] {
	return &LRUCache[K, V]{
		size:         size,
		m:            make(map[K]*list.Element),
		evictionList: list.New(),
	}
}

// Get retrieves the value associated with the given key from the cache.
// If the key is not found or has expired, it returns (zero value of V, false).
// Otherwise, it returns (value, true).
func (c *LRUCache[K, V]) Get(k K) (v V, b bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	item, ok := c.m[k]
	if !ok {
		return
	}

	lruItem := item.Value.(*lruItem[K, V])
	if lruItem.expireAt > 0 && lruItem.expireAt < time.Now().UnixNano() {
		delete(c.m, k)
		c.evictionList.Remove(item)
		return
	}

	c.evictionList.MoveToFront(item)

	return lruItem.value, true
}

// GetAll retrieves all key-value pairs from the cache.
// It returns a map containing all the key-value pairs that are not expired.
func (c *LRUCache[K, V]) GetAll() map[K]V {
	c.mu.Lock()
	defer c.mu.Unlock()

	m := make(map[K]V)
	now := time.Now().UnixNano()
	for k, v := range c.m {
		lruItem := v.Value.(*lruItem[K, V])
		if lruItem.expireAt == 0 || lruItem.expireAt >= now {
			m[k] = lruItem.value
		}
	}

	return m
}

// Set adds the key-value pair to the cache.
func (c *LRUCache[K, V]) Set(k K, v V) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.set(k, v, 0)
}

// SetWithTimeout adds the key-value pair to the cache with a specified expiration time.
func (c *LRUCache[K, V]) SetWithTimeout(k K, v V, t time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.set(k, v, t)
}

// NotFoundSet adds the key-value pair to the cache only if the key does not exist or is expired.
// It returns true if the key was added to the cache, otherwise false.
func (c *LRUCache[K, V]) NotFoundSet(k K, v V) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if item, ok := c.m[k]; ok {
		lruItem := item.Value.(*lruItem[K, V])
		// Check if existing key is expired
		if lruItem.expireAt == 0 || lruItem.expireAt >= time.Now().UnixNano() {
			return false
		}
		// Key exists but is expired, delete it first
		delete(c.m, k)
		c.evictionList.Remove(item)
	}

	c.set(k, v, 0)
	return true
}

// NotFoundSetWithTimeout adds the key-value pair to the cache only if the key does not exist or is expired.
// It sets an expiration time for the key-value pair.
// It returns true if the key was added to the cache, otherwise false.
func (c *LRUCache[K, V]) NotFoundSetWithTimeout(k K, v V, t time.Duration) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if item, ok := c.m[k]; ok {
		lruItem := item.Value.(*lruItem[K, V])
		// Check if existing key is expired
		if lruItem.expireAt == 0 || lruItem.expireAt >= time.Now().UnixNano() {
			return false
		}
		// Key exists but is expired, delete it first
		delete(c.m, k)
		c.evictionList.Remove(item)
	}

	c.set(k, v, t)
	return true
}

// Delete removes the key-value pair associated with the given key from the cache.
func (c *LRUCache[K, V]) Delete(k K) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.delete(k)
}

func (c *LRUCache[K, V]) delete(k K) {
	item, ok := c.m[k]
	if !ok {
		return
	}

	delete(c.m, k)
	c.evictionList.Remove(item)
}

// TransferTo transfers all non-expired key-value pairs from the source cache to the destination cache.
// The operation is performed in a deadlock-safe manner by not holding both locks simultaneously.
func (src *LRUCache[K, V]) TransferTo(dst *LRUCache[K, V]) {
	// Collect data with source lock
	src.mu.Lock()
	now := time.Now().UnixNano()
	toTransfer := make(map[K]V)
	var keysToDelete []K

	for k, v := range src.m {
		lruItem := v.Value.(*lruItem[K, V])
		if lruItem.expireAt == 0 || lruItem.expireAt >= now {
			toTransfer[k] = lruItem.value
			keysToDelete = append(keysToDelete, k)
		}
	}

	// Delete transferred items from source
	for _, k := range keysToDelete {
		src.delete(k)
	}
	src.mu.Unlock()

	// Insert into destination with destination lock
	dst.mu.Lock()
	for k, v := range toTransfer {
		dst.set(k, v, 0)
	}
	dst.mu.Unlock()
}

// CopyTo copies all non-expired key-value pairs from the source cache to the destination cache.
// The operation is performed in a deadlock-safe manner by not holding both locks simultaneously.
func (src *LRUCache[K, V]) CopyTo(dst *LRUCache[K, V]) {
	// Collect data with source lock
	src.mu.Lock()
	now := time.Now().UnixNano()
	toCopy := make(map[K]V)

	for k, v := range src.m {
		lruItem := v.Value.(*lruItem[K, V])
		if lruItem.expireAt == 0 || lruItem.expireAt >= now {
			toCopy[k] = lruItem.value
		}
	}
	src.mu.Unlock()

	// Insert into destination with destination lock
	dst.mu.Lock()
	for k, v := range toCopy {
		dst.set(k, v, 0)
	}
	dst.mu.Unlock()
}

// Keys returns a slice of all keys currently stored in the cache.
// The returned slice does not include expired keys.
// The order of keys in the slice is not guaranteed.
func (c *LRUCache[K, V]) Keys() []K {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now().UnixNano()
	keys := make([]K, 0, len(c.m))

	for k, v := range c.m {
		lruItem := v.Value.(*lruItem[K, V])
		if lruItem.expireAt == 0 || lruItem.expireAt >= now {
			keys = append(keys, k)
		}
	}

	return keys
}

// Purge removes all key-value pairs from the cache.
func (c *LRUCache[K, V]) Purge() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.m = make(map[K]*list.Element)
	c.evictionList.Init()
}

// Count returns the number of non-expired key-value pairs currently stored in the cache.
func (c *LRUCache[K, V]) Count() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	count := 0
	now := time.Now().UnixNano()
	for _, v := range c.m {
		lruItem := v.Value.(*lruItem[K, V])
		if lruItem.expireAt == 0 || lruItem.expireAt >= now {
			count++
		}
	}

	return count
}

// Len returns the total number of elements in the cache (including expired ones).
func (c *LRUCache[K, V]) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return len(c.m)
}

func (c *LRUCache[K, V]) set(k K, v V, exp time.Duration) {
	if c.size == 0 {
		return
	}

	var expireAt int64
	if exp > 0 {
		expireAt = time.Now().Add(exp).UnixNano()
	}

	item, ok := c.m[k]
	if ok {
		lruItem := item.Value.(*lruItem[K, V])
		lruItem.value = v
		lruItem.expireAt = expireAt
		c.evictionList.MoveToFront(item)
	} else {
		if uint(len(c.m)) >= c.size {
			c.evict(1)
		}

		lruItem := &lruItem[K, V]{
			key:      k,
			value:    v,
			expireAt: expireAt,
		}

		insertedItem := c.evictionList.PushFront(lruItem)
		c.m[k] = insertedItem
	}
}

func (c *LRUCache[K, V]) evict(i int) {
	for j := 0; j < i; j++ {
		if b := c.evictionList.Back(); b != nil {
			delete(c.m, b.Value.(*lruItem[K, V]).key)
			c.evictionList.Remove(b)
		} else {
			return
		}
	}
}
