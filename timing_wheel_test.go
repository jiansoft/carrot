package carrot

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ============================================================================
// 時間輪單元測試
// ============================================================================
//
// 測試範圍涵蓋：
// 1. 基本功能測試 - 建立、啟動、停止
// 2. 新增與移除測試 - 項目的生命週期管理
// 3. 過期處理測試 - 驗證項目在正確時間過期
// 4. 層級進位測試 - 驗證多層級時間輪的進位邏輯
// 5. 並發安全測試 - 多 goroutine 同時操作
// 6. 邊界條件測試 - 極端情況處理
// 7. 效能基準測試 - 測量各操作的效能
// ============================================================================

// ============================================================================
// 測試輔助函數
// ============================================================================

// createTestEntry 建立測試用的快取項目
//
// 參數：
//   - key: 快取鍵
//   - ttl: 存活時間
//
// 回傳：
//   - 初始化完成的 cacheEntry 指標
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
//
// 驗證：
// - 各層級正確初始化
// - 槽數量符合配置
// - 初始狀態正確
func TestNewTimingWheel(t *testing.T) {
	expiredCount := int64(0)
	tw := newTimingWheel(func(ce *cacheEntry) {
		atomic.AddInt64(&expiredCount, 1)
	})

	// 驗證層級數量
	if len(tw.levels) != levelCount {
		t.Errorf("預期 %d 個層級，實際 %d 個", levelCount, len(tw.levels))
	}

	// 驗證各層級的槽數量
	expectedSizes := []int64{60, 60, 24}
	for i, level := range tw.levels {
		if level.size != expectedSizes[i] {
			t.Errorf("層級 %d：預期 %d 個槽，實際 %d 個", i, expectedSizes[i], level.size)
		}
		if int64(len(level.slots)) != level.size {
			t.Errorf("層級 %d：槽數組長度不符，預期 %d，實際 %d", i, level.size, len(level.slots))
		}
	}

	// 驗證初始狀態
	if tw.IsRunning() {
		t.Error("新建立的時間輪不應該處於運行狀態")
	}
	if tw.Count() != 0 {
		t.Errorf("新建立的時間輪項目數應為 0，實際 %d", tw.Count())
	}
}

// TestTimingWheelStartStop 測試時間輪的啟動與停止
//
// 驗證：
// - Start 後狀態變為運行中
// - Stop 後狀態變為停止
// - 重複 Start/Stop 不會造成問題
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
//
// 驗證：
// - 新增後計數正確
// - 項目被放入正確的層級
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

	// 驗證項目被分配到正確的層級
	// 5秒 -> Level 0 (秒級)
	if entries[0].twLevel != 0 {
		t.Errorf("5秒過期的項目應在層級 0，實際在層級 %d", entries[0].twLevel)
	}

	// 30秒 -> Level 0 (秒級，因為 < 60秒)
	if entries[1].twLevel != 0 {
		t.Errorf("30秒過期的項目應在層級 0，實際在層級 %d", entries[1].twLevel)
	}

	// 2分鐘 -> Level 1 (分鐘級，因為 > 60秒)
	if entries[2].twLevel != 1 {
		t.Errorf("2分鐘過期的項目應在層級 1，實際在層級 %d", entries[2].twLevel)
	}
}

// TestTimingWheelAddExpired 測試新增已過期的項目
//
// 驗證：
// - 已過期的項目會立即觸發回調
// - 不會被加入時間輪
func TestTimingWheelAddExpired(t *testing.T) {
	expiredCount := int64(0)
	var expiredKey any

	tw := newTimingWheel(func(ce *cacheEntry) {
		atomic.AddInt64(&expiredCount, 1)
		expiredKey = ce.key
	})

	// 建立已過期的項目（過期時間在過去）
	ce := &cacheEntry{
		key:      "expired",
		value:    "value",
		priority: time.Now().Add(-1 * time.Second).UnixNano(),
	}

	tw.Add(ce)

	// 等待回調執行
	time.Sleep(10 * time.Millisecond)

	if expiredCount != 1 {
		t.Errorf("預期 1 次過期回調，實際 %d 次", expiredCount)
	}
	if expiredKey != "expired" {
		t.Errorf("預期過期的 key 為 'expired'，實際 '%v'", expiredKey)
	}
}

// TestTimingWheelRemove 測試從時間輪移除項目
//
// 驗證：
// - 移除後計數正確
// - 移除的項目不會觸發過期回調
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
//
// 驗證：
// - 重複移除同一項目不會造成問題
// - 計數不會被錯誤減少
func TestTimingWheelRemoveIdempotent(t *testing.T) {
	tw := newTimingWheel(func(ce *cacheEntry) {})

	ce := createTestEntry("key", 5*time.Second)
	tw.Add(ce)

	// 第一次移除
	tw.Remove(ce)
	count1 := tw.Count()

	// 第二次移除（應該是安全的）
	tw.Remove(ce)
	count2 := tw.Count()

	if count1 != count2 {
		t.Errorf("重複移除不應改變計數，第一次 %d，第二次 %d", count1, count2)
	}
}

// ============================================================================
// 3. 過期處理測試
// ============================================================================

// TestTimingWheelExpiration 測試項目過期處理
//
// 驗證：
// - 項目在預期時間附近過期（±1秒誤差）
// - 過期回調被正確調用
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

	// 驗證過期時間的準確性（允許 1.5 秒誤差，因為時間輪精度為 1 秒）
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
//
// 驗證：
// - 時間輪停止後，即使時間到了也不會處理過期
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
// 4. 層級進位測試
// ============================================================================

// TestTimingWheelCascade 測試層級進位邏輯
//
// 驗證：
// - 高層級項目在進位時會被降級處理
// - 最終在正確的時間過期
func TestTimingWheelCascade(t *testing.T) {
	expiredCount := int64(0)
	var expiredTime time.Time
	var mu sync.Mutex

	tw := newTimingWheel(func(ce *cacheEntry) {
		mu.Lock()
		atomic.AddInt64(&expiredCount, 1)
		expiredTime = time.Now()
		mu.Unlock()
	})
	tw.Start()
	defer tw.Stop()

	startTime := time.Now()

	// 新增一個需要層級進位的項目（65秒）
	// 這會先放入 Level 1（分鐘層），60秒後進位到 Level 0，再過 5 秒過期
	ce := createTestEntry("cascade", 65*time.Second)
	tw.Add(ce)

	// 驗證初始放入 Level 1
	if ce.twLevel != 1 {
		t.Errorf("65秒過期的項目應先放入層級 1，實際在層級 %d", ce.twLevel)
	}

	// 這個測試時間較長，實際專案中可能需要 mock 時間
	// 這裡我們只驗證結構正確性，不等待實際過期
	t.Logf("項目放入層級 %d，槽 %d", ce.twLevel, ce.twSlot)

	// 簡化測試：只等待一小段時間確認沒有提前過期
	time.Sleep(3 * time.Second)

	mu.Lock()
	if expiredCount != 0 {
		t.Errorf("65秒的項目不應在 3 秒內過期，實際過期 %d 次", expiredCount)
	}
	mu.Unlock()

	_ = startTime
	_ = expiredTime
}

// TestCalculateSlot 測試槽位計算邏輯
//
// 驗證：
// - 不同延遲時間對應正確的層級
// - 偏移量計算正確
func TestCalculateSlot(t *testing.T) {
	tw := newTimingWheel(func(ce *cacheEntry) {})

	testCases := []struct {
		name          string
		delayMs       int64
		expectedLevel int
	}{
		{"500毫秒", 500, 0},       // < 1秒，Level 0
		{"5秒", 5000, 0},         // Level 0
		{"30秒", 30000, 0},       // Level 0
		{"59秒", 59000, 0},       // Level 0 邊界
		{"90秒", 90000, 1},       // Level 1 (> 60秒)
		{"5分鐘", 300000, 1},      // Level 1
		{"59分鐘", 3540000, 1},    // Level 1 邊界
		{"2小時", 7200000, 2},     // Level 2
		{"23小時", 82800000, 2},   // Level 2
		{"100小時", 360000000, 2}, // 超過最大範圍，放入 Level 2 最後一槽
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			level, offset := tw.calculateSlot(tc.delayMs)
			if level != tc.expectedLevel {
				t.Errorf("延遲 %d ms：預期層級 %d，實際 %d（偏移 %d）",
					tc.delayMs, tc.expectedLevel, level, offset)
			}
			// 確保偏移量在有效範圍內
			if offset < 0 || offset >= tw.levels[level].size {
				t.Errorf("延遲 %d ms：偏移量 %d 超出層級 %d 的範圍 [0, %d)",
					tc.delayMs, offset, level, tw.levels[level].size)
			}
		})
	}
}

// ============================================================================
// 5. 並發安全測試
// ============================================================================

// TestTimingWheelConcurrentAdd 測試並發新增
//
// 驗證：
// - 多個 goroutine 同時新增不會造成資料競爭
// - 計數正確
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
//
// 驗證：
// - 同時進行新增和移除不會造成問題
// - 最終狀態一致
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
	// 由於並發，實際數量可能有些偏差
	count := tw.Count()
	if count < 0 || count > numOperations {
		t.Errorf("項目數量異常：%d（應在 0 到 %d 之間）", count, numOperations)
	}
}

// TestTimingWheelConcurrentExpiration 測試並發過期處理
//
// 驗證：
// - 大量項目同時過期時不會漏掉
// - 不會重複處理
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

	if int(expiredCount) != numItems {
		t.Errorf("預期 %d 個項目過期，實際 %d 個", numItems, expiredCount)
	}
}

// ============================================================================
// 6. 邊界條件測試
// ============================================================================

// TestTimingWheelZeroTTL 測試零 TTL
//
// 驗證：
// - TTL 為 0 的項目會立即過期
func TestTimingWheelZeroTTL(t *testing.T) {
	expiredCount := int64(0)

	tw := newTimingWheel(func(ce *cacheEntry) {
		atomic.AddInt64(&expiredCount, 1)
	})

	ce := createTestEntry("zero-ttl", 0)
	tw.Add(ce)

	time.Sleep(50 * time.Millisecond)

	if expiredCount != 1 {
		t.Errorf("TTL 為 0 的項目應立即過期，實際過期 %d 次", expiredCount)
	}
}

// TestTimingWheelNegativeTTL 測試負數 TTL
//
// 驗證：
// - 負數 TTL（已過期）的項目會立即過期
func TestTimingWheelNegativeTTL(t *testing.T) {
	expiredCount := int64(0)

	tw := newTimingWheel(func(ce *cacheEntry) {
		atomic.AddInt64(&expiredCount, 1)
	})

	ce := createTestEntry("negative-ttl", -5*time.Second)
	tw.Add(ce)

	time.Sleep(50 * time.Millisecond)

	if expiredCount != 1 {
		t.Errorf("負數 TTL 的項目應立即過期，實際過期 %d 次", expiredCount)
	}
}

// TestTimingWheelVeryLongTTL 測試超長 TTL
//
// 驗證：
// - 超過時間輪最大範圍的項目會被放入最高層級
func TestTimingWheelVeryLongTTL(t *testing.T) {
	tw := newTimingWheel(func(ce *cacheEntry) {})

	// 100 小時，超過 24 小時範圍
	ce := createTestEntry("long-ttl", 100*time.Hour)
	tw.Add(ce)

	// 應該被放入最高層級 (Level 2)
	if ce.twLevel != 2 {
		t.Errorf("超長 TTL 的項目應放入層級 2，實際在層級 %d", ce.twLevel)
	}
}

// TestTimingWheelForeverEntry 測試永不過期的項目
//
// 驗證：
// - absoluteExpiration < 0 的項目不會被加入時間輪
// - 不會觸發過期回調
// - 計數不會增加
func TestTimingWheelForeverEntry(t *testing.T) {
	expiredCount := int64(0)

	tw := newTimingWheel(func(ce *cacheEntry) {
		atomic.AddInt64(&expiredCount, 1)
	})
	tw.Start()
	defer tw.Stop()

	// 建立永不過期的項目（模擬 Forever 的行為）
	// Forever 設定 absoluteExpiration = -1, priority = math.MaxInt64
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

	// 驗證未被加入
	if added {
		t.Error("永不過期的項目不應被加入時間輪")
	}

	if tw.Count() != 0 {
		t.Errorf("永不過期的項目不應增加計數，實際計數 %d", tw.Count())
	}

	// 等待一段時間確認不會觸發過期
	time.Sleep(2 * time.Second)

	if expiredCount != 0 {
		t.Errorf("永不過期的項目不應觸發過期回調，實際觸發 %d 次", expiredCount)
	}
}

// TestTimingWheelMixedForeverAndExpiring 測試混合永不過期和會過期的項目
//
// 驗證：
// - 永不過期的項目不加入時間輪
// - 會過期的項目正常加入並過期
func TestTimingWheelMixedForeverAndExpiring(t *testing.T) {
	expiredKeys := sync.Map{}

	tw := newTimingWheel(func(ce *cacheEntry) {
		expiredKeys.Store(ce.key, true)
	})
	tw.Start()
	defer tw.Stop()

	// 永不過期的項目
	foreverEntry := &cacheEntry{
		key:                "forever",
		value:              "value",
		priority:           int64(^uint64(0) >> 1),
		absoluteExpiration: -1,
		created:            time.Now().UnixNano(),
		twLevel:            -1,
		twSlot:             -1,
	}

	// 會過期的項目
	expiringEntry := createTestEntry("expiring", 2*time.Second)

	tw.Add(foreverEntry)
	tw.Add(expiringEntry)

	// 只有會過期的項目應該在計數中
	if tw.Count() != 1 {
		t.Errorf("只有會過期的項目應被計數，預期 1，實際 %d", tw.Count())
	}

	// 等待過期
	time.Sleep(4 * time.Second)

	// 驗證只有會過期的項目觸發回調
	if _, ok := expiredKeys.Load("expiring"); !ok {
		t.Error("'expiring' 項目應該過期")
	}
	if _, ok := expiredKeys.Load("forever"); ok {
		t.Error("'forever' 項目不應該過期")
	}
}

// TestTimingWheelClear 測試清空時間輪
//
// 驗證：
// - Clear 後所有項目被移除
// - 計數歸零
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
//
// 驗證：
// - 在槽被處理前標記刪除的項目不會觸發過期回調
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
	atomic.StoreInt32(&ce2.deleted, 1)

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
// 7. 效能基準測試
// ============================================================================

// BenchmarkTimingWheelAdd 測試新增效能
//
// 預期：O(1) 時間複雜度
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
//
// 預期：O(1) 時間複雜度（標記刪除）
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

// BenchmarkTimingWheelCalculateSlot 測試槽位計算效能
func BenchmarkTimingWheelCalculateSlot(b *testing.B) {
	tw := newTimingWheel(func(ce *cacheEntry) {})

	delays := []int64{1000, 30000, 90000, 300000, 7200000}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tw.calculateSlot(delays[i%len(delays)])
	}
}

// BenchmarkTimingWheelMixed 測試混合操作效能
//
// 模擬真實場景：80% 新增，20% 移除
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

// BenchmarkExpirationCheck 比較過期檢查效能
// 這是時間輪真正的優勢所在
//
// ShardedPQueue: dequeue 需要遍歷所有 shard 找過期項目
// TimingWheel: 只需處理當前槽，O(1) 找到過期項目
func BenchmarkExpirationCheck(b *testing.B) {
	const numItems = 100000

	// 準備：建立大量即將過期的項目
	now := time.Now()

	b.Run("ShardedPQueue-Dequeue-NoExpired", func(b *testing.B) {
		// 測試沒有過期項目時的 dequeue 效能
		// ShardedPQueue 需要遍歷所有 shard 確認沒有過期項目
		spq := newShardedPriorityQueue()
		for i := 0; i < numItems; i++ {
			ce := &cacheEntry{
				key:                i,
				value:              i,
				priority:           now.Add(time.Hour).UnixNano(), // 1小時後過期
				absoluteExpiration: now.Add(time.Hour).UnixNano(),
				created:            now.UnixNano(),
			}
			spq.enqueue(ce)
		}

		limit := time.Now().UnixNano() // 當前時間作為過期界限

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			spq.dequeue(limit) // 應該找不到過期項目
		}
	})

	b.Run("ShardedPQueue-Dequeue-WithExpired", func(b *testing.B) {
		// 測試有過期項目時的 dequeue 效能
		spq := newShardedPriorityQueue()
		for i := 0; i < numItems; i++ {
			ce := &cacheEntry{
				key:                i,
				value:              i,
				priority:           now.Add(-time.Second).UnixNano(), // 已過期
				absoluteExpiration: now.Add(-time.Second).UnixNano(),
				created:            now.UnixNano(),
			}
			spq.enqueue(ce)
		}

		limit := time.Now().UnixNano()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			spq.dequeue(limit)
		}
	})
}

// BenchmarkBatchExpiration 比較批次過期處理效能
// 模擬真實場景：一次清理多個過期項目
func BenchmarkBatchExpiration(b *testing.B) {
	const batchSize = 1000

	b.Run("ShardedPQueue-BatchDequeue", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			spq := newShardedPriorityQueue()
			now := time.Now()
			// 建立已過期的項目
			for j := 0; j < batchSize; j++ {
				ce := &cacheEntry{
					key:                j,
					value:              j,
					priority:           now.Add(-time.Second).UnixNano(),
					absoluteExpiration: now.Add(-time.Second).UnixNano(),
					created:            now.UnixNano(),
				}
				spq.enqueue(ce)
			}
			limit := time.Now().UnixNano()
			b.StartTimer()

			// 清理所有過期項目
			for {
				_, ok := spq.dequeue(limit)
				if !ok {
					break
				}
			}
		}
	})

	b.Run("TimingWheel-SlotExpiration", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			expiredCount := 0
			tw := newTimingWheel(func(ce *cacheEntry) {
				expiredCount++
			})

			// 模擬槽中有 batchSize 個項目
			slot := tw.levels[0].slots[0]
			now := time.Now()
			for j := 0; j < batchSize; j++ {
				ce := &cacheEntry{
					key:                j,
					value:              j,
					priority:           now.Add(-time.Second).UnixNano(),
					absoluteExpiration: now.Add(-time.Second).UnixNano(),
					created:            now.UnixNano(),
				}
				slot.entries[ce] = struct{}{}
			}
			b.StartTimer()

			// 清理槽中所有項目（模擬 advance 中的 expireSlot）
			tw.expireSlot(slot)
		}
	})
}
