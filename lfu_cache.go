package incache

import (
	"container/list"
	"sync"
	"time"
)

// LFUCache implements a Least Frequently Used cache with O(1) operations.
// It uses frequency buckets to efficiently track and evict items.
type LFUCache[K comparable, V any] struct {
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
	return &LFUCache[K, V]{
		size:      size,
		minFreq:   0,
		items:     make(map[K]*list.Element),
		freqLists: make(map[uint]*list.List),
	}
}

// Set adds the key-value pair to the cache.
func (l *LFUCache[K, V]) Set(key K, value V) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.set(key, value, 0)
}

// SetWithTimeout adds the key-value pair to the cache with a specified expiration time.
func (l *LFUCache[K, V]) SetWithTimeout(key K, value V, exp time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.set(key, value, exp)
}

func (l *LFUCache[K, V]) set(key K, value V, exp time.Duration) {
	if l.size == 0 {
		return
	}

	var expireAt int64
	if exp > 0 {
		expireAt = time.Now().Add(exp).UnixNano()
	}

	// Check if key already exists
	if elem, ok := l.items[key]; ok {
		item := elem.Value.(*lfuItem[K, V])
		item.value = value
		item.expireAt = expireAt
		l.incrementFreq(elem)
		return
	}

	// Evict if at capacity
	if uint(len(l.items)) >= l.size {
		l.evict(1)
	}

	// Create new item with frequency 1
	item := &lfuItem[K, V]{
		key:      key,
		value:    value,
		freq:     1,
		expireAt: expireAt,
	}

	// Add to frequency 1 list
	if l.freqLists[1] == nil {
		l.freqLists[1] = list.New()
	}
	elem := l.freqLists[1].PushFront(item)
	l.items[key] = elem
	l.minFreq = 1
}

// Get retrieves the value associated with the given key from the cache.
// If the key is not found or has expired, it returns (zero value of V, false).
// Otherwise, it returns (value, true).
func (l *LFUCache[K, V]) Get(key K) (v V, b bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	elem, ok := l.items[key]
	if !ok {
		return
	}

	item := elem.Value.(*lfuItem[K, V])

	// Check expiration
	if item.expireAt > 0 && item.expireAt < time.Now().UnixNano() {
		l.delete(key, elem)
		return
	}

	l.incrementFreq(elem)
	return item.value, true
}

// incrementFreq moves an item to the next frequency bucket - O(1) operation
func (l *LFUCache[K, V]) incrementFreq(elem *list.Element) {
	item := elem.Value.(*lfuItem[K, V])
	oldFreq := item.freq
	newFreq := oldFreq + 1

	// Remove from old frequency list
	oldList := l.freqLists[oldFreq]
	oldList.Remove(elem)

	// Update minFreq if necessary
	if oldFreq == l.minFreq && oldList.Len() == 0 {
		l.minFreq = newFreq
		delete(l.freqLists, oldFreq)
	}

	// Add to new frequency list
	item.freq = newFreq
	if l.freqLists[newFreq] == nil {
		l.freqLists[newFreq] = list.New()
	}
	newElem := l.freqLists[newFreq].PushFront(item)
	l.items[item.key] = newElem
}

// NotFoundSet adds the key-value pair to the cache only if the key does not exist or is expired.
// It returns true if the key was added to the cache, otherwise false.
func (l *LFUCache[K, V]) NotFoundSet(k K, v V) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if elem, ok := l.items[k]; ok {
		item := elem.Value.(*lfuItem[K, V])
		// Check if existing key is expired
		if item.expireAt == 0 || item.expireAt >= time.Now().UnixNano() {
			return false
		}
		// Key exists but is expired, delete it first
		l.delete(k, elem)
	}

	l.set(k, v, 0)
	return true
}

// NotFoundSetWithTimeout adds the key-value pair to the cache only if the key does not exist or is expired.
// It sets an expiration time for the key-value pair.
// It returns true if the key was added to the cache, otherwise false.
func (l *LFUCache[K, V]) NotFoundSetWithTimeout(k K, v V, t time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if elem, ok := l.items[k]; ok {
		item := elem.Value.(*lfuItem[K, V])
		// Check if existing key is expired
		if item.expireAt == 0 || item.expireAt >= time.Now().UnixNano() {
			return false
		}
		// Key exists but is expired, delete it first
		l.delete(k, elem)
	}

	l.set(k, v, t)
	return true
}

// GetAll retrieves all key-value pairs from the cache.
// It returns a map containing all the key-value pairs that are not expired.
func (l *LFUCache[K, V]) GetAll() map[K]V {
	l.mu.Lock()
	defer l.mu.Unlock()

	m := make(map[K]V)
	now := time.Now().UnixNano()
	for k, elem := range l.items {
		item := elem.Value.(*lfuItem[K, V])
		if item.expireAt == 0 || item.expireAt >= now {
			m[k] = item.value
		}
	}
	return m
}

// TransferTo transfers all non-expired key-value pairs from the source cache to the destination cache.
// Both caches are locked during the operation to prevent deadlocks.
func (src *LFUCache[K, V]) TransferTo(dst *LFUCache[K, V]) {
	// Collect data with source lock
	src.mu.Lock()
	now := time.Now().UnixNano()
	toTransfer := make(map[K]V)
	var keysToDelete []K

	for k, elem := range src.items {
		item := elem.Value.(*lfuItem[K, V])
		if item.expireAt == 0 || item.expireAt >= now {
			toTransfer[k] = item.value
			keysToDelete = append(keysToDelete, k)
		}
	}

	// Delete transferred items from source
	for _, k := range keysToDelete {
		if elem, ok := src.items[k]; ok {
			src.delete(k, elem)
		}
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
func (src *LFUCache[K, V]) CopyTo(dst *LFUCache[K, V]) {
	// Collect data with source lock
	src.mu.Lock()
	now := time.Now().UnixNano()
	toCopy := make(map[K]V)

	for k, elem := range src.items {
		item := elem.Value.(*lfuItem[K, V])
		if item.expireAt == 0 || item.expireAt >= now {
			toCopy[k] = item.value
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
func (l *LFUCache[K, V]) Keys() []K {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now().UnixNano()
	keys := make([]K, 0, len(l.items))

	for k, elem := range l.items {
		item := elem.Value.(*lfuItem[K, V])
		if item.expireAt == 0 || item.expireAt >= now {
			keys = append(keys, k)
		}
	}
	return keys
}

// Purge removes all key-value pairs from the cache.
func (l *LFUCache[K, V]) Purge() {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.items = make(map[K]*list.Element)
	l.freqLists = make(map[uint]*list.List)
	l.minFreq = 0
}

// Count returns the number of non-expired key-value pairs currently stored in the cache.
func (l *LFUCache[K, V]) Count() int {
	l.mu.Lock()
	defer l.mu.Unlock()

	count := 0
	now := time.Now().UnixNano()
	for _, elem := range l.items {
		item := elem.Value.(*lfuItem[K, V])
		if item.expireAt == 0 || item.expireAt >= now {
			count++
		}
	}
	return count
}

// Len returns the total number of elements in the cache (including expired ones).
func (l *LFUCache[K, V]) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()

	return len(l.items)
}

// Delete removes the key-value pair associated with the given key from the cache.
func (l *LFUCache[K, V]) Delete(k K) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if elem, ok := l.items[k]; ok {
		l.delete(k, elem)
	}
}

func (l *LFUCache[K, V]) delete(key K, elem *list.Element) {
	item := elem.Value.(*lfuItem[K, V])
	freq := item.freq

	// Remove from frequency list
	freqList := l.freqLists[freq]
	if freqList != nil {
		freqList.Remove(elem)
		if freqList.Len() == 0 {
			delete(l.freqLists, freq)
			// Update minFreq if necessary
			if freq == l.minFreq {
				l.updateMinFreq()
			}
		}
	}

	delete(l.items, key)
}

func (l *LFUCache[K, V]) updateMinFreq() {
	l.minFreq = 0
	for freq := range l.freqLists {
		if l.minFreq == 0 || freq < l.minFreq {
			l.minFreq = freq
		}
	}
}

// evict removes n items with the lowest frequency - O(1) per item
func (l *LFUCache[K, V]) evict(n int) {
	for i := 0; i < n && len(l.items) > 0; i++ {
		// Get the list with minimum frequency
		minList := l.freqLists[l.minFreq]
		if minList == nil || minList.Len() == 0 {
			l.updateMinFreq()
			minList = l.freqLists[l.minFreq]
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
		l.delete(item.key, elem)
	}
}
