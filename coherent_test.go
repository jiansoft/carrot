package carrot

import (
	"fmt"
	"log"
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
		{newCacheCoherent(), "1", 1024, 1025},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for i := 0; i < tt.loop; i++ {
				key := fmt.Sprintf("QQ-%s-%v", tt.name, i)
				tt.memoryCache.KeepDelay(key, key, 1*time.Hour)

				yes := tt.memoryCache.Have(key)
				equal(t, yes, true)
				val, ok := tt.memoryCache.Read(key)
				equal(t, ok, true)
				equal(t, val, key)

				tt.memoryCache.Forget(key)
				yes = tt.memoryCache.Have(key)
				equal(t, yes, false)
				val, ok = tt.memoryCache.Read(key)
				equal(t, ok, false)
			}
			tt.memoryCache.Forget("noKey")
			_, ok := tt.memoryCache.Read("noKey")
			equal(t, ok, false)
			_, ok = tt.memoryCache.loadCacheEntryFromUsage("noKey")
			equal(t, ok, false)

			tt.memoryCache.KeepDelay(1, 1, 1*time.Hour)
			yes := tt.memoryCache.Have(1)
			equal(t, yes, true)
			val, _ := tt.memoryCache.Read(1)
			equal(t, val, 1)

			s := tt.memoryCache.statistics()
			t.Logf("statistics %+v", s)
			equal(t, s.usageNormalEntryCount, tt.want)
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
			wg.Add(5)
			robin.RightNow().Do(func(loop int, m *CacheCoherent, swg *sync.WaitGroup) {
				for i := 0; i < loop; i++ {
					key := fmt.Sprintf("RightNow-1-%v", i)
					m.KeepDelay(key, key, time.Hour)
				}
				swg.Done()
			}, tt.loop, tt.memoryCache, &wg)

			robin.RightNow().Do(func(loop int, m *CacheCoherent, swg *sync.WaitGroup) {
				for i := 0; i < loop; i++ {
					key := fmt.Sprintf("RightNow-1-%v", i)
					m.Forget(key)
				}
				swg.Done()
			}, tt.loop, tt.memoryCache, &wg)

			robin.RightNow().Do(func(loop int, m *CacheCoherent, swg *sync.WaitGroup) {
				for i := 0; i < loop; i++ {
					key := fmt.Sprintf("RightNow-1-%v", i)
					_, _ = m.Read(key)
				}
				swg.Done()
			}, tt.loop, Default, &wg)

			robin.RightNow().Do(func(loop int, m *CacheCoherent, swg *sync.WaitGroup) {
				for i := 0; i < loop; i++ {
					key := fmt.Sprintf("RightNow-1-%v", i)
					m.KeepDelay(key, key, 1*time.Hour)
					_ = m.Have(key)
					_, _ = m.Read(key)
					m.Forget(key)
					_ = m.Have(key)
					_, _ = m.Read(key)
				}
				swg.Done()
			}, tt.loop, tt.memoryCache, &wg)

			robin.RightNow().Do(func(loop int, m *CacheCoherent, swg *sync.WaitGroup) {
				for i := 0; i < loop; i++ {
					tt.memoryCache.Reset()
				}
				swg.Done()
			}, tt.loop, tt.memoryCache, &wg)

			wg.Wait()
			t.Logf("statistics %+v", tt.memoryCache.statistics())
			wg.Add(1)
			robin.RightNow().Do(keep, tt.loop, tt.memoryCache, &wg, 1)
			wg.Add(1)
			robin.RightNow().Do(read, tt.loop, tt.memoryCache, &wg, 1)
			wg.Add(1)
			robin.RightNow().Do(keep, tt.loop, tt.memoryCache, &wg, 2)
			wg.Add(1)
			robin.RightNow().Do(read, tt.loop, tt.memoryCache, &wg, 2)
			wg.Add(1)
			robin.RightNow().Do(keep, tt.loop, tt.memoryCache, &wg, 3)
			wg.Wait()

			t.Logf("statistics %+v", tt.memoryCache.statistics())
			tt.memoryCache.Reset()
			t.Logf("Reset statistics %+v", tt.memoryCache.statistics())
		})
	}
}

func keep(loop int, m *CacheCoherent, swg *sync.WaitGroup, index int) {
	for i := 0; i < loop; i++ {
		key := fmt.Sprintf("QQ-%v-%v", i, index)
		if i%2 == 0 {
			m.KeepDelayOrInactive(key, key, time.Duration(int64(10+i)*int64(time.Millisecond)), time.Second)
		} else {
			m.KeepDelay(key, key, time.Duration(int64(10+i)*int64(time.Millisecond)))
		}
	}
	swg.Done()
}

func read(want int, m *CacheCoherent, swg *sync.WaitGroup, index int) {
	for i := 0; i < want; i++ {
		key := fmt.Sprintf("QQ-%v-%v", i, index)
		_, _ = m.Read(key)
		m.Forget(key)
	}
	swg.Done()
}

func TestCacheCoherent_KeepDelayOrInactive(t *testing.T) {
	type args struct {
		key any
		val any
	}
	tests := []struct {
		name string
		args args
	}{
		{name: "one", args: args{
			key: "one",
			val: "one",
		}},
	}
	cc := newCacheCoherent(10)
	cc.SetScanFrequency(100 * time.Millisecond)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			//ttl
			cc.KeepDelayOrInactive(tt.args.key, tt.args.val, 100*time.Millisecond, time.Duration(0))
			if val, ok := cc.Read(tt.args.key); !ok {
				log.Fatalf("KeepDelayOrInactive ttl Fatal")
			} else {
				if v, o := val.(string); !o || v != tt.args.val {
					log.Fatalf("KeepDelayOrInactive ttl Fatal")
				}
			}

			state := cc.statistics()
			//t.Logf("ttl statistics %+v", state)
			equal(t, state.usageNormalEntryCount, 1)
			equal(t, state.priorityQueueCount, 1)
			equal(t, state.totalHits, int64(1))
			equal(t, state.totalMisses, int64(0))
			equal(t, state.usageSlidingEntryCount, 0)

			<-time.After(100 * time.Millisecond)
			cc.Read(tt.args.key)
			<-time.After(100 * time.Millisecond)
			state = cc.statistics()
			//t.Logf("ttl statistics %+v", state)
			equal(t, state.usageNormalEntryCount, 0)
			equal(t, state.priorityQueueCount, 0)
			equal(t, state.totalHits, int64(1))
			equal(t, state.totalMisses, int64(1))
			equal(t, state.usageSlidingEntryCount, 0)

			cc.Reset()

			//inactive
			cc.KeepDelayOrInactive(tt.args.key, tt.args.val, time.Minute, 100*time.Millisecond)
			if val, ok := cc.Read(tt.args.key); !ok {
				log.Fatalf("KeepDelayOrInactive inactive Fatal")
			} else {
				if v, o := val.(string); !o || v != tt.args.val {
					log.Fatalf("KeepDelayOrInactive ttl Fatal")
				}
			}

			state = cc.statistics()
			// t.Logf("inactive statistics %+v", state)
			equal(t, state.usageNormalEntryCount, 0)
			equal(t, state.priorityQueueCount, 0)
			equal(t, state.totalHits, int64(1))
			equal(t, state.totalMisses, int64(0))
			equal(t, state.usageSlidingEntryCount, 1)
			<-time.After(100 * time.Millisecond)
			cc.Read(tt.args.key)
			<-time.After(100 * time.Millisecond)
			state = cc.statistics()
			// t.Logf("inactive statistics %+v", state)
			equal(t, state.usageNormalEntryCount, 0)
			equal(t, state.priorityQueueCount, 0)
			equal(t, state.totalHits, int64(1))
			equal(t, state.totalMisses, int64(1))
			equal(t, state.usageSlidingEntryCount, 0)

		})
	}
}
