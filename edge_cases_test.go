package incache

import (
	"testing"
	"time"
)

func TestSizeOne_LRU(t *testing.T) {
	c := NewLRU[string, string](1)

	c.Set("a", "1")
	if v, ok := c.Get("a"); !ok || v != "1" {
		t.Fatalf("Get(a) = (%q, %v), want (1, true)", v, ok)
	}

	c.Set("b", "2")
	if _, ok := c.Get("a"); ok {
		t.Errorf("expected a to be evicted once b is set in a size-1 cache")
	}
	if v, ok := c.Get("b"); !ok || v != "2" {
		t.Fatalf("Get(b) = (%q, %v), want (2, true)", v, ok)
	}
	if l := c.Len(); l != 1 {
		t.Errorf("Len() = %d, want 1", l)
	}
}

func TestSizeOne_LFU(t *testing.T) {
	c := NewLFU[string, string](1)

	c.Set("a", "1")
	c.Set("b", "2")

	if _, ok := c.Get("a"); ok {
		t.Errorf("expected a to be evicted once b is set in a size-1 cache")
	}
	if v, ok := c.Get("b"); !ok || v != "2" {
		t.Fatalf("Get(b) = (%q, %v), want (2, true)", v, ok)
	}
	if l := c.Len(); l != 1 {
		t.Errorf("Len() = %d, want 1", l)
	}
}

func TestSizeOne_MCache(t *testing.T) {
	c := NewManual[string, string](1, 0)

	c.Set("a", "1")
	c.Set("b", "2")

	if _, ok := c.Get("a"); ok {
		t.Errorf("expected a to be evicted once b is set in a size-1 cache")
	}
	if v, ok := c.Get("b"); !ok || v != "2" {
		t.Fatalf("Get(b) = (%q, %v), want (2, true)", v, ok)
	}
	if l := c.Len(); l != 1 {
		t.Errorf("Len() = %d, want 1", l)
	}
}

// TestMixedExpiry_Sharded_LRU sets a mix of expiring and non-expiring
// entries into a cache large enough to be sharded (see largeCacheSize in
// sharding_concurrency_test.go), waits for the expiring ones to lapse, and
// checks that GetAll/Keys/Count/Len agree with each other across shards.
func TestMixedExpiry_Sharded_LRU(t *testing.T) {
	c := NewLRU[int, int](largeCacheSize)

	const total = 4000
	for i := 0; i < total; i++ {
		if i%2 == 0 {
			c.SetWithTimeout(i, i, time.Millisecond)
		} else {
			c.Set(i, i)
		}
	}

	time.Sleep(20 * time.Millisecond)

	want := total / 2 // the non-expiring half
	if got := c.Count(); got != want {
		t.Errorf("Count() = %d, want %d", got, want)
	}
	if got := len(c.GetAll()); got != want {
		t.Errorf("len(GetAll()) = %d, want %d", got, want)
	}
	if got := len(c.Keys()); got != want {
		t.Errorf("len(Keys()) = %d, want %d", got, want)
	}
	if got := c.Len(); got != total {
		t.Errorf("Len() = %d, want %d (includes not-yet-swept expired entries)", got, total)
	}
}

func TestMixedExpiry_Sharded_LFU(t *testing.T) {
	c := NewLFU[int, int](largeCacheSize)

	const total = 4000
	for i := 0; i < total; i++ {
		if i%2 == 0 {
			c.SetWithTimeout(i, i, time.Millisecond)
		} else {
			c.Set(i, i)
		}
	}

	time.Sleep(20 * time.Millisecond)

	want := total / 2
	if got := c.Count(); got != want {
		t.Errorf("Count() = %d, want %d", got, want)
	}
	if got := len(c.GetAll()); got != want {
		t.Errorf("len(GetAll()) = %d, want %d", got, want)
	}
	if got := len(c.Keys()); got != want {
		t.Errorf("len(Keys()) = %d, want %d", got, want)
	}
	if got := c.Len(); got != total {
		t.Errorf("Len() = %d, want %d (includes not-yet-swept expired entries)", got, total)
	}
}

func TestMixedExpiry_Sharded_MCache(t *testing.T) {
	c := NewManual[int, int](largeCacheSize, 0)

	const total = 4000
	for i := 0; i < total; i++ {
		if i%2 == 0 {
			c.SetWithTimeout(i, i, time.Millisecond)
		} else {
			c.Set(i, i)
		}
	}

	time.Sleep(20 * time.Millisecond)

	want := total / 2
	if got := c.Count(); got != want {
		t.Errorf("Count() = %d, want %d", got, want)
	}
	if got := len(c.GetAll()); got != want {
		t.Errorf("len(GetAll()) = %d, want %d", got, want)
	}
	if got := len(c.Keys()); got != want {
		t.Errorf("len(Keys()) = %d, want %d", got, want)
	}
	// MCache has no background sweeper here (timeInterval 0), so, like
	// LRU/LFU, Len() still counts the not-yet-swept expired half.
	if got := c.Len(); got != total {
		t.Errorf("Len() = %d, want %d", got, total)
	}
}
