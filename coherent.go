package carrot

import (
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jiansoft/robin"
)

var queueCapacity = 2048

type (
	// CacheCoherent Wrapper for the memory cache entries collection.
	CacheCoherent struct {
		// excisionNormalC chan any
		pq *priorityQueue
		// save cache item.
		usageNormal  sync.Map
		usageSliding sync.Map
		// prevent pq from data race
		muForPriorityQueue sync.Mutex
		stats              CacheStatistics
		onScanForExpired   atomic.Bool
		// how often to expiration scan all the cache
		expirationScanFrequency int64
		lastExpirationScan      int64
	}
)

func newCacheCoherent(capacity ...int) *CacheCoherent {
	if len(capacity) > 0 {
		queueCapacity = capacity[0]
	}

	coherent := &CacheCoherent{
		pq:    newPriorityQueue(queueCapacity),
		stats: CacheStatistics{},
		//excisionNormalC:         make(chan any, queueCapacity),
		lastExpirationScan:      time.Now().UTC().UnixNano(),
		expirationScanFrequency: int64(time.Minute),
	}

	return coherent
}

// SetScanFrequency sets a new frequency value
func (cc *CacheCoherent) SetScanFrequency(frequency time.Duration) {
	cc.expirationScanFrequency = int64(frequency)
}

// KeepForever never expiration
func (cc *CacheCoherent) KeepForever(key, val any) {
	cc.KeepForeverButInactive(key, val, time.Duration(0))
}

// KeepForeverButInactive never expiration but it can't be inactive for more than a period of time
func (cc *CacheCoherent) KeepForeverButInactive(key, val any, inactive time.Duration) {
	cc.KeepDelayOrInactive(key, val, -time.Duration(1), inactive)
}

// KeepUntil until when does it expire e.g. 2023-01-01 12:31:59.999
func (cc *CacheCoherent) KeepUntil(key, val any, until time.Time) {
	cc.KeepUntilOrInactive(key, val, until, time.Duration(0))
}

// KeepUntilOrInactive until a certain time does it expire, or it can't be inactive for more than a period of time e.g. 2023-01-01 12:31:59.999
func (cc *CacheCoherent) KeepUntilOrInactive(key, val any, until time.Time, inactive time.Duration) {
	var (
		untilUtc = until.UTC()
		nowUtc   = time.Now().UTC()
		ttl      = nowUtc.Sub(untilUtc)
	)

	cc.KeepDelayOrInactive(key, val, ttl, inactive)
}

// KeepDelay expires after a period of time. e.g. time.Hour  it's expired after one hour from now
func (cc *CacheCoherent) KeepDelay(key, val any, ttl time.Duration) {
	cc.KeepDelayOrInactive(key, val, ttl, time.Duration(0))
}

// KeepDelayOrInactive expires after a period of time, or it can't be inactive for more than a period of time
func (cc *CacheCoherent) KeepDelayOrInactive(key, val any, ttl, inactive time.Duration) {
	cc.Keep(key, val, CacheEntryOptions{TimeToLive: ttl, SlidingExpiration: inactive})
}

// Keep inserts an item into the memory
func (cc *CacheCoherent) Keep(key any, val any, option CacheEntryOptions) {
	if option.TimeToLive == 0 || option.SlidingExpiration < 0 {
		return
	}

	var (
		// default is never expiration
		utcAbsExp = int64(-1)
		// default is the highest
		priority = int64(math.MaxInt64)
		nowUtc   = time.Now().UTC().UnixNano()
		ttl      = option.TimeToLive.Nanoseconds()
	)

	if ttl > utcAbsExp {
		utcAbsExp = nowUtc + ttl
		priority = utcAbsExp
	}

	newEntry := &cacheEntry{
		key:                key,
		value:              val,
		priority:           priority,
		created:            nowUtc,
		lastAccessed:       nowUtc,
		absoluteExpiration: utcAbsExp,
	}

	// inactive的時效只能為正數
	if option.SlidingExpiration > 0 {
		newEntry.slidingExpiration = int64(option.SlidingExpiration)
	}

	cc.forget(key, nowUtc)

	if !newEntry.checkExpired(nowUtc) {
		if newEntry.isSlidingTypeCache() {
			// 如果有設定inactive時效時要改放在 usageSliding ，降低 map range 時鎖定的時間
			cc.usageSliding.Store(key, newEntry)
		} else {
			cc.usageNormal.Store(key, newEntry)
			cc.muForPriorityQueue.Lock()
			cc.pq.enqueue(newEntry)
			cc.muForPriorityQueue.Unlock()
		}
	}

	cc.scanForExpiredItemsIfNeeded(nowUtc)
}

// Read returns the value if the key exists in the cache
func (cc *CacheCoherent) Read(key any) (any, bool) {
	nowUtc := time.Now().UTC().UnixNano()
	if ce, exist := cc.loadCacheEntryFromUsage(key); exist {
		if !ce.checkExpired(nowUtc) {
			atomic.AddInt64(&cc.stats.totalHits, 1)
			ce.lastAccessed = nowUtc
			cc.scanForExpiredItemsIfNeeded(nowUtc)
			return ce.value, true
		}
	}

	atomic.AddInt64(&cc.stats.totalMisses, 1)

	cc.scanForExpiredItemsIfNeeded(nowUtc)

	return nil, false
}

// Have returns true if the memory has the item, and it's not expired.
func (cc *CacheCoherent) Have(key any) bool {
	_, exist := cc.Read(key)
	return exist
}

// Forget removes an item from the memory
func (cc *CacheCoherent) Forget(key any) {
	cc.forget(key, time.Now().UTC().UnixNano())
}

func (cc *CacheCoherent) forget(key any, nowUtc int64) {
	if ce, exist := cc.loadCacheEntryFromUsage(key); exist {
		ce.setExpired(removed)
		if ce.isSlidingTypeCache() {
			cc.usageSliding.Delete(key)
		} else {
			cc.usageNormal.Delete(key)
			cc.muForPriorityQueue.Lock()
			cc.pq.update(ce)
			cc.muForPriorityQueue.Unlock()
		}
	}

	cc.scanForExpiredItemsIfNeeded(nowUtc)
}

// Reset removes all items from the memory
func (cc *CacheCoherent) Reset() {
	erase(&cc.usageNormal, &cc.usageSliding)
	cc.muForPriorityQueue.Lock()
	cc.pq.erase()
	cc.muForPriorityQueue.Unlock()
	atomic.StoreInt64(&cc.stats.totalHits, 0)
	atomic.StoreInt64(&cc.stats.totalMisses, 0)
}

// loadCacheEntryFromUsage returns cacheEntry if it exists in the cache
func (cc *CacheCoherent) loadCacheEntryFromUsage(key any) (*cacheEntry, bool) {
	if val, ok := cc.usageNormal.Load(key); ok {
		return val.(*cacheEntry), true
	}

	if val, ok := cc.usageSliding.Load(key); ok {
		return val.(*cacheEntry), true
	}

	return nil, false
}

func (cc *CacheCoherent) scanForExpiredItemsIfNeeded(nowUtc int64) {
	if cc.expirationScanFrequency > nowUtc-cc.lastExpirationScan {
		return
	}

	if cc.onScanForExpired.Swap(true) {
		return
	}

	cc.flushExpired(nowUtc)
}

// flushExpired remove has expired item from the memory
func (cc *CacheCoherent) flushExpired(nowUtc int64) {
	var wg sync.WaitGroup
	defer func() {
		wg.Wait()
		cc.lastExpirationScan = nowUtc
		cc.onScanForExpired.Store(false)
	}()

	wg.Add(1)
	robin.RightNow().Do(func(nowUtc int64, wg *sync.WaitGroup) {
		defer wg.Done()
		cc.flushExpiredUsageNormal(nowUtc)
	}, nowUtc, &wg)

	wg.Add(1)
	robin.RightNow().Do(func(nowUtc int64, wg *sync.WaitGroup) {
		defer wg.Done()
		cc.flushExpiredUsageSliding(nowUtc)
	}, nowUtc, &wg)

}

func (cc *CacheCoherent) flushExpiredUsageNormal(nowUtc int64) {
	excisionC := make(chan any, queueCapacity)
	defer func() {
		close(excisionC)
	}()

	robin.RightNow().Do(func(C <-chan any) {
		for key := range C {
			cc.usageNormal.Delete(key)
		}
	}, excisionC)

	cc.muForPriorityQueue.Lock()
	defer cc.muForPriorityQueue.Unlock()

	l := cc.pq.Len()
	for i := 0; i < l; i++ {
		if ce, yes := cc.pq.dequeue(nowUtc); yes {
			excisionC <- ce.key
			continue
		}

		break
	}
}

func (cc *CacheCoherent) flushExpiredUsageSliding(nowUtc int64) {
	cc.usageSliding.Range(func(k, v any) bool {
		if ce, ok := v.(*cacheEntry); ok {
			if ce.checkExpired(nowUtc) {
				cc.usageSliding.Delete(k)
			}
		}

		return true
	})
}

func (cc *CacheCoherent) Statistics() CacheStatistics {
	sliding := &parallelCount{source: &cc.usageSliding}
	normal := &parallelCount{source: &cc.usageNormal}
	priorityQueueCount := cc.pq.Len()
	parallelCountMap(normal, sliding)

	statistics := CacheStatistics{
		usageSlidingEntryCount: sliding.count,
		usageNormalEntryCount:  normal.count,
		priorityQueueCount:     priorityQueueCount,
		totalMisses:            atomic.LoadInt64(&cc.stats.totalMisses),
		totalHits:              atomic.LoadInt64(&cc.stats.totalHits),
	}

	return statistics
}
