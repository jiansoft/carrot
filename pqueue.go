package carrot

/*
original from nsq https://github.com/nsqio/nsq/blob/master/internal/pqueue/pqueue.go
*/

import (
	"container/heap"
)

// this is a priority queue as implemented by a min heap
// the 0th element is the lowest value
type priorityQueue []*cacheEntry

func newPriorityQueue(capacity int) *priorityQueue {
	pq := make(priorityQueue, 0, capacity)
	heap.Init(&pq)
	return &pq
}

func (pq *priorityQueue) Len() int {
	return len(*pq)
}

func (pq *priorityQueue) Less(i, j int) bool {
	return (*pq)[i].priority < (*pq)[j].priority
}

func (pq *priorityQueue) Swap(i, j int) {
	(*pq)[i], (*pq)[j] = (*pq)[j], (*pq)[i]
	(*pq)[i].index = i
	(*pq)[j].index = j
}

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

func (pq *priorityQueue) Pop() any {
	var (
		n       = pq.Len()
		c       = cap(*pq)
		s       = n - 1
		capHalf = c / 2
	)

	if n < capHalf && c > queueCapacity {
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

func (pq *priorityQueue) clear() {
	*pq = (*pq)[:0]
}
