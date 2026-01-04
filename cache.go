package incache

import "time"

// Cache is the interface that wraps the basic cache operations.
type Cache[K comparable, V any] interface {
	// Get retrieves the value associated with the given key from the cache.
	Get(k K) (v V, ok bool)
	// Set adds or updates a key-value pair in the cache.
	Set(k K, v V)
	// SetWithTimeout adds or updates a key-value pair in the cache with an expiration time.
	SetWithTimeout(k K, v V, timeout time.Duration)
	// NotFoundSet adds a key-value pair to the cache if the key does not already exist.
	NotFoundSet(k K, v V) bool
	// NotFoundSetWithTimeout adds a key-value pair to the cache with an expiration time if the key does not already exist.
	NotFoundSetWithTimeout(k K, v V, timeout time.Duration) bool
	// Delete removes the key-value pair associated with the given key from the cache.
	Delete(k K)
	// TransferTo transfers all non-expired key-value pairs from the source cache to the destination cache.
	TransferTo(dst Cache[K, V])
	// CopyTo copies all non-expired key-value pairs from the source cache to the destination cache.
	CopyTo(dst Cache[K, V])
	// Keys returns a slice of all non-expired keys in the cache.
	Keys() []K
	// Purge removes all key-value pairs from the cache.
	Purge()
	// Count returns the number of non-expired key-value pairs in the cache.
	Count() int
	// Len returns the total number of key-value pairs in the cache, including expired ones.
	Len() int
	// GetAll returns a map containing all non-expired key-value pairs in the cache.
	GetAll() map[K]V
	// Close stops the background expiration goroutine if it's running.
	Close()
}
