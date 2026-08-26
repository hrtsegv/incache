package incache

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLRU_GetOrSet_ComputesOnMiss(t *testing.T) {
	c := NewLRU[string, string](10)

	v, err := c.GetOrSet("key1", func() (string, error) {
		return "computed", nil
	})
	if err != nil || v != "computed" {
		t.Fatalf("GetOrSet: got (%q, %v), want (computed, nil)", v, err)
	}

	if got, ok := c.Get("key1"); !ok || got != "computed" {
		t.Errorf("expected GetOrSet to have stored the computed value, got (%q, %v)", got, ok)
	}
}

func TestLRU_GetOrSet_HitSkipsCompute(t *testing.T) {
	c := NewLRU[string, string](10)
	c.Set("key1", "already-there")

	called := false
	v, err := c.GetOrSet("key1", func() (string, error) {
		called = true
		return "should-not-be-used", nil
	})
	if err != nil || v != "already-there" {
		t.Fatalf("GetOrSet: got (%q, %v), want (already-there, nil)", v, err)
	}
	if called {
		t.Errorf("expected fn not to be called on a cache hit")
	}
}

func TestLRU_GetOrSet_CoalescesConcurrentMisses(t *testing.T) {
	c := NewLRU[string, int](10)

	var calls atomic.Int32
	release := make(chan struct{})

	const n = 50
	results := make([]int, n)
	errs := make([]error, n)
	var wg sync.WaitGroup

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			v, err := c.GetOrSet("shared-key", func() (int, error) {
				calls.Add(1)
				<-release
				return 42, nil
			})
			results[i] = v
			errs[i] = err
		}(i)
	}

	// Give every goroutine a chance to reach GetOrSet before releasing fn,
	// so they all coalesce onto the same in-flight call rather than racing
	// each other into flightGroup.do one at a time.
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Errorf("fn called %d times, want exactly 1", got)
	}
	for i, v := range results {
		if errs[i] != nil || v != 42 {
			t.Errorf("goroutine %d: got (%d, %v), want (42, nil)", i, v, errs[i])
		}
	}
}

func TestLRU_GetOrSet_ErrorNotCached(t *testing.T) {
	c := NewLRU[string, string](10)
	wantErr := errors.New("compute failed")

	v, err := c.GetOrSet("key1", func() (string, error) {
		return "", wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("GetOrSet: got err %v, want %v", err, wantErr)
	}
	if v != "" {
		t.Errorf("GetOrSet: got value %q on error, want zero value", v)
	}

	if _, ok := c.Get("key1"); ok {
		t.Errorf("expected nothing to be cached after an error from fn")
	}

	// A subsequent call should retry fn rather than replaying the error.
	v, err = c.GetOrSet("key1", func() (string, error) {
		return "recovered", nil
	})
	if err != nil || v != "recovered" {
		t.Fatalf("GetOrSet retry: got (%q, %v), want (recovered, nil)", v, err)
	}
}

func TestLFU_GetOrSet_ComputesOnMiss(t *testing.T) {
	c := NewLFU[string, string](10)

	v, err := c.GetOrSet("key1", func() (string, error) {
		return "computed", nil
	})
	if err != nil || v != "computed" {
		t.Fatalf("GetOrSet: got (%q, %v), want (computed, nil)", v, err)
	}

	if got, ok := c.Get("key1"); !ok || got != "computed" {
		t.Errorf("expected GetOrSet to have stored the computed value, got (%q, %v)", got, ok)
	}
}

func TestLFU_GetOrSet_CoalescesConcurrentMisses(t *testing.T) {
	c := NewLFU[string, int](10)

	var calls atomic.Int32
	release := make(chan struct{})

	const n = 50
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := c.GetOrSet("shared-key", func() (int, error) {
				calls.Add(1)
				<-release
				return 42, nil
			})
			if err != nil || v != 42 {
				t.Errorf("got (%d, %v), want (42, nil)", v, err)
			}
		}()
	}

	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Errorf("fn called %d times, want exactly 1", got)
	}
}

func TestLFU_GetOrSet_ErrorNotCached(t *testing.T) {
	c := NewLFU[string, string](10)
	wantErr := errors.New("compute failed")

	if _, err := c.GetOrSet("key1", func() (string, error) {
		return "", wantErr
	}); !errors.Is(err, wantErr) {
		t.Fatalf("GetOrSet: got err %v, want %v", err, wantErr)
	}

	if _, ok := c.Get("key1"); ok {
		t.Errorf("expected nothing to be cached after an error from fn")
	}
}

func TestMCache_GetOrSet_ComputesOnMiss(t *testing.T) {
	c := NewManual[string, string](10, 0)

	v, err := c.GetOrSet("key1", func() (string, error) {
		return "computed", nil
	})
	if err != nil || v != "computed" {
		t.Fatalf("GetOrSet: got (%q, %v), want (computed, nil)", v, err)
	}

	if got, ok := c.Get("key1"); !ok || got != "computed" {
		t.Errorf("expected GetOrSet to have stored the computed value, got (%q, %v)", got, ok)
	}
}

func TestMCache_GetOrSet_CoalescesConcurrentMisses(t *testing.T) {
	c := NewManual[string, int](10, 0)

	var calls atomic.Int32
	release := make(chan struct{})

	const n = 50
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := c.GetOrSet("shared-key", func() (int, error) {
				calls.Add(1)
				<-release
				return 42, nil
			})
			if err != nil || v != 42 {
				t.Errorf("got (%d, %v), want (42, nil)", v, err)
			}
		}()
	}

	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Errorf("fn called %d times, want exactly 1", got)
	}
}

func TestMCache_GetOrSet_ErrorNotCached(t *testing.T) {
	c := NewManual[string, string](10, 0)
	wantErr := errors.New("compute failed")

	if _, err := c.GetOrSet("key1", func() (string, error) {
		return "", wantErr
	}); !errors.Is(err, wantErr) {
		t.Fatalf("GetOrSet: got err %v, want %v", err, wantErr)
	}

	if _, ok := c.Get("key1"); ok {
		t.Errorf("expected nothing to be cached after an error from fn")
	}
}

func TestLRU_GetOrSetWithTimeout_ExpiredKeyRecomputes(t *testing.T) {
	c := NewLRU[string, int](10)

	calls := 0
	compute := func() (int, error) {
		calls++
		return calls, nil
	}

	v, err := c.GetOrSetWithTimeout("key1", compute, time.Millisecond)
	if err != nil || v != 1 {
		t.Fatalf("first GetOrSetWithTimeout: got (%d, %v), want (1, nil)", v, err)
	}

	time.Sleep(5 * time.Millisecond)

	v, err = c.GetOrSetWithTimeout("key1", compute, time.Millisecond)
	if err != nil || v != 2 {
		t.Fatalf("GetOrSetWithTimeout after expiry: got (%d, %v), want (2, nil)", v, err)
	}
	if calls != 2 {
		t.Errorf("expected fn to be called twice (miss, then miss again after expiry), got %d", calls)
	}
}
