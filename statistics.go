package carrot

type cacheStatistics struct {
	totalMisses            int64
	totalHits              int64
	usageNormalEntryCount  int
	usageSlidingEntryCount int
	priorityQueueCount     int
}
