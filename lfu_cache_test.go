package incache

import (
	"sync"
	"testing"
	"time"
)

func TestLFUCache_SetGet(t *testing.T) {
	cache := NewLFU[int, string](10)

	cache.Set(1, "one")
	cache.Set(2, "two")

	if value, ok := cache.Get(1); !ok || value != "one" {
		t.Errorf("Expected to get 'one', got '%v'", value)
	}

	if value, ok := cache.Get(2); !ok || value != "two" {
		t.Errorf("Expected to get 'two', got '%v'", value)
	}
}

func TestLFUCache_Eviction(t *testing.T) {
	cache := NewLFU[int, string](2)

	cache.Set(1, "one")
	cache.Set(2, "two")
	cache.Set(3, "three")

	if _, ok := cache.Get(1); ok {
		t.Logf("all: %v\n", cache.GetAll())
		t.Errorf("Expected 1 to be evicted")
	}

	if value, ok := cache.Get(2); !ok || value != "two" {
		t.Errorf("Expected to get 'two', got '%v'", value)
	}

	if value, ok := cache.Get(3); !ok || value != "three" {
		t.Errorf("Expected to get 'three', got '%v'", value)
	}
}

func TestLFUCache_EvictionByFrequency(t *testing.T) {
	cache := NewLFU[int, string](3)

	cache.Set(1, "one")
	cache.Set(2, "two")
	cache.Set(3, "three")

	// Access 1 and 2 multiple times to increase their frequency
	cache.Get(1)
	cache.Get(1)
	cache.Get(2)

	// Now add a new item - should evict 3 (lowest frequency)
	cache.Set(4, "four")

	if _, ok := cache.Get(3); ok {
		t.Errorf("Expected 3 to be evicted (lowest frequency)")
	}

	if value, ok := cache.Get(1); !ok || value != "one" {
		t.Errorf("Expected 1 to still exist, got '%v'", value)
	}

	if value, ok := cache.Get(2); !ok || value != "two" {
		t.Errorf("Expected 2 to still exist, got '%v'", value)
	}

	if value, ok := cache.Get(4); !ok || value != "four" {
		t.Errorf("Expected 4 to exist, got '%v'", value)
	}
}

func TestLFUCache_SetWithTimeout(t *testing.T) {
	cache := NewLFU[int, string](10)

	cache.SetWithTimeout(1, "one", 2*time.Millisecond)
	time.Sleep(1 * time.Millisecond)

	if value, ok := cache.Get(1); !ok || value != "one" {
		t.Errorf("Expected to get 'one', got '%v'", value)
	}

	time.Sleep(2 * time.Millisecond)

	if v, ok := cache.Get(1); ok {
		t.Logf("v: %v | ok: %v\n", v, ok)
		t.Errorf("Expected 1 to be expired")
	}
}

func TestLFUCache_NotFoundSet(t *testing.T) {
	cache := NewLFU[int, string](10)

	if !cache.NotFoundSet(1, "one") {
		t.Errorf("Expected to set key 1")
	}

	if cache.NotFoundSet(1, "one") {
		t.Errorf("Expected not to set key 1 again")
	}

	if value, ok := cache.Get(1); !ok || value != "one" {
		t.Errorf("Expected to get 'one', got '%v'", value)
	}
}

func TestLFUCache_NotFoundSetWithExpired(t *testing.T) {
	cache := NewLFU[int, string](10)

	cache.SetWithTimeout(1, "one", time.Millisecond)
	time.Sleep(2 * time.Millisecond)

	// Key exists but is expired, should allow setting
	if !cache.NotFoundSet(1, "new_one") {
		t.Errorf("Expected to set key 1 since it's expired")
	}

	if value, ok := cache.Get(1); !ok || value != "new_one" {
		t.Errorf("Expected to get 'new_one', got '%v'", value)
	}
}

func TestLFUCache_TransferTo(t *testing.T) {
	srcCache := NewLFU[int, string](10)
	dstCache := NewLFU[int, string](10)

	srcCache.Set(1, "one")
	srcCache.Set(2, "two")
	srcCache.TransferTo(dstCache)

	if _, ok := srcCache.Get(1); ok {
		t.Errorf("Expected 1 to be transferred")
	}

	if value, ok := dstCache.Get(1); !ok || value != "one" {
		t.Errorf("Expected to get 'one' from dstCache, got '%v'", value)
	}

	if value, ok := dstCache.Get(2); !ok || value != "two" {
		t.Errorf("Expected to get 'two' from dstCache, got '%v'", value)
	}
}

func TestLFUCache_CopyTo(t *testing.T) {
	srcCache := NewLFU[int, string](10)
	dstCache := NewLFU[int, string](10)

	srcCache.Set(1, "one")
	srcCache.Set(2, "two")
	srcCache.CopyTo(dstCache)

	if value, ok := srcCache.Get(1); !ok || value != "one" {
		t.Errorf("Expected to get 'one' from srcCache, got '%v'", value)
	}

	if value, ok := dstCache.Get(1); !ok || value != "one" {
		t.Errorf("Expected to get 'one' from dstCache, got '%v'", value)
	}

	if value, ok := dstCache.Get(2); !ok || value != "two" {
		t.Errorf("Expected to get 'two' from dstCache, got '%v'", value)
	}
}

func TestLFUCache_Keys(t *testing.T) {
	cache := NewLFU[int, string](10)

	cache.Set(1, "one")
	cache.Set(2, "two")
	cache.Set(3, "three")

	keys := cache.Keys()

	expectedKeys := map[int]bool{
		1: true,
		2: true,
		3: true,
	}

	for _, key := range keys {
		if !expectedKeys[key] {
			t.Errorf("Unexpected key %v", key)
		}
	}
}

func TestLFUCache_Purge(t *testing.T) {
	cache := NewLFU[int, string](10)

	cache.Set(1, "one")
	cache.Set(2, "two")
	cache.Purge()

	if value, ok := cache.Get(1); ok {
		t.Errorf("Expected cache to be purged, got '%v'", value)
	}

	if value, ok := cache.Get(2); ok {
		t.Errorf("Expected cache to be purged, got '%v'", value)
	}

	// Should be able to use cache after purge
	cache.Set(3, "three")
	if value, ok := cache.Get(3); !ok || value != "three" {
		t.Errorf("Expected to get 'three' after purge, got '%v'", value)
	}
}

func TestLFUCache_Delete(t *testing.T) {
	cache := NewLFU[int, string](10)

	cache.Set(1, "one")
	cache.Set(2, "two")

	cache.Delete(1)

	if value, ok := cache.Get(1); ok {
		t.Errorf("Expected key 1 to be deleted, got '%v'", value)
	}

	if value, ok := cache.Get(2); !ok || value != "two" {
		t.Errorf("Expected to get 'two', got '%v'", value)
	}
}

func TestLFUCache_SizeZero(t *testing.T) {
	cache := NewLFU[int, string](0)

	cache.Set(1, "one")

	if _, ok := cache.Get(1); ok {
		t.Errorf("Expected size 0 cache to not store items")
	}

	if cache.Len() != 0 {
		t.Errorf("Expected Len to be 0")
	}
}

func TestLFUCache_Count(t *testing.T) {
	cache := NewLFU[int, string](10)

	cache.Set(1, "one")
	cache.Set(2, "two")
	cache.SetWithTimeout(3, "three", time.Millisecond)

	if cache.Count() != 3 {
		t.Errorf("Expected Count to be 3, got %d", cache.Count())
	}

	time.Sleep(2 * time.Millisecond)

	if cache.Count() != 2 {
		t.Errorf("Expected Count to be 2 after expiration, got %d", cache.Count())
	}
}

func TestLFUCache_Len(t *testing.T) {
	cache := NewLFU[int, string](10)

	cache.Set(1, "one")
	cache.Set(2, "two")
	cache.SetWithTimeout(3, "three", time.Millisecond)

	if cache.Len() != 3 {
		t.Errorf("Expected Len to be 3, got %d", cache.Len())
	}

	time.Sleep(2 * time.Millisecond)

	// Len includes expired items
	if cache.Len() != 3 {
		t.Errorf("Expected Len to still be 3 (includes expired), got %d", cache.Len())
	}
}

func TestLFUCache_Concurrent(t *testing.T) {
	cache := NewLFU[int, int](1000)
	var wg sync.WaitGroup

	// Concurrent writes
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				cache.Set(n*100+j, n*100+j)
			}
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				cache.Get(n*100 + j)
			}
		}(i)
	}

	wg.Wait()
}
