package carrot

import (
	"context"
	"sync/atomic"
	"time"
)

// EvictionReason represents why a cache entry was evicted.
type EvictionReason int32

const (
	// EvictionReasonNone indicates the entry has not been evicted.
	EvictionReasonNone EvictionReason = iota
	// EvictionReasonRemoved indicates the entry was manually removed.
	EvictionReasonRemoved
	// EvictionReasonReplaced indicates the entry was overwritten.
	EvictionReasonReplaced
	// EvictionReasonExpired indicates the entry timed out.
	EvictionReasonExpired
	// EvictionReasonCapacity indicates the entry was removed due to capacity overflow.
	EvictionReasonCapacity
	// EvictionReasonTokenExpired indicates the entry was removed due to token expiration.
	EvictionReasonTokenExpired
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

	// PostEvictionCallback is called after a cache entry is evicted.
	// key: the cache key
	// value: the cached value
	// reason: why the entry was evicted
	PostEvictionCallback func(key any, value any, reason EvictionReason)

	cacheEntry struct {
		key   any
		value any
		// eviction reason (use atomic operations)
		evictionReason int32
		// cache kind (sliding or normal)
		kind cacheKind
		// cache priority for eviction
		cachePriority CachePriority
		// size of the cache entry (for size limit calculation)
		size int64
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
		// for lazy deletion in priority queue (use atomic operations: 0 = active, 1 = deleted)
		deleted int32
		// post eviction callback
		evictionCallback PostEvictionCallback
		// cancellation context for token-based expiration
		cancelCtx context.Context
		// cancel function for the context
		cancelFunc context.CancelFunc
		// for TimingWheel use: which level the entry is in (use atomic operations)
		twLevel int32
		// for TimingWheel use: which slot in the level (use atomic operations)
		twSlot int32
		// for TimingWheel use: intrusive doubly linked list pointers
		twPrev   *cacheEntry
		twNext   *cacheEntry
		twBucket atomic.Pointer[twBucket] // 使用 atomic.Pointer 保護並發訪問
		// for TimingWheel use: epoch version for Flush/Remove race condition prevention
		twEpoch uint64
		// expirationSource 標記項目在哪個資料結構中
		// 0 = 未設定/Forever, 1 = TimingWheel, 2 = ShardedPriorityQueue
		// (use atomic operations)
		expirationSource int32
		// spqDirtyMarked 標記是否已經在 SPQ 中增加了 dirtyCount
		// 用於防止並發窗口導致 dirtyCount 被重複增加
		// 0 = 未標記, 1 = 已標記
		// (use atomic operations with CAS)
		spqDirtyMarked int32
	}

	// EntryOptions represents options for creating a cache entry.
	EntryOptions struct {
		// TimeToLive is the time to live, e.g. 5 minutes from now. A negative value means forever.
		// TimeToLive and SlidingExpiration can only choose one to set. If both are set, SlidingExpiration will take precedence.
		TimeToLive time.Duration
		// SlidingExpiration is how long a cache entry can be inactive (e.g. not accessed). **only positive**
		SlidingExpiration time.Duration
		// Priority is the cache priority for eviction. Default is PriorityNormal.
		Priority CachePriority
		// Size is the size of the cache entry. Used for size limit calculation.
		Size int64
		// PostEvictionCallback is called after the entry is evicted.
		PostEvictionCallback PostEvictionCallback
		// ExpirationToken is a context that can trigger expiration when cancelled.
		ExpirationToken context.Context
	}

	// internal use
	entryOptions = EntryOptions
)

// IsSliding returns true if the cache is sliding kind.
func (ce *cacheEntry) isSliding() bool {
	return ce.kind == KindSliding
}

// IsExpired returns true if the cache entry is expired.
func (ce *cacheEntry) isExpired() bool {
	return atomic.LoadInt32(&ce.expired) == 1
}

// GetPriority returns the priority value atomically.
func (ce *cacheEntry) getPriority() int64 {
	return atomic.LoadInt64(&ce.priority)
}

// SetPriority sets the priority value atomically.
func (ce *cacheEntry) setPriority(p int64) {
	atomic.StoreInt64(&ce.priority, p)
}

// GetLastAccessed returns the lastAccessed value atomically.
func (ce *cacheEntry) getLastAccessed() int64 {
	return atomic.LoadInt64(&ce.lastAccessed)
}

// SetLastAccessed sets the lastAccessed value atomically.
func (ce *cacheEntry) setLastAccessed(t int64) {
	atomic.StoreInt64(&ce.lastAccessed, t)
}

// GetAbsoluteExpiration returns the absoluteExpiration value atomically.
func (ce *cacheEntry) getAbsoluteExpiration() int64 {
	return atomic.LoadInt64(&ce.absoluteExpiration)
}

// SetAbsoluteExpiration sets the absoluteExpiration value atomically.
func (ce *cacheEntry) setAbsoluteExpiration(t int64) {
	atomic.StoreInt64(&ce.absoluteExpiration, t)
}

// SetExpired sets the entity expired with the given reason.
// Uses atomic compare-and-swap to ensure thread safety.
func (ce *cacheEntry) setExpired(reason int32) {
	atomic.CompareAndSwapInt32(&ce.evictionReason, int32(EvictionReasonNone), reason)
	ce.setPriority(0)
	atomic.StoreInt32(&ce.expired, 1)
}

// GetEvictionReason returns the eviction reason.
func (ce *cacheEntry) getEvictionReason() EvictionReason {
	return EvictionReason(atomic.LoadInt32(&ce.evictionReason))
}

// CheckExpired returns true if the item has expired (and set it to expire).
func (ce *cacheEntry) checkExpired(utcNow int64) bool {
	if ce.isExpired() {
		return true
	}

	// Check token expiration
	if ce.cancelCtx != nil {
		select {
		case <-ce.cancelCtx.Done():
			ce.setExpired(int32(EvictionReasonTokenExpired))
			return true
		default:
		}
	}

	return ce.checkForExpiredTime(utcNow)
}

// CheckForExpiredTime returns true if the item has expired (and set it to expire).
func (ce *cacheEntry) checkForExpiredTime(utcNow int64) bool {
	absExp := ce.getAbsoluteExpiration()
	if absExp < 0 && ce.slidingExpiration == 0 {
		// never expired
		return false
	}

	if ce.isSliding() && ce.slidingExpiration < time.Duration(utcNow-ce.getLastAccessed()) {
		ce.setExpired(int32(EvictionReasonExpired))
		return true
	}

	if absExp <= utcNow {
		ce.setExpired(int32(EvictionReasonExpired))
		return true
	}

	return false
}

// InvokeEvictionCallback calls the eviction callback if set.
func (ce *cacheEntry) invokeEvictionCallback() {
	if ce.evictionCallback != nil {
		ce.evictionCallback(ce.key, ce.value, ce.getEvictionReason())
	}
	// Cancel the context if it was created
	if ce.cancelFunc != nil {
		ce.cancelFunc()
	}
}
