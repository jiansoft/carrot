package carrot

type CacheStatistics struct {
	totalMisses int64
	totalHits   int64
	usageCount  int
	pqCount     int
}
