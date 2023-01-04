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
		excisionsC chan any
		pq         *priorityQueue
		// save cache item.
		usageNormal  sync.Map
		usageSliding sync.Map
		// prevent pq from data race
		muForPriorityQueue sync.Mutex
		stats              cacheStatistics
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
		pq:                      newPriorityQueue(queueCapacity),
		stats:                   cacheStatistics{},
		excisionsC:              make(chan any, queueCapacity),
		lastExpirationScan:      time.Now().UTC().UnixNano(),
		expirationScanFrequency: int64(time.Minute),
	}

	robin.RightNow().Do(coherent.consumeExcisions)
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

	if priorEntry, exist := cc.loadCacheEntryFromUsage(key); exist {
		priorEntry.setExpired(replaced)
		if priorEntry.isSlidingTypeCache() {
			cc.usageSliding.Delete(priorEntry.key)
		} else {
			cc.muForPriorityQueue.Lock()
			cc.pq.update(priorEntry)
			cc.muForPriorityQueue.Unlock()
		}
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

	if !newEntry.checkExpired(nowUtc) {
		if newEntry.isSlidingTypeCache() {
			// 如果有設定inactive時效時要改放在 usageSliding ，降低 map range 時鎖定的時間
			cc.usageSliding.Store(key, newEntry)
		} else {
			cc.usageNormal.Store(key, newEntry)
			if !newEntry.isNeverExpired() {
				// 永不到期不用存到 pq
				cc.muForPriorityQueue.Lock()
				cc.pq.enqueue(newEntry)
				cc.muForPriorityQueue.Unlock()
			}
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
			return ce.value, true
		}

		if ce.isSlidingTypeCache() {
			cc.usageSliding.Delete(ce.key)
		} else {
			cc.muForPriorityQueue.Lock()
			cc.pq.update(ce)
			cc.muForPriorityQueue.Unlock()
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
	if ce, exist := cc.loadCacheEntryFromUsage(key); exist {
		if ce.isSlidingTypeCache() {
			cc.usageSliding.Delete(key)
		} else {
			ce.setExpired(removed)
			cc.muForPriorityQueue.Lock()
			cc.pq.update(ce)
			cc.muForPriorityQueue.Unlock()
		}
	}

	nowUtc := time.Now().UTC().UnixNano()
	cc.scanForExpiredItemsIfNeeded(nowUtc)
}

// Reset removes all items from the memory
func (cc *CacheCoherent) Reset() {
	erase(&cc.usageNormal)
	erase(&cc.usageSliding)
	cc.muForPriorityQueue.Lock()
	cc.pq.erase()
	cc.muForPriorityQueue.Unlock()
	atomic.StoreInt64(&cc.stats.totalHits, 0)
	atomic.StoreInt64(&cc.stats.totalMisses, 0)
}

// loadCacheEntryFromUsage returns cacheEntry if it exists in the cache
func (cc *CacheCoherent) loadCacheEntryFromUsage(key any) (*cacheEntry, bool) {
	var (
		val any
		yes bool
	)

	val, yes = cc.usageNormal.Load(key)
	if !yes {
		if val, yes = cc.usageSliding.Load(key); !yes {
			return nil, false
		}
	}

	return val.(*cacheEntry), true
}

func (cc *CacheCoherent) scanForExpiredItemsIfNeeded(nowUtc int64) {
	if cc.expirationScanFrequency > nowUtc-cc.lastExpirationScan || cc.onScanForExpired.Swap(true) {
		return
	}

	cc.lastExpirationScan = nowUtc
	robin.RightNow().Do(cc.flushExpired, nowUtc)
}

// flushExpired remove has expired item from the memory
func (cc *CacheCoherent) flushExpired(nowUtc int64) {
	defer func() {
		cc.onScanForExpired.Store(false)
	}()

	for loop := 0; loop < math.MaxInt32; loop++ {
		cc.muForPriorityQueue.Lock()
		ce, yes := cc.pq.dequeue(nowUtc)
		cc.muForPriorityQueue.Unlock()
		if !yes {
			break
		}
		cc.excisionsC <- ce.key
	}

	cc.usageSliding.Range(func(k, v any) bool {
		ce := v.(*cacheEntry)
		if ce.checkExpired(nowUtc) {
			cc.usageSliding.Delete(ce.key)
		}

		return true
	})
}

func (cc *CacheCoherent) consumeExcisions() {
	for {
		key := <-cc.excisionsC
		cc.usageNormal.Delete(key)
	}
}

func (cc *CacheCoherent) statistics() cacheStatistics {
	sliding := &parallelCount{source: &cc.usageSliding}
	normal := &parallelCount{source: &cc.usageNormal}
	parallelCountMap(normal, sliding)
	/*var (
	    usageSlidingEntryCount int
	    usageNormalEntryCount  int
	    wg                     = sync.WaitGroup{}
	)*/
	//wg.Add(1)
	//robin.RightNow().Do(func(source *sync.Map, swg *sync.WaitGroup) {
	//    source.Range(func(k, v any) bool {
	//        usageSlidingEntryCount++
	//        return true
	//    })
	//    swg.Done()
	//}, &cc.usageSliding, &wg)
	//
	//wg.Add(1)
	//robin.RightNow().Do(func(source *sync.Map, swg *sync.WaitGroup) {
	//    source.Range(func(k, v any) bool {
	//        usageNormalEntryCount++
	//        return true
	//    })
	//    swg.Done()
	//}, &cc.usageNormal, &wg)
	//
	//wg.Wait()
	/* cc.usageNormal.Range(func(k, v any) bool {
	    usageNormalEntryCount++
	    return true
	})*/

	statistics := cacheStatistics{
		usageSlidingEntryCount: sliding.count,
		usageNormalEntryCount:  normal.count,
		priorityQueueCount:     cc.pq.Len(),
		totalMisses:            atomic.LoadInt64(&cc.stats.totalMisses),
		totalHits:              atomic.LoadInt64(&cc.stats.totalHits),
	}

	return statistics
}
