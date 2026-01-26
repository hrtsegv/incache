## incache

A high-performance, thread-safe in-memory cache library for Go. Designed to be embedded in monolith backend servers where a centralized cache like Redis is not needed.

### Features

- **Multiple eviction policies**: LRU (Least Recently Used), LFU (Least Frequently Used), and Manual (no automatic eviction policy)
- **O(1) operations**: Both LRU and LFU implementations provide constant-time Get, Set, and Delete operations
- **Thread-safe**: All cache types are safe for concurrent use
- **TTL support**: Optional expiration time for cache entries
- **Generic types**: Full support for Go generics
- **Zero dependencies**: Only uses Go standard library

### Installation

```bash
go get github.com/knbr13/incache
```

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

	"github.com/knbr13/incache"
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

	"github.com/knbr13/incache"
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

	"github.com/knbr13/incache"
)

func main() {
	// Create cache with background expiration check every 100ms
	cache := incache.NewManual[string, string](100, time.Millisecond*100)
	defer cache.Close() // Important: stop the background goroutine

	cache.SetWithTimeout("temp", "data", time.Second*5)

	// The background goroutine will automatically remove expired items
}
```

### Using the Cache Interface

All cache types implement the `Cache` interface, allowing you to write polymorphic code:

```go
package main

import (
	"github.com/knbr13/incache"
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

- **LRU Cache**: O(1) for Get, Set, Delete operations using a hashmap + doubly linked list
- **LFU Cache**: O(1) for Get, Set, Delete operations using frequency buckets
- **MCache**: O(1) for Get, Set, Delete; O(n) for eviction when cache is full

### Thread Safety

All cache implementations use `sync.Mutex` for thread safety. The `TransferTo` and `CopyTo` operations are designed to be deadlock-safe by not holding multiple locks simultaneously.

### Contributing

Contributions are welcome! If you find any bugs or have suggestions for improvements, please open an issue or submit a pull request on GitHub.
