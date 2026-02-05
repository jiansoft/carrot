package carrot

import (
	"errors"
	"testing"
	"time"
)

// TestTypedCacheBasic tests basic typed cache operations.
func TestTypedCacheBasic(t *testing.T) {
	cache := NewTypedCache[string, int]()

	// Test Delay and Read
	cache.Delay("one", 1, time.Hour)
	cache.Delay("two", 2, time.Hour)

	val, ok := cache.Read("one")
	if !ok || val != 1 {
		t.Errorf("Read('one') = %d, %v; want 1, true", val, ok)
	}

	val, ok = cache.Read("two")
	if !ok || val != 2 {
		t.Errorf("Read('two') = %d, %v; want 2, true", val, ok)
	}

	// Test missing key
	val, ok = cache.Read("three")
	if ok || val != 0 {
		t.Errorf("Read('three') = %d, %v; want 0, false", val, ok)
	}
}

// TestTypedCacheForever tests Forever method.
func TestTypedCacheForever(t *testing.T) {
	cache := NewTypedCache[string, string]()

	cache.Forever("key", "value")

	val, ok := cache.Read("key")
	if !ok || val != "value" {
		t.Errorf("Read('key') = %s, %v; want 'value', true", val, ok)
	}
}

// TestTypedCacheUntil tests Until method.
func TestTypedCacheUntil(t *testing.T) {
	cache := NewTypedCache[string, string]()

	// Set to expire in the future
	cache.Until("key", "value", time.Now().Add(time.Hour))

	val, ok := cache.Read("key")
	if !ok || val != "value" {
		t.Errorf("Read('key') = %s, %v; want 'value', true", val, ok)
	}
}

// TestTypedCacheSliding tests Sliding method.
func TestTypedCacheSliding(t *testing.T) {
	cache := NewTypedCache[string, string]()

	cache.Sliding("key", "value", time.Hour)

	val, ok := cache.Read("key")
	if !ok || val != "value" {
		t.Errorf("Read('key') = %s, %v; want 'value', true", val, ok)
	}
}

// TestTypedCacheInactive tests Inactive (deprecated) method.
func TestTypedCacheInactive(t *testing.T) {
	cache := NewTypedCache[string, string]()

	cache.Inactive("key", "value", time.Hour)

	val, ok := cache.Read("key")
	if !ok || val != "value" {
		t.Errorf("Read('key') = %s, %v; want 'value', true", val, ok)
	}
}

// TestTypedCacheSet tests Set method with options.
func TestTypedCacheSet(t *testing.T) {
	cache := NewTypedCache[string, int]()

	cache.Set("key", 42, EntryOptions{
		TimeToLive: time.Hour,
		Priority:   PriorityHigh,
		Size:       100,
	})

	val, ok := cache.Read("key")
	if !ok || val != 42 {
		t.Errorf("Read('key') = %d, %v; want 42, true", val, ok)
	}
}

// TestTypedCacheHave tests Have method.
func TestTypedCacheHave(t *testing.T) {
	cache := NewTypedCache[string, string]()

	if cache.Have("key") {
		t.Error("Have('key') should be false for non-existent key")
	}

	cache.Forever("key", "value")

	if !cache.Have("key") {
		t.Error("Have('key') should be true after setting")
	}
}

// TestTypedCacheForget tests Forget method.
func TestTypedCacheForget(t *testing.T) {
	cache := NewTypedCache[string, string]()

	cache.Forever("key", "value")

	if !cache.Have("key") {
		t.Error("Key should exist before Forget")
	}

	cache.Forget("key")

	if cache.Have("key") {
		t.Error("Key should not exist after Forget")
	}
}

// TestTypedCacheGetOrCreate tests GetOrCreate method.
func TestTypedCacheGetOrCreate(t *testing.T) {
	cache := NewTypedCache[string, int]()

	// First call should create
	val, existed := cache.GetOrCreate("key", 42, time.Hour)
	if existed {
		t.Error("First GetOrCreate should return existed=false")
	}
	if val != 42 {
		t.Errorf("GetOrCreate value = %d, want 42", val)
	}

	// Second call should return existing
	val, existed = cache.GetOrCreate("key", 100, time.Hour)
	if !existed {
		t.Error("Second GetOrCreate should return existed=true")
	}
	if val != 42 {
		t.Errorf("GetOrCreate should return existing value 42, got %d", val)
	}
}

// TestTypedCacheGetOrCreateFunc tests GetOrCreateFunc method.
func TestTypedCacheGetOrCreateFunc(t *testing.T) {
	cache := NewTypedCache[string, int]()
	callCount := 0

	factory := func() (int, error) {
		callCount++
		return 42, nil
	}

	// First call should invoke factory
	val, existed, err := cache.GetOrCreateFunc("key", time.Hour, factory)
	if err != nil {
		t.Errorf("GetOrCreateFunc error = %v, want nil", err)
	}
	if existed {
		t.Error("First GetOrCreateFunc should return existed=false")
	}
	if val != 42 {
		t.Errorf("GetOrCreateFunc value = %d, want 42", val)
	}
	if callCount != 1 {
		t.Errorf("Factory should be called once, got %d", callCount)
	}

	// Second call should not invoke factory
	val, existed, err = cache.GetOrCreateFunc("key", time.Hour, factory)
	if err != nil {
		t.Errorf("GetOrCreateFunc error = %v, want nil", err)
	}
	if !existed {
		t.Error("Second GetOrCreateFunc should return existed=true")
	}
	if val != 42 {
		t.Errorf("GetOrCreateFunc should return existing value 42, got %d", val)
	}
	if callCount != 1 {
		t.Errorf("Factory should still be called only once, got %d", callCount)
	}
}

// TestTypedCacheGetOrCreateFuncError tests GetOrCreateFunc with factory error.
func TestTypedCacheGetOrCreateFuncError(t *testing.T) {
	cache := NewTypedCache[string, int]()
	expectedErr := errors.New("factory error")

	factory := func() (int, error) {
		return 0, expectedErr
	}

	val, existed, err := cache.GetOrCreateFunc("key", time.Hour, factory)
	if err != expectedErr {
		t.Errorf("GetOrCreateFunc error = %v, want %v", err, expectedErr)
	}
	if existed {
		t.Error("GetOrCreateFunc with error should return existed=false")
	}
	if val != 0 {
		t.Errorf("GetOrCreateFunc with error should return zero value, got %d", val)
	}

	// Key should not exist
	if cache.Have("key") {
		t.Error("Key should not exist after factory error")
	}
}

// TestTypedCacheGetOrCreateWithOptions tests GetOrCreateWithOptions method.
func TestTypedCacheGetOrCreateWithOptions(t *testing.T) {
	cache := NewTypedCache[string, int]()

	options := EntryOptions{
		TimeToLive: time.Hour,
		Priority:   PriorityHigh,
	}

	// First call should create
	val, existed := cache.GetOrCreateWithOptions("key", 42, options)
	if existed {
		t.Error("First call should return existed=false")
	}
	if val != 42 {
		t.Errorf("Value = %d, want 42", val)
	}

	// Second call should return existing
	val, existed = cache.GetOrCreateWithOptions("key", 100, options)
	if !existed {
		t.Error("Second call should return existed=true")
	}
	if val != 42 {
		t.Errorf("Should return existing value 42, got %d", val)
	}
}

// TestTypedCacheKeys tests Keys method.
func TestTypedCacheKeys(t *testing.T) {
	cache := NewTypedCache[string, int]()

	cache.Delay("a", 1, time.Hour)
	cache.Delay("b", 2, time.Hour)
	cache.Delay("c", 3, time.Hour)

	keys := cache.Keys()
	if len(keys) != 3 {
		t.Errorf("Keys() returned %d keys, want 3", len(keys))
	}

	keyMap := make(map[string]bool)
	for _, k := range keys {
		keyMap[k] = true
	}

	for _, expected := range []string{"a", "b", "c"} {
		if !keyMap[expected] {
			t.Errorf("Keys() should contain '%s'", expected)
		}
	}
}

// TestTypedCacheCount tests Count method.
func TestTypedCacheCount(t *testing.T) {
	cache := NewTypedCache[string, int]()

	if cache.Count() != 0 {
		t.Errorf("Empty cache Count() = %d, want 0", cache.Count())
	}

	cache.Delay("a", 1, time.Hour)
	cache.Delay("b", 2, time.Hour)

	if cache.Count() != 2 {
		t.Errorf("Count() = %d, want 2", cache.Count())
	}
}

// TestTypedCacheReset tests Reset method.
func TestTypedCacheReset(t *testing.T) {
	cache := NewTypedCache[string, int]()

	cache.Delay("a", 1, time.Hour)
	cache.Delay("b", 2, time.Hour)

	cache.Reset()

	if cache.Count() != 0 {
		t.Errorf("Count() after Reset = %d, want 0", cache.Count())
	}
}

// TestTypedCacheSizeLimit tests size limit functionality.
func TestTypedCacheSizeLimit(t *testing.T) {
	cache := NewTypedCache[string, int]()

	cache.SetSizeLimit(100)

	if cache.GetSizeLimit() != 100 {
		t.Errorf("GetSizeLimit() = %d, want 100", cache.GetSizeLimit())
	}

	cache.Set("a", 1, EntryOptions{Size: 50})
	cache.Set("b", 2, EntryOptions{Size: 50})

	if cache.GetCurrentSize() != 100 {
		t.Errorf("GetCurrentSize() = %d, want 100", cache.GetCurrentSize())
	}
}

// TestTypedCacheCompact tests Compact method.
func TestTypedCacheCompact(t *testing.T) {
	cache := NewTypedCache[int, string]()

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

// TestTypedCacheStatistics tests Statistics method.
func TestTypedCacheStatistics(t *testing.T) {
	cache := NewTypedCache[string, int]()

	cache.Delay("key", 42, time.Hour)
	cache.Read("key")         // hit
	cache.Read("nonexistent") // miss

	stats := cache.Statistics()
	if stats.TotalHits() != 1 {
		t.Errorf("TotalHits() = %d, want 1", stats.TotalHits())
	}
	if stats.TotalMisses() != 1 {
		t.Errorf("TotalMisses() = %d, want 1", stats.TotalMisses())
	}
}

// TestTypedCacheUnderlying tests Underlying method.
func TestTypedCacheUnderlying(t *testing.T) {
	cache := NewTypedCache[string, int]()

	underlying := cache.Underlying()
	if underlying == nil {
		t.Error("Underlying() should not return nil")
	}

	// Operations on underlying should affect typed cache
	underlying.Delay("key", 42, time.Hour)

	val, ok := cache.Read("key")
	if !ok || val != 42 {
		t.Errorf("Read after underlying.Delay = %d, %v; want 42, true", val, ok)
	}
}

// TestNewTypedCacheFrom tests NewTypedCacheFrom.
func TestNewTypedCacheFrom(t *testing.T) {
	base := NewCache()
	base.Delay("key", 42, time.Hour)

	typed := NewTypedCacheFrom[string, int](base)

	val, ok := typed.Read("key")
	if !ok || val != 42 {
		t.Errorf("Read from wrapped cache = %d, %v; want 42, true", val, ok)
	}
}

// TestTypedCacheIntKey tests with int keys.
func TestTypedCacheIntKey(t *testing.T) {
	cache := NewTypedCache[int, string]()

	cache.Delay(1, "one", time.Hour)
	cache.Delay(2, "two", time.Hour)

	val, ok := cache.Read(1)
	if !ok || val != "one" {
		t.Errorf("Read(1) = %s, %v; want 'one', true", val, ok)
	}

	keys := cache.Keys()
	if len(keys) != 2 {
		t.Errorf("Keys() returned %d keys, want 2", len(keys))
	}
}

// TestTypedCacheStructValue tests with struct values.
func TestTypedCacheStructValue(t *testing.T) {
	type User struct {
		ID   int
		Name string
	}

	cache := NewTypedCache[string, User]()

	user := User{ID: 1, Name: "Alice"}
	cache.Delay("user:1", user, time.Hour)

	result, ok := cache.Read("user:1")
	if !ok {
		t.Fatal("Read should succeed")
	}
	if result.ID != 1 || result.Name != "Alice" {
		t.Errorf("Read = %+v, want %+v", result, user)
	}
}
