package incache

import (
	"sync"
	"testing"
	"time"
)

func TestSet_LRU(t *testing.T) {
	c := NewLRU[string, string](10)

	c.Set("key1", "value1")
	if v, ok := c.Get("key1"); !ok || v != "value1" {
		t.Errorf("Set failed")
	}
}

func TestGet_LRU(t *testing.T) {
	c := NewLRU[string, string](10)

	c.Set("key1", "value1")
	if v, ok := c.Get("key1"); !ok || v != "value1" {
		t.Errorf("Get failed")
	}
}

func TestGetAll_LRU(t *testing.T) {
	c := NewLRU[string, string](10)

	c.Set("key1", "value1")
	c.Set("key2", "value2")
	c.Set("key3", "value3")
	c.Set("key4", "value4")
	c.Set("key5", "value5")
	c.Set("key6", "value6")
	c.Set("key7", "value7")
	c.Set("key8", "value8")
	c.Set("key9", "value9")
	c.Set("key10", "value10")
	c.Set("key11", "value11")
	c.Set("key12", "value12")

	if l := len(c.GetAll()); l != 10 {
		t.Errorf("GetAll failed: expected %d, got %d\n", 10, l)
	}
}

func TestSetWithTimeout_LRU(t *testing.T) {
	c := NewLRU[string, string](10)

	c.SetWithTimeout("key1", "value1", 2*time.Millisecond)

	if v, ok := c.Get("key1"); !ok || v != "value1" {
		t.Errorf("SetWithTimeout failed: expected value1, got %v", v)
	}

	time.Sleep(3 * time.Millisecond)

	if _, ok := c.Get("key1"); ok {
		t.Errorf("SetWithTimeout failed: key should have expired")
	}
}

func TestNotFoundSet_LRU(t *testing.T) {
	c := NewLRU[string, string](10)

	if !c.NotFoundSet("key1", "value1") {
		t.Errorf("NotFoundSet failed")
	}

	if c.NotFoundSet("key1", "value2") {
		t.Errorf("NotFoundSet failed")
	}
}

func TestNotFoundSetWithExpired_LRU(t *testing.T) {
	c := NewLRU[string, string](10)

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

func TestNotFoundSetWithTimeout_LRU(t *testing.T) {
	c := NewLRU[string, string](10)

	if !c.NotFoundSetWithTimeout("key1", "value1", 0) {
		t.Errorf("NotFoundSetWithTimeout failed")
	}

	if c.NotFoundSetWithTimeout("key1", "value2", 0) {
		t.Errorf("NotFoundSetWithTimeout failed")
	}
}

func TestDelete_LRU(t *testing.T) {
	c := NewLRU[string, string](10)

	c.Set("key1", "value1")

	c.Delete("key1")

	if _, ok := c.Get("key1"); ok {
		t.Errorf("Delete failed")
	}
}

func TestTransferTo_LRU(t *testing.T) {
	c := NewLRU[string, string](10)

	c.Set("key1", "value1")
	c.Set("key2", "value2")
	c.Set("key3", "value3")
	c.Set("key4", "value4")
	c.Set("key5", "value5")

	c2 := NewLRU[string, string](10)

	c.TransferTo(c2)

	if _, ok := c2.Get("key1"); !ok {
		t.Errorf("TransferTo failed")
	}

	if c.Len() != 0 || c2.Len() != 5 {
		t.Errorf("TransferTo failed: src.Len=%d, dst.Len=%d", c.Len(), c2.Len())
	}
}

func TestCopyTo_LRU(t *testing.T) {
	c := NewLRU[string, string](10)

	c.Set("key1", "value1")
	c.Set("key2", "value2")
	c.Set("key3", "value3")
	c.Set("key4", "value4")
	c.Set("key5", "value5")
	c.SetWithTimeout("key6", "value6", time.Second)

	c2 := NewLRU[string, string](10)

	c.CopyTo(c2)

	if _, ok := c2.Get("key1"); !ok {
		t.Errorf("CopyTo failed")
	}

	if c.Len() != 6 || c2.Len() != 6 {
		t.Errorf("CopyTo failed: src.Len=%d, dst.Len=%d", c.Len(), c2.Len())
	}
}

func TestKeys_LRU(t *testing.T) {
	c := NewLRU[string, string](10)

	c.Set("key1", "value1")
	c.Set("key2", "value2")
	c.Set("key3", "value3")
	c.Set("key4", "value4")
	c.Set("key5", "value5")
	c.SetWithTimeout("key6", "value6", 1)
	c.SetWithTimeout("key7", "value7", 1)
	c.SetWithTimeout("key8", "value8", 1)
	c.SetWithTimeout("key9", "value9", 1)
	c.Set("key10", "value10")

	keys := c.Keys()

	if len(keys) != 6 {
		t.Errorf("Keys failed: expected 6, got %d", len(keys))
	}
}

func TestPurge_LRU(t *testing.T) {
	c := NewLRU[string, string](10)

	c.Set("key1", "value1")
	c.Set("key2", "value2")
	c.Set("key3", "value3")

	c.Purge()

	if _, ok := c.Get("key1"); ok {
		t.Errorf("Purge failed")
	}

	if _, ok := c.Get("key2"); ok {
		t.Errorf("Purge failed")
	}

	if _, ok := c.Get("key3"); ok {
		t.Errorf("Purge failed")
	}

	// Should be able to use cache after purge
	c.Set("key4", "value4")
	if v, ok := c.Get("key4"); !ok || v != "value4" {
		t.Errorf("Expected to use cache after purge")
	}
}

func TestCount_LRU(t *testing.T) {
	c := NewLRU[string, string](10)

	c.Set("key1", "value1")
	c.Set("key2", "value2")
	c.Set("key3", "value3")
	c.Set("key4", "value4")
	c.Set("key5", "value5")

	if c.Count() != 5 {
		t.Errorf("Count failed")
	}

	c.SetWithTimeout("key6", "value6", time.Microsecond)
	time.Sleep(time.Millisecond)

	if c.Count() != 5 {
		t.Errorf("Count failed")
	}
}

func TestLen_LRU(t *testing.T) {
	c := NewLRU[string, string](10)

	c.Set("key1", "value1")
	c.Set("key2", "value2")
	c.Set("key3", "value3")
	c.Set("key4", "value4")
	c.Set("key5", "value5")

	if c.Len() != 5 {
		t.Errorf("Len failed")
	}

	c.SetWithTimeout("key6", "value6", time.Microsecond)
	time.Sleep(time.Millisecond)

	// Len includes expired items
	if c.Len() != 6 {
		t.Errorf("Len failed")
	}
}

func TestEvict_LRU(t *testing.T) {
	c := NewLRU[string, string](5)

	c.Set("key1", "value1")
	c.Set("key2", "value2")
	c.Set("key3", "value3")
	c.Set("key4", "value4")
	c.Set("key5", "value5")

	// Access key1 to make it recently used
	c.Get("key1")

	// Add new key, should evict key2 (least recently used)
	c.Set("key6", "value6")

	if _, ok := c.Get("key2"); ok {
		t.Errorf("Expected key2 to be evicted")
	}

	if _, ok := c.Get("key1"); !ok {
		t.Errorf("Expected key1 to still exist")
	}

	if c.Len() != 5 {
		t.Errorf("Evict failed: expected Len=5, got %d", c.Len())
	}
}

func TestSizeZero_LRU(t *testing.T) {
	c := NewLRU[string, string](0)

	c.Set("key1", "value1")

	if _, ok := c.Get("key1"); ok {
		t.Errorf("Expected size 0 cache to not store items")
	}

	if c.Len() != 0 {
		t.Errorf("Expected Len to be 0")
	}
}

func TestConcurrent_LRU(t *testing.T) {
	c := NewLRU[int, int](1000)
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

func TestUpdateExisting_LRU(t *testing.T) {
	c := NewLRU[string, string](5)

	c.Set("key1", "value1")
	c.Set("key1", "value2")

	if v, ok := c.Get("key1"); !ok || v != "value2" {
		t.Errorf("Expected value2, got %v", v)
	}

	if c.Len() != 1 {
		t.Errorf("Expected Len=1 after update, got %d", c.Len())
	}
}
