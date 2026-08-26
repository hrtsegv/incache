// Package incache is a high-performance, thread-safe in-memory cache
// library for Go. It is designed to be embedded in monolith backend servers
// where a centralized cache like Redis is not needed.
//
// Three eviction policies are provided - LRUCache, LFUCache and MCache
// (manual/no eviction) - all implementing the shared Cache interface, so
// application code can depend on Cache and swap the underlying policy
// without other changes. All three are generic over comparable keys and
// any value type, support optional per-entry TTLs, and are safe for
// concurrent use; larger caches are internally sharded to reduce lock
// contention (see the LRUCache, LFUCache and MCache doc comments for the
// tradeoffs that come with sharding).
package incache
