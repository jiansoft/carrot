package carrot

import (
	"sync/atomic"
)

// ExpirationManagerStats 統計資訊
//
// 注意：所有欄位使用大寫開頭，因為這是公開 API，
// 使用者可透過 ExpirationStats() 取得此結構
type ExpirationManagerStats struct {
	// TimingWheel 相關
	AddCount    int64 // 加入 TW 的次數
	RemoveCount int64 // 從 TW 移除的次數
	ExpireCount int64 // TW 過期的次數

	// Forever 相關
	ForeverCount int64 // 永不過期項目的次數
}

// ExpirationManager 過期管理器
// 使用 TimingWheel 管理所有快取項目的過期
type ExpirationManager struct {
	// tw 處理所有有過期時間的項目（主動定時清理）
	tw *TimingWheel

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
		onExpired: onExpired,
	}

	// TimingWheel 的過期回調會更新統計並調用原始回調
	em.tw = newTimingWheel(func(ce *cacheEntry) {
		atomic.AddInt64(&em.stats.ExpireCount, 1)
		onExpired(ce)
	})

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

// Add 加入項目到 TimingWheel
//
// 路由邏輯：
// 1. absoluteExpiration < 0 → 不放入任何佇列（Forever）
// 2. 其餘 → TimingWheel
func (em *ExpirationManager) Add(ce *cacheEntry) {
	absExp := ce.getAbsoluteExpiration()

	// 永不過期的項目不放入任何佇列
	if absExp < 0 {
		// 預先標記 twCountClaimed，防止 Remove 時 claimCount 誤扣 totalCount
		atomic.StoreInt32(&ce.twCountClaimed, 1)
		atomic.AddInt64(&em.stats.ForeverCount, 1)
		return
	}

	em.tw.Add(ce)
	atomic.AddInt64(&em.stats.AddCount, 1)
}

// Remove 從 TimingWheel 中移除項目
// 回傳：true 表示贏得 CAS 並成功觸發移除流程，false 表示已被移除
func (em *ExpirationManager) Remove(ce *cacheEntry) bool {
	atomic.AddInt64(&em.stats.RemoveCount, 1)

	// 競爭刪除權 (CAS)
	if !atomic.CompareAndSwapInt32(&ce.deleted, 0, 1) {
		return false
	}

	em.tw.RemoveMarked(ce)
	return true
}

// Count 回傳 TimingWheel 中的項目總數
func (em *ExpirationManager) Count() int {
	return em.tw.Count()
}

// TimingWheelCount 回傳 TimingWheel 中的項目數
func (em *ExpirationManager) TimingWheelCount() int {
	return em.tw.Count()
}

// Stats 回傳統計資訊的快照
func (em *ExpirationManager) Stats() ExpirationManagerStats {
	return ExpirationManagerStats{
		AddCount:     atomic.LoadInt64(&em.stats.AddCount),
		RemoveCount:  atomic.LoadInt64(&em.stats.RemoveCount),
		ExpireCount:  atomic.LoadInt64(&em.stats.ExpireCount),
		ForeverCount: atomic.LoadInt64(&em.stats.ForeverCount),
	}
}

// Clear 清空所有佇列
//
// 注意：此方法會重置所有計數，但不會重置統計資訊（Stats）
// 統計資訊是累計的，不應在 Clear 時重置
func (em *ExpirationManager) Clear() {
	em.tw.Clear()
}
