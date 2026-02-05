package carrot

import "time"

// TypedCache provides type-safe cache operations using Go generics.
// It wraps a CacheCoherent instance and provides strongly-typed methods.
type TypedCache[K comparable, V any] struct {
	cache *CacheCoherent
}

// NewTypedCache is deprecated: Use New[K, V]() instead.
func NewTypedCache[K comparable, V any]() *TypedCache[K, V] {
	return New[K, V]()
}

// NewTypedCacheFrom is deprecated: Use From[K, V](cache) instead.
func NewTypedCacheFrom[K comparable, V any](cache *CacheCoherent) *TypedCache[K, V] {
	return From[K, V](cache)
}

// Forever stores an item that never expires.
func (tc *TypedCache[K, V]) Forever(key K, val V) {
	tc.cache.Forever(key, val)
}

// Until stores an item that expires at a certain time.
func (tc *TypedCache[K, V]) Until(key K, val V, until time.Time) {
	tc.cache.Until(key, val, until)
}

// Expire stores an item that expires after a period of time.
func (tc *TypedCache[K, V]) Expire(key K, val V, ttl time.Duration) {
	tc.cache.Expire(key, val, ttl)
}

// Delay is deprecated: Use Expire instead.
func (tc *TypedCache[K, V]) Delay(key K, val V, ttl time.Duration) {
	tc.Expire(key, val, ttl)
}

// Sliding stores an item with sliding expiration.
// The item expires after it has not been accessed for the specified duration.
// Each Read will reset the expiration timer.
func (tc *TypedCache[K, V]) Sliding(key K, val V, sliding time.Duration) {
	tc.cache.Sliding(key, val, sliding)
}

// Inactive is deprecated: Use Sliding instead.
func (tc *TypedCache[K, V]) Inactive(key K, val V, inactive time.Duration) {
	tc.Sliding(key, val, inactive)
}

// Set stores an item with the specified options.
func (tc *TypedCache[K, V]) Set(key K, val V, options EntryOptions) {
	tc.cache.Set(key, val, options)
}

// Read returns the value if the key exists and it's not expired.
// Returns the zero value of V and false if not found.
func (tc *TypedCache[K, V]) Read(key K) (V, bool) {
	val, ok := tc.cache.Read(key)
	if !ok {
		var zero V
		return zero, false
	}
	return val.(V), true
}

// Have returns true if the memory has the item and it's not expired.
func (tc *TypedCache[K, V]) Have(key K) bool {
	return tc.cache.Have(key)
}

// Forget removes an item from the cache.
func (tc *TypedCache[K, V]) Forget(key K) {
	tc.cache.Forget(key)
}

// GetOrCreate returns the value if the key exists, otherwise creates a new entry.
func (tc *TypedCache[K, V]) GetOrCreate(key K, val V, ttl time.Duration) (V, bool) {
	result, existed := tc.cache.GetOrCreate(key, val, ttl)
	return result.(V), existed
}

// GetOrCreateFunc returns the value if the key exists, otherwise calls the factory function.
func (tc *TypedCache[K, V]) GetOrCreateFunc(key K, ttl time.Duration, factory func() (V, error)) (V, bool, error) {
	// Wrap the typed factory in an any factory
	anyFactory := func() (any, error) {
		return factory()
	}
	result, existed, err := tc.cache.GetOrCreateFunc(key, ttl, anyFactory)
	if err != nil {
		var zero V
		return zero, false, err
	}
	return result.(V), existed, nil
}

// GetOrCreateWithOptions returns the value if the key exists, otherwise creates with options.
func (tc *TypedCache[K, V]) GetOrCreateWithOptions(key K, val V, options EntryOptions) (V, bool) {
	result, existed := tc.cache.GetOrCreateWithOptions(key, val, options)
	return result.(V), existed
}

// Keys returns all non-expired keys in the cache.
func (tc *TypedCache[K, V]) Keys() []K {
	anyKeys := tc.cache.Keys()
	keys := make([]K, 0, len(anyKeys))
	for _, k := range anyKeys {
		if typedKey, ok := k.(K); ok {
			keys = append(keys, typedKey)
		}
	}
	return keys
}

// Count returns the number of items in the cache.
func (tc *TypedCache[K, V]) Count() int {
	return tc.cache.Count()
}

// Reset removes all items from the cache.
func (tc *TypedCache[K, V]) Reset() {
	tc.cache.Reset()
}

// SetScanFrequency sets the frequency for scanning expired items.
func (tc *TypedCache[K, V]) SetScanFrequency(frequency time.Duration) bool {
	return tc.cache.SetScanFrequency(frequency)
}

// SetSizeLimit sets the maximum size limit for the cache.
func (tc *TypedCache[K, V]) SetSizeLimit(limit int64) {
	tc.cache.SetSizeLimit(limit)
}

// GetSizeLimit returns the current size limit.
func (tc *TypedCache[K, V]) GetSizeLimit() int64 {
	return tc.cache.GetSizeLimit()
}

// GetCurrentSize returns the current total size of all cache entries.
func (tc *TypedCache[K, V]) GetCurrentSize() int64 {
	return tc.cache.GetCurrentSize()
}

// Compact removes a percentage of low-priority items from the cache.
func (tc *TypedCache[K, V]) Compact(percentage float64) {
	tc.cache.Compact(percentage)
}

// Statistics returns a snapshot of cache statistics.
func (tc *TypedCache[K, V]) Statistics() CacheStatistics {
	return tc.cache.Statistics()
}

// SetExpirationStrategy sets the routing strategy for expiration management.
// See ExpirationStrategy for available options.
func (tc *TypedCache[K, V]) SetExpirationStrategy(s ExpirationStrategy) {
	tc.cache.SetExpirationStrategy(s)
}

// SetShortTTLThreshold sets the threshold for routing items to TimingWheel.
// Items with TTL <= threshold go to TimingWheel, others go to ShardedPriorityQueue.
func (tc *TypedCache[K, V]) SetShortTTLThreshold(d time.Duration) {
	tc.cache.SetShortTTLThreshold(d)
}

// ExpirationStats returns statistics from the expiration manager.
func (tc *TypedCache[K, V]) ExpirationStats() ExpirationManagerStats {
	return tc.cache.ExpirationStats()
}

// ShrinkExpirationQueue performs defragmentation on the internal priority queue.
// See CacheCoherent.ShrinkExpirationQueue for details.
func (tc *TypedCache[K, V]) ShrinkExpirationQueue() {
	tc.cache.ShrinkExpirationQueue()
}

// Underlying returns the underlying CacheCoherent instance.
// Use this for advanced operations not covered by the typed API.
func (tc *TypedCache[K, V]) Underlying() *CacheCoherent {
	return tc.cache
}
