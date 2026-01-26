package incache

import "time"

// Cache is the common interface implemented by all cache types.
// It provides a unified API for cache operations regardless of the underlying eviction policy.
type Cache[K comparable, V any] interface {
	// Get retrieves the value associated with the given key from the cache.
	// If the key is not found or has expired, it returns (zero value of V, false).
	// Otherwise, it returns (value, true).
	Get(k K) (V, bool)

	// Set adds or updates a key-value pair in the cache without setting an expiration time.
	Set(k K, v V)

	// SetWithTimeout adds or updates a key-value pair in the cache with an expiration time.
	// If the timeout duration is zero or negative, the behavior depends on the implementation.
	SetWithTimeout(k K, v V, timeout time.Duration)

	// Delete removes the key-value pair associated with the given key from the cache.
	Delete(k K)

	// NotFoundSet adds a key-value pair to the cache only if the key does not exist or is expired.
	// It returns true if the key was added to the cache, otherwise false.
	NotFoundSet(k K, v V) bool

	// NotFoundSetWithTimeout adds a key-value pair with an expiration time only if the key does not exist or is expired.
	// It returns true if the key was added to the cache, otherwise false.
	NotFoundSetWithTimeout(k K, v V, timeout time.Duration) bool

	// GetAll retrieves all non-expired key-value pairs from the cache.
	GetAll() map[K]V

	// Keys returns a slice of all non-expired keys currently stored in the cache.
	Keys() []K

	// Purge removes all key-value pairs from the cache.
	// The cache can still be used after calling Purge.
	Purge()

	// Count returns the number of non-expired key-value pairs currently stored in the cache.
	Count() int

	// Len returns the total number of elements in the cache (including expired ones).
	Len() int
}

// Compile-time checks to ensure all cache types implement the Cache interface
var (
	_ Cache[string, any] = (*LFUCache[string, any])(nil)
	_ Cache[string, any] = (*LRUCache[string, any])(nil)
	_ Cache[string, any] = (*MCache[string, any])(nil)
)
