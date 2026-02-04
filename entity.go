package carrot

import (
	"sync/atomic"
	"time"
)

const (
	reasonNone int32 = iota
	// reasonRemoved Manually removed
	reasonRemoved
	// reasonReplaced Overwritten
	reasonReplaced
	// reasonExpired Timed out
	reasonExpired
	// reasonCapacity Overflow
	reasonCapacity
)

// Cache priority levels for cache eviction policies.
const (
	// PriorityLow indicates low priority items that can be evicted first.
	PriorityLow CachePriority = iota
	// PriorityNormal indicates normal priority items.
	PriorityNormal
	// PriorityHigh indicates high priority items that should be retained longer.
	PriorityHigh
	// PriorityNeverRemove indicates items that should never be automatically removed.
	PriorityNeverRemove
)

const (
	// KindSliding indicates a sliding expiration cache entry.
	KindSliding cacheKind = iota
	// KindNormal indicates a normal (absolute) expiration cache entry.
	KindNormal
)

type (
	// CachePriority represents the priority level of a cache entry.
	CachePriority int
	cacheKind     int

	cacheEntry struct {
		key   any
		value any
		// eviction reason (use atomic operations)
		evictionReason int32
		// cache kind (sliding or normal)
		kind cacheKind
		// created time (unix nanoseconds)
		created int64
		// lives at this point in time. (unix nanoseconds, use atomic operations)
		absoluteExpiration int64
		// for PriorityQueue use (use atomic operations)
		priority int64
		// for sliding scan use (use atomic operations)
		lastAccessed int64
		// how long a cache entry can be inactive (e.g. not accessed). **only positive**
		slidingExpiration time.Duration
		// for PriorityQueue use
		index int
		// is it expired (use atomic operations: 0 = false, 1 = true)
		expired int32
	}

	entryOptions struct {
		// This is the time to live, e.g. 5 minutes from now. A negative value means forever.
		// TimeToLive and SlidingExpiration can only choose one to set. If both are set, SlidingExpiration will take precedence.
		TimeToLive time.Duration
		// how long a cache entry can be inactive (e.g. not accessed). **only positive**
		SlidingExpiration time.Duration
	}
)

// isSliding returns true if the cache is sliding kind.
func (ce *cacheEntry) isSliding() bool {
	return ce.kind == KindSliding
}

// isExpired returns true if the cache entry is expired.
func (ce *cacheEntry) isExpired() bool {
	return atomic.LoadInt32(&ce.expired) == 1
}

// getPriority returns the priority value atomically.
func (ce *cacheEntry) getPriority() int64 {
	return atomic.LoadInt64(&ce.priority)
}

// setPriority sets the priority value atomically.
func (ce *cacheEntry) setPriority(p int64) {
	atomic.StoreInt64(&ce.priority, p)
}

// getLastAccessed returns the lastAccessed value atomically.
func (ce *cacheEntry) getLastAccessed() int64 {
	return atomic.LoadInt64(&ce.lastAccessed)
}

// setLastAccessed sets the lastAccessed value atomically.
func (ce *cacheEntry) setLastAccessed(t int64) {
	atomic.StoreInt64(&ce.lastAccessed, t)
}

// getAbsoluteExpiration returns the absoluteExpiration value atomically.
func (ce *cacheEntry) getAbsoluteExpiration() int64 {
	return atomic.LoadInt64(&ce.absoluteExpiration)
}

// setAbsoluteExpiration sets the absoluteExpiration value atomically.
func (ce *cacheEntry) setAbsoluteExpiration(t int64) {
	atomic.StoreInt64(&ce.absoluteExpiration, t)
}

// setExpired sets the entity expired with the given reason.
// Uses atomic compare-and-swap to ensure thread safety.
func (ce *cacheEntry) setExpired(reason int32) {
	atomic.CompareAndSwapInt32(&ce.evictionReason, reasonNone, reason)
	ce.setPriority(0)
	atomic.StoreInt32(&ce.expired, 1)
}

// checkExpired returns true if the item has expired (and set it to expire).
func (ce *cacheEntry) checkExpired(utcNow int64) bool {
	if ce.isExpired() {
		return true
	}

	return ce.checkForExpiredTime(utcNow)
}

// checkForExpiredTime returns true if the item has expired (and set it to expire).
func (ce *cacheEntry) checkForExpiredTime(utcNow int64) bool {
	absExp := ce.getAbsoluteExpiration()
	if absExp < 0 && ce.slidingExpiration == 0 {
		// never expired
		return false
	}

	if ce.isSliding() && ce.slidingExpiration < time.Duration(utcNow-ce.getLastAccessed()) {
		ce.setExpired(reasonExpired)
		return true
	}

	if absExp <= utcNow {
		ce.setExpired(reasonExpired)
		return true
	}

	return false
}
