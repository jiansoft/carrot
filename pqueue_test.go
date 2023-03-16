package carrot

import (
	"container/heap"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func equal(t *testing.T, got, want any) {
	if !reflect.DeepEqual(got, want) {
		_, file, line, _ := runtime.Caller(1)
		t.Logf("\033[37m%s:%d:\n got: %#v\nwant: %#v\033[39m\n ", filepath.Base(file), line, got, want)
		t.FailNow()
	}
}

func lessThan(t *testing.T, a, b int64) {
	if a > b {
		_, file, line, _ := runtime.Caller(1)
		t.Logf("\033[31m%s:%d:\n a: %#v\b: %#v\033[39m\n ", filepath.Base(file), line, a, b)
		t.FailNow()
	}
}

func TestArray(t *testing.T) {
	var result = make([]int, 0, 10)
	result = append(result, 1)
	t.Logf("%v", result)
}

func Test_PriorityQueue(t *testing.T) {
	tests := []struct {
		pq   *priorityQueue
		name string
	}{
		{name: "qq", pq: newPriorityQueue(13)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			heap.Push(tt.pq, &cacheEntry{key: 4, value: 4, created: 4, lastAccessed: 4, absoluteExpiration: 4, priority: 4})
			heap.Push(tt.pq, &cacheEntry{key: 2, value: 2, created: 2, lastAccessed: 2, absoluteExpiration: 2, priority: 2})
			heap.Push(tt.pq, &cacheEntry{key: 1, value: 1, created: 1, lastAccessed: 1, absoluteExpiration: 1, priority: 1})
			heap.Push(tt.pq, &cacheEntry{key: 3, value: 3, created: 3, lastAccessed: 3, absoluteExpiration: 3, priority: 3})

			lastPriority := heap.Pop(tt.pq).(*cacheEntry).priority
			for i := 0; i < 3; i++ {
				item := heap.Pop(tt.pq)
				equal(t, lastPriority < item.(*cacheEntry).priority, true)
				lastPriority = item.(*cacheEntry).priority
			}
		})
	}
}
