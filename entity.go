package carrot

import "time"

const (
	none evictionReason = iota
	// removed Manually
	removed
	// replaced Overwritten
	replaced
	// expired Timed out
	expired
	// TokenExpired Event
	tokenExpired
	// Capacity Overflow
	capacity
)

const (
	Low CachePriority = iota
	Normal
	High
	NeverRemove
)

type (
	evictionReason int
	CachePriority  int

	cacheEntry struct {
		key            any
		value          any
		evictionReason evictionReason
		// created time (unix nanoseconds)
		created int64
		// lives at this point in time. (unix nanoseconds)
		absoluteExpiration int64
		// for PriorityQueue use
		priority int64
		// for sliding scan use
		lastAccessed int64
		// how long a cache entry can be inactive (e.g. not accessed). **only positive**
		slidingExpiration int64
		// for PriorityQueue use
		index int
		// is it expired
		isExpired bool
	}

	CacheEntryOptions struct {
		Size int64
		// how long to live e.g. 5 minutes after now
		TimeToLive time.Duration
		// how long a cache entry can be inactive (e.g. not accessed). **only positive**
		SlidingExpiration time.Duration
		Priority          CachePriority
	}
)

// isSlidingTypeCache returns true if the cache is sliding type
func (ce *cacheEntry) isSlidingTypeCache() bool {
	return ce.slidingExpiration > 0
}

// isSlidingTypeCache returns true if the cache is never expired
func (ce *cacheEntry) isNeverExpired() bool {
	return ce.absoluteExpiration < 0
}

// setExpired Sets the entity expired
func (ce *cacheEntry) setExpired(reason evictionReason) {
	if ce.evictionReason == none {
		ce.evictionReason = reason
	}

	ce.priority = 0
	ce.isExpired = true
}

// checkExpired returns true if the item has expired .
func (ce *cacheEntry) checkExpired(utcNow int64) bool {
	return ce.isExpired || ce.checkForExpiredTime(utcNow)
}

// checkForExpiredTime returns true if the item has expired .
func (ce *cacheEntry) checkForExpiredTime(utcNow int64) bool {
	if ce.absoluteExpiration < 0 && ce.slidingExpiration == 0 {
		// never expired
		return false
	}

	if ce.absoluteExpiration <= utcNow {
		ce.setExpired(expired)
		return true
	}

	if ce.isSlidingTypeCache() && (utcNow-ce.lastAccessed) >= ce.slidingExpiration {
		ce.setExpired(expired)
		return true
	}

	return false
}
