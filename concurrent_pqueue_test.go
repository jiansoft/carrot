package carrot

import (
	"sync"
	"testing"
)

// TestConcurrentPriorityQueueBasic tests basic operations of ConcurrentPriorityQueue.
func TestConcurrentPriorityQueueBasic(t *testing.T) {
	cpq := newConcurrentPriorityQueue(10)

	if cpq.Count() != 0 {
		t.Error("New queue should be empty")
	}

	// Test enqueue
	entry1 := &cacheEntry{key: "a", priority: 100}
	entry2 := &cacheEntry{key: "b", priority: 50}
	entry3 := &cacheEntry{key: "c", priority: 200}

	cpq.enqueue(entry1)
	cpq.enqueue(entry2)
	cpq.enqueue(entry3)

	if cpq.Count() != 3 {
		t.Errorf("Count() = %d, want 3", cpq.Count())
	}
}

// TestConcurrentPriorityQueueDequeue tests dequeue with limit.
func TestConcurrentPriorityQueueDequeue(t *testing.T) {
	cpq := newConcurrentPriorityQueue(10)

	entry1 := &cacheEntry{key: "a", priority: 100}
	entry2 := &cacheEntry{key: "b", priority: 50}
	entry3 := &cacheEntry{key: "c", priority: 200}

	cpq.enqueue(entry1)
	cpq.enqueue(entry2)
	cpq.enqueue(entry3)

	// Dequeue with limit 60 - should get entry2 (priority 50)
	ce, ok := cpq.dequeue(60)
	if !ok {
		t.Fatal("dequeue should succeed")
	}
	if ce.key != "b" {
		t.Errorf("Expected key 'b', got %v", ce.key)
	}

	// Dequeue with limit 60 - should fail (next is 100)
	_, ok = cpq.dequeue(60)
	if ok {
		t.Error("dequeue should fail when limit is less than min priority")
	}

	// Dequeue with limit 150 - should get entry1 (priority 100)
	ce, ok = cpq.dequeue(150)
	if !ok {
		t.Fatal("dequeue should succeed")
	}
	if ce.key != "a" {
		t.Errorf("Expected key 'a', got %v", ce.key)
	}
}

// TestConcurrentPriorityQueueDequeueEmpty tests dequeue on empty queue.
func TestConcurrentPriorityQueueDequeueEmpty(t *testing.T) {
	cpq := newConcurrentPriorityQueue(10)

	_, ok := cpq.dequeue(1000)
	if ok {
		t.Error("dequeue on empty queue should return false")
	}
}

// TestConcurrentPriorityQueueUpdate tests updating priority.
func TestConcurrentPriorityQueueUpdate(t *testing.T) {
	cpq := newConcurrentPriorityQueue(10)

	entry1 := &cacheEntry{key: "a", priority: 100}
	entry2 := &cacheEntry{key: "b", priority: 50}

	cpq.enqueue(entry1)
	cpq.enqueue(entry2)

	// Update entry1 to have lower priority
	entry1.setPriority(25)
	cpq.update(entry1)

	// Now entry1 should be first
	ce, ok := cpq.dequeue(30)
	if !ok {
		t.Fatal("dequeue should succeed")
	}
	if ce.key != "a" {
		t.Errorf("Expected key 'a' after update, got %v", ce.key)
	}
}

// TestConcurrentPriorityQueueUpdateInvalidIndex tests update with invalid index.
func TestConcurrentPriorityQueueUpdateInvalidIndex(t *testing.T) {
	cpq := newConcurrentPriorityQueue(10)

	entry := &cacheEntry{key: "a", priority: 100, index: -1}

	// Should not panic
	cpq.update(entry)

	entry.index = 999
	cpq.update(entry)
}

// TestConcurrentPriorityQueueRemove tests remove operation.
func TestConcurrentPriorityQueueRemove(t *testing.T) {
	cpq := newConcurrentPriorityQueue(10)

	entry1 := &cacheEntry{key: "a", priority: 100}
	entry2 := &cacheEntry{key: "b", priority: 50}
	entry3 := &cacheEntry{key: "c", priority: 200}

	cpq.enqueue(entry1)
	cpq.enqueue(entry2)
	cpq.enqueue(entry3)

	// Remove entry1
	cpq.remove(entry1)

	if cpq.Count() != 2 {
		t.Errorf("Count() = %d, want 2 after remove", cpq.Count())
	}
}

// TestConcurrentPriorityQueueRemoveInvalidIndex tests remove with invalid index.
func TestConcurrentPriorityQueueRemoveInvalidIndex(t *testing.T) {
	cpq := newConcurrentPriorityQueue(10)

	entry := &cacheEntry{key: "a", priority: 100, index: -1}

	// Should not panic
	cpq.remove(entry)

	entry.index = 999
	cpq.remove(entry)
}

// TestConcurrentPriorityQueueRemoveFromEmpty tests remove on empty queue.
func TestConcurrentPriorityQueueRemoveFromEmpty(t *testing.T) {
	cpq := newConcurrentPriorityQueue(10)

	entry := &cacheEntry{key: "a", priority: 100, index: 0}

	// Should not panic
	cpq.remove(entry)
}

// TestConcurrentPriorityQueueErase tests erase operation.
func TestConcurrentPriorityQueueErase(t *testing.T) {
	cpq := newConcurrentPriorityQueue(10)

	for i := 0; i < 5; i++ {
		cpq.enqueue(&cacheEntry{key: i, priority: int64(i * 10)})
	}

	if cpq.Count() != 5 {
		t.Errorf("Count() = %d, want 5", cpq.Count())
	}

	cpq.erase()

	if cpq.Count() != 0 {
		t.Errorf("Count() = %d after erase, want 0", cpq.Count())
	}
}

// TestConcurrentPriorityQueueConcurrency tests concurrent access.
func TestConcurrentPriorityQueueConcurrency(t *testing.T) {
	cpq := newConcurrentPriorityQueue(100)
	var wg sync.WaitGroup

	// Concurrent enqueue
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			cpq.enqueue(&cacheEntry{key: n, priority: int64(n)})
		}(i)
	}
	wg.Wait()

	if cpq.Count() != 100 {
		t.Errorf("Count() = %d after concurrent enqueue, want 100", cpq.Count())
	}

	// Concurrent dequeue
	dequeued := 0
	var mu sync.Mutex
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, ok := cpq.dequeue(int64(1000)); ok {
				mu.Lock()
				dequeued++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if dequeued != 100 {
		t.Errorf("Dequeued %d items, want 100", dequeued)
	}

	if cpq.Count() != 0 {
		t.Errorf("Count() = %d after all dequeued, want 0", cpq.Count())
	}
}
