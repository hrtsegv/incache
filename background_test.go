package incache

import (
	"testing"
	"time"
)

func TestBackgroundExpiration(t *testing.T) {
	tests := []struct {
		name string
		new  func(uint, ...Option) Cache[string, int]
	}{
		{"MCache", func(s uint, opts ...Option) Cache[string, int] { return NewManual[string, int](s, opts...) }},
		{"LRU", func(s uint, opts ...Option) Cache[string, int] { return NewLRU[string, int](s, opts...) }},
		{"LFU", func(s uint, opts ...Option) Cache[string, int] { return NewLFU[string, int](s, opts...) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := tt.new(10, WithCleanupInterval(50*time.Millisecond))
			defer c.Close()

			c.SetWithTimeout("k1", 1, 10*time.Millisecond)
			c.SetWithTimeout("k2", 2, 200*time.Millisecond)

			if v, ok := c.Get("k1"); !ok || v != 1 {
				t.Fatalf("expected k1 to be present")
			}

			// Wait for background goroutine to clean up k1
			time.Sleep(150 * time.Millisecond)

			if _, ok := c.Get("k1"); ok {
				t.Errorf("expected k1 to be expired and removed by background goroutine")
			}

			if v, ok := c.Get("k2"); !ok || v != 2 {
				t.Errorf("expected k2 to be still present")
			}
		})
	}
}
