package carrot

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jiansoft/robin"
)

func Test_CacheCoherent(t *testing.T) {
	tests := []struct {
		memoryCache *CacheCoherent
		name        string
		loop        int
		want        int
	}{
		{newCacheCoherent(), "1", 1024, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for i := 0; i < tt.loop; i++ {
				key := fmt.Sprintf("QQ-%s-%v", tt.name, i)
				tt.memoryCache.Delay(key, key, 1*time.Hour)

				yes := tt.memoryCache.Have(key)
				equal(t, yes, true)
				val, ok := tt.memoryCache.Read(key)
				equal(t, ok, true)
				equal(t, val, key)

				tt.memoryCache.Forget(key)
				yes = tt.memoryCache.Have(key)
				equal(t, yes, false)
				_, ok = tt.memoryCache.Read(key)
				equal(t, ok, false)
			}
			s := tt.memoryCache.Statistics()
			t.Logf("Statistics %+v", s)
			equal(t, s.usageCount, 0)
			// 所有項目都被 Forget 了，pqCount 和 twCount 都應該是 0
			equal(t, s.pqCount+s.twCount, 0)
			tt.memoryCache.Reset()

			tt.memoryCache.Forget("noKey")
			_, ok := tt.memoryCache.Read("noKey")
			equal(t, ok, false)
			_, ok = tt.memoryCache.loadCacheEntryFromUsage("noKey")
			equal(t, ok, false)

			tt.memoryCache.Delay(1, 1, 1*time.Hour)
			yes := tt.memoryCache.Have(1)
			equal(t, yes, true)
			val, _ := tt.memoryCache.Read(1)
			equal(t, val, 1)

			s = tt.memoryCache.Statistics()
			t.Logf("Statistics %+v", s)
			equal(t, s.usageCount, tt.want)
			// 1 小時 TTL 項目會進入 TimingWheel（TTL <= 閾值）
			// usageCount 應該等於 pqCount + twCount
			equal(t, s.usageCount, s.pqCount+s.twCount)
		})
	}
}

func Test_DataRace(t *testing.T) {
	tests := []struct {
		memoryCache *CacheCoherent
		name        string
		loop        int
	}{
		{newCacheCoherent(), "1", 1024000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.memoryCache.SetScanFrequency(time.Second)
			wg := sync.WaitGroup{}
			wg.Add(1)
			robin.RightNow().Do(func(loop int, m *CacheCoherent, swg *sync.WaitGroup) {
				defer swg.Done()
				for i := 0; i < loop; i++ {
					key := fmt.Sprintf("RightNow-1-%v", i)
					if i%2 == 0 {
						m.Sliding(key, struct{}{}, time.Second)
						Default.Sliding(key, struct{}{}, time.Second)
					} else {
						m.Delay(key, struct{}{}, time.Second)
						Default.Delay(key, struct{}{}, time.Second)
					}
				}

			}, tt.loop, tt.memoryCache, &wg)

			wg.Add(1)
			robin.RightNow().Do(func(loop int, m *CacheCoherent, swg *sync.WaitGroup) {
				defer swg.Done()
				<-time.After(time.Millisecond * 100)
				for i := 0; i < loop; i++ {
					key := fmt.Sprintf("RightNow-1-%v", i)
					_, _ = m.Read(key)
					_, _ = Default.Read(key)
					m.Forget(key)
					Default.Forget(key)
					_, _ = m.Read(key)
					_, _ = Default.Read(key)
				}
			}, tt.loop, tt.memoryCache, &wg)

			wg.Add(1)
			robin.RightNow().Do(func(loop int, m *CacheCoherent, swg *sync.WaitGroup) {
				defer swg.Done()
				<-time.After(time.Millisecond * 500)
				for i := 0; i < loop; i++ {
					key := fmt.Sprintf("RightNow-1-%v", i)
					m.Forget(key)
					Default.Forget(key)
				}
			}, tt.loop, tt.memoryCache, &wg)

			wg.Wait()
			state := tt.memoryCache.Statistics()
			t.Logf("Statistics %+v", state)
			equal(t, tt.loop*2, int(state.totalMisses+state.totalHits))
			tt.memoryCache.Reset()
			Default.Reset()

			wg.Add(1)
			robin.RightNow().Do(keep, tt.loop, tt.memoryCache, &wg, 1)
			<-time.After(time.Millisecond * 10)
			wg.Add(1)
			robin.RightNow().Do(read, t, tt.loop, tt.memoryCache, &wg, 1)

			wg.Add(1)
			robin.RightNow().Do(keep, tt.loop, tt.memoryCache, &wg, 2)
			<-time.After(time.Millisecond * 10)
			wg.Add(1)
			robin.RightNow().Do(read, t, tt.loop, tt.memoryCache, &wg, 2)

			wg.Add(1)
			robin.RightNow().Do(keep, tt.loop, tt.memoryCache, &wg, 3)
			wg.Wait()

			t.Logf("Statistics %+v", tt.memoryCache.Statistics())
			tt.memoryCache.Reset()
			t.Logf("Reset Statistics %+v", tt.memoryCache.Statistics())
		})
	}
}

func keep(loop int, m *CacheCoherent, swg *sync.WaitGroup, index int) {
	defer swg.Done()
	for i := 0; i < loop; i++ {
		key := fmt.Sprintf("QQ-%v-%v", i, index)
		ttl := time.Duration(int64(100+i) * int64(time.Millisecond))
		if i%2 == 0 {
			m.Sliding(key, struct{}{}, ttl)
		} else {
			m.Expire(key, struct{}{}, ttl)
		}
	}
}

func read(t *testing.T, loop int, m *CacheCoherent, swg *sync.WaitGroup, index int) {
	defer swg.Done()
	for i := 0; i < loop; i++ {
		key := fmt.Sprintf("QQ-%v-%v", i, index)
		if _, ok := m.Read(key); ok {
			//t.Logf("%s is find", key)
		}
		m.Forget(key)
	}
}

func Test_Default(t *testing.T) {
	type args struct {
		key         any
		val         any
		valUntil    any
		valDelay    any
		valInactive any
	}
	tests := []struct {
		args args
		name string
	}{
		{name: "one", args: args{
			key: "one", val: "Forever", valUntil: "Until", valDelay: "Delay", valInactive: "Inactive",
		}},
	}
	var timeBase = time.Millisecond * 50

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 使用獨立的 cache 實例，避免跨測試統計累計問題
			// 設計文件 Section 5.3.6: Reset 不重置 totalHits/totalMisses
			cache := newCacheCoherent()
			cache.SetScanFrequency(timeBase * 2)

			//----  Forever ----
			cache.Forever(tt.args.key, tt.args.val)
			if val, ok := cache.Read(tt.args.key); ok {
				equal(t, val, tt.args.val)
			} else {
				t.Fatalf("Forever can't read the key:%v", tt.args.key)
			}
			foreverStat := cache.Statistics()
			t.Logf("Forever Statistics %+v", foreverStat)
			equal(t, int64(1), foreverStat.totalHits)
			equal(t, int64(0), foreverStat.totalMisses)
			cache.Reset()

			//----  Until ----
			cache.Until(tt.args.key, tt.args.valUntil, time.Now().Add(timeBase))
			if val, ok := cache.Read(tt.args.key); ok {
				equal(t, val, tt.args.valUntil)
			} else {
				t.Fatalf("Until can't read the key:%v", tt.args.key)
			}

			<-time.After(timeBase)
			cache.flushExpired(time.Now().UTC().UnixNano())
			if _, ok := cache.Read(tt.args.key); ok {
				t.Fatalf("After calling flushExpired, Until can read the key:%v", tt.args.key)
			}
			untilStat := cache.Statistics()
			t.Logf("Until Statistics %+v", untilStat)
			// 累計值：之前 1 hit + 這次 1 hit = 2 hits; 這次 1 miss
			equal(t, int64(2), untilStat.totalHits)
			equal(t, int64(1), untilStat.totalMisses)
			cache.Reset()

			//----  Delay ----
			cache.Delay(tt.args.key, tt.args.valDelay, timeBase)
			if val, ok := cache.Read(tt.args.key); ok {
				equal(t, val, tt.args.valDelay)
			} else {
				t.Fatalf("Delay can't read the key:%v", tt.args.key)
			}

			<-time.After(timeBase)
			cache.flushExpired(time.Now().UTC().UnixNano())
			if _, ok := cache.Read(tt.args.key); ok {
				t.Fatalf("After flushExpired, the key can be read by Delay:%v", tt.args.key)
			}
			delayStat := cache.Statistics()
			t.Logf("Delay Statistics %+v", delayStat)
			// 累計值：之前 2 hits + 這次 1 hit = 3 hits; 之前 1 miss + 這次 1 miss = 2 misses
			equal(t, int64(3), delayStat.totalHits)
			equal(t, int64(2), delayStat.totalMisses)
			cache.Reset()

			//----  Sliding ----
			cache.Sliding(tt.args.key, tt.args.valInactive, timeBase)
			if val, ok := cache.Read(tt.args.key); ok {
				equal(t, val, tt.args.valInactive)
			} else {
				t.Fatalf("Sliding can't read the key:%v", tt.args.key)
			}

			<-time.After(time.Millisecond * 30)
			if _, ok := cache.Read(tt.args.key); !ok {
				t.Fatalf("after 30 ms Sliding can't read the key:%v", tt.args.key)
			}

			<-time.After(timeBase)
			if _, ok := cache.Read(tt.args.key); ok {
				t.Fatalf("After flushExpired, the key can be read by Sliding:%v", tt.args.key)
			}

			slidingStat := cache.Statistics()
			t.Logf("Sliding Statistics %+v", slidingStat)
			// 累計值：之前 3 hits + 這次 2 hits = 5 hits; 之前 2 misses + 這次 1 miss = 3 misses
			equal(t, int64(5), slidingStat.totalHits)
			equal(t, int64(3), slidingStat.totalMisses)
			cache.Reset()
		})
	}
}
