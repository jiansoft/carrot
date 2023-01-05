package carrot

type CacheStatistics struct {
	totalMisses            int64
	totalHits              int64
	usageNormalEntryCount  int
	usageSlidingEntryCount int
	priorityQueueCount     int
}
