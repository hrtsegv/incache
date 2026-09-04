## incache

A high-performance, thread-safe in-memory cache library for Go. Designed to be embedded in monolith backend servers where a centralized cache like Redis is not needed.

### Features

- **Multiple eviction policies**: LRU (Least Recently Used), LFU (Least Frequently Used), and Manual (no automatic eviction policy)
- **O(1) operations**: Both LRU and LFU implementations provide constant-time Get, Set, and Delete operations
- **Sharded for scale**: Larger caches are internally split into shards, each with its own lock, to reduce contention under concurrent access
- **Thread-safe**: All cache types are safe for concurrent use
- **TTL support**: Optional expiration time for cache entries
- **Cache-stampede protection**: `GetOrSet`/`GetOrSetWithTimeout` coalesce concurrent misses on the same key into a single compute call
- **Generic types**: Full support for Go generics
- **Zero dependencies**: Only uses Go standard library

### Installation

```bash
go get github.com/hrtsegv/incache
```

Requires Go 1.25 or later (the module pins `go 1.25.5`) - sharding uses `hash/maphash.Comparable`, added to the standard library in Go 1.24.

### Cache Types

| Type | Eviction Policy | Use Case |
|------|-----------------|----------|
| `LRUCache` | Least Recently Used | General purpose caching where recent items are more likely to be accessed again |
| `LFUCache` | Least Frequently Used | Caching where frequently accessed items should be retained |
| `MCache` | Manual/Random | Simple caching with background expiration cleanup |

### Example

```go
package main

import (
	"fmt"
	"time"

	"github.com/hrtsegv/incache"
)

func main() {
	// Create a new LRU Cache with capacity of 10 items
	c := incache.NewLRU[string, int](10)

	// Set some key-value pairs
	c.Set("one", 1)
	c.Set("two", 2)
	c.Set("three", 3)

	// Set with expiration
	c.SetWithTimeout("four", 4, time.Second*30)

	// Get values by key
	if v, ok := c.Get("one"); ok {
		fmt.Println("Value for 'one':", v)
	}

	// Delete a key
	c.Delete("one")

	// Get all keys
	fmt.Println("Keys:", c.Keys())

	// Transfer data to another cache
	c2 := incache.NewLRU[string, int](10)
	c.TransferTo(c2)

	// Copy data to another cache
	c3 := incache.NewLRU[string, int](10)
	c2.CopyTo(c3)
}
```

### LFU Cache Example

```go
package main

import (
	"fmt"

	"github.com/hrtsegv/incache"
)

func main() {
	// Create LFU cache - items accessed less frequently are evicted first
	cache := incache.NewLFU[string, string](3)

	cache.Set("a", "value-a")
	cache.Set("b", "value-b")
	cache.Set("c", "value-c")

	// Access "a" multiple times to increase its frequency
	cache.Get("a")
	cache.Get("a")
	cache.Get("a")

	// Access "b" once
	cache.Get("b")

	// Adding "d" will evict "c" (lowest frequency)
	cache.Set("d", "value-d")

	// "a" and "b" are still present, "c" was evicted
	fmt.Println(cache.Keys()) // Will contain "a", "b", "d"
}
```

### Manual Cache with Background Expiration

```go
package main

import (
	"time"

	"github.com/hrtsegv/incache"
)

func main() {
	// Create cache with background expiration check every 100ms
	cache := incache.NewManual[string, string](100, time.Millisecond*100)
	defer cache.Close() // Important: stop the background goroutine

	cache.SetWithTimeout("temp", "data", time.Second*5)

	// The background goroutine will automatically remove expired items
}
```

### Cache Stampede Protection

`GetOrSet` and `GetOrSetWithTimeout` return the cached value if present, or compute and store it otherwise. Concurrent callers that miss on the same key are coalesced onto a single call to the compute function, so a hot key expiring doesn't trigger a stampede of duplicate (often expensive) work like a database query or an upstream API call:

```go
package main

import (
	"fmt"
	"time"

	"github.com/hrtsegv/incache"
)

func main() {
	cache := incache.NewLRU[string, string](1000)

	loadUser := func() (string, error) {
		// An expensive lookup - a DB query, an API call, etc. Only one
		// goroutine will run this per missing key, even under a burst of
		// concurrent requests for the same key.
		return "user data", nil
	}

	value, err := cache.GetOrSetWithTimeout("user:42", loadUser, time.Minute)
	if err != nil {
		// The error is returned to every waiting caller; nothing is cached.
		panic(err)
	}
	fmt.Println(value)
}
```

### Using the Cache Interface

All cache types implement the `Cache` interface, allowing you to write polymorphic code:

```go
package main

import (
	"github.com/hrtsegv/incache"
)

func processWithCache(cache incache.Cache[string, int]) {
	cache.Set("key", 42)
	if v, ok := cache.Get("key"); ok {
		println(v)
	}
}

func main() {
	// Can use any cache type
	lru := incache.NewLRU[string, int](100)
	lfu := incache.NewLFU[string, int](100)

	processWithCache(lru)
	processWithCache(lfu)
}
```

### API Reference

All cache types provide the following methods:

| Method | Description |
|--------|-------------|
| `Get(key)` | Returns value and boolean indicating if found (excludes expired) |
| `Set(key, value)` | Adds or updates a key-value pair |
| `SetWithTimeout(key, value, duration)` | Adds with expiration time |
| `Delete(key)` | Removes a key-value pair |
| `NotFoundSet(key, value)` | Sets only if key doesn't exist or is expired |
| `NotFoundSetWithTimeout(key, value, duration)` | Same as above with expiration |
| `GetOrSet(key, fn)` | Returns the cached value, or calls `fn`, stores, and returns its result; concurrent misses on the same key are coalesced into one `fn` call |
| `GetOrSetWithTimeout(key, fn, duration)` | Same as above, stored with an expiration time |
| `GetAll()` | Returns all non-expired key-value pairs |
| `Keys()` | Returns all non-expired keys |
| `Purge()` | Removes all entries (cache remains usable) |
| `Count()` | Returns count of non-expired entries |
| `Len()` | Returns total count (including expired) |

Additional methods for `MCache`:
| Method | Description |
|--------|-------------|
| `Close()` | Stops background goroutine and clears cache |

### Performance

- **LRU Cache**: O(1) for Get, Set, Delete, using a hashmap plus an intrusive doubly linked list
- **LFU Cache**: O(1) for Get, Set, Delete, using intrusive frequency buckets
- **MCache**: O(1) for Get, Set, Delete, including eviction when the cache is full

Entries are stored as list nodes directly rather than boxed inside `container/list` elements, so a cache hit costs no allocation and no type assertion on any of the three policies.

When `MCache` is full it makes room by sampling a small, fixed number of entries and dropping an expired one if the sample turns one up, falling back to an arbitrary entry otherwise. Sampling is what keeps eviction O(1); because Go randomizes where a map range starts, a short sample is a random sample of the shard.

#### Sharding

Caches at or above a size threshold (1024 entries) are internally split into shards - each with its own lock and eviction state - selected by hashing the key with `hash/maphash.Comparable`. This is transparent: construction, method signatures and return values are unchanged. Smaller caches keep exactly one shard, which is identical to the single-lock behavior below the threshold: one lock, one eviction list, strictly global LRU/LFU order.

For larger caches, this trades strictly-global eviction order for one lock per shard: `GetAll`, `Keys`, `Count` and `Purge` lock one shard at a time rather than the whole cache, and eviction becomes per-shard (approximating, but not guaranteeing, a single global order).

Measured on an 11th Gen i7-11850H (`GOMAXPROCS=16`), parallel `Get`/`Set` against a 1,000,000-entry cache (`go test -bench Parallel_Large`, see [benchmark_test.go](benchmark_test.go)):

| Operation | Single lock | Sharded | Speedup |
|-----------|------------:|--------:|--------:|
| LRU Set   | 455.9 ns/op | 122.8 ns/op | 3.7x |
| LRU Get   | 512.7 ns/op |  97.1 ns/op | 5.3x |
| LFU Set   | 640.9 ns/op | 173.6 ns/op | 3.7x |
| LFU Get   | 671.9 ns/op | 140.3 ns/op | 4.8x |
| MCache Set| 526.9 ns/op | 107.8 ns/op | 4.9x |
| MCache Get| 445.1 ns/op |  59.5 ns/op | 7.5x |

Both columns come from the same code, with `shardingThreshold` raised past the cache size to produce the single-lock column, and the two runs interleaved so they see the same thermal state. Absolute figures on a laptop move around a lot between runs - the ratios are the stable part.

Actual gains depend on your workload's key distribution, `GOMAXPROCS`, and how contended the cache actually is - run `make bench` (or `go test -bench=. -benchmem ./...`) on your own hardware for numbers that matter to your use case.

### Thread Safety

All cache implementations use `sync.Mutex` (per-shard, see Sharding above) for thread safety. The `TransferTo` and `CopyTo` operations are designed to be deadlock-safe by not holding multiple locks simultaneously, including when source and destination have different shard counts.

### Contributing

Contributions are welcome! If you find any bugs or have suggestions for improvements, please open an issue or submit a pull request on GitHub.
