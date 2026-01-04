package incache

import (
	"container/list"
	"sync"
	"time"
)

type lruItem[K comparable, V any] struct {
	key      K
	value    V
	expireAt *time.Time
}

// Least Recently Used Cache
type LRUCache[K comparable, V any] struct {
	mu           sync.RWMutex
	size         uint
	m            map[K]*list.Element // where the key-value pairs are stored
	evictionList *list.List
}

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
	if lruItem.expireAt != nil && lruItem.expireAt.Before(time.Now()) {
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
	c.mu.RLock()
	defer c.mu.RUnlock()

	m := make(map[K]V)
	for k, v := range c.m {
		lruItem := v.Value.(*lruItem[K, V])
		if lruItem.expireAt == nil || !lruItem.expireAt.Before(time.Now()) {
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

// NotFoundSet adds the key-value pair to the cache only if the key does not exist.
// It returns true if the key was added to the cache, otherwise false.
func (c *LRUCache[K, V]) NotFoundSet(k K, v V) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	_, ok := c.m[k]
	if ok {
		return false
	}

	c.set(k, v, 0)
	return true
}

// NotFoundSetWithTimeout adds the key-value pair to the cache only if the key does not exist.
// It sets an expiration time for the key-value pair.
// It returns true if the key was added to the cache, otherwise false.
func (c *LRUCache[K, V]) NotFoundSetWithTimeout(k K, v V, t time.Duration) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	_, ok := c.m[k]
	if ok {
		return false
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
func (src *LRUCache[K, V]) TransferTo(dst Cache[K, V]) {
	src.mu.Lock()
	defer src.mu.Unlock()

	for k, v := range src.m {
		lruItem := v.Value.(*lruItem[K, V])
		if lruItem.expireAt == nil || !lruItem.expireAt.Before(time.Now()) {
			src.delete(k)
			dst.Set(k, lruItem.value)
		}
	}
}

// CopyTo copies all non-expired key-value pairs from the source cache to the destination cache.
func (src *LRUCache[K, V]) CopyTo(dst Cache[K, V]) {
	src.mu.Lock()
	defer src.mu.Unlock()

	for k, v := range src.m {
		if lruItem := v.Value.(*lruItem[K, V]); lruItem.expireAt == nil || !lruItem.expireAt.Before(time.Now()) {
			dst.Set(k, lruItem.value)
		}
	}
}

// Keys returns a slice of all keys currently stored in the cache.
// The returned slice does not include expired keys.
// The order of keys in the slice is not guaranteed.
func (c *LRUCache[K, V]) Keys() []K {
	c.mu.RLock()
	defer c.mu.RUnlock()

	keys := make([]K, 0, len(c.m))

	for k, v := range c.m {
		if lruItem := v.Value.(*lruItem[K, V]); lruItem.expireAt == nil || !lruItem.expireAt.Before(time.Now()) {
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

func (c *LRUCache[K, V]) Close() {}

// Count returns the number of non-expired key-value pairs currently stored in the cache.
func (c *LRUCache[K, V]) Count() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var count int
	for _, v := range c.m {
		if lruItem := v.Value.(*lruItem[K, V]); lruItem.expireAt == nil || !lruItem.expireAt.Before(time.Now()) {
			count++
		}
	}

	return count
}

// Len returns the number of elements in the cache.
func (c *LRUCache[K, V]) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.m)
}

func (c *LRUCache[K, V]) set(k K, v V, exp time.Duration) {
	if c.size == 0 {
		return
	}
	item, ok := c.m[k]
	var tm *time.Time
	if exp > 0 {
		t := time.Now().Add(exp)
		tm = &t
	}
	if ok {
		lruItem := item.Value.(*lruItem[K, V])
		lruItem.value = v
		lruItem.expireAt = tm
		c.evictionList.MoveToFront(item)
	} else {
		if len(c.m) == int(c.size) {
			c.evict(1)
		}

		lruItem := &lruItem[K, V]{
			key:      k,
			value:    v,
			expireAt: tm,
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
