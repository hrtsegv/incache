package incache

import (
	"sync"
	"testing"
	"time"
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
	buckets := len(s.freqHeads)
	s.mu.Unlock()

	// Two live items can occupy at most two distinct frequencies.
	if buckets > 2 {
		t.Errorf("freqHeads holds %d buckets after %d accesses to a 2-item cache, want <= 2", buckets, accesses)
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

// TestLFU_BucketsWellFormed checks that every entry is reachable from
// exactly one frequency bucket, that each bucket is a properly terminated
// circular list, and that entries sit in the bucket matching their own
// frequency.
func TestLFU_BucketsWellFormed(t *testing.T) {
	c := NewLFU[int, int](64)
	for i := 0; i < 5000; i++ {
		c.Set(i%80, i)
		c.Get(i % 30)
		if i%11 == 0 {
			c.Delete(i % 80)
		}
	}

	s := c.shards[0]
	s.mu.Lock()
	defer s.mu.Unlock()

	seen := make(map[int]bool)
	for freq, head := range s.freqHeads {
		if head == nil {
			t.Fatalf("bucket %d is present in freqHeads but has a nil head", freq)
		}
		for e, n := head, 0; ; e, n = e.next, n+1 {
			if n > len(s.items) {
				t.Fatalf("bucket %d does not terminate: walked %d entries with only %d items in the shard", freq, n, len(s.items))
			}
			if e.freq != freq {
				t.Errorf("entry %v has freq %d but sits in bucket %d", e.key, e.freq, freq)
			}
			if e.next.prev != e || e.prev.next != e {
				t.Errorf("entry %v has broken prev/next links in bucket %d", e.key, freq)
			}
			if seen[e.key] {
				t.Fatalf("entry %v is linked into the bucket lists more than once", e.key)
			}
			seen[e.key] = true

			if e.next == head {
				break
			}
		}
	}

	if len(seen) != len(s.items) {
		t.Errorf("%d entries reachable from freqHeads, but items map holds %d", len(seen), len(s.items))
	}
}

// TestLRU_RecencyListWellFormed is the equivalent check for the LRU shard's
// circular recency list.
func TestLRU_RecencyListWellFormed(t *testing.T) {
	c := NewLRU[int, int](64)
	for i := 0; i < 5000; i++ {
		c.Set(i%80, i)
		c.Get(i % 30)
		if i%11 == 0 {
			c.Delete(i % 80)
		}
	}

	s := c.shards[0]
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.m) == 0 {
		t.Fatal("test setup: expected the shard to hold entries")
	}
	if s.head == nil {
		t.Fatal("head is nil but the shard holds entries")
	}

	n := 0
	for e := s.head; ; e = e.next {
		n++
		if n > len(s.m) {
			t.Fatalf("recency list does not terminate: walked %d entries with only %d in the map", n, len(s.m))
		}
		if e.next.prev != e || e.prev.next != e {
			t.Fatalf("entry %v has broken prev/next links", e.key)
		}
		if s.m[e.key] != e {
			t.Fatalf("entry %v is linked into the recency list but is not the map's entry for that key", e.key)
		}
		if e.next == s.head {
			break
		}
	}

	if n != len(s.m) {
		t.Errorf("%d entries in the recency list, but map holds %d", n, len(s.m))
	}
}

// TestMCache_WritesDuringCloseAreNoOps covers the documented promise that
// calls on a closed MCache are no-ops rather than panics. Close nils each
// shard's map, so a writer that passed the closed check just before Close
// ran would otherwise assign into a nil map. Most useful under -race.
func TestMCache_WritesDuringCloseAreNoOps(t *testing.T) {
	for attempt := 0; attempt < 200; attempt++ {
		c := NewManual[int, int](largeCacheSize, time.Minute)

		var wg sync.WaitGroup
		for w := 0; w < 4; w++ {
			wg.Add(1)
			go func(w int) {
				defer wg.Done()
				for i := 0; i < 200; i++ {
					c.Set(w*200+i, i)
					c.SetWithTimeout(w*200+i, i, time.Minute)
					c.NotFoundSet(w*200+i, i)
					c.Purge()
				}
			}(w)
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Close()
		}()

		wg.Wait()
	}
}

// TestMCache_EvictionPrefersExpired checks that a full shard sheds expired
// entries ahead of live ones. Eviction samples a bounded number of entries
// rather than scanning the whole shard, so this is probabilistic: it evicts
// a live entry only when every entry in the sample is live. The insert
// count is kept well below the number of expired entries so the expired
// pool cannot drain and turn later evictions into coin flips.
func TestMCache_EvictionPrefersExpired(t *testing.T) {
	const size = 100
	const live = 10
	const inserts = 25

	c := NewManual[int, int](size, 0)

	for i := 0; i < live; i++ {
		c.Set(i, i) // no expiry
	}
	for i := live; i < size; i++ {
		c.SetWithTimeout(i, i, 5*time.Millisecond)
	}

	time.Sleep(20 * time.Millisecond)

	// The cache is full, so every one of these inserts evicts something.
	for i := 0; i < inserts; i++ {
		c.Set(1000+i, i)
	}

	survivors := 0
	for i := 0; i < live; i++ {
		if _, ok := c.Get(i); ok {
			survivors++
		}
	}

	// Expired entries outnumber live ones throughout the run, so losing
	// even one live entry is unlikely; allow a single loss so the test
	// cannot flake, but catch eviction that ignores expiry altogether.
	if survivors < live-1 {
		t.Errorf("%d of %d live entries survived eviction, want at least %d", survivors, live, live-1)
	}
}

// TestNumShardsFor_AlwaysPowerOfTwo pins the invariant shardIndexFor relies
// on: it reduces a hash with a mask, which is only equivalent to a modulo
// when the shard count is a power of two.
func TestNumShardsFor_AlwaysPowerOfTwo(t *testing.T) {
	for _, size := range []uint{
		0, 1, 2, 7, 100, shardingThreshold - 1, shardingThreshold,
		shardingThreshold + 1, 5000, 100_000, 10_000_000, 1 << 40,
	} {
		n := numShardsFor(size)
		if n < 1 {
			t.Errorf("numShardsFor(%d) = %d, want >= 1", size, n)
			continue
		}
		if n&(n-1) != 0 {
			t.Errorf("numShardsFor(%d) = %d, want a power of two", size, n)
		}
	}
}

// TestShardIndexFor_CoversEveryShard makes sure the mask reduction actually
// reaches every shard rather than folding keys into a subset of them.
func TestShardIndexFor_CoversEveryShard(t *testing.T) {
	const n = 16
	hit := make([]bool, n)
	for i := 0; i < 10_000; i++ {
		idx := shardIndexFor(i, n)
		if idx < 0 || idx >= n {
			t.Fatalf("shardIndexFor(%d, %d) = %d, out of range", i, n, idx)
		}
		hit[idx] = true
	}
	for i, h := range hit {
		if !h {
			t.Errorf("shard %d never selected across 10000 keys", i)
		}
	}
}
