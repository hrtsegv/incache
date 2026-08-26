package incache

import "sync"

// call represents an in-flight or completed invocation of a GetOrSet
// compute function for one key.
type call[V any] struct {
	wg  sync.WaitGroup
	val V
	err error
}

// flightGroup coalesces concurrent calls for the same key into a single
// invocation of the caller-supplied function, so a cache miss on a hot key
// doesn't trigger a stampede of duplicate (often expensive) recomputation.
// This is the same pattern golang.org/x/sync/singleflight implements; it's
// hand-rolled here to keep this module dependency-free.
//
// A flightGroup only guards the bookkeeping map, never the function call
// itself: the map lock is released before fn runs and re-acquired only to
// remove the completed entry, so a slow fn for one key never blocks callers
// working on other keys.
type flightGroup[K comparable, V any] struct {
	mu sync.Mutex
	m  map[K]*call[V]
}

// do runs fn for k, or waits for and returns the result of an already
// in-flight call for the same key.
func (g *flightGroup[K, V]) do(k K, fn func() (V, error)) (V, error) {
	g.mu.Lock()
	if c, ok := g.m[k]; ok {
		g.mu.Unlock()
		c.wg.Wait()
		return c.val, c.err
	}

	c := new(call[V])
	c.wg.Add(1)
	if g.m == nil {
		g.m = make(map[K]*call[V])
	}
	g.m[k] = c
	g.mu.Unlock()

	c.val, c.err = fn()
	c.wg.Done()

	g.mu.Lock()
	delete(g.m, k)
	g.mu.Unlock()

	return c.val, c.err
}
