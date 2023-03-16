package carrot

import (
	"container/heap"
	"sync"
)

type ConcurrentPriorityQueue struct {
	pq *priorityQueue
	mu *sync.Mutex
}

func newConcurrentPriorityQueue(capacity int) *ConcurrentPriorityQueue {
	pq := newPriorityQueue(capacity)
	heap.Init(pq)
	cpq := ConcurrentPriorityQueue{
		pq: pq,
		mu: new(sync.Mutex),
	}
	return &cpq
}

// enqueue pushes the element x onto the heap.
func (cpq *ConcurrentPriorityQueue) enqueue(ce *cacheEntry) {
	cpq.mu.Lock()
	heap.Push(cpq.pq, ce)
	cpq.mu.Unlock()
}

// dequeue remove and return first element.
func (cpq *ConcurrentPriorityQueue) dequeue(limit int64) (*cacheEntry, bool) {
	cpq.mu.Lock()
	defer cpq.mu.Unlock()

	if cpq.pq.isEmpty() {
		return nil, false
	}

	ce := (*cpq.pq)[0]
	if ce.priority > limit {

		return nil, false
	}

	heap.Remove(cpq.pq, 0)

	return ce, true
}

// update modifies the element in the queue.
func (cpq *ConcurrentPriorityQueue) update(ce *cacheEntry) {
	cpq.mu.Lock()
	heap.Fix(cpq.pq, ce.index)
	cpq.mu.Unlock()
}

// remove removes the element at index i from the heap.
func (cpq *ConcurrentPriorityQueue) remove(ce *cacheEntry) {
	cpq.mu.Lock()
	defer cpq.mu.Unlock()

	l := cpq.pq.Len()
	if l == 0 || l <= ce.index || ce.index < 0 {
		return
	}

	heap.Remove(cpq.pq, ce.index)
}

// eraseMap removes all elements
func (cpq *ConcurrentPriorityQueue) erase() {
	cpq.mu.Lock()
	cpq.pq.clear()
	cpq.mu.Unlock()
}
