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

// memoryCache returns CacheCoherent singleton instance.
func memoryCache() *CacheCoherent {
	once.Do(func() {
		store = newCacheCoherent()
	})

	return store
}

// New creates a new CacheCoherent instance.
func New() *CacheCoherent {
	return newCacheCoherent()
}
