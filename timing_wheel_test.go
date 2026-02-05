package carrot

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ============================================================================
// 時間輪單元測試（改進版）
// ============================================================================
//
// 測試範圍涵蓋：
// 1. 基本功能測試 - 建立、啟動、停止
// 2. 新增與移除測試 - 項目的生命週期管理
// 3. 過期處理測試 - 驗證項目在正確時間過期
// 4. 層級進位測試 - 驗證動態溢出輪的進位邏輯
// 5. 並發安全測試 - 多 goroutine 同時操作
// 6. 邊界條件測試 - 極端情況處理
// 7. DelayQueue 測試 - 事件驅動機制
// 8. Epoch 版本控制測試 - Flush/Remove 併發安全
// 9. 效能基準測試 - 測量各操作的效能
// ============================================================================

// ============================================================================
// 測試輔助函數
// ============================================================================

// createTestEntry 建立測試用的快取項目
func createTestEntry(key any, ttl time.Duration) *cacheEntry {
	now := time.Now().UnixNano()
	expireAt := now + int64(ttl)

	return &cacheEntry{
		key:                key,
		value:              key,
		priority:           expireAt,
		absoluteExpiration: expireAt,
		created:            now,
		twLevel:            -1,
		twSlot:             -1,
	}
}

// ============================================================================
// 1. 基本功能測試
// ============================================================================

// TestNewTimingWheel 測試時間輪的建立
func TestNewTimingWheel(t *testing.T) {
	expiredCount := int64(0)
	tw := newTimingWheel(func(ce *cacheEntry) {
		atomic.AddInt64(&expiredCount, 1)
	})

	// 驗證基本配置
	if tw.wheelSize != defaultWheelSize {
		t.Errorf("預期 wheelSize=%d，實際 %d", defaultWheelSize, tw.wheelSize)
	}

	if tw.wheelMask != defaultWheelSize-1 {
		t.Errorf("預期 wheelMask=%d，實際 %d", defaultWheelSize-1, tw.wheelMask)
	}

	if len(tw.buckets) != int(defaultWheelSize) {
		t.Errorf("預期 %d 個 bucket，實際 %d 個", defaultWheelSize, len(tw.buckets))
	}

	// 驗證初始狀態
	if tw.IsRunning() {
		t.Error("新建立的時間輪不應該處於運行狀態")
	}
	if tw.Count() != 0 {
		t.Errorf("新建立的時間輪項目數應為 0，實際 %d", tw.Count())
	}

	// 驗證 DelayQueue 初始化
	if tw.delayQueue == nil {
		t.Error("DelayQueue 應該被初始化")
	}
}

// TestNewTimingWheelWithConfig 測試自訂配置建立時間輪
func TestNewTimingWheelWithConfig(t *testing.T) {
	tw := newTimingWheelWithConfig(100*time.Millisecond, 32, func(ce *cacheEntry) {})

	if tw.tick != 100 {
		t.Errorf("預期 tick=100ms，實際 %d ms", tw.tick)
	}

	// wheelSize 應該對齊到 2 的冪次方
	if tw.wheelSize != 32 {
		t.Errorf("預期 wheelSize=32，實際 %d", tw.wheelSize)
	}
}

// TestTimingWheelStartStop 測試時間輪的啟動與停止
func TestTimingWheelStartStop(t *testing.T) {
	tw := newTimingWheel(func(ce *cacheEntry) {})

	// 測試啟動
	tw.Start()
	if !tw.IsRunning() {
		t.Error("Start 後時間輪應該處於運行狀態")
	}

	// 測試重複啟動（應該是安全的）
	tw.Start()
	if !tw.IsRunning() {
		t.Error("重複 Start 後時間輪應該仍處於運行狀態")
	}

	// 測試停止
	tw.Stop()
	if tw.IsRunning() {
		t.Error("Stop 後時間輪不應該處於運行狀態")
	}

	// 測試重複停止（應該是安全的）
	tw.Stop()
	if tw.IsRunning() {
		t.Error("重複 Stop 後時間輪不應該處於運行狀態")
	}
}

// ============================================================================
// 2. 新增與移除測試
// ============================================================================

// TestTimingWheelAdd 測試新增項目到時間輪
func TestTimingWheelAdd(t *testing.T) {
	tw := newTimingWheel(func(ce *cacheEntry) {})

	// 新增多個項目
	entries := []*cacheEntry{
		createTestEntry("key1", 5*time.Second),
		createTestEntry("key2", 30*time.Second),
		createTestEntry("key3", 2*time.Minute),
	}

	for _, ce := range entries {
		tw.Add(ce)
	}

	if tw.Count() != 3 {
		t.Errorf("預期 3 個項目，實際 %d 個", tw.Count())
	}

	// 驗證項目有正確的 bucket 指標
	for _, ce := range entries {
		if ce.twBucket.Load() == nil {
			t.Errorf("項目 %v 的 twBucket 應該被設定", ce.key)
		}
	}
}

// TestTimingWheelAddExpired 測試新增已過期的項目
func TestTimingWheelAddExpired(t *testing.T) {
	tw := newTimingWheel(func(ce *cacheEntry) {})

	// 建立已過期的項目（過期時間在過去）
	ce := &cacheEntry{
		key:      "expired",
		value:    "value",
		priority: time.Now().Add(-1 * time.Second).UnixNano(),
	}

	added := tw.Add(ce)

	// 已過期的項目不應該被加入
	if added {
		t.Error("已過期的項目不應該被加入時間輪")
	}
}

// TestTimingWheelRemove 測試從時間輪移除項目
func TestTimingWheelRemove(t *testing.T) {
	expiredCount := int64(0)

	tw := newTimingWheel(func(ce *cacheEntry) {
		atomic.AddInt64(&expiredCount, 1)
	})
	tw.Start()
	defer tw.Stop()

	// 新增項目
	ce := createTestEntry("to-remove", 2*time.Second)
	tw.Add(ce)

	if tw.Count() != 1 {
		t.Errorf("新增後預期 1 個項目，實際 %d 個", tw.Count())
	}

	// 移除項目
	tw.Remove(ce)

	if tw.Count() != 0 {
		t.Errorf("移除後預期 0 個項目，實際 %d 個", tw.Count())
	}

	// 等待超過過期時間，確認不會觸發回調
	time.Sleep(3 * time.Second)

	if expiredCount != 0 {
		t.Errorf("已移除的項目不應觸發過期回調，實際觸發 %d 次", expiredCount)
	}
}

// TestTimingWheelRemoveIdempotent 測試重複移除的冪等性
func TestTimingWheelRemoveIdempotent(t *testing.T) {
	tw := newTimingWheel(func(ce *cacheEntry) {})

	ce := createTestEntry("key", 5*time.Second)
	tw.Add(ce)

	// 第一次移除
	result1 := tw.Remove(ce)
	count1 := tw.Count()

	// 第二次移除（應該是安全的）
	result2 := tw.Remove(ce)
	count2 := tw.Count()

	if !result1 {
		t.Error("第一次移除應該成功")
	}

	if result2 {
		t.Error("第二次移除應該返回 false（已刪除）")
	}

	if count1 != count2 {
		t.Errorf("重複移除不應改變計數，第一次 %d，第二次 %d", count1, count2)
	}
}

// ============================================================================
// 3. 過期處理測試
// ============================================================================

// TestTimingWheelExpiration 測試項目過期處理
func TestTimingWheelExpiration(t *testing.T) {
	expiredKeys := make(map[any]time.Time)
	var mu sync.Mutex

	tw := newTimingWheel(func(ce *cacheEntry) {
		mu.Lock()
		expiredKeys[ce.key] = time.Now()
		mu.Unlock()
	})
	tw.Start()
	defer tw.Stop()

	startTime := time.Now()

	// 新增不同過期時間的項目
	ce2s := createTestEntry("2s", 2*time.Second)
	ce4s := createTestEntry("4s", 4*time.Second)

	tw.Add(ce2s)
	tw.Add(ce4s)

	// 等待所有項目過期
	time.Sleep(6 * time.Second)

	mu.Lock()
	defer mu.Unlock()

	// 驗證所有項目都已過期
	if len(expiredKeys) != 2 {
		t.Errorf("預期 2 個項目過期，實際 %d 個", len(expiredKeys))
	}

	// 驗證過期時間的準確性（允許 1.5 秒誤差）
	if expireTime, ok := expiredKeys["2s"]; ok {
		elapsed := expireTime.Sub(startTime)
		if elapsed < 1500*time.Millisecond || elapsed > 3500*time.Millisecond {
			t.Errorf("'2s' 項目過期時間不正確，預期約 2 秒，實際 %v", elapsed)
		}
	} else {
		t.Error("'2s' 項目未過期")
	}

	if expireTime, ok := expiredKeys["4s"]; ok {
		elapsed := expireTime.Sub(startTime)
		if elapsed < 3500*time.Millisecond || elapsed > 5500*time.Millisecond {
			t.Errorf("'4s' 項目過期時間不正確，預期約 4 秒，實際 %v", elapsed)
		}
	} else {
		t.Error("'4s' 項目未過期")
	}
}

// TestTimingWheelNoExpirationWhenStopped 測試停止後不處理過期
func TestTimingWheelNoExpirationWhenStopped(t *testing.T) {
	expiredCount := int64(0)

	tw := newTimingWheel(func(ce *cacheEntry) {
		atomic.AddInt64(&expiredCount, 1)
	})

	// 不啟動時間輪
	ce := createTestEntry("key", 1*time.Second)
	tw.Add(ce)

	// 等待超過過期時間
	time.Sleep(2 * time.Second)

	if expiredCount != 0 {
		t.Errorf("未啟動的時間輪不應處理過期，實際觸發 %d 次", expiredCount)
	}
}

// ============================================================================
// 4. 動態溢出輪測試
// ============================================================================

// TestTimingWheelOverflow 測試動態溢出輪
func TestTimingWheelOverflow(t *testing.T) {
	tw := newTimingWheelWithConfig(time.Second, 8, func(ce *cacheEntry) {})

	// 新增超出本輪範圍的項目（> 8 秒）
	ce := createTestEntry("overflow", 20*time.Second)
	tw.Add(ce)

	// 應該創建了溢出輪
	overflow := tw.overflowWheel.Load()
	if overflow == nil {
		t.Error("應該創建溢出輪")
	}

	// 驗證溢出輪的配置
	if overflow != nil {
		if overflow.tick != tw.interval {
			t.Errorf("溢出輪 tick 應為 %d，實際 %d", tw.interval, overflow.tick)
		}
		if overflow.level != 1 {
			t.Errorf("溢出輪 level 應為 1，實際 %d", overflow.level)
		}
	}
}

// TestTimingWheelMaxLevels 測試最大層級限制
func TestTimingWheelMaxLevels(t *testing.T) {
	// 使用很小的 tick 和 wheelSize 來測試層級限制
	tw := newTimingWheelWithConfig(10*time.Millisecond, 2, func(ce *cacheEntry) {})
	tw.maxLevels = 3 // 設定最大 3 層

	// 新增超長 TTL 的項目
	ce := createTestEntry("very-long", 100*time.Hour)
	tw.Add(ce)

	// 計算實際層級數
	levels := 1
	overflow := tw.overflowWheel.Load()
	for overflow != nil {
		levels++
		overflow = overflow.overflowWheel.Load()
	}

	if levels > 3 {
		t.Errorf("層級數不應超過 maxLevels，實際 %d 層", levels)
	}
}

// ============================================================================
// 5. 並發安全測試
// ============================================================================

// TestTimingWheelConcurrentAdd 測試並發新增
func TestTimingWheelConcurrentAdd(t *testing.T) {
	tw := newTimingWheel(func(ce *cacheEntry) {})
	tw.Start()
	defer tw.Stop()

	const numGoroutines = 100
	const numPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for g := 0; g < numGoroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < numPerGoroutine; i++ {
				ce := createTestEntry(id*numPerGoroutine+i, 10*time.Second)
				tw.Add(ce)
			}
		}(g)
	}

	wg.Wait()

	expectedCount := numGoroutines * numPerGoroutine
	actualCount := tw.Count()

	if actualCount != expectedCount {
		t.Errorf("預期 %d 個項目，實際 %d 個", expectedCount, actualCount)
	}
}

// TestTimingWheelConcurrentAddRemove 測試並發新增和移除
func TestTimingWheelConcurrentAddRemove(t *testing.T) {
	tw := newTimingWheel(func(ce *cacheEntry) {})
	tw.Start()
	defer tw.Stop()

	const numOperations = 1000
	entries := make([]*cacheEntry, numOperations)

	// 先建立所有項目
	for i := 0; i < numOperations; i++ {
		entries[i] = createTestEntry(i, 30*time.Second)
	}

	var wg sync.WaitGroup
	wg.Add(2)

	// Goroutine 1: 新增項目
	go func() {
		defer wg.Done()
		for i := 0; i < numOperations; i++ {
			tw.Add(entries[i])
		}
	}()

	// Goroutine 2: 移除偶數索引的項目
	go func() {
		defer wg.Done()
		time.Sleep(1 * time.Millisecond) // 稍微延遲確保有項目可移除
		for i := 0; i < numOperations; i += 2 {
			tw.Remove(entries[i])
		}
	}()

	wg.Wait()

	// 最終應該剩下約一半的項目（奇數索引）
	count := tw.Count()
	if count < 0 || count > numOperations {
		t.Errorf("項目數量異常：%d（應在 0 到 %d 之間）", count, numOperations)
	}
}

// TestTimingWheelConcurrentExpiration 測試並發過期處理
func TestTimingWheelConcurrentExpiration(t *testing.T) {
	var expiredCount int64
	expiredKeys := sync.Map{}

	tw := newTimingWheel(func(ce *cacheEntry) {
		atomic.AddInt64(&expiredCount, 1)
		// 檢查是否重複過期
		if _, loaded := expiredKeys.LoadOrStore(ce.key, true); loaded {
			t.Errorf("項目 %v 被重複處理過期", ce.key)
		}
	})
	tw.Start()
	defer tw.Stop()

	const numItems = 100

	// 新增大量即將過期的項目
	for i := 0; i < numItems; i++ {
		ce := createTestEntry(i, 2*time.Second)
		tw.Add(ce)
	}

	// 等待所有項目過期
	time.Sleep(4 * time.Second)

	if int(atomic.LoadInt64(&expiredCount)) != numItems {
		t.Errorf("預期 %d 個項目過期，實際 %d 個", numItems, atomic.LoadInt64(&expiredCount))
	}
}

// ============================================================================
// 6. 邊界條件測試
// ============================================================================

// TestTimingWheelZeroTTL 測試零 TTL
func TestTimingWheelZeroTTL(t *testing.T) {
	tw := newTimingWheel(func(ce *cacheEntry) {})

	ce := createTestEntry("zero-ttl", 0)
	added := tw.Add(ce)

	// TTL 為 0 意味著已過期，不應被加入
	if added {
		t.Error("TTL 為 0 的項目不應被加入時間輪")
	}
}

// TestTimingWheelNegativeTTL 測試負數 TTL
func TestTimingWheelNegativeTTL(t *testing.T) {
	tw := newTimingWheel(func(ce *cacheEntry) {})

	ce := createTestEntry("negative-ttl", -5*time.Second)
	added := tw.Add(ce)

	// 負數 TTL 意味著已過期，不應被加入
	if added {
		t.Error("負數 TTL 的項目不應被加入時間輪")
	}
}

// TestTimingWheelVeryLongTTL 測試超長 TTL
func TestTimingWheelVeryLongTTL(t *testing.T) {
	tw := newTimingWheel(func(ce *cacheEntry) {})

	// 100 小時
	ce := createTestEntry("long-ttl", 100*time.Hour)
	tw.Add(ce)

	// 應該被成功加入（通過溢出輪）
	if tw.Count() != 1 {
		t.Errorf("超長 TTL 的項目應該被加入，實際計數 %d", tw.Count())
	}
}

// TestTimingWheelForeverEntry 測試永不過期的項目
func TestTimingWheelForeverEntry(t *testing.T) {
	expiredCount := int64(0)

	tw := newTimingWheel(func(ce *cacheEntry) {
		atomic.AddInt64(&expiredCount, 1)
	})
	tw.Start()
	defer tw.Stop()

	// 建立永不過期的項目
	foreverEntry := &cacheEntry{
		key:                "forever",
		value:              "value",
		priority:           int64(^uint64(0) >> 1), // math.MaxInt64
		absoluteExpiration: -1,                     // 永不過期的標記
		created:            time.Now().UnixNano(),
		twLevel:            -1,
		twSlot:             -1,
	}

	// 嘗試加入時間輪
	added := tw.Add(foreverEntry)

	// 永不過期的項目會被放入最遠的槽
	if !added {
		t.Log("永不過期的項目未被加入時間輪（視實作而定）")
	}

	// 等待一段時間確認不會提前觸發過期
	time.Sleep(2 * time.Second)

	// 如果被加入，不應該在短時間內過期
	if expiredCount > 0 {
		t.Errorf("永不過期的項目不應觸發過期回調，實際觸發 %d 次", expiredCount)
	}
}

// TestTimingWheelClear 測試清空時間輪
func TestTimingWheelClear(t *testing.T) {
	tw := newTimingWheel(func(ce *cacheEntry) {})

	// 新增多個項目
	for i := 0; i < 100; i++ {
		ce := createTestEntry(i, time.Duration(i+1)*time.Second)
		tw.Add(ce)
	}

	if tw.Count() != 100 {
		t.Errorf("新增後預期 100 個項目，實際 %d 個", tw.Count())
	}

	tw.Clear()

	if tw.Count() != 0 {
		t.Errorf("Clear 後預期 0 個項目，實際 %d 個", tw.Count())
	}
}

// TestTimingWheelDeletedEntryNotExpired 測試已刪除項目不觸發過期
func TestTimingWheelDeletedEntryNotExpired(t *testing.T) {
	expiredKeys := sync.Map{}

	tw := newTimingWheel(func(ce *cacheEntry) {
		expiredKeys.Store(ce.key, true)
	})
	tw.Start()
	defer tw.Stop()

	ce1 := createTestEntry("keep", 2*time.Second)
	ce2 := createTestEntry("delete", 2*time.Second)

	tw.Add(ce1)
	tw.Add(ce2)

	// 在過期前刪除 ce2
	time.Sleep(500 * time.Millisecond)
	tw.Remove(ce2)

	// 等待過期時間
	time.Sleep(3 * time.Second)

	if _, ok := expiredKeys.Load("keep"); !ok {
		t.Error("'keep' 項目應該過期")
	}
	if _, ok := expiredKeys.Load("delete"); ok {
		t.Error("'delete' 項目已標記刪除，不應觸發過期")
	}
}

// ============================================================================
// 7. DelayQueue 測試
// ============================================================================

// TestDelayQueueBasic 測試 DelayQueue 基本功能
func TestDelayQueueBasic(t *testing.T) {
	stopCh := make(chan struct{})
	dq := newDelayQueue(stopCh, defaultClock)

	// 建立並加入 bucket
	b1 := newTwBucket()
	b2 := newTwBucket()

	now := defaultClock.Now()

	// b2 較早過期
	dq.Offer(b2, now+100)
	dq.Offer(b1, now+200)

	// 驗證佇列長度
	if dq.Len() != 2 {
		t.Errorf("預期 2 個 bucket，實際 %d 個", dq.Len())
	}
}

// TestDelayQueueStop 測試 DelayQueue 停止
func TestDelayQueueStop(t *testing.T) {
	stopCh := make(chan struct{})
	dq := newDelayQueue(stopCh, defaultClock)

	// 啟動 Poll
	done := make(chan struct{})
	go func() {
		dq.Poll()
		close(done)
	}()

	// 發送停止信號
	time.Sleep(100 * time.Millisecond)
	close(stopCh)

	// 等待 Poll 返回
	select {
	case <-done:
		// OK
	case <-time.After(2 * time.Second):
		t.Error("Poll 應該在收到停止信號後返回")
	}
}

// ============================================================================
// 8. Epoch 版本控制測試
// ============================================================================

// TestBucketEpoch 測試 bucket 的 epoch 版本控制
func TestBucketEpoch(t *testing.T) {
	b := newTwBucket()
	ce := createTestEntry("key", 5*time.Second)

	// 加入項目
	b.Add(ce)
	initialEpoch := ce.twEpoch

	if ce.twBucket.Load() != b {
		t.Error("項目應該屬於 bucket")
	}

	// Flush 後 epoch 應該增加
	b.Flush()

	if b.epoch <= initialEpoch {
		t.Error("Flush 後 epoch 應該增加")
	}

	// 嘗試用舊 epoch 移除（應該失敗）
	ce2 := createTestEntry("key2", 5*time.Second)
	b.Add(ce2)
	ce2.twEpoch = 0 // 設定錯誤的 epoch

	removed := b.Remove(ce2)
	if removed {
		t.Error("使用錯誤 epoch 移除應該失敗")
	}
}

// TestFlushRemoveRace 測試 Flush/Remove 併發
func TestFlushRemoveRace(t *testing.T) {
	b := newTwBucket()
	const numItems = 1000

	// 加入大量項目
	entries := make([]*cacheEntry, numItems)
	for i := 0; i < numItems; i++ {
		entries[i] = createTestEntry(i, 5*time.Second)
		b.Add(entries[i])
	}

	var wg sync.WaitGroup
	wg.Add(2)

	// Goroutine 1: Flush
	go func() {
		defer wg.Done()
		b.Flush()
	}()

	// Goroutine 2: 嘗試移除
	go func() {
		defer wg.Done()
		for _, ce := range entries {
			b.Remove(ce)
		}
	}()

	wg.Wait()

	// 確保不會 panic
	t.Log("Flush/Remove 併發測試完成，無 panic")
}

// ============================================================================
// 9. 工具函數測試
// ============================================================================

// TestNextPowerOfTwo 測試 2 的冪次對齊
func TestNextPowerOfTwo(t *testing.T) {
	testCases := []struct {
		input    int64
		expected int64
	}{
		{0, 1},
		{1, 1},
		{2, 2},
		{3, 4},
		{4, 4},
		{5, 8},
		{7, 8},
		{8, 8},
		{9, 16},
		{60, 64},
		{64, 64},
		{65, 128},
	}

	for _, tc := range testCases {
		result := nextPowerOfTwo(tc.input)
		if result != tc.expected {
			t.Errorf("nextPowerOfTwo(%d) = %d，預期 %d", tc.input, result, tc.expected)
		}
	}
}

// TestGetMetrics 測試指標收集
func TestGetMetrics(t *testing.T) {
	tw := newTimingWheel(func(ce *cacheEntry) {})

	// 新增一些項目
	for i := 0; i < 10; i++ {
		ce := createTestEntry(i, time.Duration(i+1)*time.Second)
		tw.Add(ce)
	}

	metrics := tw.GetMetrics()

	if metrics.TotalCount != 10 {
		t.Errorf("預期 TotalCount=10，實際 %d", metrics.TotalCount)
	}

	if metrics.Level != 0 {
		t.Errorf("預期 Level=0，實際 %d", metrics.Level)
	}
}

// TestValidate 測試完整性驗證
func TestValidate(t *testing.T) {
	tw := newTimingWheel(func(ce *cacheEntry) {})

	// 新增一些項目
	for i := 0; i < 100; i++ {
		ce := createTestEntry(i, time.Duration(i+1)*time.Second)
		tw.Add(ce)
	}

	err := tw.Validate()
	if err != nil {
		t.Errorf("驗證失敗: %v", err)
	}
}

// ============================================================================
// 10. 效能基準測試
// ============================================================================

// BenchmarkTimingWheelAdd 測試新增效能
func BenchmarkTimingWheelAdd(b *testing.B) {
	tw := newTimingWheel(func(ce *cacheEntry) {})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ce := createTestEntry(i, 10*time.Second)
		tw.Add(ce)
	}
}

// BenchmarkTimingWheelAddParallel 測試並發新增效能
func BenchmarkTimingWheelAddParallel(b *testing.B) {
	tw := newTimingWheel(func(ce *cacheEntry) {})
	tw.Start()
	defer tw.Stop()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			ce := createTestEntry(i, 10*time.Second)
			tw.Add(ce)
			i++
		}
	})
}

// BenchmarkTimingWheelRemove 測試移除效能
func BenchmarkTimingWheelRemove(b *testing.B) {
	tw := newTimingWheel(func(ce *cacheEntry) {})

	// 預先新增項目
	entries := make([]*cacheEntry, b.N)
	for i := 0; i < b.N; i++ {
		entries[i] = createTestEntry(i, time.Hour)
		tw.Add(entries[i])
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tw.Remove(entries[i])
	}
}

// BenchmarkBucketAdd 測試 bucket 新增效能
func BenchmarkBucketAdd(b *testing.B) {
	bucket := newTwBucket()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ce := createTestEntry(i, 10*time.Second)
		bucket.Add(ce)
	}
}

// BenchmarkBucketRemove 測試 bucket 移除效能
func BenchmarkBucketRemove(b *testing.B) {
	bucket := newTwBucket()

	entries := make([]*cacheEntry, b.N)
	for i := 0; i < b.N; i++ {
		entries[i] = createTestEntry(i, time.Hour)
		bucket.Add(entries[i])
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bucket.Remove(entries[i])
	}
}

// BenchmarkDelayQueueOffer 測試 DelayQueue Offer 效能
func BenchmarkDelayQueueOffer(b *testing.B) {
	stopCh := make(chan struct{})
	dq := newDelayQueue(stopCh, defaultClock)

	buckets := make([]*twBucket, b.N)
	for i := 0; i < b.N; i++ {
		buckets[i] = newTwBucket()
	}

	now := defaultClock.Now()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dq.Offer(buckets[i], now+int64(i))
	}
}

// BenchmarkTimingWheelMixed 測試混合操作效能
func BenchmarkTimingWheelMixed(b *testing.B) {
	tw := newTimingWheel(func(ce *cacheEntry) {})
	tw.Start()
	defer tw.Stop()

	entries := make([]*cacheEntry, 10000)
	for i := range entries {
		entries[i] = createTestEntry(i, 10*time.Minute)
		tw.Add(entries[i])
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if i%5 == 0 {
				// 20% 移除
				tw.Remove(entries[i%len(entries)])
			} else {
				// 80% 新增
				ce := createTestEntry(i+len(entries), 10*time.Second)
				tw.Add(ce)
			}
			i++
		}
	})
}

// ============================================================================
// 與 ShardedPriorityQueue 比較測試
// ============================================================================

// BenchmarkTimingWheelVsShardedPQueue 比較時間輪和分片優先佇列
func BenchmarkTimingWheelVsShardedPQueue(b *testing.B) {
	b.Run("TimingWheel-Add", func(b *testing.B) {
		tw := newTimingWheel(func(ce *cacheEntry) {})
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			ce := createTestEntry(i, 10*time.Second)
			tw.Add(ce)
		}
	})

	b.Run("ShardedPQueue-Enqueue", func(b *testing.B) {
		spq := newShardedPriorityQueue()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			ce := createTestEntry(i, 10*time.Second)
			spq.enqueue(ce)
		}
	})

	b.Run("TimingWheel-Remove", func(b *testing.B) {
		tw := newTimingWheel(func(ce *cacheEntry) {})
		entries := make([]*cacheEntry, b.N)
		for i := 0; i < b.N; i++ {
			entries[i] = createTestEntry(i, time.Hour)
			tw.Add(entries[i])
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			tw.Remove(entries[i])
		}
	})

	b.Run("ShardedPQueue-Remove", func(b *testing.B) {
		spq := newShardedPriorityQueue()
		entries := make([]*cacheEntry, b.N)
		for i := 0; i < b.N; i++ {
			entries[i] = createTestEntry(i, time.Hour)
			spq.enqueue(entries[i])
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			spq.remove(entries[i])
		}
	})
}
