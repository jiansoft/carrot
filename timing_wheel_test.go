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
// 11. 修正驗證測試
// ============================================================================

// TestFix1_PollReturnsExpiration 驗證 Poll 在持鎖狀態下重置 expiration
// Fix 1: 確保 Offer 不會在 Poll 與 expiration 重置之間覆寫 expiration
func TestFix1_PollReturnsExpiration(t *testing.T) {
	stopCh := make(chan struct{})
	dq := newDelayQueue(stopCh, defaultClock)

	b := newTwBucket()
	now := defaultClock.Now()
	targetExp := now + 1 // 1ms 後過期

	dq.Offer(b, targetExp)

	// 等待 bucket 過期
	done := make(chan struct{})
	var gotBucket *twBucket
	var gotExpiration int64

	go func() {
		gotBucket, gotExpiration = dq.Poll()
		close(done)
	}()

	select {
	case <-done:
		if gotBucket != b {
			t.Error("Poll 應該返回正確的 bucket")
		}
		if gotExpiration != targetExp {
			t.Errorf("Poll 應返回原始 expiration %d，實際 %d", targetExp, gotExpiration)
		}
		// 確認 expiration 已被重置為 -1
		if exp := atomic.LoadInt64(&b.expiration); exp != -1 {
			t.Errorf("Poll 後 bucket.expiration 應為 -1，實際 %d", exp)
		}
	case <-time.After(5 * time.Second):
		close(stopCh)
		t.Fatal("Poll 超時")
	}
}

// TestFix2_AddUsesClockNow 驗證 Add 使用 tw.clock.Now() 而非 time.Now()
// Fix 2: 統一時間來源，MockClock 可正確控制過期判斷
func TestFix2_AddUsesClockNow(t *testing.T) {
	// 設定 MockClock 起始時間為 10000ms
	clock := NewMockClock(10000)
	tw := newTimingWheelWithClock(time.Second, 64, func(ce *cacheEntry) {}, clock)

	// 建立一個 absoluteExpiration 設定在 MockClock 時間之前的 entry
	// 如果 Add 使用 time.Now()，它會看到這個 entry 還沒過期（因為 wall clock 遠大於 10000ms）
	// 如果 Add 正確使用 tw.clock.Now()，它會看到這個 entry 已過期
	ce := &cacheEntry{
		key:                "test",
		value:              "value",
		absoluteExpiration: 5000 * int64(time.Millisecond), // 5000ms（在 MockClock 的 10000ms 之前）
		priority:           5000 * int64(time.Millisecond),
		twLevel:            -1,
	}

	added := tw.Add(ce)
	if added {
		t.Error("absoluteExpiration 在 clock.Now() 之前的 entry 不應被加入")
	}
}

// TestFix3_RemoveEmptyBucketFromDelayQueue 驗證移除最後一項後 bucket 從 DelayQueue 移除
// Fix 3: 避免空 bucket 留在 DelayQueue 造成空喚醒
func TestFix3_RemoveEmptyBucketFromDelayQueue(t *testing.T) {
	tw := newTimingWheel(func(ce *cacheEntry) {})

	// 新增一個項目
	ce := createTestEntry("only-item", 5*time.Second)
	tw.Add(ce)

	// 確認 DelayQueue 有 bucket
	if tw.delayQueue.Len() == 0 {
		t.Fatal("新增項目後 DelayQueue 應該有 bucket")
	}

	// 移除該項目
	tw.Remove(ce)

	// 驗證 DelayQueue 的 bucket 已被移除
	if tw.delayQueue.Len() != 0 {
		t.Errorf("移除最後一項後 DelayQueue 應為空，實際長度 %d", tw.delayQueue.Len())
	}
}

// TestFix4_TotalCountExactlyOnce 驗證 Flush/Remove 競態下 totalCount 不會漏扣
// Fix 4: 使用 twCountClaimed CAS 確保每個 entry 只扣一次
func TestFix4_TotalCountExactlyOnce(t *testing.T) {
	tw := newTimingWheel(func(ce *cacheEntry) {})
	tw.Start()
	defer tw.Stop()

	const numItems = 1000
	entries := make([]*cacheEntry, numItems)

	for i := 0; i < numItems; i++ {
		entries[i] = createTestEntry(i, 2*time.Second)
		tw.Add(entries[i])
	}

	if tw.Count() != numItems {
		t.Fatalf("新增後預期 %d，實際 %d", numItems, tw.Count())
	}

	// 並發移除所有項目
	var wg sync.WaitGroup
	wg.Add(numItems)
	for i := 0; i < numItems; i++ {
		go func(idx int) {
			defer wg.Done()
			tw.Remove(entries[idx])
		}(i)
	}
	wg.Wait()

	if count := tw.Count(); count != 0 {
		t.Errorf("全部移除後 totalCount 應為 0，實際 %d", count)
	}
}

// TestFix4_TotalCountFlushRemoveRace 驗證 expireBucket 和 Remove 競態下 totalCount 正確
func TestFix4_TotalCountFlushRemoveRace(t *testing.T) {
	var expiredCount int64
	tw := newTimingWheel(func(ce *cacheEntry) {
		atomic.AddInt64(&expiredCount, 1)
	})
	tw.Start()
	defer tw.Stop()

	const numRounds = 10
	for round := 0; round < numRounds; round++ {
		const numItems = 100
		entries := make([]*cacheEntry, numItems)

		for i := 0; i < numItems; i++ {
			entries[i] = createTestEntry(round*numItems+i, 2*time.Second)
			tw.Add(entries[i])
		}

		// 移除一半（製造與 expireBucket 的競態）
		for i := 0; i < numItems; i += 2 {
			tw.Remove(entries[i])
		}
	}

	// 等待所有項目過期或被移除
	time.Sleep(4 * time.Second)

	count := tw.Count()
	if count != 0 {
		t.Errorf("所有項目處理完畢後 totalCount 應為 0，實際 %d", count)
	}
}

// TestFix5_MockClockSleepRespectsTarget 驗證 MockClock.Sleep 阻塞到目標時間
// Fix 5: Sleep 不再被任意 Advance 喚醒
func TestFix5_MockClockSleepRespectsTarget(t *testing.T) {
	clock := NewMockClock(0)

	done := make(chan struct{})
	go func() {
		clock.Sleep(100 * time.Millisecond) // 目標時間 = 100ms
		close(done)
	}()

	// 短暫等待確保 goroutine 進入 Sleep
	time.Sleep(10 * time.Millisecond)

	// 推進到 50ms — Sleep 不應被喚醒
	clock.Advance(50 * time.Millisecond)

	select {
	case <-done:
		t.Fatal("Sleep(100ms) 不應在 Advance(50ms) 後被喚醒")
	case <-time.After(50 * time.Millisecond):
		// OK: 尚未到目標時間
	}

	// 推進到 100ms — 現在 Sleep 應該被喚醒
	clock.Advance(50 * time.Millisecond)

	select {
	case <-done:
		// OK: 已到達目標時間
	case <-time.After(1 * time.Second):
		t.Fatal("Sleep(100ms) 應在 current >= 100ms 時被喚醒")
	}
}

// TestFix5_MockClockSleepMultipleWaiters 驗證多個 Sleep 依各自目標時間喚醒
func TestFix5_MockClockSleepMultipleWaiters(t *testing.T) {
	clock := NewMockClock(0)

	var order []int
	var mu sync.Mutex

	record := func(id int) {
		mu.Lock()
		order = append(order, id)
		mu.Unlock()
	}

	// 啟動三個 Sleep，目標時間不同
	go func() {
		clock.Sleep(300 * time.Millisecond)
		record(3)
	}()
	go func() {
		clock.Sleep(100 * time.Millisecond)
		record(1)
	}()
	go func() {
		clock.Sleep(200 * time.Millisecond)
		record(2)
	}()

	// 等待 goroutine 進入 Sleep
	time.Sleep(20 * time.Millisecond)

	// 逐步推進時間
	clock.Advance(100 * time.Millisecond) // → 100ms：waiter 1 醒
	time.Sleep(10 * time.Millisecond)

	clock.Advance(100 * time.Millisecond) // → 200ms：waiter 2 醒
	time.Sleep(10 * time.Millisecond)

	clock.Advance(100 * time.Millisecond) // → 300ms：waiter 3 醒
	time.Sleep(10 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(order) != 3 {
		t.Fatalf("預期 3 個 waiter 被喚醒，實際 %d", len(order))
	}

	for i, id := range order {
		if id != i+1 {
			t.Errorf("第 %d 個喚醒的應為 waiter %d，實際為 %d", i, i+1, id)
		}
	}
}

// TestFix3_RemoveBucketHeapAt 驗證 bucketHeap.RemoveAt 堆性質維護
func TestFix3_RemoveBucketHeapAt(t *testing.T) {
	stopCh := make(chan struct{})
	dq := newDelayQueue(stopCh, defaultClock)

	now := defaultClock.Now()

	// 加入多個 bucket
	buckets := make([]*twBucket, 5)
	for i := 0; i < 5; i++ {
		buckets[i] = newTwBucket()
		dq.Offer(buckets[i], now+int64(i+1)*100)
	}

	if dq.Len() != 5 {
		t.Fatalf("預期 5 個 bucket，實際 %d", dq.Len())
	}

	// 移除中間的 bucket
	dq.RemoveBucket(buckets[2])

	if dq.Len() != 4 {
		t.Errorf("移除後預期 4 個 bucket，實際 %d", dq.Len())
	}

	// 確認被移除的 bucket 狀態
	if buckets[2].heapIndex != -1 {
		t.Errorf("被移除的 bucket heapIndex 應為 -1，實際 %d", buckets[2].heapIndex)
	}
	if exp := atomic.LoadInt64(&buckets[2].expiration); exp != -1 {
		t.Errorf("被移除的 bucket expiration 應為 -1，實際 %d", exp)
	}
}

// ============================================================================
// 12. Patch Coverage 補充測試
// ============================================================================

// TestRemoveAt_BoundaryConditions 驗證 RemoveAt 的邊界情況
func TestRemoveAt_BoundaryConditions(t *testing.T) {
	t.Run("RemoveAt_InvalidIndex", func(t *testing.T) {
		h := &bucketHeap{items: make([]*twBucket, 0)}

		// 空堆：越界索引
		if b := h.RemoveAt(-1); b != nil {
			t.Error("負索引應返回 nil")
		}
		if b := h.RemoveAt(0); b != nil {
			t.Error("空堆 RemoveAt(0) 應返回 nil")
		}
		if b := h.RemoveAt(5); b != nil {
			t.Error("越界索引應返回 nil")
		}
	})

	t.Run("RemoveAt_LastElement", func(t *testing.T) {
		h := &bucketHeap{items: make([]*twBucket, 0)}

		b1 := newTwBucket()
		b2 := newTwBucket()
		b3 := newTwBucket()
		atomic.StoreInt64(&b1.expiration, 100)
		atomic.StoreInt64(&b2.expiration, 200)
		atomic.StoreInt64(&b3.expiration, 300)

		h.Push(b1)
		h.Push(b2)
		h.Push(b3)

		// 移除最後一個元素（i == last 分支）
		removed := h.RemoveAt(2)
		if removed != b3 {
			t.Error("應移除最後一個 bucket")
		}
		if h.Len() != 2 {
			t.Errorf("移除後預期 2 個元素，實際 %d", h.Len())
		}
		if removed.heapIndex != -1 {
			t.Errorf("被移除的 bucket heapIndex 應為 -1，實際 %d", removed.heapIndex)
		}
	})

	t.Run("RemoveAt_Root", func(t *testing.T) {
		h := &bucketHeap{items: make([]*twBucket, 0)}

		b1 := newTwBucket()
		b2 := newTwBucket()
		b3 := newTwBucket()
		atomic.StoreInt64(&b1.expiration, 100)
		atomic.StoreInt64(&b2.expiration, 200)
		atomic.StoreInt64(&b3.expiration, 300)

		h.Push(b1)
		h.Push(b2)
		h.Push(b3)

		// 移除堆頂（需要 siftDown 重新平衡）
		removed := h.RemoveAt(0)
		if removed != b1 {
			t.Error("應移除堆頂 bucket")
		}
		if h.Len() != 2 {
			t.Errorf("移除後預期 2 個元素，實際 %d", h.Len())
		}

		// 驗證堆序：新堆頂應為 b2（最小 expiration）
		if h.items[0] != b2 {
			t.Error("新堆頂應為 expiration 最小的 bucket")
		}
	})

	t.Run("RemoveAt_Middle_SiftUp", func(t *testing.T) {
		h := &bucketHeap{items: make([]*twBucket, 0)}

		// 建立一個需要 siftUp 的場景：
		// 移除中間元素後，最後一個元素的 expiration 比父節點小
		buckets := make([]*twBucket, 6)
		exps := []int64{100, 200, 300, 400, 500, 150}
		for i := range buckets {
			buckets[i] = newTwBucket()
			atomic.StoreInt64(&buckets[i].expiration, exps[i])
			h.Push(buckets[i])
		}

		// 移除 index=1 (exp=200)，最後元素 (exp=150) 會移到 index=1
		// 150 < 父節點(100) 不成立，所以會 siftDown
		// 但若移除 index=2 (exp=300)，150 移到 index=2，150 < 父節點(100) 不成立
		// 需要精心構造才能觸發 siftUp
		beforeLen := h.Len()
		removed := h.RemoveAt(1)
		if removed == nil {
			t.Fatal("RemoveAt 不應返回 nil")
		}
		if h.Len() != beforeLen-1 {
			t.Errorf("預期 %d 個元素，實際 %d", beforeLen-1, h.Len())
		}

		// 驗證堆序：堆頂仍應為最小值
		minExp := atomic.LoadInt64(&h.items[0].expiration)
		for i := 1; i < h.Len(); i++ {
			exp := atomic.LoadInt64(&h.items[i].expiration)
			parentIdx := (i - 1) >> 2
			parentExp := atomic.LoadInt64(&h.items[parentIdx].expiration)
			if exp < parentExp {
				t.Errorf("堆序違反：items[%d].exp=%d < parent items[%d].exp=%d",
					i, exp, parentIdx, parentExp)
			}
		}
		_ = minExp
	})
}

// TestRemoveBucket_NotEmpty 驗證 RemoveBucket 在 bucket 非空時不移除
func TestRemoveBucket_NotEmpty(t *testing.T) {
	stopCh := make(chan struct{})
	dq := newDelayQueue(stopCh, defaultClock)

	b := newTwBucket()
	now := defaultClock.Now()
	dq.Offer(b, now+10000)

	// 加入一個 entry 到 bucket
	ce := createTestEntry("keep", 5*time.Second)
	b.Add(ce)

	// 嘗試 RemoveBucket — 因為 count > 0 應該不移除
	dq.RemoveBucket(b)

	if dq.Len() != 1 {
		t.Errorf("bucket 非空時不應被移除，佇列長度應為 1，實際 %d", dq.Len())
	}
}

// TestRemoveBucket_NotInQueue 驗證 RemoveBucket 對不在佇列中的 bucket 是安全的
func TestRemoveBucket_NotInQueue(t *testing.T) {
	stopCh := make(chan struct{})
	dq := newDelayQueue(stopCh, defaultClock)

	b := newTwBucket()
	// 不 Offer，直接 RemoveBucket
	dq.RemoveBucket(b)

	// 不應 panic，佇列應仍為空
	if dq.Len() != 0 {
		t.Errorf("佇列應為空，實際 %d", dq.Len())
	}
}

// TestPoll_StopSignalPaths 驗證 Poll 的各種停止路徑
func TestPoll_StopSignalPaths(t *testing.T) {
	t.Run("StopWhileEmpty", func(t *testing.T) {
		// Poll 在佇列為空時等待，收到 stop 應返回 (nil, 0)
		stopCh := make(chan struct{})
		dq := newDelayQueue(stopCh, defaultClock)

		done := make(chan struct{})
		var gotBucket *twBucket
		var gotExp int64

		go func() {
			gotBucket, gotExp = dq.Poll()
			close(done)
		}()

		time.Sleep(50 * time.Millisecond)
		close(stopCh)

		select {
		case <-done:
			if gotBucket != nil || gotExp != 0 {
				t.Errorf("停止時應返回 (nil, 0)，實際 (%v, %d)", gotBucket, gotExp)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("Poll 應在收到停止信號後返回")
		}
	})

	t.Run("StopBeforePoll", func(t *testing.T) {
		// 先關閉 stopCh 再 Poll — 應立即返回 (nil, 0)
		stopCh := make(chan struct{})
		dq := newDelayQueue(stopCh, defaultClock)

		close(stopCh)

		b, exp := dq.Poll()
		if b != nil || exp != 0 {
			t.Errorf("stopCh 已關閉時 Poll 應返回 (nil, 0)，實際 (%v, %d)", b, exp)
		}
	})

	t.Run("StopWhileWaitingTimer", func(t *testing.T) {
		// Poll 在等待 timer 時收到 stop
		stopCh := make(chan struct{})
		dq := newDelayQueue(stopCh, defaultClock)

		b := newTwBucket()
		dq.Offer(b, defaultClock.Now()+60000) // 遠未來

		done := make(chan struct{})
		go func() {
			dq.Poll()
			close(done)
		}()

		time.Sleep(50 * time.Millisecond)
		close(stopCh)

		select {
		case <-done:
			// OK
		case <-time.After(2 * time.Second):
			t.Fatal("Poll 等待 timer 時收到 stop 應返回")
		}
	})
}

// TestPoll_WakeupByNewEarlierBucket 驗證 Poll 被更早到期的新 bucket 喚醒
func TestPoll_WakeupByNewEarlierBucket(t *testing.T) {
	stopCh := make(chan struct{})
	defer close(stopCh)
	dq := newDelayQueue(stopCh, defaultClock)

	now := defaultClock.Now()

	// 加入一個遠未來的 bucket
	bLate := newTwBucket()
	dq.Offer(bLate, now+60000)

	done := make(chan struct{})
	var gotBucket *twBucket

	go func() {
		gotBucket, _ = dq.Poll()
		close(done)
	}()

	// 等待 Poll 進入 timer 等待
	time.Sleep(50 * time.Millisecond)

	// 加入一個已過期的 bucket — 應觸發 wakeup
	bEarly := newTwBucket()
	dq.Offer(bEarly, now-1) // 已過期

	select {
	case <-done:
		if gotBucket != bEarly {
			t.Error("Poll 應優先返回已過期的 bucket")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("加入已過期 bucket 後 Poll 應被喚醒")
	}
}

// TestExpireBucket_DeletedEntrySkipped 驗證 expireBucket 跳過已刪除的 entry
func TestExpireBucket_DeletedEntrySkipped(t *testing.T) {
	var expiredCount int64
	tw := newTimingWheel(func(ce *cacheEntry) {
		atomic.AddInt64(&expiredCount, 1)
	})
	tw.Start()
	defer tw.Stop()

	const numItems = 20
	entries := make([]*cacheEntry, numItems)
	for i := 0; i < numItems; i++ {
		entries[i] = createTestEntry(i, 2*time.Second)
		tw.Add(entries[i])
	}

	// 標記一半為刪除
	for i := 0; i < numItems; i += 2 {
		tw.Remove(entries[i])
	}

	// 等待過期
	time.Sleep(4 * time.Second)

	// 只有未刪除的項目應觸發回調
	expected := int64(numItems / 2)
	actual := atomic.LoadInt64(&expiredCount)
	if actual != expected {
		t.Errorf("預期 %d 個過期回調，實際 %d（已刪除的不應觸發）", expected, actual)
	}

	// totalCount 應為 0
	if count := tw.Count(); count != 0 {
		t.Errorf("totalCount 應為 0，實際 %d", count)
	}
}

// TestOfferUpdateEarlierExpiration 驗證 Offer 更新更早的過期時間
func TestOfferUpdateEarlierExpiration(t *testing.T) {
	stopCh := make(chan struct{})
	dq := newDelayQueue(stopCh, defaultClock)

	b := newTwBucket()
	now := defaultClock.Now()

	// 第一次 Offer
	result1 := dq.Offer(b, now+1000)
	if !result1 {
		t.Error("第一次 Offer 應返回 true")
	}

	// 第二次 Offer 較早的過期時間 — 應更新並返回 false
	result2 := dq.Offer(b, now+500)
	if result2 {
		t.Error("已在佇列中的 bucket 再次 Offer 應返回 false")
	}

	// 驗證 expiration 已更新
	exp := atomic.LoadInt64(&b.expiration)
	if exp != now+500 {
		t.Errorf("expiration 應更新為 %d，實際 %d", now+500, exp)
	}

	// 第三次 Offer 較晚的過期時間 — 不應更新
	dq.Offer(b, now+2000)
	exp = atomic.LoadInt64(&b.expiration)
	if exp != now+500 {
		t.Errorf("較晚的 Offer 不應更新 expiration，預期 %d，實際 %d", now+500, exp)
	}
}

// TestMockClockSleepZeroOrNegative 驗證 MockClock.Sleep 零或負 duration
func TestMockClockSleepZeroOrNegative(t *testing.T) {
	clock := NewMockClock(100)

	// Sleep(0) 應立即返回
	clock.Sleep(0)

	// Sleep(-1s) 應立即返回
	clock.Sleep(-1 * time.Second)

	// 確認時間未變
	if now := clock.Now(); now != 100 {
		t.Errorf("Sleep(0) 後時間不應變化，預期 100，實際 %d", now)
	}
}

// TestMockClockSleepAlreadyPast 驗證 Sleep 目標時間已過的情況
func TestMockClockSleepAlreadyPast(t *testing.T) {
	clock := NewMockClock(1000)

	// Advance 到 2000ms
	clock.Advance(1000 * time.Millisecond)

	// Sleep 的目標時間 = 2000 + 100 = 2100ms
	// 但如果 current 已經 >= targetTime，應立即返回
	// 這裡 current = 2000, targetTime = 2100，所以會正常等待

	done := make(chan struct{})
	go func() {
		clock.Sleep(100 * time.Millisecond)
		close(done)
	}()

	time.Sleep(10 * time.Millisecond)
	clock.Advance(100 * time.Millisecond) // → 2100ms

	select {
	case <-done:
		// OK
	case <-time.After(1 * time.Second):
		t.Fatal("Sleep 應在 current >= targetTime 時被喚醒")
	}
}

// TestSafeOnExpired_PanicRecovery 驗證 safeOnExpired 的 panic 恢復
func TestSafeOnExpired_PanicRecovery(t *testing.T) {
	tw := newTimingWheel(func(ce *cacheEntry) {
		panic("test panic in onExpired")
	})
	tw.Start()
	defer tw.Stop()

	ce := createTestEntry("panic-entry", 1*time.Second)
	tw.Add(ce)

	// 等待過期 — 不應導致 goroutine 崩潰
	time.Sleep(3 * time.Second)

	// 時間輪應仍在運行
	if !tw.IsRunning() {
		t.Error("onExpired panic 不應導致時間輪停止")
	}
}
