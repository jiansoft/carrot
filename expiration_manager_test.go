package carrot

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestExpirationManagerCreate tests ExpirationManager creation.
func TestExpirationManagerCreate(t *testing.T) {
	called := int32(0)
	em := newExpirationManager(func(ce *cacheEntry) {
		atomic.AddInt32(&called, 1)
	})

	if em == nil {
		t.Fatal("newExpirationManager should not return nil")
	}

	if em.tw == nil {
		t.Error("TimingWheel should be initialized")
	}
}

// TestExpirationManagerStartStop tests Start and Stop.
func TestExpirationManagerStartStop(t *testing.T) {
	em := newExpirationManager(func(ce *cacheEntry) {})

	em.Start()
	if !em.tw.IsRunning() {
		t.Error("TimingWheel should be running after Start")
	}

	em.Stop()
	if em.tw.IsRunning() {
		t.Error("TimingWheel should not be running after Stop")
	}
}

// TestExpirationManagerRouting_Forever tests Forever items are not added to TimingWheel.
func TestExpirationManagerRouting_Forever(t *testing.T) {
	em := newExpirationManager(func(ce *cacheEntry) {})
	defer em.Stop()

	ce := &cacheEntry{
		absoluteExpiration: -1, // Forever
	}

	em.Add(ce)

	stats := em.Stats()
	if stats.ForeverCount != 1 {
		t.Errorf("ForeverCount = %d, want 1", stats.ForeverCount)
	}
	if stats.AddCount != 0 {
		t.Errorf("AddCount = %d, want 0", stats.AddCount)
	}
}

// TestExpirationManagerRouting_TTL tests all TTL items go to TimingWheel.
func TestExpirationManagerRouting_TTL(t *testing.T) {
	em := newExpirationManager(func(ce *cacheEntry) {})
	em.Start()
	defer em.Stop()

	now := time.Now().UnixNano()

	// Short TTL
	ce1 := &cacheEntry{
		absoluteExpiration: now + int64(30*time.Minute),
		priority:           now + int64(30*time.Minute),
	}
	em.Add(ce1)

	// Long TTL (previously went to SPQ, now also goes to TW)
	ce2 := &cacheEntry{
		absoluteExpiration: now + int64(2*time.Hour),
		priority:           now + int64(2*time.Hour),
	}
	em.Add(ce2)

	stats := em.Stats()
	if stats.AddCount != 2 {
		t.Errorf("AddCount = %d, want 2", stats.AddCount)
	}

	if em.TimingWheelCount() != 2 {
		t.Errorf("TimingWheelCount() = %d, want 2", em.TimingWheelCount())
	}
}

// TestExpirationManagerRemove tests Remove functionality.
// 設計文件 Section 4.4.2: RemoveCount 計數的是「呼叫 Remove() 的次數」，
// 即使 CAS 失敗也會計數。
func TestExpirationManagerRemove(t *testing.T) {
	em := newExpirationManager(func(ce *cacheEntry) {})
	em.Start()
	defer em.Stop()

	now := time.Now().UnixNano()

	ce := &cacheEntry{
		absoluteExpiration: now + int64(30*time.Minute),
		priority:           now + int64(30*time.Minute),
	}
	em.Add(ce)

	// Remove should succeed on first call (CAS 成功)
	if !em.Remove(ce) {
		t.Error("First Remove should succeed")
	}

	// Remove should fail on second call (CAS 失敗，但仍計數)
	if em.Remove(ce) {
		t.Error("Second Remove should fail")
	}

	stats := em.Stats()
	// 設計文件：即使 CAS 失敗也要計數，所以 RemoveCount=2
	if stats.RemoveCount != 2 {
		t.Errorf("RemoveCount = %d, want 2 (both calls counted)", stats.RemoveCount)
	}
	if stats.RemoveSuccessCount != 1 {
		t.Errorf("RemoveSuccessCount = %d, want 1", stats.RemoveSuccessCount)
	}
}

// TestExpirationManagerRemove_Forever tests Remove on Forever items.
func TestExpirationManagerRemove_Forever(t *testing.T) {
	em := newExpirationManager(func(ce *cacheEntry) {})
	em.Start()
	defer em.Stop()

	ce := &cacheEntry{
		absoluteExpiration: -1, // Forever
	}

	em.Add(ce)

	// Forever 項目不應進入 TW，Count 應為 0
	if got := em.Count(); got != 0 {
		t.Errorf("after Add Forever, Count = %d, want 0", got)
	}

	// Remove should succeed (CAS)
	if !em.Remove(ce) {
		t.Error("Remove on Forever item should succeed (CAS)")
	}

	// 修復驗證：Count 不應變成負數
	if got := em.Count(); got != 0 {
		t.Errorf("after Remove Forever, Count = %d, want 0 (must not go negative)", got)
	}

	stats := em.Stats()
	if stats.RemoveCount != 1 {
		t.Errorf("RemoveCount = %d, want 1", stats.RemoveCount)
	}
	if stats.RemoveSuccessCount != 1 {
		t.Errorf("RemoveSuccessCount = %d, want 1", stats.RemoveSuccessCount)
	}
}

// TestExpirationManagerClear tests Clear functionality.
func TestExpirationManagerClear(t *testing.T) {
	em := newExpirationManager(func(ce *cacheEntry) {})
	em.Start()
	defer em.Stop()

	now := time.Now().UnixNano()

	// Add items with varying TTLs (all go to TimingWheel now)
	for i := 0; i < 10; i++ {
		ttl := time.Duration(i%3+1) * time.Hour
		ce := &cacheEntry{
			absoluteExpiration: now + int64(ttl),
			priority:           now + int64(ttl),
		}
		em.Add(ce)
	}

	if em.Count() != 10 {
		t.Errorf("Count() = %d, want 10", em.Count())
	}

	em.Clear()

	if em.Count() != 0 {
		t.Errorf("Count() after Clear = %d, want 0", em.Count())
	}

	// Stats should be preserved (cumulative)
	stats := em.Stats()
	if stats.AddCount != 10 {
		t.Errorf("AddCount after Clear = %d, want 10", stats.AddCount)
	}
}

// TestExpirationManagerConcurrency tests concurrent operations.
func TestExpirationManagerConcurrency(t *testing.T) {
	expiredCount := int64(0)
	em := newExpirationManager(func(ce *cacheEntry) {
		atomic.AddInt64(&expiredCount, 1)
	})
	em.Start()
	defer em.Stop()

	var wg sync.WaitGroup
	numGoroutines := 10
	numOperations := 100

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			now := time.Now().UnixNano()

			for i := 0; i < numOperations; i++ {
				// Add items with varying TTLs (all go to TimingWheel now)
				ttl := time.Duration(i%3+1) * time.Hour
				ce := &cacheEntry{
					absoluteExpiration: now + int64(ttl),
					priority:           now + int64(ttl),
				}
				em.Add(ce)

				// Randomly remove some items
				if i%5 == 0 {
					em.Remove(ce)
				}
			}
		}(g)
	}

	wg.Wait()

	// Verify counts are reasonable
	stats := em.Stats()
	if stats.AddCount != int64(numGoroutines*numOperations) {
		t.Errorf("AddCount = %d, want %d", stats.AddCount, numGoroutines*numOperations)
	}
}

// TestCAS_CallbackOnlyOnce tests that CAS ensures callback is called only once.
func TestCAS_CallbackOnlyOnce(t *testing.T) {
	callbackCount := int64(0)
	em := newExpirationManager(func(ce *cacheEntry) {
		atomic.AddInt64(&callbackCount, 1)
	})
	em.Start()
	defer em.Stop()

	now := time.Now().UnixNano()
	ce := &cacheEntry{
		absoluteExpiration: now + int64(30*time.Minute),
		priority:           now + int64(30*time.Minute),
	}
	em.Add(ce)

	// Simulate concurrent Remove calls
	var wg sync.WaitGroup
	successCount := int64(0)

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if em.Remove(ce) {
				atomic.AddInt64(&successCount, 1)
			}
		}()
	}

	wg.Wait()

	// Only one Remove should succeed
	if successCount != 1 {
		t.Errorf("successCount = %d, want 1", successCount)
	}
}

// TestExpirationManagerTotalCount tests totalCount consistency.
func TestExpirationManagerTotalCount(t *testing.T) {
	em := newExpirationManager(func(ce *cacheEntry) {})
	em.Start()
	defer em.Stop()

	now := time.Now().UnixNano()

	// Add 10 items to TimingWheel
	entries := make([]*cacheEntry, 10)
	for i := 0; i < 10; i++ {
		entries[i] = &cacheEntry{
			absoluteExpiration: now + int64(30*time.Minute),
			priority:           now + int64(30*time.Minute),
		}
		em.Add(entries[i])
	}

	if em.TimingWheelCount() != 10 {
		t.Errorf("TimingWheelCount() = %d, want 10", em.TimingWheelCount())
	}

	// Remove 5 items
	for i := 0; i < 5; i++ {
		em.Remove(entries[i])
	}

	if em.TimingWheelCount() != 5 {
		t.Errorf("TimingWheelCount() after remove = %d, want 5", em.TimingWheelCount())
	}
}

// TestTimingWheelSliding tests Sliding item reschedule in TimingWheel.
func TestTimingWheelSliding(t *testing.T) {
	expiredCount := int64(0)
	em := newExpirationManager(func(ce *cacheEntry) {
		atomic.AddInt64(&expiredCount, 1)
	})
	em.Start()
	defer em.Stop()

	now := time.Now().UnixNano()

	// Create a sliding item with very short expiration for testing
	ce := &cacheEntry{
		absoluteExpiration: now + int64(2*time.Second),
		priority:           now + int64(2*time.Second),
		lastAccessed:       now,
		slidingExpiration:  2 * time.Second,
		kind:               KindSliding,
	}
	em.Add(ce)

	// Simulate a Read that extends the expiration
	ce.setLastAccessed(time.Now().UnixNano())

	// Note: Full reschedule testing would require waiting for the TimingWheel
	// to process the slot, which is time-dependent
}

// =============================================================================
// 競態條件測試（Race Condition Tests）
// 使用 go test -race 執行以檢測數據競態
// =============================================================================

// TestRace_ForgetAndExpire 測試 Forget 與 TimingWheel 過期同時處理同一項目
// 確保 callback 只觸發一次，不會重複扣減計數
func TestRace_ForgetAndExpire(t *testing.T) {
	var callbackCount int64
	cache := newCacheCoherent()

	// 設定極短的 TTL 來增加競態發生機率
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("key-%d", i)
		cache.Set(key, i, EntryOptions{
			TimeToLive: 10 * time.Millisecond,
			PostEvictionCallback: func(k, v any, reason EvictionReason) {
				atomic.AddInt64(&callbackCount, 1)
			},
		})
	}

	var wg sync.WaitGroup
	// Goroutine 1: 持續 Forget
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			cache.Forget(fmt.Sprintf("key-%d", i))
		}
	}()

	wg.Wait()
	// 等待 TimingWheel 處理過期項目及異步 callback 完成
	time.Sleep(200 * time.Millisecond)

	// 每個項目的 callback 應該只觸發一次
	finalCount := atomic.LoadInt64(&callbackCount)
	if finalCount != 100 {
		t.Errorf("Callback 應該觸發 100 次，實際 %d 次", finalCount)
	}

	// usageCount 應為 0
	stats := cache.Statistics()
	if stats.UsageCount() != 0 {
		t.Errorf("UsageCount 應為 0，實際 %d", stats.UsageCount())
	}
}

// TestRace_KeepAndExpire 測試 keep 替換與 TimingWheel 過期同時處理
func TestRace_KeepAndExpire(t *testing.T) {
	var callbackCount int64
	cache := newCacheCoherent()

	var wg sync.WaitGroup

	// Goroutine 1: 持續替換同一個 key
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			cache.Set("same-key", i, EntryOptions{
				TimeToLive: 10 * time.Millisecond,
				PostEvictionCallback: func(k, v any, reason EvictionReason) {
					atomic.AddInt64(&callbackCount, 1)
				},
			})
			time.Sleep(time.Millisecond)
		}
	}()

	wg.Wait()
	// 等待 TimingWheel 處理過期項目及異步 callback 完成
	time.Sleep(200 * time.Millisecond)

	// 至少應該有一些 callback（99 次替換 + 可能的過期）
	finalCount := atomic.LoadInt64(&callbackCount)
	if finalCount < 99 {
		t.Errorf("Callback 應該至少觸發 99 次（替換），實際 %d 次", finalCount)
	}

	// usageCount 應為 0 或 1
	stats := cache.Statistics()
	if stats.UsageCount() > 1 {
		t.Errorf("UsageCount 應為 0 或 1，實際 %d", stats.UsageCount())
	}
}

// TestRace_ReadSlidingAndExpire 測試 Read 更新 Sliding 與 TimingWheel 過期同時處理
func TestRace_ReadSlidingAndExpire(t *testing.T) {
	cache := newCacheCoherent()

	// 建立 Sliding 項目
	for i := 0; i < 50; i++ {
		key := fmt.Sprintf("sliding-%d", i)
		cache.Sliding(key, i, 20*time.Millisecond)
	}

	var wg sync.WaitGroup

	// Goroutine 1: 持續 Read（觸發 Sliding 延長）
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 10; j++ {
			for i := 0; i < 50; i++ {
				cache.Read(fmt.Sprintf("sliding-%d", i))
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()

	wg.Wait()
	// 等待 TimingWheel 處理
	time.Sleep(100 * time.Millisecond)

	// 驗證 Statistics 的一致性
	stats := cache.Statistics()
	if stats.UsageCount() < 0 {
		t.Error("UsageCount 不應為負數")
	}
}

// TestRace_RescheduleAndRemove 測試 reschedule 與 Remove 的競態
func TestRace_RescheduleAndRemove(t *testing.T) {
	cache := newCacheCoherent()

	// 建立 Sliding 項目
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("sliding-%d", i)
		cache.Sliding(key, i, 20*time.Millisecond)
	}

	var wg sync.WaitGroup

	// Goroutine 1: 持續 Read（觸發 Sliding 延長/reschedule）
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 10; j++ {
			for i := 0; i < 100; i++ {
				cache.Read(fmt.Sprintf("sliding-%d", i))
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()

	// Goroutine 2: 隨機 Forget（觸發 Remove）
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(25 * time.Millisecond)
		for i := 0; i < 50; i++ {
			cache.Forget(fmt.Sprintf("sliding-%d", i*2))
		}
	}()

	wg.Wait()
	time.Sleep(100 * time.Millisecond)

	// 驗證 Statistics 的一致性
	stats := cache.Statistics()
	if stats.UsageCount() < 0 {
		t.Error("UsageCount 不應為負數")
	}
}

// TestRace_ConcurrentExpiration 測試 TimingWheel 處理大量同時過期的項目
// 驗證每個項目的 callback 只被觸發一次
// 注意：預設 TimingWheel tick = 1 秒，TTL 必須 > 1 秒才能確保 TW 處理
func TestRace_ConcurrentExpiration(t *testing.T) {
	var callbackCount int64
	cache := newCacheCoherent()

	// 設定會同時過期的項目（TTL > 預設 tick 1s）
	for i := 0; i < 50; i++ {
		key := fmt.Sprintf("key-%d", i)
		cache.Set(key, i, EntryOptions{
			TimeToLive: 2 * time.Second,
			PostEvictionCallback: func(k, v any, reason EvictionReason) {
				atomic.AddInt64(&callbackCount, 1)
			},
		})
	}

	// 等待 TimingWheel 處理過期項目及異步 callback 完成
	time.Sleep(4 * time.Second)

	// 每個項目只應觸發一次 callback
	finalCount := atomic.LoadInt64(&callbackCount)
	if finalCount != 50 {
		t.Errorf("Callback 應該觸發 50 次，實際 %d 次", finalCount)
	}
}

// TestCAS_CallbackOnlyOnce_Forget 測試 Forget 與 TimingWheel 過期只觸發一次 callback
func TestCAS_CallbackOnlyOnce_Forget(t *testing.T) {
	var callbackCount int64
	cache := newCacheCoherent()

	key := "test-key"
	cache.Set(key, "value", EntryOptions{
		TimeToLive: 50 * time.Millisecond,
		PostEvictionCallback: func(k, v any, reason EvictionReason) {
			atomic.AddInt64(&callbackCount, 1)
		},
	})

	var wg sync.WaitGroup

	// 多個 goroutine 同時 Forget
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cache.Forget(key)
		}()
	}

	wg.Wait()
	// 等待 TimingWheel 處理過期及異步 callback 完成
	time.Sleep(200 * time.Millisecond)

	// Callback 只應觸發一次
	finalCount := atomic.LoadInt64(&callbackCount)
	if finalCount != 1 {
		t.Errorf("Callback 應該只觸發 1 次，實際 %d 次", finalCount)
	}
}

// TestCAS_CallbackOnlyOnce_Replace 測試 keep() 替換時 CAS 保護
func TestCAS_CallbackOnlyOnce_Replace(t *testing.T) {
	var callbackCount int64
	cache := newCacheCoherent()

	for i := 0; i < 100; i++ {
		cache.Set("same-key", i, EntryOptions{
			TimeToLive: 50 * time.Millisecond,
			PostEvictionCallback: func(k, v any, reason EvictionReason) {
				atomic.AddInt64(&callbackCount, 1)
			},
		})
		// 每次 keep() 都會替換舊項目，觸發 EvictionReasonReplaced
	}

	time.Sleep(100 * time.Millisecond) // 等待非同步 callback 和最後一個過期

	// 99 次替換 + 最終過期（如果還在）
	// 由於是同步替換，最後一個項目可能還沒過期
	finalCount := atomic.LoadInt64(&callbackCount)
	if finalCount < 99 || finalCount > 100 {
		t.Errorf("Callback 應該觸發 99-100 次，實際 %d 次", finalCount)
	}
}

// TestCAS_CallbackOnlyOnce_Multiple 測試多重競態下 callback 只觸發一次
func TestCAS_CallbackOnlyOnce_Multiple(t *testing.T) {
	var callbackCount int64
	cache := newCacheCoherent()

	key := "contested-key"
	cache.Set(key, "initial", EntryOptions{
		TimeToLive: 20 * time.Millisecond,
		PostEvictionCallback: func(k, v any, reason EvictionReason) {
			atomic.AddInt64(&callbackCount, 1)
		},
	})

	var wg sync.WaitGroup

	// Goroutine 1: 嘗試 Forget
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(15 * time.Millisecond)
		cache.Forget(key)
	}()

	// Goroutine 2: 嘗試替換
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(15 * time.Millisecond)
		cache.Set(key, "replaced", EntryOptions{
			TimeToLive: 100 * time.Millisecond,
			PostEvictionCallback: func(k, v any, reason EvictionReason) {
				atomic.AddInt64(&callbackCount, 1)
			},
		})
	}()

	wg.Wait()
	// 等待 TimingWheel 處理過期及異步 callback 完成
	time.Sleep(200 * time.Millisecond)

	// 原始項目的 callback 只應觸發一次
	// 替換的新項目可能也觸發一次（共 1-2 次）
	finalCount := atomic.LoadInt64(&callbackCount)
	if finalCount < 1 || finalCount > 2 {
		t.Errorf("Callback 應該觸發 1-2 次，實際 %d 次", finalCount)
	}
}

// TestConcurrency_TotalCount 測試高並發下 totalCount 一致性
func TestConcurrency_TotalCount(t *testing.T) {
	cache := newCacheCoherent()

	var wg sync.WaitGroup
	numGoroutines := 10
	numOperations := 100

	// 多個 goroutine 同時進行 add/remove 操作
	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < numOperations; i++ {
				key := fmt.Sprintf("key-%d-%d", id, i)
				// Add
				cache.Expire(key, i, 30*time.Minute)
				// Read
				cache.Read(key)
				// Sometimes Forget
				if i%3 == 0 {
					cache.Forget(key)
				}
			}
		}(g)
	}

	wg.Wait()

	// 驗證計數一致性
	stats := cache.Statistics()

	// usageCount 應該非負
	if stats.UsageCount() < 0 {
		t.Errorf("UsageCount 不應為負數: %d", stats.UsageCount())
	}

	// TimingWheel count 應該非負
	twCount := stats.TwCount()
	if twCount < 0 {
		t.Errorf("TwCount 不應為負數: %d", twCount)
	}

	// twCount 應該不超過 usageCount
	// （可能有些 Forever 項目不在佇列中）
	if twCount > stats.UsageCount() {
		t.Errorf("TwCount (%d) 不應超過 usageCount (%d)", twCount, stats.UsageCount())
	}
}
