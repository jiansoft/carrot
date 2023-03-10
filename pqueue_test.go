package carrot

import (
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

/*func Test_priorityQueue_expired(t *testing.T) {
	tests := []struct {
		pq   *priorityQueue
		name string
	}{
		{name: "qq", pq: newPriorityQueue(13)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var newEntity *cacheEntry

			newEntity = &cacheEntry{key: 1, value: 1, created: 1, lastAccessed: 1, absoluteExpiration: 1, priority: 1}
			tt.pq.enqueue(newEntity)
			newEntity = &cacheEntry{key: 3, value: 3, created: 3, lastAccessed: 3, absoluteExpiration: 3, priority: 3}
			tt.pq.enqueue(newEntity)
			newEntity = &cacheEntry{key: 2, value: 2, created: 2, lastAccessed: 2, absoluteExpiration: 2, priority: 2}
			tt.pq.enqueue(newEntity)

			var loop = 1
			for {
				item, ok := tt.pq.dequeue(2)
				if !ok {
					break
				}
				if item.key != loop {
					log.Fatalf("error loop:%v key:%v loop and key didn't match", loop, item.key)
				}
				log.Printf("dequeue:%+v", item)
				loop++
			}

			tt.pq.dequeue(3)

			*tt.pq = append(*tt.pq,
				&cacheEntry{key: 1, value: 1, created: 1, lastAccessed: 1, absoluteExpiration: 1, priority: 1},
				&cacheEntry{key: 3, value: 3, created: 3, lastAccessed: 3, absoluteExpiration: 3, priority: 3},
				&cacheEntry{key: 2, value: 2, created: 2, lastAccessed: 2, absoluteExpiration: 2, priority: 2})
			tt.pq.expired()
			loop = 1
			for {
				item, ok := tt.pq.dequeue(2)
				if !ok {
					break
				}
				if item.key != loop {
					log.Fatalf("error loop:%v key:%v loop and key didn't match", loop, item.key)
				}
				log.Printf("dequeue:%+v", item)
				loop++
			}
		})
	}
}*/
