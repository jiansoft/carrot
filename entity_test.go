package carrot

import (
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

	entry.setExpired(reasonExpired)

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

	entry.setExpired(reasonRemoved)

	if !entry.isExpired() {
		t.Error("Entry should be expired after setExpired")
	}

	if entry.getPriority() != 0 {
		t.Errorf("Priority should be 0 after setExpired, got %d", entry.getPriority())
	}

	if entry.evictionReason != reasonRemoved {
		t.Errorf("evictionReason should be reasonRemoved")
	}

	// Second call should not change reason
	entry.setExpired(reasonExpired)
	if entry.evictionReason != reasonRemoved {
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
