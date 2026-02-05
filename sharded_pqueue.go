package carrot

import (
	"container/heap"
	"math"
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
//
// 並發安全：如果在加入佇列後發現 deleted==1（表示在 usage.Store 和 em.Add 之間
// 有其他 goroutine 執行了 Forget/Replace），會使用 CAS 嘗試增加 dirtyCount。
// 這與 removeMarked 共用相同的 CAS 機制，確保 dirtyCount 只增加一次。
func (spq *ShardedPriorityQueue) enqueue(ce *cacheEntry) {
	shard := spq.getShard(ce)
	shard.mu.Lock()
	heap.Push(&shard.pq, ce)
	shard.mu.Unlock()
	atomic.AddInt64(&spq.totalCount, 1)

	// 檢查是否在入隊過程中被刪除
	// 如果 deleted==1，使用 CAS 嘗試標記 dirty（確保只增加一次）
	if atomic.LoadInt32(&ce.deleted) == 1 {
		spq.tryMarkDirty(ce)
	}
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

// removeMarked 處理已被標記刪除的項目（lazy deletion）
// 這個方法假設 ce.deleted 已經被設為 1（由 ExpirationManager.Remove 執行 CAS）
//
// 並發安全：使用 CAS 嘗試標記 dirty。如果在 enqueue 完成前被呼叫，
// enqueue 會在完成後檢測到 deleted==1 並嘗試標記 dirty。
// CAS 機制確保 dirtyCount 只增加一次。
//
// 參數：
//   - ce: 已被標記刪除的快取項目
func (spq *ShardedPriorityQueue) removeMarked(ce *cacheEntry) {
	spq.tryMarkDirty(ce)
}

// tryMarkDirty 嘗試將項目標記為 dirty（增加 dirtyCount）
// 使用 CAS 確保每個項目只增加一次 dirtyCount，防止並發窗口導致重複計數
func (spq *ShardedPriorityQueue) tryMarkDirty(ce *cacheEntry) {
	// 使用 CAS 確保只標記一次
	if atomic.CompareAndSwapInt32(&ce.spqDirtyMarked, 0, 1) {
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

// Dequeue removes and returns the globally minimum expired entry up to the given time limit.
// It scans all shards to find the entry with the smallest priority (earliest expiration).
// It skips entries marked as deleted.
//
// 設計文件要求：「遍歷所有 shard，找出最小 priority 的過期項目」
func (spq *ShardedPriorityQueue) dequeue(limit int64) (*cacheEntry, bool) {
	var minEntry *cacheEntry
	var minShard *pqShard
	minPriority := int64(math.MaxInt64)

	// 第一輪：掃描所有 shard，找出最小的過期項目
	for i := range spq.shards {
		shard := spq.shards[i]
		shard.mu.Lock()

		// 跳過並清理 deleted 項目
		for len(shard.pq) > 0 {
			top := shard.pq[0]
			if atomic.LoadInt32(&top.deleted) == 1 {
				// 物理移除 dirty entry
				heap.Pop(&shard.pq)
				atomic.AddInt64(&shard.dirtyCount, -1)
				atomic.AddInt64(&spq.dirtyCount, -1)
				atomic.AddInt64(&spq.totalCount, -1)
				continue
			}
			break
		}

		// 檢查是否有過期項目
		if len(shard.pq) > 0 {
			top := shard.pq[0]
			if top.getPriority() <= limit && top.getPriority() < minPriority {
				minPriority = top.getPriority()
				minEntry = top
				minShard = shard
			}
		}

		shard.mu.Unlock()
	}

	// 如果找到過期項目，從對應的 shard 移除
	if minEntry != nil && minShard != nil {
		minShard.mu.Lock()
		// Double-check：確認仍是最小、未被刪除、且仍然過期
		// 注意：必須重新檢查 priority <= limit，因為在第一輪掃描與加鎖之間，
		// Sliding 項目可能被 Read 更新了 priority，導致項目尚未過期
		if len(minShard.pq) > 0 && minShard.pq[0] == minEntry {
			if atomic.LoadInt32(&minEntry.deleted) == 0 && minEntry.getPriority() <= limit {
				heap.Pop(&minShard.pq)
				atomic.AddInt64(&spq.totalCount, -1)
				minShard.mu.Unlock()
				return minEntry, true
			}
		}
		minShard.mu.Unlock()
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

// shrink 移除碎片率 > 50% 的 shard 中的已刪除項目
//
// 這是內部記憶體整理方法（私有），與 CacheCoherent.Compact()（業務驅逐）不同。
// 設計文件 Section 4.1.1 強調區分：
// - CacheCoherent.Compact: 業務層級的快取驅逐策略
// - ExpirationManager.Shrink → spq.shrink: 內部記憶體碎片整理
//
// 使用小寫命名以避免與快取驅逐策略混淆。
func (spq *ShardedPriorityQueue) shrink() {
	for _, shard := range spq.shards {
		dirty := atomic.LoadInt64(&shard.dirtyCount)
		if dirty == 0 {
			continue
		}

		shard.mu.Lock()
		total := int64(len(shard.pq))
		// 只有當碎片率 > 50% 時才重建該 shard 的 heap
		// 設計文件 Section 4.1.1: shrink() 觸發條件
		if total > 0 && shard.dirtyCount*100/total > 50 {
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
