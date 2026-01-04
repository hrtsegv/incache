package incache

import (
	"fmt"
	"testing"
	"time"
)

func TestSet(t *testing.T) {
	c := NewManual[string, string](10)

	c.Set("key1", "value1")
	if c.m["key1"].value != "value1" {
		t.Errorf("Set failed")
	}
}

func TestNotFoundSet(t *testing.T) {
	c := NewManual[string, string](10)

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

func TestNotFoundSetWithTimeout(t *testing.T) {
	c := NewManual[string, string](10)
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

	time.Sleep(c.timeInterval + timeout)

	_, ok = c.Get("key2")
	if ok {
		t.Error("Expected value to be expired and removed after the specified timeout")
	}

	ok = c.NotFoundSetWithTimeout("key3", "value3", -time.Second)
	if !ok {
		t.Error("Expected NotFoundSetWithTimeout to return true for a new key with negative timeout")
	}
}

func TestSetWithTimeout(t *testing.T) {
	c := NewManual[string, string](10)
	key := "test"
	value := "test value"
	timeout := time.Second

	c.SetWithTimeout(key, value, timeout)

	v, ok := c.Get(key)
	if value != v || !ok {
		t.Errorf("SetWithTimeout failed")
	}

	time.Sleep(time.Second)

	v, ok = c.Get(key)
	if v != "" || ok {
		t.Errorf("SetWithTimeout failed")
	}
}

func TestGet(t *testing.T) {
	c := NewManual[string, string](10)

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
	c := NewManual[string, string](10)

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
	c := NewManual[string, string](10)

	c.Set("key1", "value1")
	c.Delete("key1")

	_, ok := c.Get("key1")
	if ok {
		t.Errorf("Get returned true for a deleted key")
	}
}

func TestTransferTo(t *testing.T) {
	src := NewManual[string, string](10)
	dst := NewManual[string, string](10)

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
	src := NewManual[string, string](10)
	dst := NewManual[string, string](10)

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
	c := NewManual[string, string](10)

	c.Set("key1", "value1")
	c.Set("key2", "value2")
	c.SetWithTimeout("key3", "value3", 1)
	c.SetWithTimeout("key4", "value4", 1)
	c.SetWithTimeout("key5", "value5", 1)
	c.Set("key6", "value6")

	keys := c.Keys()

	if len(keys) != 3 {
		t.Errorf("Unexpected number of keys returned")
	}

	expectedKeys := map[string]bool{"key1": true, "key2": true, "key6": true}
	for _, key := range keys {
		if !expectedKeys[key] {
			t.Errorf("Unexpected key %s returned", key)
		}
	}
}

func TestClose(t *testing.T) {
	c := NewManual[string, string](10, WithCleanupInterval(time.Millisecond*100))

	c.Set("key1", "value1")
	c.Close()

	select {
	case _, ok := <-c.stopCh:
		if ok {
			t.Errorf("Close: expiration goroutine did not stop as expected")
		}
	case <-time.After(time.Second):
		t.Errorf("Close: timeout waiting for stopCh to close")
	}
}

func TestPurge(t *testing.T) {
	c := NewManual[string, string](10)
	c.Set("key1", "value1")
	c.Set("key2", "value2")
	c.Purge()
	if c.Len() != 0 {
		t.Errorf("Purge: expected length 0, got %d", c.Len())
	}
}

func TestCount(t *testing.T) {
	c := NewManual[int, string](10)
	c.Set(1, "one")
	c.Set(2, "two")
	c.SetWithTimeout(3, "three", time.Millisecond*100)
	c.SetWithTimeout(4, "four", time.Millisecond*100)
	c.SetWithTimeout(5, "five", time.Millisecond*100)

	count := c.Count()
	if count != 5 {
		t.Errorf("Count: expected: %d, got: %d", 5, count)
	}

	time.Sleep(time.Millisecond * 300)

	count = c.Count()
	if count != 2 {
		t.Errorf("Count: expected: %d, got: %d", 2, count)
	}

	c = NewManual[int, string](10)
	c.Set(1, "one")
	c.Set(2, "two")
	c.SetWithTimeout(3, "three", time.Millisecond*100)
	c.SetWithTimeout(4, "four", time.Millisecond*100)
	c.SetWithTimeout(5, "five", time.Millisecond*100)

	if count := c.Count(); count != 5 {
		t.Errorf("Count: expected: %d, got: %d", 5, count)
	}

	time.Sleep(time.Millisecond * 300)

	if count := c.Count(); count != 2 {
		t.Errorf("Count: expected: %d, got: %d", 2, count)
	}
}

func TestLen(t *testing.T) {
	c := NewManual[string, string](10)
	c.Set("1", "one")
	c.Set("2", "two")
	c.SetWithTimeout("3", "three", time.Millisecond*100)
	c.SetWithTimeout("4", "four", time.Millisecond*100)

	if l := c.Len(); l != 4 {
		t.Errorf("Len: expected: %d, got: %d", 4, l)
	}

	c = NewManual[string, string](10, WithCleanupInterval(time.Millisecond*100))

	c.Set("1", "one")
	c.Set("2", "two")
	c.SetWithTimeout("3", "three", time.Millisecond*50)
	c.SetWithTimeout("4", "four", time.Millisecond*50)

	time.Sleep(time.Millisecond * 150)

	if l := c.Len(); l != 2 {
		t.Errorf("Len: expected: %d, got: %d", 2, l)
	}
}

func TestEvict(t *testing.T) {
	c := NewManual[string, string](10)

	c.Set("1", "one")
	c.Set("2", "two")
	c.Set("3", "three")
	c.Set("4", "four")

	fmt.Println(c.Keys())

	if count := c.Count(); count != 4 {
		t.Errorf("Count: expected: %d, got: %d", 4, count)
	}

	c.evict(3)

	if count := c.Count(); count != 1 {
		t.Errorf("Count: expected: %d, got: %d", 1, count)
	}
}
