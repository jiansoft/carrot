package carrot

import (
	"context"
	"testing"
	"time"
)

// TestCacheEntryIsSliding tests the isSliding method.
func TestCacheEntryIsSliding(t *testing.T) {
	slidingEntry := &cacheEntry{kind: KindSliding}
	normalEntry := &cacheEntry{kind: KindNormal}

	if !slidingEntry.isSliding() {
		t.Error("KindSliding entry should return true for isSliding()")
	}

	if normalEntry.isSliding() {
		t.Error("KindNormal entry should return false for isSliding()")
	}
}

// TestCacheEntryIsExpired tests the isExpired method.
func TestCacheEntryIsExpired(t *testing.T) {
	entry := &cacheEntry{}

	if entry.isExpired() {
		t.Error("New entry should not be expired")
	}

	entry.setExpired(int32(EvictionReasonExpired))

	if !entry.isExpired() {
		t.Error("Entry should be expired after setExpired")
	}
}

// TestCacheEntryPriority tests the getPriority and setPriority methods.
func TestCacheEntryPriority(t *testing.T) {
	entry := &cacheEntry{priority: 100}

	if entry.getPriority() != 100 {
		t.Errorf("getPriority() = %d, want 100", entry.getPriority())
	}

	entry.setPriority(200)
	if entry.getPriority() != 200 {
		t.Errorf("getPriority() = %d after setPriority(200), want 200", entry.getPriority())
	}
}

// TestCacheEntryLastAccessed tests the getLastAccessed and setLastAccessed methods.
func TestCacheEntryLastAccessed(t *testing.T) {
	now := time.Now().UnixNano()
	entry := &cacheEntry{lastAccessed: now}

	if entry.getLastAccessed() != now {
		t.Error("getLastAccessed should return the set value")
	}

	later := now + int64(time.Hour)
	entry.setLastAccessed(later)
	if entry.getLastAccessed() != later {
		t.Error("setLastAccessed should update the value")
	}
}

// TestCacheEntryAbsoluteExpiration tests the getAbsoluteExpiration and setAbsoluteExpiration methods.
func TestCacheEntryAbsoluteExpiration(t *testing.T) {
	now := time.Now().UnixNano()
	entry := &cacheEntry{absoluteExpiration: now}

	if entry.getAbsoluteExpiration() != now {
		t.Error("getAbsoluteExpiration should return the set value")
	}

	later := now + int64(time.Hour)
	entry.setAbsoluteExpiration(later)
	if entry.getAbsoluteExpiration() != later {
		t.Error("setAbsoluteExpiration should update the value")
	}
}

// TestCacheEntrySetExpired tests the setExpired method.
func TestCacheEntrySetExpired(t *testing.T) {
	entry := &cacheEntry{priority: 100}

	entry.setExpired(int32(EvictionReasonRemoved))

	if !entry.isExpired() {
		t.Error("Entry should be expired after setExpired")
	}

	if entry.getPriority() != 0 {
		t.Errorf("Priority should be 0 after setExpired, got %d", entry.getPriority())
	}

	if entry.getEvictionReason() != EvictionReasonRemoved {
		t.Errorf("evictionReason should be EvictionReasonRemoved")
	}

	// Second call should not change reason
	entry.setExpired(int32(EvictionReasonExpired))
	if entry.getEvictionReason() != EvictionReasonRemoved {
		t.Error("setExpired should not change reason if already set")
	}
}

// TestCacheEntryCheckExpired tests the checkExpired method.
func TestCacheEntryCheckExpired(t *testing.T) {
	now := time.Now().UnixNano()

	// Test already expired entry
	expiredEntry := &cacheEntry{expired: 1}
	if !expiredEntry.checkExpired(now) {
		t.Error("Already expired entry should return true")
	}

	// Test not expired entry with future absoluteExpiration
	futureExp := now + int64(time.Hour)
	notExpiredEntry := &cacheEntry{
		kind:               KindNormal,
		absoluteExpiration: futureExp,
		priority:           futureExp,
	}
	if notExpiredEntry.checkExpired(now) {
		t.Error("Entry with future expiration should not be expired")
	}

	// Test never expire entry
	neverExpireEntry := &cacheEntry{
		kind:               KindNormal,
		absoluteExpiration: -1,
		slidingExpiration:  0,
	}
	if neverExpireEntry.checkExpired(now) {
		t.Error("Never expire entry should not be expired")
	}
}

// TestCacheEntryCheckForExpiredTime tests the checkForExpiredTime method.
func TestCacheEntryCheckForExpiredTime(t *testing.T) {
	now := time.Now().UnixNano()

	// Test never expire
	neverExpireEntry := &cacheEntry{
		absoluteExpiration: -1,
		slidingExpiration:  0,
	}
	if neverExpireEntry.checkForExpiredTime(now) {
		t.Error("Never expire entry should not be expired")
	}

	// Test sliding expiration - expired
	slidingExpiredEntry := &cacheEntry{
		kind:               KindSliding,
		absoluteExpiration: now + int64(time.Second),
		slidingExpiration:  time.Millisecond * 100,
		lastAccessed:       now - int64(time.Second),
	}
	if !slidingExpiredEntry.checkForExpiredTime(now) {
		t.Error("Sliding entry with old lastAccessed should be expired")
	}

	// Test sliding expiration - not expired
	slidingNotExpiredEntry := &cacheEntry{
		kind:               KindSliding,
		absoluteExpiration: now + int64(time.Hour),
		slidingExpiration:  time.Hour,
		lastAccessed:       now,
	}
	if slidingNotExpiredEntry.checkForExpiredTime(now) {
		t.Error("Sliding entry with recent lastAccessed should not be expired")
	}

	// Test absolute expiration - expired
	absoluteExpiredEntry := &cacheEntry{
		kind:               KindNormal,
		absoluteExpiration: now - int64(time.Second),
	}
	if !absoluteExpiredEntry.checkForExpiredTime(now) {
		t.Error("Entry with past absoluteExpiration should be expired")
	}

	// Test absolute expiration - not expired
	absoluteNotExpiredEntry := &cacheEntry{
		kind:               KindNormal,
		absoluteExpiration: now + int64(time.Hour),
	}
	if absoluteNotExpiredEntry.checkForExpiredTime(now) {
		t.Error("Entry with future absoluteExpiration should not be expired")
	}
}

// TestCacheEntryTokenExpiration tests context-based token expiration.
func TestCacheEntryTokenExpiration(t *testing.T) {
	now := time.Now().UnixNano()
	ctx, cancel := context.WithCancel(context.Background())

	entry := &cacheEntry{
		kind:               KindNormal,
		absoluteExpiration: now + int64(time.Hour),
		cancelCtx:          ctx,
		cancelFunc:         cancel,
	}

	// Should not be expired initially
	if entry.checkExpired(now) {
		t.Error("Entry should not be expired before token cancellation")
	}

	// Cancel the token
	cancel()

	// Should be expired after token cancellation
	if !entry.checkExpired(now) {
		t.Error("Entry should be expired after token cancellation")
	}

	if entry.getEvictionReason() != EvictionReasonTokenExpired {
		t.Errorf("Eviction reason should be EvictionReasonTokenExpired, got %v", entry.getEvictionReason())
	}
}

// TestCacheEntryInvokeEvictionCallback tests the eviction callback invocation.
func TestCacheEntryInvokeEvictionCallback(t *testing.T) {
	called := false
	var callbackKey, callbackValue any
	var callbackReason EvictionReason

	entry := &cacheEntry{
		key:   "testKey",
		value: "testValue",
		evictionCallback: func(key, value any, reason EvictionReason) {
			called = true
			callbackKey = key
			callbackValue = value
			callbackReason = reason
		},
	}

	entry.setExpired(int32(EvictionReasonExpired))
	entry.invokeEvictionCallback()

	if !called {
		t.Error("Eviction callback should have been called")
	}
	if callbackKey != "testKey" {
		t.Errorf("Callback key = %v, want 'testKey'", callbackKey)
	}
	if callbackValue != "testValue" {
		t.Errorf("Callback value = %v, want 'testValue'", callbackValue)
	}
	if callbackReason != EvictionReasonExpired {
		t.Errorf("Callback reason = %v, want EvictionReasonExpired", callbackReason)
	}
}

// TestCacheEntryInvokeEvictionCallbackNil tests that nil callback doesn't panic.
func TestCacheEntryInvokeEvictionCallbackNil(t *testing.T) {
	entry := &cacheEntry{
		key:              "testKey",
		value:            "testValue",
		evictionCallback: nil,
	}

	// Should not panic
	entry.invokeEvictionCallback()
}
