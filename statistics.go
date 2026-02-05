package carrot

// CacheStatistics contains cache performance metrics.
type CacheStatistics struct {
	totalMisses int64
	totalHits   int64
	usageCount  int
	pqCount     int // 僅 ShardedPQueue 中的有效項目數
	twCount     int // TimingWheel 中的有效項目數
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
// 注意：語意變更 - 僅包含 ShardedPQueue 中的項目，不包含 TimingWheel
func (cs CacheStatistics) PqCount() int {
	return cs.pqCount
}

// TwCount returns the current number of items in the timing wheel.
func (cs CacheStatistics) TwCount() int {
	return cs.twCount
}
