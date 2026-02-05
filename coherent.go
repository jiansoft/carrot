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
		// em 過期管理器（替換原本的 spq）
		em *ExpirationManager
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
		stats:                   CacheStatistics{},
		lastExpirationScan:      time.Now().UTC().UnixNano(),
		expirationScanFrequency: int64(time.Minute),
	}

	// 建立過期管理器，設定過期回調
	coherent.em = newExpirationManager(coherent.handleExpired)

	// 啟動 TimingWheel 的背景 goroutine
	coherent.em.Start()

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

// Sliding stores an item with sliding expiration.
// The item expires after it has not been accessed for the specified duration.
// Each Read will reset the expiration timer. Does nothing if duration is not positive.
//
// Example:
//
//	cache.Sliding("session", userData, 30*time.Minute)
//	// Item expires 30 minutes after last access
func (cc *CacheCoherent) Sliding(key, val any, sliding time.Duration) {
	if sliding <= 0 {
		return
	}

	cc.keep(key, val, entryOptions{SlidingExpiration: sliding})
}

// Inactive is deprecated: Use Sliding instead.
func (cc *CacheCoherent) Inactive(key, val any, inactive time.Duration) {
	cc.Sliding(key, val, inactive)
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
			// 只有 ShardedPQueue 需要 update
			source := atomic.LoadInt32(&ce.expirationSource)
			if source == expirationSourcePriorityQueue {
				cc.em.Update(ce)
			}
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

// ShrinkExpirationQueue 對內部過期管理的優先佇列進行碎片整理
//
// 當大量項目過期或被刪除後，ShardedPriorityQueue 底層可能存在記憶體碎片。
// 此方法會重建碎片率超過 50% 的 shard，釋放未使用的空間。
//
// 注意：此操作不影響任何快取項目，只是內部記憶體優化。
// 與 Compact(percentage) 不同，後者會主動驅逐快取項目。
//
// 使用場景：
//   - 定期維護（如每小時執行一次）
//   - 在記憶體壓力時主動呼叫
func (cc *CacheCoherent) ShrinkExpirationQueue() {
	cc.em.Shrink()
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
		// 使用統一移除邏輯，原因為 Replaced
		cc.removeEntry(key, priorEntry, int32(EvictionReasonReplaced))
	}

	// 判斷 newEntry 是否已過期
	expired := newEntry.checkExpired(nowUtc)

	if expired {
		// newEntry 已過期，不放入快取
		// 注意：此時 priorEntry 已被移除，但 key 可能在並發下被其他 goroutine 設定了新值
		// 由於我們不打算存入 newEntry，也不應該刪除其他 goroutine 存入的值
		// 因此這裡「不做任何 Delete 操作」，讓其他可能存入的值保持不變
		return
	}

	// newEntry 未過期，正常存入
	cc.usage.Store(key, newEntry)
	cc.em.Add(newEntry)
	atomic.AddInt64(&cc.currentSize, newEntry.size)
	// 每次成功 Store 都遞增 usageCount
	// 因為 removeEntry 已經透過 CompareAndDelete 處理了 priorEntry 的遞減
	atomic.AddInt64(&cc.usageCount, 1)

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

// removeEntry 統一處理項目的移除、計數扣減與回調
//
// 所有移除操作（手動、過期、替換、容量不足）都必須透過此方法，
// 以確保 CAS 機制正確運作，callback 只觸發一次
//
// 回傳：true 表示成功移除，false 表示已被其他 goroutine 處理
func (cc *CacheCoherent) removeEntry(key any, ce *cacheEntry, reason int32) bool {
	// 1. 透過 ExpirationManager 競爭刪除權並從過期佇列移除
	// em.Remove 內部會執行 CAS atomic.CompareAndSwapInt32(&ce.deleted, 0, 1)
	if !cc.em.Remove(ce) {
		return false // 已被其他 goroutine 處理
	}

	// 2. 標記狀態
	ce.setExpired(reason)

	// 3. 從 usage map 移除
	// 使用 CompareAndDelete（Go 1.20+）確保原子性，不刪除被 keep() 替換的新 entry
	if cc.usage.CompareAndDelete(key, ce) {
		atomic.AddInt64(&cc.usageCount, -1)
	}

	// 4. 更新容量計數並觸發回調
	atomic.AddInt64(&cc.currentSize, -ce.size)
	if ce.evictionCallback != nil {
		robin.RightNow().Do(ce.invokeEvictionCallback)
	} else if ce.cancelFunc != nil {
		ce.cancelFunc()
	}

	return true
}

// handleExpired 處理來自 ExpirationManager 的過期回調
//
// 這個方法由 TimingWheel 的 onExpired 回調調用
// 透過統一的 removeEntry 邏輯處理，確保 CAS 機制正確運作
func (cc *CacheCoherent) handleExpired(ce *cacheEntry) {
	// 透過統一邏輯處理，確保 CAS 機制正確
	cc.removeEntry(ce.key, ce, int32(EvictionReasonExpired))
}

// Read returns the value if the key exists in the cache and it's not expired.
// For sliding expiration items, each read resets the expiration timer.
//
// 效能特性：
//   - 主要讀取來自 sync.Map，O(1)
//   - Sliding 項目在 TimingWheel 中：O(1)（Lazy 檢查，不需移動項目）
//   - Sliding 項目在 ShardedPQueue 中：O(log n)（需要 heap.Fix）
//
// 並發安全性：
//   - setLastAccessed 使用 atomic.StoreInt64，確保與 expireSlot 的讀取不會競態
//   - TimingWheel 的 Lazy 檢查只依賴 lastAccessed + slidingExpiration（不依賴 priority）
//   - ShardedPQueue 的 update 在自己的 shard lock 保護下執行
func (cc *CacheCoherent) Read(key any) (any, bool) {
	nowUtc := time.Now().UTC().UnixNano()
	ce, exist := cc.loadCacheEntryFromUsage(key)

	if exist && !ce.checkExpired(nowUtc) {
		atomic.AddInt64(&cc.stats.totalHits, 1)

		// 重要：先更新 lastAccessed（atomic 操作）
		// TimingWheel 的 Lazy 檢查會讀取此值來判斷是否真的過期
		ce.setLastAccessed(nowUtc)

		// Sliding 項目：更新過期時間
		if ce.isSliding() {
			newExp := nowUtc + ce.slidingExpiration.Nanoseconds()

			// 這兩個更新主要用於 ShardedPQueue
			// TimingWheel 不依賴這些值（使用 lastAccessed + slidingExpiration 計算）
			ce.setAbsoluteExpiration(newExp)
			ce.setPriority(newExp)

			// 只有 ShardedPQueue 需要 update（heap.Fix）
			// TimingWheel 採用 Lazy 檢查，在槽處理時才會重新排程
			// 這樣可以保持 Read 操作的 O(1) 效能
			source := atomic.LoadInt32(&ce.expirationSource)
			if source == expirationSourcePriorityQueue {
				cc.em.Update(ce)
			}
			// TimingWheel: 不需要任何操作
			// expireSlot 時會檢查 lastAccessed + slidingExpiration
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
//
// 重要：Reset 不會觸發 PostEvictionCallback
//
// 設計決定說明：
// 1. Reset 是「清空快取」的語意，不是「逐一移除項目」
// 2. 如果觸發所有 callback，可能造成：
//   - 大量快取時效能問題（N 個 goroutine 同時執行）
//   - 使用者可能不預期 Reset 會觸發 callback
//
// 3. 這與現有行為一致（向後相容）
//
// 如果使用者需要在 Reset 時執行清理邏輯，建議：
//   - 在呼叫 Reset 前自行遍歷 Keys() 並處理
//   - 或使用 Forget 逐一移除（會觸發 callback）
func (cc *CacheCoherent) Reset() {
	cc.usage.Range(func(key, _ any) bool {
		cc.usage.Delete(key)
		return true
	})
	// 清空 ExpirationManager（不觸發 callback）
	cc.em.Clear()
	atomic.StoreInt64(&cc.usageCount, 0)
	atomic.StoreInt64(&cc.currentSize, 0)
	// 注意：不重置 totalHits/totalMisses（設計文件 Section 5.3.6）
	// 統計資訊是累計的，Reset 只清空快取內容
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
//
// 僅處理 SPQ 中的項目，TW 會主動回調 handleExpired
func (cc *CacheCoherent) flushExpired(nowUtc int64) {
	defer func() {
		atomic.StoreInt64(&cc.lastExpirationScan, nowUtc)
		cc.onScanForExpired.Store(false)
	}()

	// 僅處理 ShardedPQueue 中的項目
	// TimingWheel 是主動清理，會透過 handleExpired 回調處理
	loop := cc.em.PriorityQueueCount()
	for i := 0; i < loop; i++ {
		ce, yes := cc.em.Dequeue(nowUtc)
		if !yes {
			break
		}
		// 透過統一邏輯處理，確保 CAS 機制正確
		cc.removeEntry(ce.key, ce, int32(EvictionReasonExpired))
	}
}

// Statistics returns a snapshot of cache statistics.
func (cc *CacheCoherent) Statistics() CacheStatistics {
	statistics := CacheStatistics{
		usageCount:  int(atomic.LoadInt64(&cc.usageCount)),
		pqCount:     cc.em.PriorityQueueCount(),
		twCount:     cc.em.TimingWheelCount(),
		totalMisses: atomic.LoadInt64(&cc.stats.totalMisses),
		totalHits:   atomic.LoadInt64(&cc.stats.totalHits),
	}

	return statistics
}

// SetExpirationStrategy 設定過期策略
func (cc *CacheCoherent) SetExpirationStrategy(s ExpirationStrategy) {
	cc.em.SetStrategy(s)
}

// SetShortTTLThreshold 設定短 TTL 閾值
func (cc *CacheCoherent) SetShortTTLThreshold(d time.Duration) {
	cc.em.SetThreshold(d)
}

// ExpirationStats 取得過期統計資訊
func (cc *CacheCoherent) ExpirationStats() ExpirationManagerStats {
	return cc.em.Stats()
}

// Stop 停止過期管理器
// 應在不再使用快取時調用，以釋放 TimingWheel 的背景 goroutine
func (cc *CacheCoherent) Stop() {
	cc.em.Stop()
}
