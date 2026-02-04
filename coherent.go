package carrot

import (
	"context"
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jiansoft/robin"
)

type (
	// CacheCoherent is a thread-safe in-memory cache that supports multiple expiration policies.
	CacheCoherent struct {
		spq *ShardedPriorityQueue
		// save cache entry.
		usage sync.Map
		// number of items in the usage map.
		usageCount int64
		// current total size of all cache entries.
		currentSize int64
		// maximum size limit (0 means no limit).
		sizeLimit int64
		stats     CacheStatistics
		// currently in the state of scanning for expired.
		onScanForExpired atomic.Bool
		// the minimum length of time between successive scans for expired items.
		expirationScanFrequency int64
		// last expired scanning time(UnixNano)
		lastExpirationScan int64
	}
)

// NewCacheCoherent creates a new CacheCoherent instance.
func newCacheCoherent() *CacheCoherent {
	coherent := &CacheCoherent{
		spq:                     newShardedPriorityQueue(),
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

// SetSizeLimit sets the maximum size limit for the cache.
// When the limit is exceeded, items will be evicted based on priority.
// Set to 0 to disable size limit.
func (cc *CacheCoherent) SetSizeLimit(limit int64) {
	atomic.StoreInt64(&cc.sizeLimit, limit)
	if limit > 0 {
		cc.enforceSizeLimit()
	}
}

// GetSizeLimit returns the current size limit.
func (cc *CacheCoherent) GetSizeLimit() int64 {
	return atomic.LoadInt64(&cc.sizeLimit)
}

// GetCurrentSize returns the current total size of all cache entries.
func (cc *CacheCoherent) GetCurrentSize() int64 {
	return atomic.LoadInt64(&cc.currentSize)
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

// Expire stores an item that expires after a period of time. e.g. time.Hour will expire after one hour from now.
// Use a negative or zero duration to store an item that never expires.
func (cc *CacheCoherent) Expire(key, val any, ttl time.Duration) {
	cc.keep(key, val, entryOptions{TimeToLive: ttl})
}

// Delay is deprecated: Use Expire instead.
func (cc *CacheCoherent) Delay(key, val any, ttl time.Duration) {
	cc.Expire(key, val, ttl)
}

// Inactive stores an item that expires after it is inactive for more than a period of time.
// Each read will reset the expiration timer. Does nothing if inactive is not positive.
func (cc *CacheCoherent) Inactive(key, val any, inactive time.Duration) {
	if inactive <= 0 {
		return
	}

	cc.keep(key, val, entryOptions{SlidingExpiration: inactive})
}

// Set stores an item with the specified options.
// This is the most flexible method for storing cache entries.
func (cc *CacheCoherent) Set(key, val any, options EntryOptions) {
	cc.keep(key, val, options)
}

// GetOrCreate returns the value if the key exists, otherwise creates a new entry with the specified value.
// Returns the value and true if the key existed, or the new value and false if it was created.
// This method is atomic - if two goroutines call GetOrCreate simultaneously with the same key,
// only one will create the entry and both will receive the same value.
func (cc *CacheCoherent) GetOrCreate(key, val any, ttl time.Duration) (any, bool) {
	nowUtc := time.Now().UTC().UnixNano()

	// Fast path: check if already exists
	if ce, exist := cc.loadCacheEntryFromUsage(key); exist && !ce.checkExpired(nowUtc) {
		atomic.AddInt64(&cc.stats.totalHits, 1)
		ce.setLastAccessed(nowUtc)
		if ce.isSliding() {
			newExp := nowUtc + ce.slidingExpiration.Nanoseconds()
			ce.setAbsoluteExpiration(newExp)
			ce.setPriority(newExp)
			cc.spq.update(ce)
		}
		return ce.value, true
	}

	// Slow path: create new entry
	cc.Delay(key, val, ttl)
	return val, false
}

// GetOrCreateFunc returns the value if the key exists, otherwise calls the factory function to create a new entry.
// Returns the value and true if the key existed, or the new value and false if it was created.
// If the factory function returns an error, the error is returned and no entry is created.
func (cc *CacheCoherent) GetOrCreateFunc(key any, ttl time.Duration, factory func() (any, error)) (any, bool, error) {
	if v, ok := cc.Read(key); ok {
		return v, true, nil
	}

	val, err := factory()
	if err != nil {
		return nil, false, err
	}

	cc.Delay(key, val, ttl)
	return val, false, nil
}

// GetOrCreateWithOptions returns the value if the key exists, otherwise creates a new entry with the specified options.
func (cc *CacheCoherent) GetOrCreateWithOptions(key, val any, options EntryOptions) (any, bool) {
	if v, ok := cc.Read(key); ok {
		return v, true
	}

	cc.Set(key, val, options)
	return val, false
}

// Keys returns all non-expired keys in the cache.
func (cc *CacheCoherent) Keys() []any {
	nowUtc := time.Now().UTC().UnixNano()
	// Pre-allocate with estimated capacity to reduce allocations
	count := atomic.LoadInt64(&cc.usageCount)
	keys := make([]any, 0, count)

	cc.usage.Range(func(key, value any) bool {
		ce := value.(*cacheEntry)
		if !ce.checkExpired(nowUtc) {
			keys = append(keys, key)
		}
		return true
	})

	return keys
}

// Count returns the number of items in the cache.
func (cc *CacheCoherent) Count() int {
	return int(atomic.LoadInt64(&cc.usageCount))
}

// A compactEntryInfo is used internally for sorting during Compact.
type compactEntryInfo struct {
	key      any
	entry    *cacheEntry
	priority int64
	cachePri CachePriority
}

// Compact removes a percentage of low-priority items from the cache.
// The percentage should be between 0 and 1 (e.g., 0.2 for 20%).
func (cc *CacheCoherent) Compact(percentage float64) {
	if percentage <= 0 || percentage > 1 {
		return
	}

	nowUtc := time.Now().UTC().UnixNano()
	count := cc.Count()
	toRemove := int(float64(count) * percentage)

	if toRemove == 0 {
		return
	}

	// Pre-allocate with estimated capacity
	entries := make([]compactEntryInfo, 0, count)
	cc.usage.Range(func(key, value any) bool {
		ce := value.(*cacheEntry)
		if !ce.checkExpired(nowUtc) && ce.cachePriority != PriorityNeverRemove {
			entries = append(entries, compactEntryInfo{
				key:      key,
				entry:    ce,
				priority: ce.getPriority(),
				cachePri: ce.cachePriority,
			})
		}
		return true
	})

	// Sort by cache priority (low first), then by expiration priority
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].cachePri != entries[j].cachePri {
			return entries[i].cachePri < entries[j].cachePri
		}
		return entries[i].priority < entries[j].priority
	})

	// Remove the lowest priority items
	if toRemove > len(entries) {
		toRemove = len(entries)
	}
	for i := 0; i < toRemove; i++ {
		cc.removeEntry(entries[i].key, entries[i].entry, int32(EvictionReasonCapacity))
	}
}

// Keep inserts an item into the memory cache.
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

	// Handle expiration token
	var cancelCtx context.Context
	var cancelFunc context.CancelFunc
	if option.ExpirationToken != nil {
		cancelCtx, cancelFunc = context.WithCancel(option.ExpirationToken)
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
		cachePriority:      option.Priority,
		size:               option.Size,
		evictionCallback:   option.PostEvictionCallback,
		cancelCtx:          cancelCtx,
		cancelFunc:         cancelFunc,
	}

	priorEntry, priorExist := cc.loadCacheEntryFromUsage(key)
	if priorExist {
		// remove from queue first to avoid race condition
		cc.spq.remove(priorEntry)
		priorEntry.setExpired(int32(EvictionReasonReplaced))
		atomic.AddInt64(&cc.currentSize, -priorEntry.size)
		// Invoke callback asynchronously
		robin.RightNow().Do(priorEntry.invokeEvictionCallback)
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
		cc.spq.enqueue(newEntry)
		atomic.AddInt64(&cc.currentSize, newEntry.size)
		if !priorExist {
			atomic.AddInt64(&cc.usageCount, 1)
		}
	}

	cc.enforceSizeLimit()
	cc.scanForExpiredItemsIfNeeded(nowUtc)
}

// EnforceSizeLimit removes items if the size limit is exceeded.
func (cc *CacheCoherent) enforceSizeLimit() {
	limit := atomic.LoadInt64(&cc.sizeLimit)
	if limit <= 0 {
		return
	}

	currentSize := atomic.LoadInt64(&cc.currentSize)
	if currentSize <= limit {
		return
	}

	// Calculate how much to remove
	toRemove := currentSize - limit
	cc.compactBySize(toRemove)
}

// A compactBySizeEntryInfo is used internally for sorting during compactBySize.
type compactBySizeEntryInfo struct {
	key      any
	entry    *cacheEntry
	priority int64
	cachePri CachePriority
	size     int64
}

// CompactBySize removes items until the specified size is freed.
func (cc *CacheCoherent) compactBySize(targetSize int64) {
	nowUtc := time.Now().UTC().UnixNano()
	count := atomic.LoadInt64(&cc.usageCount)

	entries := make([]compactBySizeEntryInfo, 0, count)
	cc.usage.Range(func(key, value any) bool {
		ce := value.(*cacheEntry)
		if !ce.checkExpired(nowUtc) && ce.cachePriority != PriorityNeverRemove {
			entries = append(entries, compactBySizeEntryInfo{
				key:      key,
				entry:    ce,
				priority: ce.getPriority(),
				cachePri: ce.cachePriority,
				size:     ce.size,
			})
		}
		return true
	})

	// Sort by cache priority (low first), then by expiration priority
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].cachePri != entries[j].cachePri {
			return entries[i].cachePri < entries[j].cachePri
		}
		return entries[i].priority < entries[j].priority
	})

	var freedSize int64
	for i := range entries {
		if freedSize >= targetSize {
			break
		}
		cc.removeEntry(entries[i].key, entries[i].entry, int32(EvictionReasonCapacity))
		freedSize += entries[i].size
	}
}

// RemoveEntry removes a cache entry and invokes its callback.
func (cc *CacheCoherent) removeEntry(key any, ce *cacheEntry, reason int32) {
	cc.spq.remove(ce)
	ce.setExpired(reason)
	cc.usage.Delete(key)
	atomic.AddInt64(&cc.usageCount, -1)
	atomic.AddInt64(&cc.currentSize, -ce.size)
	// Only invoke callback asynchronously if one is set
	if ce.evictionCallback != nil {
		robin.RightNow().Do(ce.invokeEvictionCallback)
	} else if ce.cancelFunc != nil {
		ce.cancelFunc()
	}
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
			cc.spq.update(ce)
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

func (cc *CacheCoherent) forget(key any, _ int64) {
	if ce, exist := cc.loadCacheEntryFromUsage(key); exist {
		cc.removeEntry(key, ce, int32(EvictionReasonRemoved))
	}
	// Skip scanForExpiredItemsIfNeeded on forget to improve performance
	// Expired items will be cleaned up on subsequent operations
}

// Reset removes all items from the memory and resets statistics.
func (cc *CacheCoherent) Reset() {
	// Invoke callbacks for all entries
	cc.usage.Range(func(key, value any) bool {
		ce := value.(*cacheEntry)
		ce.setExpired(int32(EvictionReasonRemoved))
		robin.RightNow().Do(ce.invokeEvictionCallback)
		return true
	})

	cc.usage = sync.Map{}
	atomic.StoreInt64(&cc.usageCount, 0)
	atomic.StoreInt64(&cc.currentSize, 0)
	cc.spq.erase()
	atomic.StoreInt64(&cc.stats.totalHits, 0)
	atomic.StoreInt64(&cc.stats.totalMisses, 0)
}

// LoadCacheEntryFromUsage returns cacheEntry if it exists in the cache.
func (cc *CacheCoherent) loadCacheEntryFromUsage(key any) (*cacheEntry, bool) {
	if val, ok := cc.usage.Load(key); ok {
		return val.(*cacheEntry), true
	}

	return nil, false
}

func (cc *CacheCoherent) scanForExpiredItemsIfNeeded(nowUtc int64) {
	// Quick check without CAS to avoid contention
	lastScan := atomic.LoadInt64(&cc.lastExpirationScan)
	freq := atomic.LoadInt64(&cc.expirationScanFrequency)
	if freq > nowUtc-lastScan {
		return
	}

	// Use CAS to ensure only one goroutine triggers the scan
	if cc.onScanForExpired.Swap(true) {
		return
	}

	robin.RightNow().Do(cc.flushExpired, nowUtc)
}

// FlushExpired removes expired items from the memory.
func (cc *CacheCoherent) flushExpired(nowUtc int64) {
	defer func() {
		atomic.StoreInt64(&cc.lastExpirationScan, nowUtc)
		cc.onScanForExpired.Store(false)
	}()

	loop := cc.spq.Count()
	for i := 0; i < loop; i++ {
		ce, yes := cc.spq.dequeue(nowUtc)
		if !yes {
			break
		}

		cc.usage.Delete(ce.key)
		atomic.AddInt64(&cc.usageCount, -1)
		atomic.AddInt64(&cc.currentSize, -ce.size)
		// Invoke callback asynchronously
		robin.RightNow().Do(ce.invokeEvictionCallback)
	}
}

// Statistics returns a snapshot of cache statistics.
func (cc *CacheCoherent) Statistics() CacheStatistics {
	statistics := CacheStatistics{
		usageCount:  int(atomic.LoadInt64(&cc.usageCount)),
		pqCount:     cc.spq.ActiveCount(),
		totalMisses: atomic.LoadInt64(&cc.stats.totalMisses),
		totalHits:   atomic.LoadInt64(&cc.stats.totalHits),
	}

	return statistics
}
