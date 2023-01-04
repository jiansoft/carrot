package carrot

import (
	"sync"

	"github.com/jiansoft/robin"
)

type MemoryCache interface {
	// Keep Create or overwrite an entry in the cache.
	Keep(key any, value any, option CacheEntryOptions)
	// Read Gets the item associated with this key if present.
	Read(key any) (value any, ok bool)
	//Have Returns true if this key present.
	Have(key any) (ok bool)
	// Forget Removes the object associated with the given key.
	Forget(key any)
	// Reset Removes all object from the cache
	Reset()
}

var (
	store   *CacheCoherent
	once    sync.Once
	Default = memoryCache()
)

// memoryCache returns CacheCoherent singleton instance
func memoryCache() MemoryCache {
	once.Do(func() {
		store = newCacheCoherent()
	})

	return store
}

func New() *CacheCoherent {
	return newCacheCoherent()
}

func erase(m *sync.Map) {
	m.Range(func(key, value any) bool {
		m.Delete(key)
		return true
	})
}

type parallelCount struct {
	source *sync.Map
	count  int
}

func parallelCountMap(targets ...*parallelCount) {
	wg := sync.WaitGroup{}
	for _, target := range targets {
		wg.Add(1)
		robin.RightNow().Do(func(item *parallelCount, wg *sync.WaitGroup) {
			item.source.Range(func(k, v any) bool {
				item.count++
				return true
			})
			wg.Done()
		}, target, &wg)
	}
	wg.Wait()
}
