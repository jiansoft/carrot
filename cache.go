package carrot

import (
	"sync"
)

var (
	store *CacheCoherent
	once  sync.Once
	// Default is the global singleton cache instance.
	// Use this for simple use cases where a single shared cache is sufficient.
	Default = memoryCache()
)

// MemoryCache returns CacheCoherent singleton instance.
func memoryCache() *CacheCoherent {
	once.Do(func() {
		store = newCacheCoherent()
	})

	return store
}

// New creates a new TypedCache instance with the specified key and value types.
// This is the recommended way to create a cache with type safety.
//
// Example:
//
//	cache := carrot.New[string, int]()
//	cache.Expire("count", 42, time.Hour)
//	val, ok := cache.Read("count") // val is int, no type assertion needed
func New[K comparable, V any]() *TypedCache[K, V] {
	return &TypedCache[K, V]{
		cache: newCacheCoherent(),
	}
}

// From creates a TypedCache that wraps an existing CacheCoherent.
// This allows sharing the underlying cache between typed and untyped access.
//
// Example:
//
//	base := carrot.NewCache()
//	intCache := carrot.From[string, int](base)
//	strCache := carrot.From[string, string](base)
//	// Both share the same underlying storage
func From[K comparable, V any](cache *CacheCoherent) *TypedCache[K, V] {
	return &TypedCache[K, V]{
		cache: cache,
	}
}

// NewCache creates a new untyped CacheCoherent instance.
// Use New[K, V]() for type-safe operations.
// Use this when you need to store mixed types or for backward compatibility.
func NewCache() *CacheCoherent {
	return newCacheCoherent()
}
