package carrot

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// BenchmarkExpire benchmarks the Expire method.
func BenchmarkExpire(b *testing.B) {
	cache := NewCache()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		cache.Expire(i, i, time.Hour)
	}
}

// BenchmarkExpireParallel benchmarks parallel Expire operations.
func BenchmarkExpireParallel(b *testing.B) {
	cache := NewCache()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			cache.Expire(i, i, time.Hour)
			i++
		}
	})
}

// BenchmarkRead benchmarks the Read method.
func BenchmarkRead(b *testing.B) {
	cache := NewCache()
	// Pre-populate cache
	for i := 0; i < 10000; i++ {
		cache.Expire(i, i, time.Hour)
	}
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		cache.Read(i % 10000)
	}
}

// BenchmarkReadParallel benchmarks parallel Read operations.
func BenchmarkReadParallel(b *testing.B) {
	cache := NewCache()
	// Pre-populate cache
	for i := 0; i < 10000; i++ {
		cache.Expire(i, i, time.Hour)
	}
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			cache.Read(i % 10000)
			i++
		}
	})
}

// BenchmarkMixedReadWrite benchmarks mixed read/write operations (90% read, 10% write).
func BenchmarkMixedReadWrite(b *testing.B) {
	cache := NewCache()
	// Pre-populate cache
	for i := 0; i < 10000; i++ {
		cache.Expire(i, i, time.Hour)
	}
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if i%10 == 0 {
				cache.Expire(i, i, time.Hour)
			} else {
				cache.Read(i % 10000)
			}
			i++
		}
	})
}

// BenchmarkGetOrCreate benchmarks the GetOrCreate method.
func BenchmarkGetOrCreate(b *testing.B) {
	cache := NewCache()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		cache.GetOrCreate(i%1000, i, time.Hour)
	}
}

// BenchmarkGetOrCreateParallel benchmarks parallel GetOrCreate operations.
func BenchmarkGetOrCreateParallel(b *testing.B) {
	cache := NewCache()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			cache.GetOrCreate(i%1000, i, time.Hour)
			i++
		}
	})
}

// BenchmarkSlidingExpiration benchmarks sliding expiration items.
func BenchmarkSlidingExpiration(b *testing.B) {
	cache := NewCache()
	// Pre-populate cache with sliding items
	for i := 0; i < 10000; i++ {
		cache.Sliding(i, i, time.Hour)
	}
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		cache.Read(i % 10000)
	}
}

// BenchmarkSlidingExpirationParallel benchmarks parallel sliding expiration operations.
func BenchmarkSlidingExpirationParallel(b *testing.B) {
	cache := NewCache()
	// Pre-populate cache with sliding items
	for i := 0; i < 10000; i++ {
		cache.Sliding(i, i, time.Hour)
	}
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			cache.Read(i % 10000)
			i++
		}
	})
}

// BenchmarkForget benchmarks the Forget method.
func BenchmarkForget(b *testing.B) {
	cache := NewCache()
	// Pre-populate cache
	for i := 0; i < b.N; i++ {
		cache.Expire(i, i, time.Hour)
	}
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		cache.Forget(i)
	}
}

// BenchmarkKeys benchmarks the Keys method.
func BenchmarkKeys(b *testing.B) {
	cache := NewCache()
	// Pre-populate cache
	for i := 0; i < 10000; i++ {
		cache.Expire(i, i, time.Hour)
	}
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		cache.Keys()
	}
}

// BenchmarkTypedCache benchmarks TypedCache operations.
func BenchmarkTypedCache(b *testing.B) {
	cache := NewTypedCache[int, int]()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		cache.Expire(i, i, time.Hour)
	}
}

// BenchmarkTypedCacheRead benchmarks TypedCache read operations.
func BenchmarkTypedCacheRead(b *testing.B) {
	cache := NewTypedCache[int, int]()
	// Pre-populate cache
	for i := 0; i < 10000; i++ {
		cache.Expire(i, i, time.Hour)
	}
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		cache.Read(i % 10000)
	}
}

// BenchmarkHighContention simulates high contention scenario.
func BenchmarkHighContention(b *testing.B) {
	cache := NewCache()
	cache.Expire("hotkey", "value", time.Hour)
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			cache.Read("hotkey")
		}
	})
}

// BenchmarkCompact benchmarks the Compact method.
func BenchmarkCompact(b *testing.B) {
	for _, size := range []int{1000, 10000, 100000} {
		b.Run(fmt.Sprintf("size=%d", size), func(b *testing.B) {
			cache := NewCache()
			for i := 0; i < size; i++ {
				cache.Set(i, i, EntryOptions{
					TimeToLive: time.Hour,
					Priority:   CachePriority(i % 4),
				})
			}
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				cache.Compact(0.1)
			}
		})
	}
}

// TestConcurrencyCorrectness tests correctness under high concurrency.
func TestConcurrencyCorrectness(t *testing.T) {
	cache := NewCache()
	const numGoroutines = 100
	const numOperations = 1000

	var wg sync.WaitGroup

	// Writers
	for g := 0; g < numGoroutines/2; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < numOperations; i++ {
				key := fmt.Sprintf("key-%d-%d", id, i)
				cache.Expire(key, i, time.Hour)
			}
		}(g)
	}

	// Readers
	for g := 0; g < numGoroutines/2; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < numOperations; i++ {
				key := fmt.Sprintf("key-%d-%d", id%25, i)
				cache.Read(key)
			}
		}(g)
	}

	wg.Wait()
}
