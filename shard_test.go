package incache

import "testing"

func TestNumShardsFor_SmallSizesUseOneShard(t *testing.T) {
	// Below the sharding threshold, caches must use exactly one shard so
	// eviction order and locking behavior stay identical to the
	// pre-sharding implementation - this is what keeps all the small-cache
	// tests (LRU/LFU eviction order, MCache eviction, etc.) valid.
	for _, size := range []uint{0, 1, 2, 5, 10, 100, shardingThreshold - 1} {
		if n := numShardsFor(size); n != 1 {
			t.Errorf("numShardsFor(%d) = %d, want 1 (below sharding threshold %d)", size, n, shardingThreshold)
		}
	}
}

func TestNumShardsFor_LargeSizesShard(t *testing.T) {
	for _, size := range []uint{shardingThreshold, shardingThreshold + 1, 100_000, 10_000_000} {
		if n := numShardsFor(size); n <= 1 {
			t.Errorf("numShardsFor(%d) = %d, want > 1 (at/above sharding threshold %d)", size, n, shardingThreshold)
		}
	}
}

func TestNumShardsFor_NeverExceedsMaxShards(t *testing.T) {
	if n := numShardsFor(1 << 40); n > maxShards {
		t.Errorf("numShardsFor(2^40) = %d, want <= maxShards (%d)", n, maxShards)
	}
}

func TestShardSizes_SumsToTotal(t *testing.T) {
	for _, tc := range []struct {
		size uint
		n    int
	}{
		{0, 1}, {1, 1}, {10, 1}, {1024, 32}, {1025, 32}, {1030, 7}, {7, 16},
	} {
		sizes := shardSizes(tc.size, tc.n)
		if len(sizes) != tc.n {
			t.Fatalf("shardSizes(%d, %d): got %d entries, want %d", tc.size, tc.n, len(sizes), tc.n)
		}

		var sum uint
		for _, s := range sizes {
			sum += s
		}
		if sum != tc.size {
			t.Errorf("shardSizes(%d, %d): sizes sum to %d, want %d", tc.size, tc.n, sum, tc.size)
		}

		// No shard's size should differ from another by more than 1.
		min, max := sizes[0], sizes[0]
		for _, s := range sizes {
			if s < min {
				min = s
			}
			if s > max {
				max = s
			}
		}
		if max-min > 1 {
			t.Errorf("shardSizes(%d, %d): uneven distribution, min=%d max=%d", tc.size, tc.n, min, max)
		}
	}
}

func TestShardIndexFor_SingleShardAlwaysZero(t *testing.T) {
	for _, k := range []string{"a", "b", "some-longer-key", ""} {
		if idx := shardIndexFor(k, 1); idx != 0 {
			t.Errorf("shardIndexFor(%q, 1) = %d, want 0", k, idx)
		}
	}
}

func TestShardIndexFor_InRangeAndDistributed(t *testing.T) {
	const n = 8
	counts := make([]int, n)
	const samples = 100_000

	for i := 0; i < samples; i++ {
		idx := shardIndexFor(i, n)
		if idx < 0 || idx >= n {
			t.Fatalf("shardIndexFor(%d, %d) = %d, out of range", i, n, idx)
		}
		counts[idx]++
	}

	// Not a strict uniformity requirement, just a sanity check that the
	// hash isn't degenerate (e.g. everything landing in one shard).
	expected := samples / n
	for i, c := range counts {
		if c < expected/2 || c > expected*2 {
			t.Errorf("shard %d got %d of %d samples, expected roughly %d", i, c, samples, expected)
		}
	}
}
