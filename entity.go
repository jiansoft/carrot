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
	PriorityLow CachePriority = iota
	PriorityNormal
	PriorityHigh
	PriorityNeverRemove
)

const (
	KindSliding cacheKind = iota
	KindNormal
)

type (
	evictionReason int
	CachePriority  int
	cacheKind      int

	cacheEntry struct {
		key            any
		value          any
		evictionReason evictionReason
		kind           cacheKind
		// created time (unix nanoseconds)
		created int64
		// lives at this point in time. (unix nanoseconds)
		absoluteExpiration int64
		// for PriorityQueue use
		priority int64
		// for sliding scan use
		lastAccessed int64
		// how long a cache entry can be inactive (e.g. not accessed). **only positive**
		slidingExpiration time.Duration
		// for PriorityQueue use
		index int
		// is it expired
		isExpired bool
	}

	entryOptions struct {
		Size int64
		// This is the time to live, e.g. 5 minutes from now. A negative value means forever.
		// TimeToLive and SlidingExpiration can only choose one to set  if both are set, SlidingExpiration will take precedence.
		TimeToLive time.Duration
		// how long a cache entry can be inactive (e.g. not accessed). **only positive**
		SlidingExpiration time.Duration
		Priority          CachePriority
	}
)

// isSliding returns true if the cache is sliding kind
func (ce *cacheEntry) isSliding() bool {
	return ce.kind == KindSliding
}

// setExpired Sets the entity expired
func (ce *cacheEntry) setExpired(reason evictionReason) {
	if ce.evictionReason == none {
		ce.evictionReason = reason
	}

	ce.priority = 0
	ce.isExpired = true
}

// checkExpired returns true if the item has expired (and set it to expire).
func (ce *cacheEntry) checkExpired(utcNow int64) bool {
	if ce.isExpired {
		return true
	}

	return ce.checkForExpiredTime(utcNow)
}

// checkForExpiredTime returns true if the item has expired (and set it to expire).
func (ce *cacheEntry) checkForExpiredTime(utcNow int64) bool {
	if ce.absoluteExpiration < 0 && ce.slidingExpiration == 0 {
		// never expired
		return false
	}

	if ce.isSliding() && ce.slidingExpiration < time.Duration(utcNow-ce.lastAccessed) {
		ce.setExpired(expired)
		return true
	}

	if ce.absoluteExpiration <= utcNow {
		ce.setExpired(expired)
		return true
	}

	return false
}
