## incache

The `incache` package provides a simple cache implementation in Go. It can be used as a package in other Go projects to store key-value pairs in memory. The package is safe to use concurrently with multiple goroutines.

### Installation

To use this package in your Go project, you can install it using `go get`:

```bash
go get github.com/knbr13/incache
```

### Example

```go
package main

import (
	"fmt"
	"time"

	"github.com/knbr13/incache"
)

func main() {
	// create a new LRU Cache
	var c incache.Cache[string, int]
	c = incache.NewLRU[string, int](10)

	fmt.Println("keys:", c.Keys())

	// set some key-value pairs
	c.Set("one", 1)
	c.Set("two", 2)
	c.Set("three", 3)
	c.Set("four", 4)
	c.Set("five", 5)

	c.SetWithTimeout("six", 6, time.Millisecond)

	// Get values by key
	v, ok := c.Get("one")
	if ok {
		fmt.Println("Value for key1:", v)
	}

	v, ok = c.Get("two")
	if ok {
		fmt.Println("Value for key1:", v)
	}

	c.Delete("one")

	// create new cache, move data from 'c' to 'c2'
	c2 := incache.NewLRU[string, int](10)
	c.TransferTo(c2)

	// create new cache, copy data from 'c2' to 'c3'
	c3 := incache.NewLRU[string, int](10)
	c2.CopyTo(c3)
}
```

### Contributing

Contributions are welcome! 
If you find any bugs or have suggestions for improvements, please open an issue or submit a pull request on GitHub.