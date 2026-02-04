package carrot

import (
	"testing"
	"time"
)

// TestNew tests the New function creates independent cache instances.
func TestNew(t *testing.T) {
	cache1 := New()
	cache2 := New()

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
	cache := New()
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
	cache.flushExpired(time.Now().UTC().UnixNano())

	if !cache.Have("forever-key") {
		t.Error("Item with negative duration should never expire")
	}
}

// TestDelayZeroDuration tests Delay with zero duration for never expire.
func TestDelayZeroDuration(t *testing.T) {
	cache := New()
	defer cache.Reset()

	cache.Delay("zero-key", "zero-value", 0)

	if !cache.Have("zero-key") {
		t.Error("Delay with zero duration should store the item as never expire")
	}
}

// TestUntilPastTime tests Until with a time that has already passed.
func TestUntilPastTime(t *testing.T) {
	cache := New()
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

// TestSetScanFrequencyNegative tests SetScanFrequency with negative duration.
func TestSetScanFrequencyNegative(t *testing.T) {
	cache := New()
	defer cache.Reset()

	result := cache.SetScanFrequency(-time.Second)
	if result {
		t.Error("SetScanFrequency should return false for negative duration")
	}

	result = cache.SetScanFrequency(0)
	if result {
		t.Error("SetScanFrequency should return false for zero duration")
	}

	result = cache.SetScanFrequency(time.Second)
	if !result {
		t.Error("SetScanFrequency should return true for positive duration")
	}
}

// TestInactiveNegative tests Inactive with negative duration.
func TestInactiveNegative(t *testing.T) {
	cache := New()
	defer cache.Reset()

	cache.Inactive("test-key", "value", -time.Second)

	if cache.Have("test-key") {
		t.Error("Inactive with negative duration should not store the item")
	}
}

// TestInactiveZero tests Inactive with zero duration.
func TestInactiveZero(t *testing.T) {
	cache := New()
	defer cache.Reset()

	cache.Inactive("test-key", "value", 0)

	if cache.Have("test-key") {
		t.Error("Inactive with zero duration should not store the item")
	}
}

// TestCacheStatisticsGetters tests all getter methods of CacheStatistics.
func TestCacheStatisticsGetters(t *testing.T) {
	cache := New()
	defer cache.Reset()

	// Add some items and perform operations
	cache.Forever("key1", "value1")
	cache.Forever("key2", "value2")

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

	if stats.UsageCount() != 2 {
		t.Errorf("UsageCount() = %d, want 2", stats.UsageCount())
	}

	if stats.PqCount() != 2 {
		t.Errorf("PqCount() = %d, want 2", stats.PqCount())
	}
}

// TestForgetNonexistent tests Forget on a key that doesn't exist.
func TestForgetNonexistent(t *testing.T) {
	cache := New()
	defer cache.Reset()

	// Should not panic
	cache.Forget("nonexistent-key")

	stats := cache.Statistics()
	if stats.UsageCount() != 0 {
		t.Error("UsageCount should be 0 after forgetting nonexistent key")
	}
}

// TestResetClearsEverything tests that Reset clears all items and statistics.
func TestResetClearsEverything(t *testing.T) {
	cache := New()

	cache.Forever("key1", "value1")
	cache.Forever("key2", "value2")
	cache.Read("key1")
	cache.Read("nonexistent")

	cache.Reset()

	stats := cache.Statistics()

	if stats.TotalHits() != 0 {
		t.Error("Reset should clear totalHits")
	}
	if stats.TotalMisses() != 0 {
		t.Error("Reset should clear totalMisses")
	}
	if stats.UsageCount() != 0 {
		t.Error("Reset should clear usageCount")
	}
	if stats.PqCount() != 0 {
		t.Error("Reset should clear pqCount")
	}
}

// TestReplaceExistingKey tests replacing an existing key with new value.
func TestReplaceExistingKey(t *testing.T) {
	cache := New()
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
