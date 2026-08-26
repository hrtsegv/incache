package incache_test

import (
	"fmt"
	"time"

	"github.com/hrtsegv/incache"
)

func ExampleNewLRU() {
	c := incache.NewLRU[string, int](10)

	c.Set("one", 1)
	c.Set("two", 2)

	if v, ok := c.Get("one"); ok {
		fmt.Println("one:", v)
	}

	c.Delete("one")
	if _, ok := c.Get("one"); !ok {
		fmt.Println("one: deleted")
	}

	// Output:
	// one: 1
	// one: deleted
}

func ExampleNewLFU() {
	c := incache.NewLFU[string, string](3)

	c.Set("a", "value-a")
	c.Set("b", "value-b")
	c.Set("c", "value-c")

	// Access "a" and "b" so their frequency is higher than "c" before the
	// cache fills up.
	c.Get("a")
	c.Get("b")

	// "c" has the lowest frequency, so it's evicted to make room for "d".
	c.Set("d", "value-d")

	_, aOK := c.Get("a")
	_, cOK := c.Get("c")
	fmt.Println("count:", c.Count())
	fmt.Println("a present:", aOK)
	fmt.Println("c present:", cOK)

	// Output:
	// count: 3
	// a present: true
	// c present: false
}

func ExampleNewManual() {
	// A timeInterval of 0 disables the background expiration goroutine;
	// expired keys are still cleaned up lazily on access.
	c := incache.NewManual[string, string](10, 0)
	defer c.Close()

	c.SetWithTimeout("session", "abc123", time.Minute)

	if v, ok := c.Get("session"); ok {
		fmt.Println("session:", v)
	}

	// Output:
	// session: abc123
}

// Example_cacheInterface shows writing code against the Cache interface so
// it works with any eviction policy.
func Example_cacheInterface() {
	var c incache.Cache[string, int] = incache.NewLRU[string, int](10)

	c.Set("answer", 42)
	if v, ok := c.Get("answer"); ok {
		fmt.Println(v)
	}

	// Output:
	// 42
}

// ExampleLRUCache_GetOrSet demonstrates cache-stampede protection: an
// expensive compute function only runs on a genuine miss, never again for a
// key that's already cached.
func ExampleLRUCache_GetOrSet() {
	c := incache.NewLRU[string, int](10)

	compute := func() (int, error) {
		fmt.Println("computing...")
		return 42, nil
	}

	v1, _ := c.GetOrSet("answer", compute)
	v2, _ := c.GetOrSet("answer", compute) // already cached, compute isn't called again

	fmt.Println(v1, v2)

	// Output:
	// computing...
	// 42 42
}
