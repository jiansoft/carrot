package carrot

import (
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jiansoft/robin"
)

type (
	// CacheCoherent is a thread-safe in-memory cache that supports multiple expiration policies.
	CacheCoherent struct {
		cpq *ConcurrentPriorityQueue
		// save cache entry.
		usage sync.Map
		// number of items in the usage map.
		usageCount int64
		stats      CacheStatistics
		// currently in the state of scanning for expired.
		onScanForExpired atomic.Bool
		// the minimum length of time between successive scans for expired items.
		expirationScanFrequency int64
		// last expired scanning time(UnixNano)
		lastExpirationScan int64
	}
)

// newCacheCoherent creates a new CacheCoherent instance.
func newCacheCoherent() *CacheCoherent {
	coherent := &CacheCoherent{
		cpq:                     newConcurrentPriorityQueue(defaultQueueCapacity),
		stats:                   CacheStatistics{},
		lastExpirationScan:      time.Now().UTC().UnixNano(),
		expirationScanFrequency: int64(time.Minute),
	}

	return coherent
}

// SetScanFrequency sets a new frequency value for scanning expired items.
// Returns false if the frequency is not positive.
func (cc *CacheCoherent) SetScanFrequency(frequency time.Duration) bool {
	if frequency <= 0 {
		return false
	}

	atomic.StoreInt64(&cc.expirationScanFrequency, int64(frequency))

	return true
}

// Forever stores an item that never expires.
func (cc *CacheCoherent) Forever(key, val any) {
	cc.keep(key, val, entryOptions{TimeToLive: -1})
}

// Until stores an item that expires at a certain time. e.g. 2023-01-01 12:31:59.999
// If the specified time has already passed, the existing item with the same key will be removed.
func (cc *CacheCoherent) Until(key, val any, until time.Time) {
	var (
		untilUtc = until.UTC()
		nowUtc   = time.Now().UTC()
		ttl      = untilUtc.Sub(nowUtc)
	)

	if ttl <= 0 {
		cc.Forget(key)
		return
	}

	cc.keep(key, val, entryOptions{TimeToLive: ttl})
}

// Delay stores an item that expires after a period of time. e.g. time.Hour will expire after one hour from now.
// Use a negative or zero duration to store an item that never expires.
func (cc *CacheCoherent) Delay(key, val any, ttl time.Duration) {
	cc.keep(key, val, entryOptions{TimeToLive: ttl})
}

// Inactive stores an item that expires after it is inactive for more than a period of time.
// Each read will reset the expiration timer. Does nothing if inactive is not positive.
func (cc *CacheCoherent) Inactive(key, val any, inactive time.Duration) {
	if inactive <= 0 {
		return
	}

	cc.keep(key, val, entryOptions{SlidingExpiration: inactive})
}

// keep inserts an item into the memory cache.
func (cc *CacheCoherent) keep(key any, val any, option entryOptions) {
	var (
		nowUtc    = time.Now().UTC().UnixNano()
		ttl       = option.TimeToLive.Nanoseconds()
		priority  int64
		utcAbsExp int64
		kind      cacheKind
	)

	if option.SlidingExpiration > 0 {
		// sliding: initial expiration = now + SlidingExpiration
		utcAbsExp = nowUtc + option.SlidingExpiration.Nanoseconds()
		priority = utcAbsExp
		kind = KindSliding
	} else {
		// ttl <= 0 means never expire
		if ttl > 0 {
			utcAbsExp = nowUtc + ttl
			priority = utcAbsExp
		} else {
			// never expire
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
		// remove from queue first to avoid race condition
		cc.cpq.remove(priorEntry)
		priorEntry.setExpired(reasonReplaced)
	}

	if newEntry.checkExpired(nowUtc) {
		// already expired
		if priorExist {
			cc.usage.Delete(key)
			atomic.AddInt64(&cc.usageCount, -1)
		}
	} else {
		// Try to add or update the new entry.
		cc.usage.Store(key, newEntry)
		cc.cpq.enqueue(newEntry)
		if !priorExist {
			atomic.AddInt64(&cc.usageCount, 1)
		}
	}

	cc.scanForExpiredItemsIfNeeded(nowUtc)
}

// Read returns the value if the key exists in the cache and it's not expired.
// For sliding expiration items, each read resets the expiration timer.
func (cc *CacheCoherent) Read(key any) (any, bool) {
	nowUtc := time.Now().UTC().UnixNano()
	ce, exist := cc.loadCacheEntryFromUsage(key)

	if exist && !ce.checkExpired(nowUtc) {
		atomic.AddInt64(&cc.stats.totalHits, 1)
		ce.setLastAccessed(nowUtc)

		if ce.isSliding() {
			newExp := nowUtc + ce.slidingExpiration.Nanoseconds()
			ce.setAbsoluteExpiration(newExp)
			ce.setPriority(newExp)
			cc.cpq.update(ce)
		}

		cc.scanForExpiredItemsIfNeeded(nowUtc)
		return ce.value, true
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

// Forget removes an item from the memory.
func (cc *CacheCoherent) Forget(key any) {
	cc.forget(key, time.Now().UTC().UnixNano())
}

func (cc *CacheCoherent) forget(key any, nowUtc int64) {
	if ce, exist := cc.loadCacheEntryFromUsage(key); exist {
		// remove from queue first to avoid race condition
		cc.cpq.remove(ce)
		ce.setExpired(reasonRemoved)
		cc.usage.Delete(key)
		atomic.AddInt64(&cc.usageCount, -1)
	}

	cc.scanForExpiredItemsIfNeeded(nowUtc)
}

// Reset removes all items from the memory and resets statistics.
func (cc *CacheCoherent) Reset() {
	cc.usage = sync.Map{}
	atomic.StoreInt64(&cc.usageCount, 0)
	cc.cpq.erase()
	atomic.StoreInt64(&cc.stats.totalHits, 0)
	atomic.StoreInt64(&cc.stats.totalMisses, 0)
}

// loadCacheEntryFromUsage returns cacheEntry if it exists in the cache.
func (cc *CacheCoherent) loadCacheEntryFromUsage(key any) (*cacheEntry, bool) {
	if val, ok := cc.usage.Load(key); ok {
		return val.(*cacheEntry), true
	}

	return nil, false
}

func (cc *CacheCoherent) scanForExpiredItemsIfNeeded(nowUtc int64) {
	if atomic.LoadInt64(&cc.expirationScanFrequency) > nowUtc-atomic.LoadInt64(&cc.lastExpirationScan) {
		return
	}

	if cc.onScanForExpired.Swap(true) {
		return
	}

	robin.RightNow().Do(cc.flushExpired, nowUtc)
}

// flushExpired removes expired items from the memory.
func (cc *CacheCoherent) flushExpired(nowUtc int64) {
	defer func() {
		atomic.StoreInt64(&cc.lastExpirationScan, nowUtc)
		cc.onScanForExpired.Store(false)
	}()

	loop := cc.cpq.Count()
	for i := 0; i < loop; i++ {
		ce, yes := cc.cpq.dequeue(nowUtc)
		if !yes {
			break
		}

		cc.usage.Delete(ce.key)
		atomic.AddInt64(&cc.usageCount, -1)
	}
}

// Statistics returns a snapshot of cache statistics.
func (cc *CacheCoherent) Statistics() CacheStatistics {
	statistics := CacheStatistics{
		usageCount:  int(atomic.LoadInt64(&cc.usageCount)),
		pqCount:     cc.cpq.Count(),
		totalMisses: atomic.LoadInt64(&cc.stats.totalMisses),
		totalHits:   atomic.LoadInt64(&cc.stats.totalHits),
	}

	return statistics
}
