package carrot

// CacheStatistics contains cache performance metrics.
type CacheStatistics struct {
	totalMisses int64
	totalHits   int64
	usageCount  int
	pqCount     int
}

// TotalMisses returns the total number of cache misses.
func (cs CacheStatistics) TotalMisses() int64 {
	return cs.totalMisses
}

// TotalHits returns the total number of cache hits.
func (cs CacheStatistics) TotalHits() int64 {
	return cs.totalHits
}

// UsageCount returns the current number of items in the cache.
func (cs CacheStatistics) UsageCount() int {
	return cs.usageCount
}

// PqCount returns the current number of items in the priority queue.
func (cs CacheStatistics) PqCount() int {
	return cs.pqCount
}
