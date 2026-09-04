package incache

import (
	"testing"
)

// These tests reach into shard internals to pin down invariants the public
// API cannot observe directly - the ones that, when they broke, showed up
// only as slow memory growth or as a cache quietly holding more than its
// size.

// TestLFU_FrequencyBucketsDoNotLeak guards a regression where a frequency
// bucket was only dropped when it emptied at the current minimum frequency.
// A hot key climbing past a colder one left an empty bucket behind on every
// single access, so the bucket map grew with the number of Gets rather than
// with the number of items - unbounded growth on a cache holding two keys.
func TestLFU_FrequencyBucketsDoNotLeak(t *testing.T) {
	c := NewLFU[string, int](4)

	// "cold" stays at frequency 1 and so pins minFreq there, which is what
	// stops "hot" from ever emptying a bucket at the minimum.
	c.Set("cold", 0)
	c.Set("hot", 1)

	const accesses = 10_000
	for i := 0; i < accesses; i++ {
		c.Get("hot")
	}

	s := c.shards[0]
	s.mu.Lock()
	buckets := len(s.freqLists)
	s.mu.Unlock()

	// Two live items can occupy at most two distinct frequencies.
	if buckets > 2 {
		t.Errorf("freqLists holds %d buckets after %d accesses to a 2-item cache, want <= 2", buckets, accesses)
	}
}

// TestLFU_StaysWithinCapacity churns a cache through sets, gets and deletes
// so that minFreq repeatedly goes stale, and checks eviction still keeps the
// cache at its configured size. Eviction used to give up when minFreq
// pointed at a bucket that no longer had anything in it.
func TestLFU_StaysWithinCapacity(t *testing.T) {
	const size = 50
	c := NewLFU[int, int](size)

	for i := 0; i < 20_000; i++ {
		c.Set(i%200, i)
		c.Get(i % 7) // keep a few keys hot so frequencies spread out
		if i%5 == 0 {
			c.Delete(i % 100)
		}
		if l := c.Len(); l > size {
			t.Fatalf("Len() = %d after %d operations, want <= %d", l, i+1, size)
		}
	}
}
