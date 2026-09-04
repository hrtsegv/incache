package incache

import (
	"hash/maphash"
	"runtime"
)

// maxShards caps how many shards a cache is split into, regardless of size
// or GOMAXPROCS, to keep per-shard capacity meaningful and per-cache memory
// overhead (one mutex, map and eviction structure per shard) bounded.
const maxShards = 256

// shardingThreshold is the minimum cache size before sharding kicks in.
// Below it, a cache uses a single shard, which is exactly the pre-sharding
// behavior: one lock, one eviction list, strict global LRU/LFU order. This
// keeps small caches (the common case, and what most tests exercise) free
// of both hashing overhead and the "eviction order is now per-shard"
// tradeoff that only pays off at scale.
const shardingThreshold = 1024

// cacheLineSize is the cache line width on the architectures this library
// targets (64 bytes on amd64 and arm64). It only affects padding, so being
// wrong on an exotic architecture costs a little memory, never correctness.
const cacheLineSize = 64

// cacheLinePad is embedded at the end of every shard struct so that two
// shards allocated back to back can never share a cache line. Without it
// the hot fields of several shards (each only a few words wide) land in one
// line, and a write to one shard's mutex invalidates that line for every
// core working on the others - false sharing that shows up precisely in the
// concurrent workloads sharding exists to speed up.
type cacheLinePad struct {
	_ [cacheLineSize]byte
}

// numShardsFor picks how many shards a cache of the given size should use.
// It scales with GOMAXPROCS so throughput headroom tracks the machine the
// cache runs on, while never handing out a shard count that would leave
// shards under-filled relative to size.
//
// The result is always a power of two, which is what lets shardIndexFor
// reduce a hash with a mask instead of a division.
func numShardsFor(size uint) int {
	if size < shardingThreshold {
		return 1
	}

	target := runtime.GOMAXPROCS(0) * 4
	shards := 1
	for shards < target && shards < maxShards && uint(shards*2) <= size {
		shards <<= 1
	}
	return shards
}

// shardSizes splits size as evenly as possible across n shards. The
// remainder is distributed across the first shards so the returned sizes
// always sum to exactly size.
func shardSizes(size uint, n int) []uint {
	sizes := make([]uint, n)
	base := size / uint(n)
	rem := size % uint(n)
	for i := range sizes {
		sizes[i] = base
		if uint(i) < rem {
			sizes[i]++
		}
	}
	return sizes
}

// shardHashSeed is process-lifetime and shared across all cache instances:
// it only needs to distribute keys evenly across a given cache's shards, not
// to be unpredictable, so there's no benefit to a per-cache seed.
var shardHashSeed = maphash.MakeSeed()

// shardIndexFor returns which of n shards k belongs to. Comparable is a
// stdlib hash over arbitrary comparable types (Go 1.24+), which is what
// lets caches shard on a generic key type without requiring callers to
// supply their own hash function.
//
// n must be a power of two, as numShardsFor guarantees; the reduction is a
// mask rather than a modulo because a division by a runtime value costs
// tens of cycles on every single cache operation.
func shardIndexFor[K comparable](k K, n int) int {
	if n == 1 {
		return 0
	}
	return int(maphash.Comparable(shardHashSeed, k) & uint64(n-1))
}
