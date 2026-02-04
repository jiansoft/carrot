package carrot

/*
original from nsq https://github.com/nsqio/nsq/blob/master/internal/pqueue/pqueue.go
*/

import (
	"container/heap"
)

const defaultQueueCapacity = 512

// priorityQueue is a priority queue implemented as a min heap.
// The 0th element is the lowest value (earliest expiration).
type priorityQueue []*cacheEntry

// newPriorityQueue creates a new priority queue with the specified capacity.
func newPriorityQueue(capacity int) *priorityQueue {
	pq := make(priorityQueue, 0, capacity)
	heap.Init(&pq)
	return &pq
}

// Len returns the number of elements in the queue.
func (pq *priorityQueue) Len() int {
	return len(*pq)
}

// Less reports whether the element at index i should sort before the element at index j.
func (pq *priorityQueue) Less(i, j int) bool {
	return (*pq)[i].getPriority() < (*pq)[j].getPriority()
}

// Swap swaps the elements at indexes i and j.
func (pq *priorityQueue) Swap(i, j int) {
	(*pq)[i], (*pq)[j] = (*pq)[j], (*pq)[i]
	(*pq)[i].index = i
	(*pq)[j].index = j
}

// Push adds an element to the queue.
func (pq *priorityQueue) Push(x any) {
	var (
		n = pq.Len()
		c = cap(*pq)
		s = n + 1
	)

	if s > c {
		npq := make(priorityQueue, n, c*2)
		copy(npq, *pq)
		*pq = npq
	}

	*pq = (*pq)[0:s]
	ce := x.(*cacheEntry)
	ce.index = n
	(*pq)[n] = ce
}

// Pop removes and returns the last element from the queue.
func (pq *priorityQueue) Pop() any {
	var (
		n       = pq.Len()
		c       = cap(*pq)
		s       = n - 1
		capHalf = c / 2
	)

	if n < capHalf && c > defaultQueueCapacity {
		npq := make(priorityQueue, n, capHalf)
		copy(npq, *pq)
		*pq = npq
	}

	ce := (*pq)[s]
	(*pq)[s] = nil // avoid memory leak
	ce.index = -1  // for safety
	*pq = (*pq)[0:s]

	return ce
}

// isEmpty returns true if the element amount is zero.
func (pq *priorityQueue) isEmpty() bool {
	return pq.Len() == 0
}

// clear removes all elements and releases references to avoid memory leak.
func (pq *priorityQueue) clear() {
	for i := range *pq {
		(*pq)[i] = nil
	}
	*pq = (*pq)[:0]
}
