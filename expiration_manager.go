package carrot

import (
	"sync/atomic"
	"time"
)

// DefaultShortTTLThreshold 預設短 TTL 閾值
// 小於等於此值的 TTL 使用 TimingWheel
const DefaultShortTTLThreshold = time.Hour

const (
	// expirationSourceNone 未設定或 Forever
	expirationSourceNone int32 = 0
	// expirationSourceTimingWheel 使用 TimingWheel
	expirationSourceTimingWheel int32 = 1
	// expirationSourcePriorityQueue 使用 ShardedPriorityQueue
	expirationSourcePriorityQueue int32 = 2
)

// ExpirationStrategy 過期策略類型
type ExpirationStrategy int32

const (
	// StrategyAuto 自動選擇（根據 TTL 長度）
	StrategyAuto ExpirationStrategy = iota
	// StrategyTimingWheel 強制使用時間輪
	StrategyTimingWheel
	// StrategyPriorityQueue 強制使用優先佇列
	StrategyPriorityQueue
)

// ExpirationManagerStats 統計資訊
//
// 注意：所有欄位使用大寫開頭，因為這是公開 API，
// 使用者可透過 ExpirationStats() 取得此結構
type ExpirationManagerStats struct {
	// TimingWheel 相關
	TwAddCount    int64 // 加入 TW 的次數
	TwRemoveCount int64 // 從 TW 移除的次數
	TwExpireCount int64 // TW 過期的次數

	// ShardedPQueue 相關
	SpqAddCount    int64 // 加入 SPQ 的次數
	SpqRemoveCount int64 // 從 SPQ 移除的次數
	SpqExpireCount int64 // SPQ 過期的次數

	// Forever 相關
	ForeverCount int64 // 永不過期項目的次數
}

// ExpirationManager 過期管理器
// 根據 TTL 長度自動選擇最佳的資料結構來管理快取項目的過期
type ExpirationManager struct {
	// tw 處理短 TTL 項目（主動定時清理）
	tw *TimingWheel

	// spq 處理長 TTL 項目（被動清理）
	spq *ShardedPriorityQueue

	// threshold 短 TTL 閾值（納秒）
	// TTL <= threshold 的項目使用 TimingWheel
	threshold int64

	// strategy 當前策略（原子操作）
	strategy int32

	// onExpired 項目過期時的回調函數
	onExpired func(*cacheEntry)

	// stats 統計資訊
	stats ExpirationManagerStats
}

// newExpirationManager 建立過期管理器
//
// 參數：
//   - onExpired: 項目過期時的回調函數
//
// 回傳：
//   - 初始化完成的 ExpirationManager
func newExpirationManager(onExpired func(*cacheEntry)) *ExpirationManager {
	em := &ExpirationManager{
		threshold: int64(DefaultShortTTLThreshold),
		onExpired: onExpired,
		strategy:  int32(StrategyAuto),
	}

	// TimingWheel 的過期回調會更新統計並調用原始回調
	em.tw = newTimingWheel(func(ce *cacheEntry) {
		atomic.AddInt64(&em.stats.TwExpireCount, 1)
		onExpired(ce)
	})

	em.spq = newShardedPriorityQueue()

	return em
}

// Start 啟動過期管理器
// 這會啟動 TimingWheel 的背景 goroutine
func (em *ExpirationManager) Start() {
	em.tw.Start()
}

// Stop 停止過期管理器
func (em *ExpirationManager) Stop() {
	em.tw.Stop()
}

// SetThreshold 設定短 TTL 閾值
// TTL <= threshold 的項目會使用 TimingWheel
//
// 參數：
//   - d: 閾值時間長度，必須 > 0
//
// 行為：
//   - d <= 0: 忽略此次呼叫，保持原有閾值（預設 1 小時）
//   - d > 0: 更新閾值為指定值
//
// 範例：
//
//	em.SetThreshold(5 * time.Minute)  // TTL <= 5 分鐘使用 TimingWheel
//	em.SetThreshold(0)                // 無效，保持原閾值
//	em.SetThreshold(-time.Second)     // 無效，保持原閾值
func (em *ExpirationManager) SetThreshold(d time.Duration) {
	if d <= 0 {
		return
	}
	atomic.StoreInt64(&em.threshold, int64(d))
}

// Threshold 取得當前短 TTL 閾值
func (em *ExpirationManager) Threshold() time.Duration {
	return time.Duration(atomic.LoadInt64(&em.threshold))
}

// SetStrategy 設定過期策略
//
// 參數：
//   - s: 策略類型，必須是有效的 ExpirationStrategy 值
//
// 有效值：
//   - StrategyAuto (0): 自動選擇（預設）
//   - StrategyTimingWheel (1): 強制使用時間輪
//   - StrategyPriorityQueue (2): 強制使用優先佇列
//
// 非法值行為：
//   - s < 0 或 s > 2: 忽略此次呼叫，保持原有策略
//   - 這是防禦性設計，避免未來新增策略時造成問題
//
// 範例：
//
//	em.SetStrategy(StrategyTimingWheel)  // 強制使用時間輪
//	em.SetStrategy(ExpirationStrategy(99)) // 無效，保持原策略
func (em *ExpirationManager) SetStrategy(s ExpirationStrategy) {
	// 驗證策略值的有效性
	if s < StrategyAuto || s > StrategyPriorityQueue {
		return
	}
	atomic.StoreInt32(&em.strategy, int32(s))
}

// Strategy 取得當前策略
func (em *ExpirationManager) Strategy() ExpirationStrategy {
	return ExpirationStrategy(atomic.LoadInt32(&em.strategy))
}

// Add 加入項目到適當的資料結構
//
// 路由邏輯：
// 1. absoluteExpiration < 0 → 不放入任何佇列（Forever）
// 2. strategy == TimingWheel → TimingWheel
// 3. strategy == PriorityQueue → ShardedPQueue
// 4. strategy == Auto && TTL <= threshold → TimingWheel
// 5. strategy == Auto && TTL > threshold → ShardedPQueue
//
// 並發安全：
// 如果在 usage.Store 和 em.Add 之間有其他 goroutine 呼叫了 Forget/Replace：
//   - 對於 SPQ：enqueue 完成後會檢測 deleted 狀態，並使用 CAS 確保 dirtyCount
//     只增加一次（與 removeMarked 共用同一個 CAS）
//   - 對於 TW：addToWheel 會檢測 deleted 狀態並適當處理
func (em *ExpirationManager) Add(ce *cacheEntry) {
	absExp := ce.getAbsoluteExpiration()

	// 永不過期的項目不放入任何佇列
	if absExp < 0 {
		atomic.StoreInt32(&ce.expirationSource, expirationSourceNone)
		atomic.AddInt64(&em.stats.ForeverCount, 1)
		return
	}

	// 計算 TTL
	ttl := absExp - time.Now().UnixNano()
	if ttl < 0 {
		ttl = 0
	}

	// 決定使用哪個資料結構
	useTimingWheel := em.shouldUseTimingWheel(ttl)

	if useTimingWheel {
		atomic.StoreInt32(&ce.expirationSource, expirationSourceTimingWheel)
		em.tw.Add(ce)
		atomic.AddInt64(&em.stats.TwAddCount, 1)
	} else {
		atomic.StoreInt32(&ce.expirationSource, expirationSourcePriorityQueue)
		em.spq.enqueue(ce)
		atomic.AddInt64(&em.stats.SpqAddCount, 1)
	}
}

// shouldUseTimingWheel 判斷是否應該使用 TimingWheel
func (em *ExpirationManager) shouldUseTimingWheel(ttlNano int64) bool {
	strategy := ExpirationStrategy(atomic.LoadInt32(&em.strategy))

	switch strategy {
	case StrategyTimingWheel:
		return true
	case StrategyPriorityQueue:
		return false
	default: // StrategyAuto
		threshold := atomic.LoadInt64(&em.threshold)
		return ttlNano <= threshold
	}
}

// Remove 從對應的資料結構中移除項目
// 回傳：true 表示贏得 CAS 並成功觸發移除流程，false 表示已被移除
//
// RemoveCount 語意（設計文件 Section 4.4.2）：
// TwRemoveCount/SpqRemoveCount 計數的是「呼叫 Remove() 的次數」，
// 而非「成功從資料結構移除的次數」。即使 ce.deleted=1 也會計數。
func (em *ExpirationManager) Remove(ce *cacheEntry) bool {
	// 先計數（無論 CAS 是否成功）
	// 這符合設計文件：「Remove(ce) 且 ce.deleted=1 仍會計數」
	source := atomic.LoadInt32(&ce.expirationSource)
	switch source {
	case expirationSourceTimingWheel:
		atomic.AddInt64(&em.stats.TwRemoveCount, 1)
	case expirationSourcePriorityQueue:
		atomic.AddInt64(&em.stats.SpqRemoveCount, 1)
	}

	// 1. 競爭刪除權 (CAS)
	if !atomic.CompareAndSwapInt32(&ce.deleted, 0, 1) {
		return false
	}

	// 2. 依據來源移除（CAS 成功後才真正移除）
	switch source {
	case expirationSourceTimingWheel:
		em.tw.RemoveMarked(ce)
	case expirationSourcePriorityQueue:
		em.spq.removeMarked(ce)
	}
	// expirationSourceNone: Forever 項目不需要從佇列移除
	return true
}

// Update 更新項目的優先級（用於 sliding expiration）
//
// 注意：TimingWheel 不支援原地更新，採用 Lazy 檢查
// 在 expireSlot 時會重新計算實際過期時間
func (em *ExpirationManager) Update(ce *cacheEntry) {
	source := atomic.LoadInt32(&ce.expirationSource)

	if source == expirationSourcePriorityQueue {
		em.spq.update(ce)
	}
	// TimingWheel: sliding 項目的處理方式不同
	// 會在 expireSlot 時檢查 lastAccessed + slidingExpiration
}

// Dequeue 從 ShardedPQueue 取出過期項目
// 這是為了支援被動清理模式（scanForExpiredItemsIfNeeded）
//
// 注意：TimingWheel 是主動清理，不需要 Dequeue
//
// 重要：Dequeue 成功後會將 expirationSource 設為 expirationSourceNone，
// 表示項目已不在任何過期佇列中。這確保後續呼叫 Remove 時不會再次
// 增加 dirtyCount（因為項目已經從 heap 中彈出了）。
func (em *ExpirationManager) Dequeue(limit int64) (*cacheEntry, bool) {
	ce, ok := em.spq.dequeue(limit)
	if ok {
		// 標記項目已不在任何過期佇列中
		// 這防止後續 Remove 時錯誤地增加 dirtyCount
		atomic.StoreInt32(&ce.expirationSource, expirationSourceNone)
		atomic.AddInt64(&em.stats.SpqExpireCount, 1)
	}
	return ce, ok
}

// Count 回傳所有佇列中的項目總數
func (em *ExpirationManager) Count() int {
	return em.tw.Count() + em.spq.ActiveCount()
}

// TimingWheelCount 回傳 TimingWheel 中的項目數
func (em *ExpirationManager) TimingWheelCount() int {
	return em.tw.Count()
}

// PriorityQueueCount 回傳 ShardedPQueue 中的項目數
func (em *ExpirationManager) PriorityQueueCount() int {
	return em.spq.ActiveCount()
}

// Stats 回傳統計資訊的快照
func (em *ExpirationManager) Stats() ExpirationManagerStats {
	return ExpirationManagerStats{
		TwAddCount:     atomic.LoadInt64(&em.stats.TwAddCount),
		TwRemoveCount:  atomic.LoadInt64(&em.stats.TwRemoveCount),
		TwExpireCount:  atomic.LoadInt64(&em.stats.TwExpireCount),
		SpqAddCount:    atomic.LoadInt64(&em.stats.SpqAddCount),
		SpqRemoveCount: atomic.LoadInt64(&em.stats.SpqRemoveCount),
		SpqExpireCount: atomic.LoadInt64(&em.stats.SpqExpireCount),
		ForeverCount:   atomic.LoadInt64(&em.stats.ForeverCount),
	}
}

// Clear 清空所有佇列
//
// 注意：此方法會重置所有計數，但不會重置統計資訊（Stats）
// 統計資訊是累計的，不應在 Clear 時重置
func (em *ExpirationManager) Clear() {
	em.tw.Clear()
	em.spq.erase()
}

// Shrink 對 SPQ 進行碎片整理，釋放底層陣列的未使用空間
//
// 使用場景：
//   - 大量項目過期或被刪除後，SPQ 底層 heap 可能佔用過多記憶體
//   - 定期執行或在記憶體壓力時調用
//
// 注意：此操作不影響任何快取項目，只是內部記憶體優化
// 與 CacheCoherent.Compact(percentage) 不同，後者會驅逐快取項目
func (em *ExpirationManager) Shrink() {
	em.spq.shrink()
}
