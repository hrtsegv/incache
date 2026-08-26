package incache

import "testing"

// fuzzCacheSize is comfortably above shardingThreshold so these fuzz tests
// exercise the sharded/hashed key-routing path (shardIndexFor via
// maphash.Comparable) rather than the single-shard fast path, since that's
// the newer, riskier code these arbitrary-input keys are meant to stress.
const fuzzCacheSize = shardingThreshold * 2

func FuzzLRU_SetGetRoundTrip(f *testing.F) {
	f.Add("key", "value")
	f.Add("", "")
	f.Add("k", "long value with spaces and unicode: héllo 世界")

	f.Fuzz(func(t *testing.T, key, value string) {
		// A fresh cache per call, since go test -fuzz runs the target
		// concurrently across workers; sharing one cache risks two workers
		// racing a Set/Delete of the same fuzzer-generated key against
		// each other, which would be a false failure, not a real bug.
		c := NewLRU[string, string](fuzzCacheSize)

		c.Set(key, value)

		got, ok := c.Get(key)
		if !ok {
			t.Fatalf("Get(%q): not found immediately after Set", key)
		}
		if got != value {
			t.Fatalf("Get(%q) = %q, want %q", key, got, value)
		}

		c.Delete(key)
		if _, ok := c.Get(key); ok {
			t.Fatalf("Get(%q): still found after Delete", key)
		}
	})
}

func FuzzLFU_SetGetRoundTrip(f *testing.F) {
	f.Add("key", "value")
	f.Add("", "")
	f.Add("k", "long value with spaces and unicode: héllo 世界")

	f.Fuzz(func(t *testing.T, key, value string) {
		// See FuzzLRU_SetGetRoundTrip: a fresh cache per call avoids a
		// cross-worker race on the same fuzzer-generated key.
		c := NewLFU[string, string](fuzzCacheSize)

		c.Set(key, value)

		got, ok := c.Get(key)
		if !ok {
			t.Fatalf("Get(%q): not found immediately after Set", key)
		}
		if got != value {
			t.Fatalf("Get(%q) = %q, want %q", key, got, value)
		}

		c.Delete(key)
		if _, ok := c.Get(key); ok {
			t.Fatalf("Get(%q): still found after Delete", key)
		}
	})
}
