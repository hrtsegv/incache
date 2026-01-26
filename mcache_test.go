package incache

import (
	"sync"
	"testing"
	"time"
)

func TestSet(t *testing.T) {
	c := NewManual[string, string](10, 0)

	c.Set("key1", "value1")
	if v, ok := c.Get("key1"); !ok || v != "value1" {
		t.Errorf("Set failed")
	}
}

func TestNotFoundSet(t *testing.T) {
	c := NewManual[string, string](10, 0)

	key := "key1"
	value := "value1"

	ok := c.NotFoundSet(key, value)
	if !ok {
		t.Error("Expected NotFoundSet to return true for a new key")
	}

	v, ok := c.Get(key)
	if !ok || v != value {
		t.Error("Expected value to be added using NotFoundSet")
	}

	ok = c.NotFoundSet(key, value)
	if ok {
		t.Error("Expected NotFoundSet to return false for an existing key")
	}
}

func TestNotFoundSetWithExpired(t *testing.T) {
	c := NewManual[string, string](10, 0)

	c.SetWithTimeout("key1", "value1", time.Millisecond)
	time.Sleep(2 * time.Millisecond)

	// Key exists but is expired, should allow setting
	if !c.NotFoundSet("key1", "new_value") {
		t.Errorf("Expected to set key1 since it's expired")
	}

	if v, ok := c.Get("key1"); !ok || v != "new_value" {
		t.Errorf("Expected to get 'new_value', got '%v'", v)
	}
}

func TestNotFoundSetWithTimeout(t *testing.T) {
	c := NewManual[string, string](10, time.Millisecond*200)
	defer c.Close()

	key := "key1"
	value := "value1"
	timeout := time.Second

	ok := c.NotFoundSetWithTimeout(key, value, timeout)
	if !ok {
		t.Error("Expected NotFoundSetWithTimeout to return true for a new key")
	}

	v, ok := c.Get(key)
	if !ok || v != value {
		t.Error("Expected value to be added using NotFoundSetWithTimeout")
	}

	ok = c.NotFoundSetWithTimeout(key, value, timeout)
	if ok {
		t.Error("Expected NotFoundSetWithTimeout to return false for an existing key")
	}

	ok = c.NotFoundSetWithTimeout("key2", "value2", timeout)
	if !ok {
		t.Error("Expected NotFoundSetWithTimeout to return true for a new key with timeout")
	}

	ok = c.NotFoundSetWithTimeout("key3", "value3", -time.Second)
	if !ok {
		t.Error("Expected NotFoundSetWithTimeout to return true for a new key with negative timeout")
	}
}

func TestSetWithTimeout(t *testing.T) {
	c := NewManual[string, string](10, 0)
	key := "test"
	value := "test value"
	timeout := time.Millisecond * 50

	c.SetWithTimeout(key, value, timeout)

	v, ok := c.Get(key)
	if value != v || !ok {
		t.Errorf("SetWithTimeout failed")
	}

	time.Sleep(time.Millisecond * 100)

	v, ok = c.Get(key)
	if v != "" || ok {
		t.Errorf("SetWithTimeout failed: key should have expired")
	}
}

func TestGet(t *testing.T) {
	c := NewManual[string, string](10, 0)

	c.Set("key1", "value1")

	value, ok := c.Get("key1")
	if !ok || value != "value1" {
		t.Errorf("Get returned unexpected value for key1")
	}

	_, ok = c.Get("nonexistent")
	if ok {
		t.Errorf("Get returned true for a non-existent key")
	}
}

func TestGetAll(t *testing.T) {
	c := NewManual[string, string](10, 0)

	c.Set("key1", "value1")
	c.Set("key2", "value2")
	c.SetWithTimeout("key3", "value3", time.Millisecond)

	if m := c.GetAll(); len(m) != 3 {
		t.Errorf("GetAll returned unexpected number of keys: %d", len(m))
	}

	time.Sleep(time.Millisecond * 2)

	if m := c.GetAll(); len(m) != 2 {
		t.Errorf("GetAll returned unexpected number of keys: %d", len(m))
	}
}

func TestDelete(t *testing.T) {
	c := NewManual[string, string](10, 0)

	c.Set("key1", "value1")
	c.Delete("key1")

	_, ok := c.Get("key1")
	if ok {
		t.Errorf("Get returned true for a deleted key")
	}
}

func TestTransferTo(t *testing.T) {
	src := NewManual[string, string](10, 0)
	dst := NewManual[string, string](10, 0)

	src.Set("key1", "value1")
	src.TransferTo(dst)

	value, ok := dst.Get("key1")
	if !ok || value != "value1" {
		t.Errorf("TransferTo did not transfer data correctly")
	}

	_, ok = src.Get("key1")
	if ok {
		t.Errorf("TransferTo did not clear source database")
	}
}

func TestCopyTo(t *testing.T) {
	src := NewManual[string, string](10, 0)
	dst := NewManual[string, string](10, 0)

	src.Set("key1", "value1")
	src.CopyTo(dst)

	value, ok := dst.Get("key1")
	if !ok || value != "value1" {
		t.Errorf("CopyTo did not copy data correctly")
	}

	value, ok = src.Get("key1")
	if !ok || value != "value1" {
		t.Errorf("CopyTo modified the source database")
	}
}

func TestKeys(t *testing.T) {
	c := NewManual[string, string](10, 0)

	c.Set("key1", "value1")
	c.Set("key2", "value2")
	c.SetWithTimeout("key3", "value3", 1)
	c.SetWithTimeout("key4", "value4", 1)
	c.SetWithTimeout("key5", "value5", 1)
	c.Set("key6", "value6")

	keys := c.Keys()

	if len(keys) != 3 {
		t.Errorf("Unexpected number of keys returned: %d", len(keys))
	}

	expectedKeys := map[string]bool{"key1": true, "key2": true, "key6": true}
	for _, key := range keys {
		if !expectedKeys[key] {
			t.Errorf("Unexpected key %s returned", key)
		}
	}
}

func TestPurge(t *testing.T) {
	c := NewManual[string, string](10, 0)

	c.Set("key1", "value1")
	c.Set("key2", "value2")
	c.Set("key3", "value3")

	c.Purge()

	if c.Len() != 0 {
		t.Errorf("Purge: cache should be empty")
	}

	// Should be able to use cache after purge
	c.Set("key4", "value4")
	if v, ok := c.Get("key4"); !ok || v != "value4" {
		t.Errorf("Expected to use cache after purge")
	}
}

func TestClose(t *testing.T) {
	c := NewManual[string, string](10, time.Millisecond*100)

	c.Set("key1", "value1")
	c.Set("key2", "value2")
	c.SetWithTimeout("key3", "value3", 1)

	c.Close()

	// After close, the stopCh should be closed
	select {
	case _, ok := <-c.stopCh:
		if ok {
			t.Errorf("Close: expiration goroutine did not stop as expected")
		}
	default:
		t.Errorf("Close: expiration goroutine did not stop as expected")
	}
}

func TestCount(t *testing.T) {
	c := NewManual[int, string](10, 0)
	c.Set(1, "one")
	c.Set(2, "two")
	c.SetWithTimeout(3, "three", time.Millisecond*100)
	c.SetWithTimeout(4, "four", time.Millisecond*100)
	c.SetWithTimeout(5, "five", time.Millisecond*100)

	count := c.Count()
	if count != 5 {
		t.Errorf("Count: expected: %d, got: %d", 5, count)
	}

	time.Sleep(time.Millisecond * 200)

	count = c.Count()
	if count != 2 {
		t.Errorf("Count: expected: %d, got: %d", 2, count)
	}
}

func TestLen(t *testing.T) {
	c := NewManual[string, string](10, 0)
	c.Set("1", "one")
	c.Set("2", "two")
	c.SetWithTimeout("3", "three", time.Millisecond*100)
	c.SetWithTimeout("4", "four", time.Millisecond*100)

	if l := c.Len(); l != 4 {
		t.Errorf("Len: expected: %d, got: %d", 4, l)
	}
}

func TestLenWithExpiry(t *testing.T) {
	c := NewManual[string, string](10, time.Millisecond*50)
	defer c.Close()

	c.Set("1", "one")
	c.Set("2", "two")
	c.SetWithTimeout("3", "three", time.Millisecond*30)
	c.SetWithTimeout("4", "four", time.Millisecond*30)

	time.Sleep(time.Millisecond * 100)

	if l := c.Len(); l != 2 {
		t.Errorf("Len: expected: %d, got: %d", 2, l)
	}
}

func TestEvict(t *testing.T) {
	c := NewManual[string, string](4, 0)

	c.Set("1", "one")
	c.Set("2", "two")
	c.Set("3", "three")
	c.Set("4", "four")

	if count := c.Count(); count != 4 {
		t.Errorf("Count: expected: %d, got: %d", 4, count)
	}

	// Adding a new item should trigger eviction
	c.Set("5", "five")

	if count := c.Count(); count != 4 {
		t.Errorf("Count: expected: %d, got: %d", 4, count)
	}
}

func TestSizeZero(t *testing.T) {
	c := NewManual[string, string](0, 0)

	c.Set("key1", "value1")

	if _, ok := c.Get("key1"); ok {
		t.Errorf("Expected size 0 cache to not store items")
	}

	if c.Len() != 0 {
		t.Errorf("Expected Len to be 0")
	}

	if c.NotFoundSet("key2", "value2") {
		t.Errorf("Expected NotFoundSet to return false for size 0 cache")
	}
}

func TestConcurrent(t *testing.T) {
	c := NewManual[int, int](1000, 0)
	var wg sync.WaitGroup

	// Concurrent writes
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				c.Set(n*100+j, n*100+j)
			}
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				c.Get(n*100 + j)
			}
		}(i)
	}

	wg.Wait()
}

func TestConcurrentWithExpiry(t *testing.T) {
	c := NewManual[int, int](1000, time.Millisecond*10)
	defer c.Close()

	var wg sync.WaitGroup

	// Concurrent writes
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				c.SetWithTimeout(n*50+j, n*50+j, time.Millisecond*5)
			}
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				c.Get(n*50 + j)
			}
		}(i)
	}

	wg.Wait()
}

func TestUpdateExisting(t *testing.T) {
	c := NewManual[string, string](5, 0)

	c.Set("key1", "value1")
	c.Set("key1", "value2")

	if v, ok := c.Get("key1"); !ok || v != "value2" {
		t.Errorf("Expected value2, got %v", v)
	}

	if c.Len() != 1 {
		t.Errorf("Expected Len=1 after update, got %d", c.Len())
	}
}
