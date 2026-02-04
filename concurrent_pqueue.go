package carrot

import (
	"container/heap"
	"sync"
)

// ConcurrentPriorityQueue is a thread-safe priority queue.
type ConcurrentPriorityQueue struct {
	pq *priorityQueue
	mu sync.RWMutex
}

// NewConcurrentPriorityQueue creates a new ConcurrentPriorityQueue with the specified capacity.
func newConcurrentPriorityQueue(capacity int) *ConcurrentPriorityQueue {
	pq := newPriorityQueue(capacity)
	cpq := ConcurrentPriorityQueue{
		pq: pq,
	}
	return &cpq
}

// Enqueue pushes the element x onto the heap.
func (cpq *ConcurrentPriorityQueue) enqueue(ce *cacheEntry) {
	cpq.mu.Lock()
	heap.Push(cpq.pq, ce)
	cpq.mu.Unlock()
}

// Dequeue removes and returns the first element if its priority <= limit.
func (cpq *ConcurrentPriorityQueue) dequeue(limit int64) (*cacheEntry, bool) {
	cpq.mu.Lock()
	defer cpq.mu.Unlock()

	if cpq.pq.isEmpty() {
		return nil, false
	}

	ce := (*cpq.pq)[0]
	if ce.getPriority() > limit {
		return nil, false
	}

	heap.Remove(cpq.pq, 0)

	return ce, true
}

// Update modifies the element's position in the queue based on its new priority.
func (cpq *ConcurrentPriorityQueue) update(ce *cacheEntry) {
	cpq.mu.Lock()
	// boundary check to prevent panic
	if ce.index >= 0 && ce.index < cpq.pq.Len() {
		heap.Fix(cpq.pq, ce.index)
	}
	cpq.mu.Unlock()
}

// Remove removes the element from the heap.
func (cpq *ConcurrentPriorityQueue) remove(ce *cacheEntry) {
	cpq.mu.Lock()
	l := cpq.pq.Len()
	if l > 0 && ce.index >= 0 && ce.index < l {
		heap.Remove(cpq.pq, ce.index)
	}
	cpq.mu.Unlock()
}

// Erase removes all elements from the queue.
func (cpq *ConcurrentPriorityQueue) erase() {
	cpq.mu.Lock()
	cpq.pq.clear()
	cpq.mu.Unlock()
}

// Count returns the number of elements in the queue.
func (cpq *ConcurrentPriorityQueue) Count() int {
	cpq.mu.RLock()
	n := cpq.pq.Len()
	cpq.mu.RUnlock()
	return n
}

// Peek returns the minimum priority without removing it.
func (cpq *ConcurrentPriorityQueue) peek() (int64, bool) {
	cpq.mu.RLock()
	defer cpq.mu.RUnlock()

	if cpq.pq.isEmpty() {
		return 0, false
	}

	return (*cpq.pq)[0].getPriority(), true
}
