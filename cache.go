package carrot

import (
	"sync"

	"github.com/jiansoft/robin"
)

var (
	store   *CacheCoherent
	once    sync.Once
	Default = memoryCache()
)

// memoryCache returns CacheCoherent singleton instance
func memoryCache() *CacheCoherent {
	once.Do(func() {
		store = newCacheCoherent()
	})

	return store
}

func New() *CacheCoherent {
	return newCacheCoherent()
}

func erase(targets ...*sync.Map) {
	for _, target := range targets {
		target.Range(func(k, v any) bool {
			target.Delete(k)
			return true
		})
	}

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
