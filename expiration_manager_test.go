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

	if em.spq == nil {
		t.Error("ShardedPriorityQueue should be initialized")
	}

	if em.Threshold() != DefaultShortTTLThreshold {
		t.Errorf("Threshold() = %v, want %v", em.Threshold(), DefaultShortTTLThreshold)
	}

	if em.Strategy() != StrategyAuto {
		t.Errorf("Strategy() = %v, want StrategyAuto", em.Strategy())
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

// TestExpirationManagerRouting_Forever tests Forever items routing.
func TestExpirationManagerRouting_Forever(t *testing.T) {
	em := newExpirationManager(func(ce *cacheEntry) {})
	defer em.Stop()

	ce := &cacheEntry{
		absoluteExpiration: -1, // Forever
	}

	em.Add(ce)

	if atomic.LoadInt32(&ce.expirationSource) != expirationSourceNone {
		t.Error("Forever items should have expirationSourceNone")
	}

	stats := em.Stats()
	if stats.ForeverCount != 1 {
		t.Errorf("ForeverCount = %d, want 1", stats.ForeverCount)
	}
	if stats.TwAddCount != 0 {
		t.Errorf("TwAddCount = %d, want 0", stats.TwAddCount)
	}
	if stats.SpqAddCount != 0 {
		t.Errorf("SpqAddCount = %d, want 0", stats.SpqAddCount)
	}
}

// TestExpirationManagerRouting_ShortTTL tests short TTL items routing to TimingWheel.
func TestExpirationManagerRouting_ShortTTL(t *testing.T) {
	em := newExpirationManager(func(ce *cacheEntry) {})
	em.Start()
	defer em.Stop()

	now := time.Now().UnixNano()
	ce := &cacheEntry{
		absoluteExpiration: now + int64(30*time.Minute), // 30 minutes < 1 hour threshold
		priority:           now + int64(30*time.Minute),
	}

	em.Add(ce)

	if atomic.LoadInt32(&ce.expirationSource) != expirationSourceTimingWheel {
		t.Error("Short TTL items should go to TimingWheel")
	}

	stats := em.Stats()
	if stats.TwAddCount != 1 {
		t.Errorf("TwAddCount = %d, want 1", stats.TwAddCount)
	}

	if em.TimingWheelCount() != 1 {
		t.Errorf("TimingWheelCount() = %d, want 1", em.TimingWheelCount())
	}
}

// TestExpirationManagerRouting_LongTTL tests long TTL items routing to ShardedPQueue.
func TestExpirationManagerRouting_LongTTL(t *testing.T) {
	em := newExpirationManager(func(ce *cacheEntry) {})
	defer em.Stop()

	now := time.Now().UnixNano()
	ce := &cacheEntry{
		absoluteExpiration: now + int64(2*time.Hour), // 2 hours > 1 hour threshold
		priority:           now + int64(2*time.Hour),
	}

	em.Add(ce)

	if atomic.LoadInt32(&ce.expirationSource) != expirationSourcePriorityQueue {
		t.Error("Long TTL items should go to ShardedPQueue")
	}

	stats := em.Stats()
	if stats.SpqAddCount != 1 {
		t.Errorf("SpqAddCount = %d, want 1", stats.SpqAddCount)
	}

	if em.PriorityQueueCount() != 1 {
		t.Errorf("PriorityQueueCount() = %d, want 1", em.PriorityQueueCount())
	}
}

// TestExpirationManagerRouting_Strategy tests strategy override.
func TestExpirationManagerRouting_Strategy(t *testing.T) {
	em := newExpirationManager(func(ce *cacheEntry) {})
	em.Start()
	defer em.Stop()

	now := time.Now().UnixNano()

	// Force all items to TimingWheel
	em.SetStrategy(StrategyTimingWheel)

	ce1 := &cacheEntry{
		absoluteExpiration: now + int64(2*time.Hour), // Long TTL
		priority:           now + int64(2*time.Hour),
	}
	em.Add(ce1)

	if atomic.LoadInt32(&ce1.expirationSource) != expirationSourceTimingWheel {
		t.Error("With StrategyTimingWheel, long TTL items should go to TimingWheel")
	}

	// Force all items to PriorityQueue
	em.SetStrategy(StrategyPriorityQueue)

	ce2 := &cacheEntry{
		absoluteExpiration: now + int64(30*time.Minute), // Short TTL
		priority:           now + int64(30*time.Minute),
	}
	em.Add(ce2)

	if atomic.LoadInt32(&ce2.expirationSource) != expirationSourcePriorityQueue {
		t.Error("With StrategyPriorityQueue, short TTL items should go to PriorityQueue")
	}
}

// TestExpirationManagerSetThreshold tests threshold configuration.
func TestExpirationManagerSetThreshold(t *testing.T) {
	em := newExpirationManager(func(ce *cacheEntry) {})
	em.Start()
	defer em.Stop()

	// Test invalid values
	em.SetThreshold(0)
	if em.Threshold() != DefaultShortTTLThreshold {
		t.Error("SetThreshold(0) should be ignored")
	}

	em.SetThreshold(-time.Second)
	if em.Threshold() != DefaultShortTTLThreshold {
		t.Error("SetThreshold(-1s) should be ignored")
	}

	// Test valid value
	em.SetThreshold(5 * time.Minute)
	if em.Threshold() != 5*time.Minute {
		t.Errorf("Threshold() = %v, want 5m", em.Threshold())
	}

	// Now items with TTL > 5 minutes should go to PriorityQueue
	now := time.Now().UnixNano()
	ce := &cacheEntry{
		absoluteExpiration: now + int64(10*time.Minute), // 10 min > 5 min threshold
		priority:           now + int64(10*time.Minute),
	}
	em.Add(ce)

	if atomic.LoadInt32(&ce.expirationSource) != expirationSourcePriorityQueue {
		t.Error("With 5min threshold, 10min TTL items should go to PriorityQueue")
	}
}

// TestExpirationManagerSetStrategy tests strategy validation.
func TestExpirationManagerSetStrategy(t *testing.T) {
	em := newExpirationManager(func(ce *cacheEntry) {})

	// Test invalid values
	em.SetStrategy(ExpirationStrategy(-1))
	if em.Strategy() != StrategyAuto {
		t.Error("SetStrategy(-1) should be ignored")
	}

	em.SetStrategy(ExpirationStrategy(99))
	if em.Strategy() != StrategyAuto {
		t.Error("SetStrategy(99) should be ignored")
	}

	// Test valid values
	em.SetStrategy(StrategyTimingWheel)
	if em.Strategy() != StrategyTimingWheel {
		t.Errorf("Strategy() = %v, want StrategyTimingWheel", em.Strategy())
	}

	em.SetStrategy(StrategyPriorityQueue)
	if em.Strategy() != StrategyPriorityQueue {
		t.Errorf("Strategy() = %v, want StrategyPriorityQueue", em.Strategy())
	}
}

// TestExpirationManagerRemove tests Remove functionality.
// 設計文件 Section 4.4.2: RemoveCount 計數的是「呼叫 Remove() 的次數」，
// 即使 ce.deleted=1 也會計數。
func TestExpirationManagerRemove(t *testing.T) {
	em := newExpirationManager(func(ce *cacheEntry) {})
	em.Start()
	defer em.Stop()

	now := time.Now().UnixNano()

	// Add item to TimingWheel
	ce1 := &cacheEntry{
		absoluteExpiration: now + int64(30*time.Minute),
		priority:           now + int64(30*time.Minute),
	}
	em.Add(ce1)

	// Remove should succeed on first call (CAS 成功)
	if !em.Remove(ce1) {
		t.Error("First Remove should succeed")
	}

	// Remove should fail on second call (CAS 失敗，但仍計數)
	if em.Remove(ce1) {
		t.Error("Second Remove should fail")
	}

	stats := em.Stats()
	// 設計文件：即使 CAS 失敗也要計數，所以 TwRemoveCount=2
	if stats.TwRemoveCount != 2 {
		t.Errorf("TwRemoveCount = %d, want 2 (both calls counted)", stats.TwRemoveCount)
	}

	// Add item to PriorityQueue
	ce2 := &cacheEntry{
		absoluteExpiration: now + int64(2*time.Hour),
		priority:           now + int64(2*time.Hour),
	}
	em.Add(ce2)

	if !em.Remove(ce2) {
		t.Error("Remove from PriorityQueue should succeed")
	}

	// 第二次 Remove ce2 (CAS 失敗，但仍計數)
	em.Remove(ce2)

	stats = em.Stats()
	// 設計文件：即使 CAS 失敗也要計數，所以 SpqRemoveCount=2
	if stats.SpqRemoveCount != 2 {
		t.Errorf("SpqRemoveCount = %d, want 2 (both calls counted)", stats.SpqRemoveCount)
	}
}

// TestExpirationManagerRemove_Forever tests Remove on Forever items.
func TestExpirationManagerRemove_Forever(t *testing.T) {
	em := newExpirationManager(func(ce *cacheEntry) {})

	ce := &cacheEntry{
		absoluteExpiration: -1, // Forever
	}

	em.Add(ce)

	// Remove should succeed (CAS)
	if !em.Remove(ce) {
		t.Error("Remove on Forever item should succeed (CAS)")
	}

	// Counters should not increase for Forever items
	stats := em.Stats()
	if stats.TwRemoveCount != 0 || stats.SpqRemoveCount != 0 {
		t.Error("Forever items should not increment remove counters")
	}
}

// TestExpirationManagerClear tests Clear functionality.
func TestExpirationManagerClear(t *testing.T) {
	em := newExpirationManager(func(ce *cacheEntry) {})
	em.Start()
	defer em.Stop()

	now := time.Now().UnixNano()

	// Add items to both structures
	for i := 0; i < 5; i++ {
		ce := &cacheEntry{
			absoluteExpiration: now + int64(30*time.Minute),
			priority:           now + int64(30*time.Minute),
		}
		em.Add(ce)
	}

	for i := 0; i < 5; i++ {
		ce := &cacheEntry{
			absoluteExpiration: now + int64(2*time.Hour),
			priority:           now + int64(2*time.Hour),
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
	if stats.TwAddCount != 5 {
		t.Errorf("TwAddCount after Clear = %d, want 5", stats.TwAddCount)
	}
	if stats.SpqAddCount != 5 {
		t.Errorf("SpqAddCount after Clear = %d, want 5", stats.SpqAddCount)
	}
}

// TestExpirationManagerDequeue tests Dequeue from PriorityQueue.
func TestExpirationManagerDequeue(t *testing.T) {
	expiredCount := int64(0)
	em := newExpirationManager(func(ce *cacheEntry) {
		atomic.AddInt64(&expiredCount, 1)
	})

	now := time.Now().UnixNano()

	// Add expired item to PriorityQueue (long TTL but already expired)
	ce := &cacheEntry{
		absoluteExpiration: now - int64(time.Second), // Already expired
		priority:           now - int64(time.Second),
	}
	// Force to PriorityQueue
	em.SetStrategy(StrategyPriorityQueue)
	em.Add(ce)

	// Dequeue should return the item
	result, ok := em.Dequeue(now)
	if !ok {
		t.Error("Dequeue should return expired item")
	}
	if result != ce {
		t.Error("Dequeue should return the correct item")
	}

	stats := em.Stats()
	if stats.SpqExpireCount != 1 {
		t.Errorf("SpqExpireCount = %d, want 1", stats.SpqExpireCount)
	}
}

// TestDequeue_RemoveAfterDequeue_NoDirtyCountIncrease 測試 Dequeue 後 Remove 不會增加 dirtyCount
// 這是修復的 Bug：SPQ 出隊後不應再標記為 dirty
func TestDequeue_RemoveAfterDequeue_NoDirtyCountIncrease(t *testing.T) {
	em := newExpirationManager(func(ce *cacheEntry) {})
	em.SetStrategy(StrategyPriorityQueue)

	now := time.Now().UnixNano()

	// 加入多個過期項目
	entries := make([]*cacheEntry, 10)
	for i := 0; i < 10; i++ {
		entries[i] = &cacheEntry{
			absoluteExpiration: now - int64(time.Second),
			priority:           now - int64(time.Second),
		}
		em.Add(entries[i])
	}

	// 確認初始狀態
	if em.PriorityQueueCount() != 10 {
		t.Errorf("Initial PriorityQueueCount = %d, want 10", em.PriorityQueueCount())
	}

	// Dequeue 所有項目，然後對每個呼叫 Remove
	for i := 0; i < 10; i++ {
		ce, ok := em.Dequeue(now)
		if !ok {
			t.Fatalf("Dequeue %d failed", i)
		}

		// Dequeue 後 expirationSource 應該被設為 None
		source := atomic.LoadInt32(&ce.expirationSource)
		if source != expirationSourceNone {
			t.Errorf("After Dequeue, expirationSource = %d, want %d (None)", source, expirationSourceNone)
		}

		// Remove 應該不會再增加 dirtyCount（因為項目已經不在 heap 中）
		em.Remove(ce)
	}

	// PriorityQueueCount 應該為 0，不應為負數
	pqCount := em.PriorityQueueCount()
	if pqCount != 0 {
		t.Errorf("After Dequeue+Remove, PriorityQueueCount = %d, want 0", pqCount)
	}
	if pqCount < 0 {
		t.Errorf("PriorityQueueCount is negative: %d (dirtyCount exceeded totalCount)", pqCount)
	}
}

// TestDequeue_SlidingUpdate_NotEvictedPrematurely 測試 Sliding 項目更新後不會被錯誤逐出
// 這是修復的 Bug：全域最小值出隊前需要重新校驗是否過期
func TestDequeue_SlidingUpdate_NotEvictedPrematurely(t *testing.T) {
	em := newExpirationManager(func(ce *cacheEntry) {})
	em.SetStrategy(StrategyPriorityQueue)

	now := time.Now().UnixNano()

	// 建立一個 Sliding 項目，設定為「剛好過期」
	ce := &cacheEntry{
		absoluteExpiration: now - int64(time.Millisecond), // 剛好過期
		priority:           now - int64(time.Millisecond),
		lastAccessed:       now - int64(time.Second),
		slidingExpiration:  time.Minute,
		kind:               KindSliding,
	}
	em.Add(ce)

	// 模擬 Read 更新 priority（在 Dequeue 掃描期間可能發生）
	newExp := now + int64(time.Minute)
	ce.setPriority(newExp)
	ce.setAbsoluteExpiration(newExp)
	ce.setLastAccessed(now)

	// Dequeue 應該不會返回這個項目（因為 priority 已經更新為未過期）
	result, ok := em.Dequeue(now)
	if ok && result == ce {
		t.Error("Dequeue should not return the sliding item after priority update")
	}

	// 項目應該仍在佇列中
	if em.PriorityQueueCount() != 1 {
		t.Errorf("PriorityQueueCount = %d, want 1", em.PriorityQueueCount())
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
				// Add items with varying TTLs
				ttl := time.Duration(i%3+1) * time.Hour // 1h, 2h, or 3h
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
	totalAdds := stats.TwAddCount + stats.SpqAddCount
	if totalAdds != int64(numGoroutines*numOperations) {
		t.Errorf("Total adds = %d, want %d", totalAdds, numGoroutines*numOperations)
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

// TestRace_ForgetAndExpire 測試 Forget 與 handleExpired 同時處理同一項目
// 確保 callback 只觸發一次，不會重複扣減計數
func TestRace_ForgetAndExpire(t *testing.T) {
	var callbackCount int64
	cache := newCacheCoherent()
	cache.SetScanFrequency(5 * time.Millisecond)

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

	// Goroutine 2: 觸發過期掃描
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(15 * time.Millisecond)
		cache.flushExpired(time.Now().UTC().UnixNano())
	}()

	wg.Wait()
	time.Sleep(50 * time.Millisecond) // 等待所有非同步 callback 完成

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

// TestRace_KeepAndExpire 測試 keep 替換與 handleExpired 同時處理
func TestRace_KeepAndExpire(t *testing.T) {
	var callbackCount int64
	cache := newCacheCoherent()
	cache.SetScanFrequency(5 * time.Millisecond)

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

	// Goroutine 2: 觸發過期掃描
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			time.Sleep(10 * time.Millisecond)
			cache.flushExpired(time.Now().UTC().UnixNano())
		}
	}()

	wg.Wait()
	time.Sleep(50 * time.Millisecond)

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

// TestRace_ReadSlidingAndExpire 測試 Read 更新 Sliding 與 expireSlot 同時處理
func TestRace_ReadSlidingAndExpire(t *testing.T) {
	cache := newCacheCoherent()
	cache.SetScanFrequency(10 * time.Millisecond)

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

	// Goroutine 2: 觸發過期掃描
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			time.Sleep(10 * time.Millisecond)
			cache.flushExpired(time.Now().UTC().UnixNano())
		}
	}()

	wg.Wait()

	// 驗證 Statistics 的一致性
	stats := cache.Statistics()
	if stats.UsageCount() < 0 {
		t.Error("UsageCount 不應為負數")
	}
}

// TestRace_RescheduleAndRemove 測試 reschedule 與 Remove 的競態
func TestRace_RescheduleAndRemove(t *testing.T) {
	cache := newCacheCoherent()
	cache.SetScanFrequency(10 * time.Millisecond)

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

// TestRace_MultipleExpireHandlers 測試多個 goroutine 同時處理過期項目
// 這個測試驗證當多個 goroutine 同時嘗試處理相同的過期項目時，
// CAS 機制確保每個項目的 callback 只被觸發一次
func TestRace_MultipleExpireHandlers(t *testing.T) {
	var callbackCount int64
	cache := newCacheCoherent()

	// 強制使用 SPQ 策略，這樣 flushExpired 可以處理
	cache.SetExpirationStrategy(StrategyPriorityQueue)
	cache.SetScanFrequency(10 * time.Millisecond)

	// 設定會進入 SPQ 的項目
	for i := 0; i < 50; i++ {
		key := fmt.Sprintf("key-%d", i)
		cache.Set(key, i, EntryOptions{
			TimeToLive: 30 * time.Millisecond,
			PostEvictionCallback: func(k, v any, reason EvictionReason) {
				atomic.AddInt64(&callbackCount, 1)
			},
		})
	}

	var wg sync.WaitGroup

	// 多個 goroutine 同時觸發過期掃描
	for g := 0; g < 5; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(40 * time.Millisecond)
			cache.flushExpired(time.Now().UTC().UnixNano())
		}()
	}

	wg.Wait()
	time.Sleep(100 * time.Millisecond) // 等待異步 callback 完成

	// 每個項目只應觸發一次 callback
	finalCount := atomic.LoadInt64(&callbackCount)
	if finalCount != 50 {
		t.Errorf("Callback 應該觸發 50 次，實際 %d 次", finalCount)
	}
}

// TestCAS_CallbackOnlyOnce_Forget 測試 Forget 與 Expire 只觸發一次 callback
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

	// 同時觸發過期掃描
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(60 * time.Millisecond)
		cache.flushExpired(time.Now().UTC().UnixNano())
	}()

	wg.Wait()
	time.Sleep(50 * time.Millisecond)

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
	cache.SetScanFrequency(5 * time.Millisecond)

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

	// Goroutine 3: 觸發過期掃描
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(25 * time.Millisecond)
		cache.flushExpired(time.Now().UTC().UnixNano())
	}()

	wg.Wait()
	time.Sleep(150 * time.Millisecond)

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
	cache.SetScanFrequency(10 * time.Millisecond)

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

	// pqCount + twCount 應該接近或等於 usageCount
	// （可能有些 Forever 項目不在任何佇列中）
	totalInQueues := stats.PqCount() + stats.TwCount()
	if totalInQueues > stats.UsageCount() {
		t.Errorf("佇列項目數 (%d) 不應超過 usageCount (%d)", totalInQueues, stats.UsageCount())
	}
}
