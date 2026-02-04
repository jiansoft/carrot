package carrot

import (
	"container/heap"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"
)

// ShardedPriorityQueue is a high-performance sharded priority queue with lazy deletion.
// It reduces lock contention by distributing entries across multiple shards.
type ShardedPriorityQueue struct {
	shards     []*pqShard
	shardCount uint64
	shardMask  uint64
	totalCount int64 // atomic counter for O(1) Count()
	dirtyCount int64 // atomic counter for O(1) ActiveCount()
}

// A pqShard is a single shard containing a heap and its own lock.
type pqShard struct {
	pq         lazyPriorityQueue
	mu         sync.Mutex
	dirtyCount int64 // number of deleted items in heap
}

// A lazyPriorityQueue is a min-heap that supports lazy deletion.
type lazyPriorityQueue []*cacheEntry

// NewShardedPriorityQueue creates a new sharded priority queue.
// The shardCount should be a power of 2 for efficient modulo operation.
func newShardedPriorityQueue() *ShardedPriorityQueue {
	// Use number of CPUs as base, minimum 16 shards
	numShards := runtime.NumCPU() * 4
	if numShards < 16 {
		numShards = 16
	}
	// Round up to next power of 2
	numShards = nextPowerOf2(numShards)

	shards := make([]*pqShard, numShards)
	for i := range shards {
		pq := make(lazyPriorityQueue, 0, 64)
		heap.Init(&pq)
		shards[i] = &pqShard{pq: pq}
	}

	return &ShardedPriorityQueue{
		shards:     shards,
		shardCount: uint64(numShards),
		shardMask:  uint64(numShards - 1),
	}
}

// NextPowerOf2 returns the smallest power of 2 >= n.
func nextPowerOf2(n int) int {
	n--
	n |= n >> 1
	n |= n >> 2
	n |= n >> 4
	n |= n >> 8
	n |= n >> 16
	n++
	return n
}

// GetShard returns the shard for a given entry based on its key hash.
func (spq *ShardedPriorityQueue) getShard(ce *cacheEntry) *pqShard {
	// Use the entry's pointer address as hash for distribution
	hash := uint64(uintptr(unsafe.Pointer(ce)))
	return spq.shards[hash&spq.shardMask]
}

// GetShardByIndex returns the shard by index.
func (spq *ShardedPriorityQueue) getShardByIndex(idx int) *pqShard {
	return spq.shards[uint64(idx)&spq.shardMask]
}

// Enqueue adds an entry to the appropriate shard.
func (spq *ShardedPriorityQueue) enqueue(ce *cacheEntry) {
	shard := spq.getShard(ce)
	shard.mu.Lock()
	heap.Push(&shard.pq, ce)
	shard.mu.Unlock()
	atomic.AddInt64(&spq.totalCount, 1)
}

// Remove marks an entry as deleted (lazy deletion).
// This is O(1) and doesn't require finding the entry in the heap.
func (spq *ShardedPriorityQueue) remove(ce *cacheEntry) {
	// Just mark as deleted - will be cleaned up during dequeue
	if atomic.CompareAndSwapInt32(&ce.deleted, 0, 1) {
		shard := spq.getShard(ce)
		atomic.AddInt64(&shard.dirtyCount, 1)
		atomic.AddInt64(&spq.dirtyCount, 1)
	}
}

// Update re-heapifies the entry's position after priority change.
// For lazy deletion mode, this creates a new entry instead of updating in place.
func (spq *ShardedPriorityQueue) update(ce *cacheEntry) {
	shard := spq.getShard(ce)
	shard.mu.Lock()
	if ce.index >= 0 && ce.index < len(shard.pq) && atomic.LoadInt32(&ce.deleted) == 0 {
		heap.Fix(&shard.pq, ce.index)
	}
	shard.mu.Unlock()
}

// Dequeue removes and returns expired entries up to the given time limit.
// It skips entries marked as deleted.
func (spq *ShardedPriorityQueue) dequeue(limit int64) (*cacheEntry, bool) {
	// Try each shard to find an expired entry
	for i := range spq.shards {
		shard := spq.shards[i]
		shard.mu.Lock()

		for len(shard.pq) > 0 {
			ce := shard.pq[0]

			// Skip deleted entries
			if atomic.LoadInt32(&ce.deleted) == 1 {
				heap.Pop(&shard.pq)
				atomic.AddInt64(&shard.dirtyCount, -1)
				atomic.AddInt64(&spq.dirtyCount, -1)
				atomic.AddInt64(&spq.totalCount, -1)
				continue
			}

			// Check if expired
			if ce.getPriority() > limit {
				shard.mu.Unlock()
				goto nextShard
			}

			heap.Pop(&shard.pq)
			atomic.AddInt64(&spq.totalCount, -1)
			shard.mu.Unlock()
			return ce, true
		}

		shard.mu.Unlock()
	nextShard:
	}

	return nil, false
}

// DequeueFromShard dequeues from a specific shard.
func (spq *ShardedPriorityQueue) dequeueFromShard(shardIdx int, limit int64) (*cacheEntry, bool) {
	shard := spq.shards[shardIdx]
	shard.mu.Lock()
	defer shard.mu.Unlock()

	for len(shard.pq) > 0 {
		ce := shard.pq[0]

		// Skip deleted entries
		if atomic.LoadInt32(&ce.deleted) == 1 {
			heap.Pop(&shard.pq)
			atomic.AddInt64(&shard.dirtyCount, -1)
			atomic.AddInt64(&spq.dirtyCount, -1)
			atomic.AddInt64(&spq.totalCount, -1)
			continue
		}

		// Check if expired
		if ce.getPriority() > limit {
			return nil, false
		}

		heap.Pop(&shard.pq)
		atomic.AddInt64(&spq.totalCount, -1)
		return ce, true
	}

	return nil, false
}

// Erase removes all entries from all shards.
func (spq *ShardedPriorityQueue) erase() {
	for _, shard := range spq.shards {
		shard.mu.Lock()
		// Clear references to avoid memory leak
		for i := range shard.pq {
			shard.pq[i] = nil
		}
		shard.pq = shard.pq[:0]
		shard.dirtyCount = 0
		shard.mu.Unlock()
	}
	atomic.StoreInt64(&spq.totalCount, 0)
	atomic.StoreInt64(&spq.dirtyCount, 0)
}

// Count returns the total number of entries across all shards.
// Note: This includes entries marked for deletion.
// This is O(1) using an atomic counter.
func (spq *ShardedPriorityQueue) Count() int {
	return int(atomic.LoadInt64(&spq.totalCount))
}

// ActiveCount returns the count of non-deleted entries.
// This is O(1) using atomic counters.
func (spq *ShardedPriorityQueue) ActiveCount() int {
	total := atomic.LoadInt64(&spq.totalCount)
	dirty := atomic.LoadInt64(&spq.dirtyCount)
	return int(total - dirty)
}

// ShardCount returns the number of shards.
func (spq *ShardedPriorityQueue) ShardCount() int {
	return int(spq.shardCount)
}

// Compact removes all deleted entries from the heaps.
// Call this periodically if there are many deletions.
func (spq *ShardedPriorityQueue) Compact() {
	for _, shard := range spq.shards {
		if atomic.LoadInt64(&shard.dirtyCount) == 0 {
			continue
		}

		shard.mu.Lock()
		if shard.dirtyCount > 0 {
			// Rebuild the heap without deleted entries
			oldLen := len(shard.pq)
			newPq := make(lazyPriorityQueue, 0, oldLen)
			for _, ce := range shard.pq {
				if atomic.LoadInt32(&ce.deleted) == 0 {
					newPq = append(newPq, ce)
				}
			}
			heap.Init(&newPq)
			removed := int64(oldLen - len(newPq))
			shard.pq = newPq
			shard.dirtyCount = 0
			// Update global counters
			atomic.AddInt64(&spq.totalCount, -removed)
			atomic.AddInt64(&spq.dirtyCount, -removed)
		}
		shard.mu.Unlock()
	}
}

// Len LazyPriorityQueue implements heap.Interface.
func (pq *lazyPriorityQueue) Len() int { return len(*pq) }

func (pq *lazyPriorityQueue) Less(i, j int) bool {
	return (*pq)[i].getPriority() < (*pq)[j].getPriority()
}

func (pq *lazyPriorityQueue) Swap(i, j int) {
	(*pq)[i], (*pq)[j] = (*pq)[j], (*pq)[i]
	(*pq)[i].index = i
	(*pq)[j].index = j
}

func (pq *lazyPriorityQueue) Push(x any) {
	ce := x.(*cacheEntry)
	ce.index = len(*pq)
	*pq = append(*pq, ce)
}

func (pq *lazyPriorityQueue) Pop() any {
	old := *pq
	n := len(old)
	ce := old[n-1]
	old[n-1] = nil // avoid memory leak
	ce.index = -1
	*pq = old[0 : n-1]
	return ce
}
