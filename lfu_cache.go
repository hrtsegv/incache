package incache

import (
	"container/list"
	"fmt"
	"sync"
	"time"
)

// Least Frequently Used Cache
type LFUCache[K comparable, V any] struct {
	mu              sync.RWMutex
	size            uint
	m               map[K]*list.Element
	freqs           map[uint]*list.List
	minFreq         uint
	stopCh          chan struct{}
	cleanupInterval time.Duration
}

type lfuItem[K comparable, V any] struct {
	key      K
	value    V
	freq     uint
	expireAt *time.Time
}

func NewLFU[K comparable, V any](size uint, opts ...Option) *LFUCache[K, V] {
	o := &Options{}
	for _, opt := range opts {
		opt(o)
	}

	c := &LFUCache[K, V]{
		size:            size,
		m:               make(map[K]*list.Element),
		freqs:           make(map[uint]*list.List),
		stopCh:          make(chan struct{}),
		cleanupInterval: o.CleanupInterval,
	}

	if c.cleanupInterval > 0 {
		go c.expireKeys()
	}

	return c
}

func (l *LFUCache[K, V]) Set(key K, value V) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.set(key, value, 0)
}

func (l *LFUCache[K, V]) SetWithTimeout(key K, value V, exp time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.set(key, value, exp)
}

func (l *LFUCache[K, V]) set(key K, value V, exp time.Duration) {
	if l.size == 0 {
		return
	}

	var tm *time.Time
	if exp > 0 {
		t := time.Now().Add(exp)
		tm = &t
	}

	if elem, ok := l.m[key]; ok {
		item := elem.Value.(*lfuItem[K, V])
		item.value = value
		item.expireAt = tm
		l.incrementFreq(elem)
		return
	}

	if len(l.m) >= int(l.size) {
		l.evict(1)
	}

	item := &lfuItem[K, V]{
		key:      key,
		value:    value,
		freq:     1,
		expireAt: tm,
	}
	l.minFreq = 1
	if _, ok := l.freqs[1]; !ok {
		l.freqs[1] = list.New()
	}
	l.m[key] = l.freqs[1].PushFront(item)
}

func (l *LFUCache[K, V]) Get(key K) (v V, b bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	elem, ok := l.m[key]
	if !ok {
		return
	}

	item := elem.Value.(*lfuItem[K, V])
	if item.expireAt != nil && item.expireAt.Before(time.Now()) {
		l.removeElement(elem)
		return
	}

	l.incrementFreq(elem)
	return item.value, true
}

func (l *LFUCache[K, V]) incrementFreq(elem *list.Element) {
	item := elem.Value.(*lfuItem[K, V])
	oldFreq := item.freq
	l.freqs[oldFreq].Remove(elem)

	if l.freqs[oldFreq].Len() == 0 {
		delete(l.freqs, oldFreq)
		if l.minFreq == oldFreq {
			l.minFreq++
		}
	}

	item.freq++
	if _, ok := l.freqs[item.freq]; !ok {
		l.freqs[item.freq] = list.New()
	}
	l.m[item.key] = l.freqs[item.freq].PushFront(item)
}

func (l *LFUCache[K, V]) NotFoundSet(k K, v V) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.m[k]; ok {
		return false
	}
	l.set(k, v, 0)
	return true
}

func (l *LFUCache[K, V]) NotFoundSetWithTimeout(k K, v V, t time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.m[k]; ok {
		return false
	}
	l.set(k, v, t)
	return true
}

func (l *LFUCache[K, V]) GetAll() map[K]V {
	l.mu.RLock()
	defer l.mu.RUnlock()

	m := make(map[K]V)
	now := time.Now()
	for k, elem := range l.m {
		item := elem.Value.(*lfuItem[K, V])
		if item.expireAt == nil || !item.expireAt.Before(now) {
			m[k] = item.value
		}
	}
	return m
}

func (src *LFUCache[K, V]) TransferTo(dst Cache[K, V]) {
	src.mu.Lock()
	defer src.mu.Unlock()

	now := time.Now()
	for k, elem := range src.m {
		item := elem.Value.(*lfuItem[K, V])
		if item.expireAt == nil || !item.expireAt.Before(now) {
			src.removeElement(elem)
			dst.Set(k, item.value)
		}
	}
}

func (src *LFUCache[K, V]) CopyTo(dst Cache[K, V]) {
	src.mu.RLock()
	defer src.mu.RUnlock()

	now := time.Now()
	for k, elem := range src.m {
		item := elem.Value.(*lfuItem[K, V])
		if item.expireAt == nil || !item.expireAt.Before(now) {
			dst.Set(k, item.value)
		}
	}
}

func (l *LFUCache[K, V]) Keys() []K {
	l.mu.RLock()
	defer l.mu.RUnlock()

	keys := make([]K, 0, len(l.m))
	now := time.Now()
	for k, elem := range l.m {
		item := elem.Value.(*lfuItem[K, V])
		if item.expireAt == nil || !item.expireAt.Before(now) {
			keys = append(keys, k)
		}
	}
	return keys
}

func (l *LFUCache[K, V]) Purge() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.m = make(map[K]*list.Element)
	l.freqs = make(map[uint]*list.List)
	l.minFreq = 0
}

func (l *LFUCache[K, V]) Close() {
	if l.cleanupInterval > 0 {
		l.mu.Lock()
		defer l.mu.Unlock()
		select {
		case <-l.stopCh:
			// already closed
		default:
			close(l.stopCh)
		}
	}
}

func (l *LFUCache[K, V]) expireKeys() {
	ticker := time.NewTicker(l.cleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			now := time.Now()
			l.mu.Lock()
			for _, elem := range l.m {
				item := elem.Value.(*lfuItem[K, V])
				if item.expireAt != nil && item.expireAt.Before(now) {
					l.removeElement(elem)
				}
			}
			l.mu.Unlock()
		case <-l.stopCh:
			return
		}
	}
}

func (l *LFUCache[K, V]) Count() int {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var count int
	now := time.Now()
	for _, elem := range l.m {
		item := elem.Value.(*lfuItem[K, V])
		if item.expireAt == nil || !item.expireAt.Before(now) {
			count++
		}
	}
	return count
}

func (l *LFUCache[K, V]) Len() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.m)
}

func (l *LFUCache[K, V]) Delete(k K) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if elem, ok := l.m[k]; ok {
		l.removeElement(elem)
	}
}

func (l *LFUCache[K, V]) removeElement(elem *list.Element) {
	item := elem.Value.(*lfuItem[K, V])
	delete(l.m, item.key)
	l.freqs[item.freq].Remove(elem)
	if l.freqs[item.freq].Len() == 0 {
		delete(l.freqs, item.freq)
		if l.minFreq == item.freq {
			// This might be tricky, but we only really care about minFreq during eviction
			// and minFreq will be reset to 1 on new Set.
		}
	}
}

func (l *LFUCache[K, V]) evict(n int) {
	for i := 0; i < n; i++ {
		if len(l.m) == 0 {
			return
		}
		for l.freqs[l.minFreq] == nil || l.freqs[l.minFreq].Len() == 0 {
			l.minFreq++
		}
		list := l.freqs[l.minFreq]
		elem := list.Back()
		l.removeElement(elem)
	}
}

func (l *LFUCache[K, V]) Inspect() {
	l.mu.RLock()
	defer l.mu.RUnlock()

	// Iterate through frequencies in ascending order
	var freqs []uint
	for f := range l.freqs {
		freqs = append(freqs, f)
	}
	// Sort frequencies
	for i := 0; i < len(freqs); i++ {
		for j := i + 1; j < len(freqs); j++ {
			if freqs[i] > freqs[j] {
				freqs[i], freqs[j] = freqs[j], freqs[i]
			}
		}
	}

	for _, f := range freqs {
		for elem := l.freqs[f].Front(); elem != nil; elem = elem.Next() {
			item := elem.Value.(*lfuItem[K, V])
			fmt.Printf("key: %v, value: %v, expireAt: %v, freq: %v\n", item.key, item.value, item.expireAt, item.freq)
		}
	}
}
