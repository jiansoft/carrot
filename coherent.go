package carrot

import (
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jiansoft/robin"
)

var queueCapacity = 512

type (
	// CacheCoherent Wrapper for the memory cache entries collection.
	CacheCoherent struct {
		cpq *ConcurrentPriorityQueue
		// save cache entry.
		usage sync.Map
		stats CacheStatistics
		// currently in the state of scanning for expired.
		onScanForExpired atomic.Bool
		// the minimum length of time between successive scans for expired items.
		expirationScanFrequency time.Duration
		// last expired scanning time(UnixNano)
		lastExpirationScan int64
	}
)

func newCacheCoherent(capacity ...int) *CacheCoherent {
	if len(capacity) > 0 {
		queueCapacity = capacity[0]
	}

	coherent := &CacheCoherent{
		cpq:                     newConcurrentPriorityQueue(queueCapacity),
		stats:                   CacheStatistics{},
		lastExpirationScan:      time.Now().UTC().UnixNano(),
		expirationScanFrequency: time.Minute,
	}

	return coherent
}

// SetScanFrequency sets a new frequency value
func (cc *CacheCoherent) SetScanFrequency(frequency time.Duration) bool {
	if frequency <= 0 {
		return false
	}

	cc.expirationScanFrequency = frequency

	return true
}

// Forever never expiration
func (cc *CacheCoherent) Forever(key, val any) {
	cc.keep(key, val, CacheEntryOptions{TimeToLive: -time.Duration(1)})
}

// Until expires at a certain time. e.g. 2023-01-01 12:31:59.999
func (cc *CacheCoherent) Until(key, val any, until time.Time) {
	var (
		untilUtc = until.UTC()
		nowUtc   = time.Now().UTC()
		ttl      = untilUtc.Sub(nowUtc)
	)

	cc.Delay(key, val, ttl)
}

// Delay expires after a period of time. e.g. time.Hour, it will expire after one hour from now
func (cc *CacheCoherent) Delay(key, val any, ttl time.Duration) {
	if ttl <= 0 {
		return
	}

	cc.keep(key, val, CacheEntryOptions{TimeToLive: ttl})
}

// Inactive expires after it is inactive for more than a period of time
func (cc *CacheCoherent) Inactive(key, val any, inactive time.Duration) {
	if inactive <= 0 {
		return
	}

	cc.keep(key, val, CacheEntryOptions{SlidingExpiration: inactive})
}

// Forever inserts an item into the memory
func (cc *CacheCoherent) keep(key any, val any, option CacheEntryOptions) {
	var (
		nowUtc    = time.Now().UTC().UnixNano()
		ttl       = option.TimeToLive.Nanoseconds()
		priority  int64
		utcAbsExp int64
		kind      cacheKind
	)

	if option.SlidingExpiration > 0 {
		//滑動首次的到期時間為 now + SlidingExpiration
		utcAbsExp = nowUtc + option.SlidingExpiration.Nanoseconds()
		priority = utcAbsExp
		kind = KindSliding
	} else {
		//永不過期 ttl 為 -1
		if ttl > 0 {
			utcAbsExp = nowUtc + ttl
			priority = utcAbsExp
		} else {
			// 永不到期
			utcAbsExp = -1
			priority = int64(math.MaxInt64)
		}
		kind = KindNormal
	}

	newEntry := &cacheEntry{
		key:                key,
		value:              val,
		priority:           priority,
		created:            nowUtc,
		lastAccessed:       nowUtc,
		absoluteExpiration: utcAbsExp,
		slidingExpiration:  option.SlidingExpiration,
		kind:               kind,
	}

	priorEntry, priorExist := cc.loadCacheEntryFromUsage(key)
	if priorExist {
		priorEntry.setExpired(replaced)
		cc.cpq.remove(priorEntry)
	}

	if newEntry.checkExpired(nowUtc) {
		// already expired
		if priorExist {
			cc.usage.Delete(key)
		}
	} else {
		// Try to add or update the new entry.
		cc.usage.Store(key, newEntry)
		cc.cpq.enqueue(newEntry)
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

			if ce.isSliding() {
				ce.absoluteExpiration = nowUtc + ce.slidingExpiration.Nanoseconds()
				ce.priority = ce.absoluteExpiration
				cc.cpq.update(ce)
			}

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
		cc.usage.Delete(key)
		cc.cpq.remove(ce)
	}

	cc.scanForExpiredItemsIfNeeded(nowUtc)
}

// Reset removes all items from the memory
func (cc *CacheCoherent) Reset() {
	eraseMap(&cc.usage)
	cc.cpq.erase()
	atomic.StoreInt64(&cc.stats.totalHits, 0)
	atomic.StoreInt64(&cc.stats.totalMisses, 0)
}

// loadCacheEntryFromUsage returns cacheEntry if it exists in the cache
func (cc *CacheCoherent) loadCacheEntryFromUsage(key any) (*cacheEntry, bool) {
	if val, ok := cc.usage.Load(key); ok {
		return val.(*cacheEntry), true
	}

	return nil, false
}

func (cc *CacheCoherent) scanForExpiredItemsIfNeeded(nowUtc int64) {
	if cc.expirationScanFrequency > time.Duration(nowUtc-cc.lastExpirationScan) {
		return
	}

	if cc.onScanForExpired.Swap(true) {
		return
	}

	robin.RightNow().Do(cc.flushExpired, nowUtc)
}

// flushExpired remove has expired item from the memory
func (cc *CacheCoherent) flushExpired(nowUtc int64) {
	defer func() {
		cc.lastExpirationScan = nowUtc
		cc.onScanForExpired.Store(false)
	}()

	loop := cc.cpq.pq.Len()
	for i := 0; i < loop; i++ {
		ce, yes := cc.cpq.dequeue(nowUtc)
		if !yes {
			break
		}

		//if ce.evictionReason != removed {
		//fmt.Printf("Delete(%s) %+v\n",ce.key,ce)
		cc.usage.Delete(ce.key)
		//}
	}
}

func (cc *CacheCoherent) Statistics() CacheStatistics {
	normal := &parallelCount{source: &cc.usage}
	priorityQueueCount := cc.cpq.pq.Len()
	countMap(normal)

	statistics := CacheStatistics{
		usageCount:  normal.count,
		pqCount:     priorityQueueCount,
		totalMisses: atomic.LoadInt64(&cc.stats.totalMisses),
		totalHits:   atomic.LoadInt64(&cc.stats.totalHits),
	}

	return statistics
}
