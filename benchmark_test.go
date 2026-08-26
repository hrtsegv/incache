package incache

import (
	"strconv"
	"testing"
)

// LFU Benchmarks

func BenchmarkLFU_Set(b *testing.B) {
	cache := NewLFU[int, int](10000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Set(i%10000, i)
	}
}

func BenchmarkLFU_Get(b *testing.B) {
	cache := NewLFU[int, int](10000)
	for i := 0; i < 10000; i++ {
		cache.Set(i, i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Get(i % 10000)
	}
}

func BenchmarkLFU_SetGet(b *testing.B) {
	cache := NewLFU[int, int](10000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i%2 == 0 {
			cache.Set(i%10000, i)
		} else {
			cache.Get(i % 10000)
		}
	}
}

func BenchmarkLFU_Delete(b *testing.B) {
	cache := NewLFU[int, int](10000)
	for i := 0; i < 10000; i++ {
		cache.Set(i, i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Delete(i % 10000)
		cache.Set(i%10000, i) // Re-add to keep cache populated
	}
}

// LRU Benchmarks

func BenchmarkLRU_Set(b *testing.B) {
	cache := NewLRU[int, int](10000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Set(i%10000, i)
	}
}

func BenchmarkLRU_Get(b *testing.B) {
	cache := NewLRU[int, int](10000)
	for i := 0; i < 10000; i++ {
		cache.Set(i, i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Get(i % 10000)
	}
}

func BenchmarkLRU_SetGet(b *testing.B) {
	cache := NewLRU[int, int](10000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i%2 == 0 {
			cache.Set(i%10000, i)
		} else {
			cache.Get(i % 10000)
		}
	}
}

func BenchmarkLRU_Delete(b *testing.B) {
	cache := NewLRU[int, int](10000)
	for i := 0; i < 10000; i++ {
		cache.Set(i, i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Delete(i % 10000)
		cache.Set(i%10000, i) // Re-add to keep cache populated
	}
}

// MCache Benchmarks

func BenchmarkMCache_Set(b *testing.B) {
	cache := NewManual[int, int](10000, 0)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Set(i%10000, i)
	}
}

func BenchmarkMCache_Get(b *testing.B) {
	cache := NewManual[int, int](10000, 0)
	for i := 0; i < 10000; i++ {
		cache.Set(i, i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Get(i % 10000)
	}
}

func BenchmarkMCache_SetGet(b *testing.B) {
	cache := NewManual[int, int](10000, 0)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i%2 == 0 {
			cache.Set(i%10000, i)
		} else {
			cache.Get(i % 10000)
		}
	}
}

func BenchmarkMCache_Delete(b *testing.B) {
	cache := NewManual[int, int](10000, 0)
	for i := 0; i < 10000; i++ {
		cache.Set(i, i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Delete(i % 10000)
		cache.Set(i%10000, i) // Re-add to keep cache populated
	}
}

// String key benchmarks (more realistic)

func BenchmarkLFU_StringKey_Set(b *testing.B) {
	cache := NewLFU[string, string](10000)
	keys := make([]string, 10000)
	for i := 0; i < 10000; i++ {
		keys[i] = "key-" + strconv.Itoa(i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Set(keys[i%10000], keys[i%10000])
	}
}

func BenchmarkLFU_StringKey_Get(b *testing.B) {
	cache := NewLFU[string, string](10000)
	keys := make([]string, 10000)
	for i := 0; i < 10000; i++ {
		keys[i] = "key-" + strconv.Itoa(i)
		cache.Set(keys[i], keys[i])
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Get(keys[i%10000])
	}
}

func BenchmarkLRU_StringKey_Set(b *testing.B) {
	cache := NewLRU[string, string](10000)
	keys := make([]string, 10000)
	for i := 0; i < 10000; i++ {
		keys[i] = "key-" + strconv.Itoa(i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Set(keys[i%10000], keys[i%10000])
	}
}

func BenchmarkLRU_StringKey_Get(b *testing.B) {
	cache := NewLRU[string, string](10000)
	keys := make([]string, 10000)
	for i := 0; i < 10000; i++ {
		keys[i] = "key-" + strconv.Itoa(i)
		cache.Set(keys[i], keys[i])
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Get(keys[i%10000])
	}
}

// Parallel benchmarks

func BenchmarkLFU_Parallel_Set(b *testing.B) {
	cache := NewLFU[int, int](10000)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			cache.Set(i%10000, i)
			i++
		}
	})
}

func BenchmarkLFU_Parallel_Get(b *testing.B) {
	cache := NewLFU[int, int](10000)
	for i := 0; i < 10000; i++ {
		cache.Set(i, i)
	}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			cache.Get(i % 10000)
			i++
		}
	})
}

func BenchmarkLRU_Parallel_Set(b *testing.B) {
	cache := NewLRU[int, int](10000)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			cache.Set(i%10000, i)
			i++
		}
	})
}

func BenchmarkLRU_Parallel_Get(b *testing.B) {
	cache := NewLRU[int, int](10000)
	for i := 0; i < 10000; i++ {
		cache.Set(i, i)
	}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			cache.Get(i % 10000)
			i++
		}
	})
}

func BenchmarkMCache_Parallel_Set(b *testing.B) {
	cache := NewManual[int, int](10000, 0)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			cache.Set(i%10000, i)
			i++
		}
	})
}

func BenchmarkMCache_Parallel_Get(b *testing.B) {
	cache := NewManual[int, int](10000, 0)
	for i := 0; i < 10000; i++ {
		cache.Set(i, i)
	}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			cache.Get(i % 10000)
			i++
		}
	})
}

// Large parallel benchmarks - big enough (see shardingThreshold) to
// exercise the sharded path with many shards under real cross-goroutine
// contention, rather than the single-shard path the 10000-sized benchmarks
// above mostly stay within on a typical multi-core machine's shard count.

const benchLargeSize = 1_000_000

func BenchmarkLFU_Parallel_Large_Set(b *testing.B) {
	cache := NewLFU[int, int](benchLargeSize)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			cache.Set(i%benchLargeSize, i)
			i++
		}
	})
}

func BenchmarkLFU_Parallel_Large_Get(b *testing.B) {
	cache := NewLFU[int, int](benchLargeSize)
	for i := 0; i < benchLargeSize; i++ {
		cache.Set(i, i)
	}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			cache.Get(i % benchLargeSize)
			i++
		}
	})
}

func BenchmarkLRU_Parallel_Large_Set(b *testing.B) {
	cache := NewLRU[int, int](benchLargeSize)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			cache.Set(i%benchLargeSize, i)
			i++
		}
	})
}

func BenchmarkLRU_Parallel_Large_Get(b *testing.B) {
	cache := NewLRU[int, int](benchLargeSize)
	for i := 0; i < benchLargeSize; i++ {
		cache.Set(i, i)
	}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			cache.Get(i % benchLargeSize)
			i++
		}
	})
}

func BenchmarkMCache_Parallel_Large_Set(b *testing.B) {
	cache := NewManual[int, int](benchLargeSize, 0)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			cache.Set(i%benchLargeSize, i)
			i++
		}
	})
}

func BenchmarkMCache_Parallel_Large_Get(b *testing.B) {
	cache := NewManual[int, int](benchLargeSize, 0)
	for i := 0; i < benchLargeSize; i++ {
		cache.Set(i, i)
	}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			cache.Get(i % benchLargeSize)
			i++
		}
	})
}
