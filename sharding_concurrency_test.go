package incache

import (
	"sync"
	"testing"
)

// largeCacheSize is comfortably above shardingThreshold so these tests
// actually exercise the multi-shard path (numShardsFor returns > 1), not
// just the single-shard path every other test in this package uses.
const largeCacheSize = shardingThreshold * 8

func TestLRU_Sharded_Concurrent(t *testing.T) {
	if n := numShardsFor(largeCacheSize); n <= 1 {
		t.Fatalf("test setup: expected multiple shards for size %d, got %d", largeCacheSize, n)
	}

	c := NewLRU[int, int](largeCacheSize)
	var wg sync.WaitGroup

	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				k := n*500 + j
				c.Set(k, k)
				c.Get(k)
				if j%7 == 0 {
					c.Delete(k)
				}
			}
		}(i)
	}
	wg.Wait()

	if l := c.Len(); uint(l) > largeCacheSize {
		t.Errorf("Len() = %d, must not exceed configured size %d", l, largeCacheSize)
	}
	if count := c.Count(); count > c.Len() {
		t.Errorf("Count() = %d, must not exceed Len() = %d", count, c.Len())
	}
	if got := len(c.GetAll()); got != c.Count() {
		t.Errorf("len(GetAll()) = %d, want Count() = %d", got, c.Count())
	}
}

func TestLFU_Sharded_Concurrent(t *testing.T) {
	if n := numShardsFor(largeCacheSize); n <= 1 {
		t.Fatalf("test setup: expected multiple shards for size %d, got %d", largeCacheSize, n)
	}

	c := NewLFU[int, int](largeCacheSize)
	var wg sync.WaitGroup

	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				k := n*500 + j
				c.Set(k, k)
				c.Get(k)
				if j%7 == 0 {
					c.Delete(k)
				}
			}
		}(i)
	}
	wg.Wait()

	if l := c.Len(); uint(l) > largeCacheSize {
		t.Errorf("Len() = %d, must not exceed configured size %d", l, largeCacheSize)
	}
	if count := c.Count(); count > c.Len() {
		t.Errorf("Count() = %d, must not exceed Len() = %d", count, c.Len())
	}
	if got := len(c.GetAll()); got != c.Count() {
		t.Errorf("len(GetAll()) = %d, want Count() = %d", got, c.Count())
	}
}

func TestMCache_Sharded_Concurrent(t *testing.T) {
	if n := numShardsFor(largeCacheSize); n <= 1 {
		t.Fatalf("test setup: expected multiple shards for size %d, got %d", largeCacheSize, n)
	}

	c := NewManual[int, int](largeCacheSize, 0)
	var wg sync.WaitGroup

	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				k := n*500 + j
				c.Set(k, k)
				c.Get(k)
				if j%7 == 0 {
					c.Delete(k)
				}
			}
		}(i)
	}
	wg.Wait()

	if l := c.Len(); uint(l) > largeCacheSize {
		t.Errorf("Len() = %d, must not exceed configured size %d", l, largeCacheSize)
	}
	if count := c.Count(); count > c.Len() {
		t.Errorf("Count() = %d, must not exceed Len() = %d", count, c.Len())
	}
	if got := len(c.GetAll()); got != c.Count() {
		t.Errorf("len(GetAll()) = %d, want Count() = %d", got, c.Count())
	}
}

// TestSharded_TransferAndCopy_CrossShardCounts exercises TransferTo/CopyTo
// between caches with different shard counts (large sharded source, small
// single-shard destination and vice versa) to make sure routing keys
// through the destination's own hashing works regardless of how the source
// was partitioned.
func TestSharded_TransferAndCopy_CrossShardCounts(t *testing.T) {
	big := NewLRU[int, int](largeCacheSize)
	small := NewLRU[int, int](10)

	for i := 0; i < 8; i++ {
		big.Set(i, i*10)
	}

	big.CopyTo(small)
	if got := small.Count(); got != 8 {
		t.Errorf("CopyTo big->small: Count() = %d, want 8", got)
	}
	if got := big.Count(); got != 8 {
		t.Errorf("CopyTo must not remove from source: Count() = %d, want 8", got)
	}

	big.TransferTo(small)
	if got := big.Count(); got != 0 {
		t.Errorf("TransferTo big->small: source Count() = %d, want 0", got)
	}
}
