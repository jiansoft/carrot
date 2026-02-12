package carrot

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestNew tests the New function creates independent cache instances.
func TestNew(t *testing.T) {
	cache1 := NewCache()
	cache2 := NewCache()

	if cache1 == cache2 {
		t.Error("New() should return different instances")
	}

	if cache1 == Default {
		t.Error("New() should return a different instance from Default")
	}

	// Test independence
	cache1.Forever("key1", "value1")
	cache2.Forever("key2", "value2")

	if cache1.Have("key2") {
		t.Error("cache1 should not have key2")
	}
	if cache2.Have("key1") {
		t.Error("cache2 should not have key1")
	}
}

// TestDefault tests the Default singleton instance.
func TestDefaultSingleton(t *testing.T) {
	d1 := Default
	d2 := Default

	if d1 != d2 {
		t.Error("Default should be a singleton")
	}
}

// TestDelayNeverExpire tests Delay with negative duration for never expire.
func TestDelayNeverExpire(t *testing.T) {
	cache := NewCache()
	defer cache.Reset()

	// Test with negative duration (never expire)
	cache.Delay("forever-key", "forever-value", -time.Second)

	val, ok := cache.Read("forever-key")
	if !ok {
		t.Fatal("Delay with negative duration should store the item")
	}
	if val != "forever-value" {
		t.Errorf("got %v, want forever-value", val)
	}

	// Wait and verify it's still there
	time.Sleep(50 * time.Millisecond)

	if !cache.Have("forever-key") {
		t.Error("Item with negative duration should never expire")
	}
}

// TestDelayZeroDuration tests Delay with zero duration for never expire.
func TestDelayZeroDuration(t *testing.T) {
	cache := NewCache()
	defer cache.Reset()

	cache.Delay("zero-key", "zero-value", 0)

	if !cache.Have("zero-key") {
		t.Error("Delay with zero duration should store the item as never expire")
	}
}

// TestUntilPastTime tests Until with a time that has already passed.
func TestUntilPastTime(t *testing.T) {
	cache := NewCache()
	defer cache.Reset()

	// First add an item
	cache.Forever("test-key", "original-value")

	// Then call Until with past time - should remove the item
	pastTime := time.Now().Add(-time.Hour)
	cache.Until("test-key", "new-value", pastTime)

	if cache.Have("test-key") {
		t.Error("Until with past time should remove the existing item")
	}
}

// TestSlidingBasic tests Sliding expiration basic functionality.
func TestSlidingBasic(t *testing.T) {
	cache := NewCache()
	defer cache.Reset()

	cache.Sliding("test-key", "value", time.Hour)

	val, ok := cache.Read("test-key")
	if !ok || val != "value" {
		t.Error("Sliding should store the item")
	}
}

// TestSlidingNegative tests Sliding with negative duration.
func TestSlidingNegative(t *testing.T) {
	cache := NewCache()
	defer cache.Reset()

	cache.Sliding("test-key", "value", -time.Second)

	if cache.Have("test-key") {
		t.Error("Sliding with negative duration should not store the item")
	}
}

// TestSlidingZero tests Sliding with zero duration.
func TestSlidingZero(t *testing.T) {
	cache := NewCache()
	defer cache.Reset()

	cache.Sliding("test-key", "value", 0)

	if cache.Have("test-key") {
		t.Error("Sliding with zero duration should not store the item")
	}
}

// TestInactiveNegative tests Inactive (deprecated) with negative duration.
func TestInactiveNegative(t *testing.T) {
	cache := NewCache()
	defer cache.Reset()

	cache.Inactive("test-key", "value", -time.Second)

	if cache.Have("test-key") {
		t.Error("Inactive with negative duration should not store the item")
	}
}

// TestInactiveZero tests Inactive (deprecated) with zero duration.
func TestInactiveZero(t *testing.T) {
	cache := NewCache()
	defer cache.Reset()

	cache.Inactive("test-key", "value", 0)

	if cache.Have("test-key") {
		t.Error("Inactive with zero duration should not store the item")
	}
}

// TestCacheStatisticsGetters tests all getter methods of CacheStatistics.
func TestCacheStatisticsGetters(t *testing.T) {
	cache := NewCache()
	defer cache.Reset()

	// Add Forever items (not in any queue)
	cache.Forever("key1", "value1")
	cache.Forever("key2", "value2")

	// Add items with TTL (all go to TimingWheel now)
	cache.Expire("key3", "value3", 30*time.Minute)
	cache.Expire("key4", "value4", 2*time.Hour)

	// Hit
	cache.Read("key1")
	cache.Read("key2")

	// Miss
	cache.Read("nonexistent")

	stats := cache.Statistics()

	if stats.TotalHits() != 2 {
		t.Errorf("TotalHits() = %d, want 2", stats.TotalHits())
	}

	if stats.TotalMisses() != 1 {
		t.Errorf("TotalMisses() = %d, want 1", stats.TotalMisses())
	}

	if stats.UsageCount() != 4 {
		t.Errorf("UsageCount() = %d, want 4", stats.UsageCount())
	}

	// Forever items are not in any queue
	// All TTL items go to TimingWheel
	if stats.TwCount() != 2 {
		t.Errorf("TwCount() = %d, want 2", stats.TwCount())
	}
}

// TestForgetNonexistent tests Forget on a key that doesn't exist.
func TestForgetNonexistent(t *testing.T) {
	cache := NewCache()
	defer cache.Reset()

	// Should not panic
	cache.Forget("nonexistent-key")

	stats := cache.Statistics()
	if stats.UsageCount() != 0 {
		t.Error("UsageCount should be 0 after forgetting nonexistent key")
	}
}

// TestResetClearsEverything tests that Reset clears cache items but preserves statistics.
// 設計文件 Section 5.3.6: Reset 只清空快取內容，不重置 totalHits/totalMisses
func TestResetClearsEverything(t *testing.T) {
	cache := NewCache()

	cache.Forever("key1", "value1")
	cache.Forever("key2", "value2")
	cache.Read("key1")
	cache.Read("nonexistent")

	statsBefore := cache.Statistics()

	cache.Reset()

	stats := cache.Statistics()

	// Reset 不應清除 totalHits/totalMisses（設計文件要求）
	if stats.TotalHits() != statsBefore.TotalHits() {
		t.Errorf("Reset should preserve totalHits, got %d want %d", stats.TotalHits(), statsBefore.TotalHits())
	}
	if stats.TotalMisses() != statsBefore.TotalMisses() {
		t.Errorf("Reset should preserve totalMisses, got %d want %d", stats.TotalMisses(), statsBefore.TotalMisses())
	}
	// Reset 應清除 usageCount 和 twCount
	if stats.UsageCount() != 0 {
		t.Error("Reset should clear usageCount")
	}
	if stats.TwCount() != 0 {
		t.Error("Reset should clear twCount")
	}
}

// TestTwCountNeverNegative verifies that TwCount does not go negative
// when removing Forever entries or entries that were never enqueued in the TimingWheel.
func TestTwCountNeverNegative(t *testing.T) {
	cache := NewCache()

	// Case 1: Forever 項目 Forget 後 TwCount 不應為負
	cache.Forever("forever-key", "value")
	stats := cache.Statistics()
	if stats.TwCount() != 0 {
		t.Errorf("Forever item should not be in TW, TwCount = %d", stats.TwCount())
	}

	cache.Forget("forever-key")
	stats = cache.Statistics()
	if stats.TwCount() < 0 {
		t.Errorf("TwCount went negative after Forget Forever: %d", stats.TwCount())
	}
	if stats.TwCount() != 0 {
		t.Errorf("TwCount should be 0 after Forget Forever, got %d", stats.TwCount())
	}

	// Case 2: 多個 Forever 項目全部 Forget
	for i := 0; i < 100; i++ {
		cache.Forever(i, i)
	}
	for i := 0; i < 100; i++ {
		cache.Forget(i)
	}
	stats = cache.Statistics()
	if stats.TwCount() != 0 {
		t.Errorf("TwCount should be 0 after removing all Forever items, got %d", stats.TwCount())
	}

	// Case 3: 混合 Forever 和 TTL 項目
	cache.Forever("f1", "v1")
	cache.Expire("e1", "v1", time.Hour)
	stats = cache.Statistics()
	if stats.TwCount() != 1 {
		t.Errorf("Only TTL item should be in TW, TwCount = %d, want 1", stats.TwCount())
	}
	cache.Forget("f1")
	stats = cache.Statistics()
	if stats.TwCount() != 1 {
		t.Errorf("After Forget Forever, TTL item still in TW, TwCount = %d, want 1", stats.TwCount())
	}
	cache.Forget("e1")
	stats = cache.Statistics()
	if stats.TwCount() != 0 {
		t.Errorf("After Forget all, TwCount = %d, want 0", stats.TwCount())
	}

	cache.Reset()
}

// TestReplaceExistingKey tests replacing an existing key with new value.
func TestReplaceExistingKey(t *testing.T) {
	cache := NewCache()
	defer cache.Reset()

	cache.Forever("key", "original")
	cache.Forever("key", "replaced")

	val, ok := cache.Read("key")
	if !ok {
		t.Fatal("Key should exist")
	}
	if val != "replaced" {
		t.Errorf("Value should be replaced, got %v", val)
	}

	stats := cache.Statistics()
	if stats.UsageCount() != 1 {
		t.Errorf("UsageCount should be 1, got %d", stats.UsageCount())
	}
}

func TestReplaceExistingKeyConcurrent(t *testing.T) {
	cache := NewCache()
	defer cache.Stop()
	defer cache.Reset()

	const goroutines = 32
	const loopsPerGoroutine = 500
	start := make(chan struct{})
	var wg sync.WaitGroup

	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		id := g
		go func() {
			defer wg.Done()
			<-start
			for i := 0; i < loopsPerGoroutine; i++ {
				cache.Expire("same-key", id*loopsPerGoroutine+i, time.Hour)
			}
		}()
	}

	close(start)
	wg.Wait()

	stats := cache.Statistics()
	if stats.UsageCount() != 1 {
		t.Fatalf("UsageCount = %d, want 1", stats.UsageCount())
	}
	if stats.TwCount() != 1 {
		t.Fatalf("TwCount = %d, want 1", stats.TwCount())
	}
}

// TestGetOrCreate tests the GetOrCreate method.
func TestGetOrCreate(t *testing.T) {
	cache := NewCache()
	defer cache.Reset()

	// First call should create
	val, existed := cache.GetOrCreate("key", "value1", time.Hour)
	if existed {
		t.Error("First GetOrCreate should return existed=false")
	}
	if val != "value1" {
		t.Errorf("Value = %v, want 'value1'", val)
	}

	// Second call should return existing
	val, existed = cache.GetOrCreate("key", "value2", time.Hour)
	if !existed {
		t.Error("Second GetOrCreate should return existed=true")
	}
	if val != "value1" {
		t.Errorf("Should return existing value 'value1', got %v", val)
	}
}

func TestGetOrCreateConcurrentAtomic(t *testing.T) {
	cache := NewCache()
	defer cache.Stop()
	defer cache.Reset()

	const goroutines = 128
	start := make(chan struct{})
	var wg sync.WaitGroup
	var createdCount atomic.Int64
	var existedCount atomic.Int64

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		v := i
		go func() {
			defer wg.Done()
			<-start
			_, existed := cache.GetOrCreate("key", v, time.Hour)
			if existed {
				existedCount.Add(1)
			} else {
				createdCount.Add(1)
			}
		}()
	}

	close(start)
	wg.Wait()

	if got := createdCount.Load(); got != 1 {
		t.Fatalf("GetOrCreate should create exactly once, got %d", got)
	}
	if got := existedCount.Load(); got != goroutines-1 {
		t.Fatalf("GetOrCreate should return existing for others, got %d", got)
	}
}

// TestGetOrCreateFunc tests the GetOrCreateFunc method.
func TestGetOrCreateFunc(t *testing.T) {
	cache := NewCache()
	defer cache.Reset()

	callCount := 0
	factory := func() (any, error) {
		callCount++
		return "created-value", nil
	}

	// First call should invoke factory
	val, existed, err := cache.GetOrCreateFunc("key", time.Hour, factory)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if existed {
		t.Error("First call should return existed=false")
	}
	if val != "created-value" {
		t.Errorf("Value = %v, want 'created-value'", val)
	}
	if callCount != 1 {
		t.Errorf("Factory should be called once, got %d", callCount)
	}

	// Second call should not invoke factory
	val, existed, err = cache.GetOrCreateFunc("key", time.Hour, factory)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if !existed {
		t.Error("Second call should return existed=true")
	}
	if val != "created-value" {
		t.Errorf("Should return existing value, got %v", val)
	}
	if callCount != 1 {
		t.Errorf("Factory should still be called only once, got %d", callCount)
	}
}

func TestGetOrCreateFuncConcurrentAtomic(t *testing.T) {
	cache := NewCache()
	defer cache.Stop()
	defer cache.Reset()

	var factoryCalls atomic.Int64
	factory := func() (any, error) {
		factoryCalls.Add(1)
		// 拉長 factory 執行時間，放大並發競態窗口
		time.Sleep(100 * time.Microsecond)
		return "created-value", nil
	}

	const goroutines = 128
	start := make(chan struct{})
	var wg sync.WaitGroup
	var createdCount atomic.Int64
	var existedCount atomic.Int64

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			<-start
			_, existed, err := cache.GetOrCreateFunc("key", time.Hour, factory)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if existed {
				existedCount.Add(1)
			} else {
				createdCount.Add(1)
			}
		}()
	}

	close(start)
	wg.Wait()

	if got := factoryCalls.Load(); got != 1 {
		t.Fatalf("factory should be called exactly once, got %d", got)
	}
	if got := createdCount.Load(); got != 1 {
		t.Fatalf("GetOrCreateFunc should create exactly once, got %d", got)
	}
	if got := existedCount.Load(); got != goroutines-1 {
		t.Fatalf("GetOrCreateFunc should return existing for others, got %d", got)
	}
}

// TestGetOrCreateFuncError tests GetOrCreateFunc with factory error.
func TestGetOrCreateFuncError(t *testing.T) {
	cache := NewCache()
	defer cache.Reset()

	expectedErr := errFactoryFailed
	factory := func() (any, error) {
		return nil, expectedErr
	}

	val, existed, err := cache.GetOrCreateFunc("key", time.Hour, factory)
	if err != expectedErr {
		t.Errorf("Error = %v, want %v", err, expectedErr)
	}
	if existed {
		t.Error("Should return existed=false on error")
	}
	if val != nil {
		t.Errorf("Value should be nil on error, got %v", val)
	}

	// Key should not exist
	if cache.Have("key") {
		t.Error("Key should not exist after factory error")
	}
}

var errFactoryFailed = &testError{msg: "factory failed"}

type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}

// TestGetOrCreateWithOptions tests GetOrCreateWithOptions method.
func TestGetOrCreateWithOptions(t *testing.T) {
	cache := NewCache()
	defer cache.Reset()

	options := EntryOptions{
		TimeToLive: time.Hour,
		Priority:   PriorityHigh,
	}

	// First call should create
	val, existed := cache.GetOrCreateWithOptions("key", "value1", options)
	if existed {
		t.Error("First call should return existed=false")
	}
	if val != "value1" {
		t.Errorf("Value = %v, want 'value1'", val)
	}

	// Second call should return existing
	val, existed = cache.GetOrCreateWithOptions("key", "value2", options)
	if !existed {
		t.Error("Second call should return existed=true")
	}
	if val != "value1" {
		t.Errorf("Should return existing value 'value1', got %v", val)
	}
}

// TestKeys tests the Keys method.
func TestKeys(t *testing.T) {
	cache := NewCache()
	defer cache.Reset()

	cache.Forever("key1", "value1")
	cache.Forever("key2", "value2")
	cache.Forever("key3", "value3")

	keys := cache.Keys()
	if len(keys) != 3 {
		t.Errorf("Keys() returned %d keys, want 3", len(keys))
	}

	keyMap := make(map[any]bool)
	for _, k := range keys {
		keyMap[k] = true
	}

	for _, expected := range []string{"key1", "key2", "key3"} {
		if !keyMap[expected] {
			t.Errorf("Keys() should contain '%s'", expected)
		}
	}
}

// TestCount tests the Count method.
func TestCount(t *testing.T) {
	cache := NewCache()
	defer cache.Reset()

	if cache.Count() != 0 {
		t.Errorf("Empty cache Count() = %d, want 0", cache.Count())
	}

	cache.Forever("key1", "value1")
	cache.Forever("key2", "value2")

	if cache.Count() != 2 {
		t.Errorf("Count() = %d, want 2", cache.Count())
	}

	cache.Forget("key1")

	if cache.Count() != 1 {
		t.Errorf("Count() after Forget = %d, want 1", cache.Count())
	}
}

// TestSizeLimit tests the size limit functionality.
func TestSizeLimit(t *testing.T) {
	cache := NewCache()
	defer cache.Reset()

	cache.SetSizeLimit(100)

	if cache.GetSizeLimit() != 100 {
		t.Errorf("GetSizeLimit() = %d, want 100", cache.GetSizeLimit())
	}

	// Add items with sizes
	cache.Set("a", "value", EntryOptions{Size: 40, TimeToLive: time.Hour, Priority: PriorityLow})
	cache.Set("b", "value", EntryOptions{Size: 40, TimeToLive: time.Hour, Priority: PriorityLow})

	if cache.GetCurrentSize() != 80 {
		t.Errorf("GetCurrentSize() = %d, want 80", cache.GetCurrentSize())
	}

	// Adding more should trigger eviction
	cache.Set("c", "value", EntryOptions{Size: 40, TimeToLive: time.Hour, Priority: PriorityLow})

	// After eviction, size should be <= 100
	if cache.GetCurrentSize() > 100 {
		t.Errorf("GetCurrentSize() = %d, should be <= 100 after eviction", cache.GetCurrentSize())
	}
}

// TestCompact tests the Compact method.
func TestCompact(t *testing.T) {
	cache := NewCache()
	defer cache.Reset()

	// Add items with low priority
	for i := 0; i < 10; i++ {
		cache.Set(i, "value", EntryOptions{
			TimeToLive: time.Hour,
			Priority:   PriorityLow,
		})
	}

	if cache.Count() != 10 {
		t.Errorf("Count() = %d before compact, want 10", cache.Count())
	}

	cache.Compact(0.5) // Remove 50%

	count := cache.Count()
	if count > 5 {
		t.Errorf("Count() = %d after 50%% compact, want <= 5", count)
	}
}

// TestCompactInvalidPercentage tests Compact with invalid percentages.
func TestCompactInvalidPercentage(t *testing.T) {
	cache := NewCache()
	defer cache.Reset()

	cache.Forever("key", "value")

	// Should not panic or do anything
	cache.Compact(0)
	cache.Compact(-0.5)
	cache.Compact(1.5)

	if cache.Count() != 1 {
		t.Error("Compact with invalid percentage should not remove items")
	}
}

// TestPriorityNeverRemove tests that PriorityNeverRemove items are not compacted.
func TestPriorityNeverRemove(t *testing.T) {
	cache := NewCache()
	defer cache.Reset()

	// Add one item with PriorityNeverRemove
	cache.Set("protected", "value", EntryOptions{
		TimeToLive: time.Hour,
		Priority:   PriorityNeverRemove,
	})

	// Add items with low priority
	for i := 0; i < 9; i++ {
		cache.Set(i, "value", EntryOptions{
			TimeToLive: time.Hour,
			Priority:   PriorityLow,
		})
	}

	cache.Compact(1.0) // Try to remove all

	// Protected item should still exist
	if !cache.Have("protected") {
		t.Error("PriorityNeverRemove item should not be removed by Compact")
	}
}

// TestSet tests the Set method with all options.
func TestSet(t *testing.T) {
	cache := NewCache()
	defer cache.Reset()

	var callbackCalled atomic.Bool
	cache.Set("key", "value", EntryOptions{
		TimeToLive: time.Hour,
		Priority:   PriorityHigh,
		Size:       100,
		PostEvictionCallback: func(key, value any, reason EvictionReason) {
			callbackCalled.Store(true)
		},
	})

	if !cache.Have("key") {
		t.Error("Set should store the item")
	}

	if cache.GetCurrentSize() != 100 {
		t.Errorf("GetCurrentSize() = %d, want 100", cache.GetCurrentSize())
	}

	// Replace to trigger callback
	cache.Set("key", "new-value", EntryOptions{TimeToLive: time.Hour})
	time.Sleep(10 * time.Millisecond) // Allow async callback

	if !callbackCalled.Load() {
		t.Error("PostEvictionCallback should be called when item is replaced")
	}
}

// TestExpirationToken tests the ExpirationToken functionality.
func TestExpirationToken(t *testing.T) {
	cache := NewCache()
	defer cache.Reset()

	ctx, cancel := context.WithCancel(context.Background())

	cache.Set("key", "value", EntryOptions{
		TimeToLive:      time.Hour,
		ExpirationToken: ctx,
	})

	// Item should exist before cancellation
	if !cache.Have("key") {
		t.Error("Item should exist before token cancellation")
	}

	// Cancel the token
	cancel()

	// Item should be expired after cancellation
	_, ok := cache.Read("key")
	if ok {
		t.Error("Item should be expired after token cancellation")
	}
}

// TestPostEvictionCallbackReason tests the callback receives correct reason.
func TestPostEvictionCallbackReason(t *testing.T) {
	cache := NewCache()
	defer cache.Reset()

	var receivedReason atomic.Int32
	var receivedKey, receivedValue atomic.Value

	callback := func(key, value any, reason EvictionReason) {
		receivedKey.Store(key)
		receivedValue.Store(value)
		receivedReason.Store(int32(reason))
	}

	// Test EvictionReasonRemoved
	cache.Set("key1", "value1", EntryOptions{
		TimeToLive:           time.Hour,
		PostEvictionCallback: callback,
	})
	cache.Forget("key1")
	time.Sleep(20 * time.Millisecond)

	if EvictionReason(receivedReason.Load()) != EvictionReasonRemoved {
		t.Errorf("Reason = %v, want EvictionReasonRemoved", EvictionReason(receivedReason.Load()))
	}
	if receivedKey.Load() != "key1" {
		t.Errorf("Key = %v, want 'key1'", receivedKey.Load())
	}
	if receivedValue.Load() != "value1" {
		t.Errorf("Value = %v, want 'value1'", receivedValue.Load())
	}

	// Test EvictionReasonReplaced
	cache.Set("key2", "value2", EntryOptions{
		TimeToLive:           time.Hour,
		PostEvictionCallback: callback,
	})
	cache.Set("key2", "value2-new", EntryOptions{TimeToLive: time.Hour})
	time.Sleep(20 * time.Millisecond)

	if EvictionReason(receivedReason.Load()) != EvictionReasonReplaced {
		t.Errorf("Reason = %v, want EvictionReasonReplaced", EvictionReason(receivedReason.Load()))
	}
}

// TestSizeLimitEviction tests size limit triggers eviction.
func TestSizeLimitEviction(t *testing.T) {
	cache := NewCache()
	defer cache.Reset()

	var evictedCount atomic.Int64
	callback := func(key, value any, reason EvictionReason) {
		if reason == EvictionReasonCapacity {
			evictedCount.Add(1)
		}
	}

	cache.SetSizeLimit(50)

	// Add items that exceed size limit
	cache.Set("a", "value", EntryOptions{Size: 30, TimeToLive: time.Hour, Priority: PriorityLow, PostEvictionCallback: callback})
	cache.Set("b", "value", EntryOptions{Size: 30, TimeToLive: time.Hour, Priority: PriorityLow, PostEvictionCallback: callback})

	time.Sleep(20 * time.Millisecond)

	// At least one item should have been evicted
	if evictedCount.Load() == 0 {
		t.Error("Size limit should trigger eviction")
	}
}

// TestPriorityEvictionOrder tests that lower priority items are evicted first.
func TestPriorityEvictionOrder(t *testing.T) {
	cache := NewCache()
	defer cache.Reset()

	cache.SetSizeLimit(60)

	// Add high priority item first
	cache.Set("high", "value", EntryOptions{Size: 30, TimeToLive: time.Hour, Priority: PriorityHigh})

	// Add low priority item
	cache.Set("low", "value", EntryOptions{Size: 30, TimeToLive: time.Hour, Priority: PriorityLow})

	// Add another item to trigger eviction
	cache.Set("new", "value", EntryOptions{Size: 30, TimeToLive: time.Hour, Priority: PriorityNormal})

	// High priority item should still exist, low priority might be evicted
	if !cache.Have("high") {
		t.Error("High priority item should not be evicted before low priority")
	}
}
