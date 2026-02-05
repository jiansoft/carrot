package carrot

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestShardedPriorityQueueBasic tests basic operations.
func TestShardedPriorityQueueBasic(t *testing.T) {
	spq := newShardedPriorityQueue()

	if spq.Count() != 0 {
		t.Error("New queue should be empty")
	}

	// Test enqueue
	entry1 := &cacheEntry{key: "a", priority: 100}
	entry2 := &cacheEntry{key: "b", priority: 50}
	entry3 := &cacheEntry{key: "c", priority: 200}

	spq.enqueue(entry1)
	spq.enqueue(entry2)
	spq.enqueue(entry3)

	if spq.Count() != 3 {
		t.Errorf("Count() = %d, want 3", spq.Count())
	}
}

// TestShardedPriorityQueueDequeue tests dequeue with expiration limit.
func TestShardedPriorityQueueDequeue(t *testing.T) {
	spq := newShardedPriorityQueue()

	entries := []*cacheEntry{
		{key: "a", priority: 100},
		{key: "b", priority: 50},
		{key: "c", priority: 200},
		{key: "d", priority: 25},
	}

	for _, e := range entries {
		spq.enqueue(e)
	}

	// Dequeue with limit 60 - should get entries with priority <= 60
	dequeued := 0
	for {
		ce, ok := spq.dequeue(60)
		if !ok {
			break
		}
		if ce.priority > 60 {
			t.Errorf("Dequeued entry with priority %d > 60", ce.priority)
		}
		dequeued++
	}

	if dequeued != 2 { // entries with priority 25 and 50
		t.Errorf("Dequeued %d entries, want 2", dequeued)
	}
}

// TestShardedPriorityQueueLazyDelete tests lazy deletion.
func TestShardedPriorityQueueLazyDelete(t *testing.T) {
	spq := newShardedPriorityQueue()

	entry1 := &cacheEntry{key: "a", priority: 100}
	entry2 := &cacheEntry{key: "b", priority: 50}

	spq.enqueue(entry1)
	spq.enqueue(entry2)

	// Mark entry2 as deleted
	spq.remove(entry2)

	if atomic.LoadInt32(&entry2.deleted) != 1 {
		t.Error("Entry should be marked as deleted")
	}

	// Dequeue should skip deleted entry and return entry1
	ce, ok := spq.dequeue(200)
	if !ok {
		t.Fatal("Dequeue should succeed")
	}
	if ce.key != "a" {
		t.Errorf("Expected key 'a', got %v", ce.key)
	}

	// No more valid entries
	_, ok = spq.dequeue(200)
	if ok {
		t.Error("Dequeue should fail after all entries are dequeued or deleted")
	}
}

// TestShardedPriorityQueueConcurrency tests concurrent operations.
func TestShardedPriorityQueueConcurrency(t *testing.T) {
	spq := newShardedPriorityQueue()
	var wg sync.WaitGroup
	numGoroutines := 100
	numOpsPerGoroutine := 1000

	// Concurrent enqueue
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOpsPerGoroutine; j++ {
				entry := &cacheEntry{
					key:      id*numOpsPerGoroutine + j,
					priority: int64(id*numOpsPerGoroutine + j),
				}
				spq.enqueue(entry)
			}
		}(i)
	}
	wg.Wait()

	expectedCount := numGoroutines * numOpsPerGoroutine
	if spq.Count() != expectedCount {
		t.Errorf("Count() = %d, want %d", spq.Count(), expectedCount)
	}

	// Concurrent dequeue
	dequeued := int64(0)
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				_, ok := spq.dequeue(int64(expectedCount))
				if !ok {
					break
				}
				atomic.AddInt64(&dequeued, 1)
			}
		}()
	}
	wg.Wait()

	if dequeued != int64(expectedCount) {
		t.Errorf("Dequeued %d entries, want %d", dequeued, expectedCount)
	}
}

// TestShardedPriorityQueueErase tests the erase operation.
func TestShardedPriorityQueueErase(t *testing.T) {
	spq := newShardedPriorityQueue()

	for i := 0; i < 100; i++ {
		spq.enqueue(&cacheEntry{key: i, priority: int64(i)})
	}

	spq.erase()

	if spq.Count() != 0 {
		t.Errorf("Count() = %d after erase, want 0", spq.Count())
	}
}

// TestShardedPriorityQueueCompact tests the compact operation.
func TestShardedPriorityQueueCompact(t *testing.T) {
	spq := newShardedPriorityQueue()

	entries := make([]*cacheEntry, 100)
	for i := 0; i < 100; i++ {
		entries[i] = &cacheEntry{key: i, priority: int64(i)}
		spq.enqueue(entries[i])
	}

	// Mark half as deleted
	for i := 0; i < 50; i++ {
		spq.remove(entries[i])
	}

	// Before compact, Count includes deleted entries
	beforeCompact := spq.Count()
	activeBeforeCompact := spq.ActiveCount()

	if activeBeforeCompact != 50 {
		t.Errorf("ActiveCount() = %d before compact, want 50", activeBeforeCompact)
	}

	// Shrink (internal memory defrag)
	spq.shrink()

	// After compact, deleted entries should be removed
	afterCompact := spq.Count()
	if afterCompact > beforeCompact {
		t.Error("Count should not increase after compact")
	}
}

// BenchmarkShardedEnqueue benchmarks enqueue operation.
func BenchmarkShardedEnqueue(b *testing.B) {
	spq := newShardedPriorityQueue()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		spq.enqueue(&cacheEntry{key: i, priority: int64(i)})
	}
}

// BenchmarkShardedEnqueueParallel benchmarks parallel enqueue operations.
func BenchmarkShardedEnqueueParallel(b *testing.B) {
	spq := newShardedPriorityQueue()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			spq.enqueue(&cacheEntry{key: i, priority: int64(i)})
			i++
		}
	})
}

// BenchmarkShardedRemove benchmarks lazy delete operation.
func BenchmarkShardedRemove(b *testing.B) {
	entries := make([]*cacheEntry, b.N)
	spq := newShardedPriorityQueue()
	for i := 0; i < b.N; i++ {
		entries[i] = &cacheEntry{key: i, priority: int64(i)}
		spq.enqueue(entries[i])
	}
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		spq.remove(entries[i])
	}
}

// BenchmarkShardedRemoveParallel benchmarks parallel lazy delete operations.
func BenchmarkShardedRemoveParallel(b *testing.B) {
	entries := make([]*cacheEntry, b.N)
	spq := newShardedPriorityQueue()
	for i := 0; i < b.N; i++ {
		entries[i] = &cacheEntry{key: i, priority: int64(i)}
		spq.enqueue(entries[i])
	}
	b.ResetTimer()

	var idx int64
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			i := atomic.AddInt64(&idx, 1) - 1
			if int(i) < len(entries) {
				spq.remove(entries[i])
			}
		}
	})
}

// BenchmarkOldVsNewRemove compares old and new remove operations.
func BenchmarkOldVsNewRemove(b *testing.B) {
	const size = 10000

	b.Run("OldPQueue", func(b *testing.B) {
		entries := make([]*cacheEntry, size)
		cpq := newConcurrentPriorityQueue(size)
		for i := 0; i < size; i++ {
			entries[i] = &cacheEntry{key: i, priority: int64(i)}
			cpq.enqueue(entries[i])
		}
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			cpq.remove(entries[i%size])
		}
	})

	b.Run("NewShardedPQueue", func(b *testing.B) {
		entries := make([]*cacheEntry, size)
		spq := newShardedPriorityQueue()
		for i := 0; i < size; i++ {
			entries[i] = &cacheEntry{key: i, priority: int64(i)}
			spq.enqueue(entries[i])
		}
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			spq.remove(entries[i%size])
		}
	})
}

// BenchmarkOldVsNewEnqueueParallel compares parallel enqueue performance.
func BenchmarkOldVsNewEnqueueParallel(b *testing.B) {
	b.Run("OldPQueue", func(b *testing.B) {
		cpq := newConcurrentPriorityQueue(1024)
		b.ResetTimer()

		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				cpq.enqueue(&cacheEntry{key: i, priority: int64(i)})
				i++
			}
		})
	})

	b.Run("NewShardedPQueue", func(b *testing.B) {
		spq := newShardedPriorityQueue()
		b.ResetTimer()

		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				spq.enqueue(&cacheEntry{key: i, priority: int64(i)})
				i++
			}
		})
	})
}

// BenchmarkOldVsNewMixed compares mixed operations performance.
func BenchmarkOldVsNewMixed(b *testing.B) {
	b.Run("OldPQueue", func(b *testing.B) {
		cpq := newConcurrentPriorityQueue(10000)
		// Pre-populate
		entries := make([]*cacheEntry, 10000)
		for i := 0; i < 10000; i++ {
			entries[i] = &cacheEntry{key: i, priority: int64(time.Now().UnixNano() + int64(i))}
			cpq.enqueue(entries[i])
		}
		b.ResetTimer()

		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				if i%10 == 0 {
					// 10% writes
					cpq.enqueue(&cacheEntry{key: i + 10000, priority: int64(time.Now().UnixNano())})
				} else if i%10 == 1 {
					// 10% deletes
					cpq.remove(entries[i%10000])
				} else {
					// 80% just count (simulating read)
					cpq.Count()
				}
				i++
			}
		})
	})

	b.Run("NewShardedPQueue", func(b *testing.B) {
		spq := newShardedPriorityQueue()
		// Pre-populate
		entries := make([]*cacheEntry, 10000)
		for i := 0; i < 10000; i++ {
			entries[i] = &cacheEntry{key: i, priority: int64(time.Now().UnixNano() + int64(i))}
			spq.enqueue(entries[i])
		}
		b.ResetTimer()

		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				if i%10 == 0 {
					// 10% writes
					spq.enqueue(&cacheEntry{key: i + 10000, priority: int64(time.Now().UnixNano())})
				} else if i%10 == 1 {
					// 10% deletes
					spq.remove(entries[i%10000])
				} else {
					// 80% just count (simulating read)
					spq.Count()
				}
				i++
			}
		})
	})
}
