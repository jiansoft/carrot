package carrot

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	_ "unsafe" // for go:linkname
)

// ============================================================================
// 改進版時間輪 (Timing Wheel) 實作
// ============================================================================
//
// 核心改進：
// 1. DelayQueue 驅動（事件驅動，消除空跳）
// 2. 侵入式雙向鏈表（O(1) 移除，零分配）
// 3. 動態溢出輪（支援任意長度 TTL）
// 4. 位運算索引（wheelSize 為 2 的冪次方）
// 5. 單調時鐘（避免壁鐘回退影響）
// 6. Clock 介面（支援 MockClock 測試）
//
// 架構：
//
//     ┌─────────────┐
//     │ DelayQueue  │◄── 按過期時間排序的最小堆
//     │  (事件驅動)  │    只有非空 bucket 才入隊
//     └──────┬──────┘
//            │ Poll() 阻塞等待
//            ▼
//     ┌─────────────┐     ┌─────────────┐     ┌─────────────┐
//     │   Level 0   │────►│   Level 1   │────►│  Overflow   │
//     │  64 slots   │     │  64 slots   │     │   (按需)     │
//     │  tick=1s    │     │  tick=64s   │     │             │
//     └─────────────┘     └─────────────┘     └─────────────┘
//
// ============================================================================

const (
	// defaultTick 預設時間精度（1 秒）
	defaultTick = time.Second

	// defaultWheelSize 預設槽數量（必須是 2 的冪次方）
	defaultWheelSize int64 = 64

	// maxOverflowLevels 最大溢出輪層級（防止惡意 TTL）
	maxOverflowLevels = 10
)

// ============================================================================
// Clock 介面 - 支援測試時的時間模擬
// ============================================================================

// Clock 介面抽象時間操作，支援 MockClock 用於測試
type Clock interface {
	// Now 返回當前時間（單調時鐘，毫秒）
	Now() int64
	// Sleep 睡眠指定時間
	Sleep(d time.Duration)
	// NewTimer 建立計時器
	NewTimer(d time.Duration) Timer
}

// Timer 介面抽象計時器
type Timer interface {
	C() <-chan time.Time
	Stop() bool
	Reset(d time.Duration) bool
}

// realClock 是使用真實時間的 Clock 實作
type realClock struct {
	// startTime 是程式啟動時的壁鐘時間（奈秒）
	startTime int64
	// startMono 是程式啟動時的單調時間（奈秒）
	startMono int64
}

// 全域單調時鐘實例
var defaultClock = newRealClock()

// newRealClock 建立真實時鐘
func newRealClock() *realClock {
	now := time.Now()
	return &realClock{
		startTime: now.UnixNano(),
		startMono: nanotime(),
	}
}

// nanotime 返回單調時間（奈秒）
// 使用 runtime.nanotime 避免壁鐘回退影響
//
//go:linkname nanotime runtime.nanotime
func nanotime() int64

// Now 返回單調時間（毫秒）
func (c *realClock) Now() int64 {
	elapsed := nanotime() - c.startMono
	return (c.startTime + elapsed) / int64(time.Millisecond)
}

// clockNowUnixNano returns current time in unix nanoseconds from the provided clock.
// For realClock it preserves nanosecond precision using monotonic time; for other clock
// implementations it falls back to millisecond precision.
func clockNowUnixNano(clock Clock) int64 {
	if rc, ok := clock.(*realClock); ok {
		elapsed := nanotime() - rc.startMono
		return rc.startTime + elapsed
	}

	return clock.Now() * int64(time.Millisecond)
}

// Sleep 睡眠指定時間
func (c *realClock) Sleep(d time.Duration) {
	time.Sleep(d)
}

// NewTimer 建立計時器
func (c *realClock) NewTimer(d time.Duration) Timer {
	return &realTimer{timer: time.NewTimer(d)}
}

// realTimer 包裝 time.Timer
type realTimer struct {
	timer *time.Timer
}

func (t *realTimer) C() <-chan time.Time { return t.timer.C }
func (t *realTimer) Stop() bool          { return t.timer.Stop() }
func (t *realTimer) Reset(d time.Duration) bool {
	// 安全重置：先停止再重置
	if !t.timer.Stop() {
		select {
		case <-t.timer.C:
		default:
		}
	}
	t.timer.Reset(d)
	return true
}

// ============================================================================
// MockClock - 用於測試的模擬時鐘
// ============================================================================

// mockWaiter 代表一個等待 Sleep 完成的 goroutine
type mockWaiter struct {
	targetTime int64
	ch         chan struct{}
}

// MockClock 是可手動控制的模擬時鐘
type MockClock struct {
	mu      sync.Mutex
	current int64        // 當前時間（毫秒）
	timers  []*mockTimer // 所有計時器
	waiters []mockWaiter // 等待 Advance 的 goroutine（帶目標時間）
}

// NewMockClock 建立模擬時鐘
func NewMockClock(startMs int64) *MockClock {
	return &MockClock{
		current: startMs,
		timers:  make([]*mockTimer, 0),
	}
}

// Now 返回當前模擬時間（毫秒）
func (c *MockClock) Now() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.current
}

// Sleep 模擬睡眠，阻塞直到 Advance 推進時間到 >= targetTime
func (c *MockClock) Sleep(d time.Duration) {
	if d <= 0 {
		return
	}

	c.mu.Lock()
	targetTime := c.current + int64(d/time.Millisecond)

	// 若已到達目標時間，直接返回
	if c.current >= targetTime {
		c.mu.Unlock()
		return
	}

	w := mockWaiter{
		targetTime: targetTime,
		ch:         make(chan struct{}),
	}
	c.waiters = append(c.waiters, w)
	c.mu.Unlock()

	<-w.ch
}

// NewTimer 建立模擬計時器
func (c *MockClock) NewTimer(d time.Duration) Timer {
	c.mu.Lock()
	defer c.mu.Unlock()

	t := &mockTimer{
		clock:    c,
		deadline: c.current + int64(d/time.Millisecond),
		ch:       make(chan time.Time, 1),
	}
	c.timers = append(c.timers, t)
	return t
}

// Advance 推進模擬時間，只喚醒已達到目標時間的 waiter
func (c *MockClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.current += int64(d / time.Millisecond)

	// 觸發已到期的計時器
	for _, t := range c.timers {
		if t.deadline > 0 && t.deadline <= c.current {
			select {
			case t.ch <- time.Now():
			default:
			}
			t.deadline = 0 // 標記已觸發
		}
	}

	// 只喚醒已達到目標時間的等待者
	remaining := c.waiters[:0]
	for _, w := range c.waiters {
		if c.current >= w.targetTime {
			close(w.ch)
		} else {
			remaining = append(remaining, w)
		}
	}
	c.waiters = remaining
}

// Set 直接設定時間
func (c *MockClock) Set(ms int64) {
	c.mu.Lock()
	c.current = ms
	c.mu.Unlock()
}

// mockTimer 是模擬計時器
type mockTimer struct {
	clock    *MockClock
	deadline int64
	ch       chan time.Time
}

func (t *mockTimer) C() <-chan time.Time { return t.ch }

func (t *mockTimer) Stop() bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()

	wasActive := t.deadline > 0
	t.deadline = 0
	return wasActive
}

func (t *mockTimer) Reset(d time.Duration) bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()

	wasActive := t.deadline > 0
	t.deadline = t.clock.current + int64(d/time.Millisecond)
	return wasActive
}

// ============================================================================
// DelayQueue - 事件驅動的延遲佇列
// ============================================================================

// delayQueue 是按過期時間排序的阻塞佇列
// 使用統一的 wakeupCh 機制，支援優雅關閉
type delayQueue struct {
	mu       sync.Mutex
	pq       bucketHeap
	wakeupCh chan struct{} // 統一喚醒信號（緩衝 1）
	stopCh   chan struct{} // 停止信號
	timer    Timer         // 重用的 Timer（支援 MockClock）
	clock    Clock         // 時鐘介面
}

// bucketHeap 是按過期時間排序的 4-ary 最小堆（四叉堆）。
//
// bucketHeap 用於 [delayQueue] 的內部實作，負責維護所有非空 [twBucket]
// 的優先順序。過期時間最早的 bucket 會被放在堆頂，使 [delayQueue.Poll]
// 可以高效地取得下一個需要處理的 bucket。
//
// # 為何使用 4-ary heap
//
// 相較於標準二元堆，4-ary heap 具有以下優勢：
//   - 樹高更低：log4(n) vs log2(n)，減少 siftUp/siftDown 迭代次數
//   - 更 cache-friendly：子節點在記憶體中更連續，減少 cache miss
//   - 適合現代 CPU：分支預測和預取機制對 4 個子節點的比較更友善
//
// 權衡：每次 siftDown 需比較最多 4 個子節點（vs 二元堆的 2 個），
// 但在實務上，減少的樹高通常能彌補額外的比較開銷。
//
// # 索引計算
//
// 對於索引 i 的節點：
//   - 父節點：(i - 1) / 4
//   - 第 k 個子節點（k=0,1,2,3）：4*i + k + 1
//
// # 實作細節
//
//   - 透過 [twBucket.heapIndex] 追蹤每個 bucket 在堆中的位置
//   - 所有方法假設呼叫端已持有 [delayQueue.mu] 鎖
//   - 內聯實作避免 container/heap 的介面調用開銷
//
// 注意：bucketHeap 不應直接使用，應透過 [delayQueue] 的方法操作。
type bucketHeap struct {
	items []*twBucket
}

// Len 返回堆中 bucket 的數量。
func (h *bucketHeap) Len() int { return len(h.items) }

// Push 將 bucket 加入堆並維護堆性質。
// 時間複雜度：O(log4 n)
func (h *bucketHeap) Push(b *twBucket) {
	b.heapIndex = len(h.items)
	h.items = append(h.items, b)
	h.siftUp(b.heapIndex)
}

// Pop 移除並返回堆頂（最小過期時間）的 bucket。
// 若堆為空則返回 nil。
// 時間複雜度：O(log4 n)
func (h *bucketHeap) Pop() *twBucket {
	n := len(h.items)
	if n == 0 {
		return nil
	}

	b := h.items[0]
	b.heapIndex = -1

	// 將最後一個元素移到堆頂
	last := h.items[n-1]
	h.items[n-1] = nil // 避免記憶體洩漏
	h.items = h.items[:n-1]

	if len(h.items) > 0 {
		h.items[0] = last
		last.heapIndex = 0
		h.siftDown(0)
	}

	return b
}

// Fix 在 bucket 的過期時間改變後重新調整堆。
// 時間複雜度：O(log4 n)
func (h *bucketHeap) Fix(i int) {
	if i < 0 || i >= len(h.items) {
		return
	}
	// 先嘗試上浮，若無法上浮則下沉
	if !h.siftUp(i) {
		h.siftDown(i)
	}
}

// RemoveAt 移除指定索引的 bucket 並維護堆性質。
// 時間複雜度：O(log4 n)
func (h *bucketHeap) RemoveAt(i int) *twBucket {
	n := len(h.items)
	if i < 0 || i >= n {
		return nil
	}

	b := h.items[i]
	b.heapIndex = -1

	last := n - 1
	if i == last {
		// 移除的是最後一個元素，直接截斷
		h.items[last] = nil
		h.items = h.items[:last]
		return b
	}

	// 將最後一個元素移到被移除的位置
	h.items[i] = h.items[last]
	h.items[i].heapIndex = i
	h.items[last] = nil
	h.items = h.items[:last]

	// 重新平衡
	if !h.siftUp(i) {
		h.siftDown(i)
	}

	return b
}

// Peek 返回堆頂 bucket 但不移除。
// 若堆為空則返回 nil。
func (h *bucketHeap) Peek() *twBucket {
	if len(h.items) == 0 {
		return nil
	}
	return h.items[0]
}

// siftUp 將索引 i 的元素上浮到正確位置。
// 返回 true 表示發生了上浮。
// 注意：expiration 可能被 addInternal 通過 SetExpiration 寫入（atomic），
// 因此必須使用 atomic 讀取以避免競態。
func (h *bucketHeap) siftUp(i int) bool {
	if i <= 0 {
		return false
	}

	moved := false
	item := h.items[i]
	exp := atomic.LoadInt64(&item.expiration)

	for i > 0 {
		parent := (i - 1) >> 2 // (i - 1) / 4
		if atomic.LoadInt64(&h.items[parent].expiration) <= exp {
			break
		}
		// 父節點下移
		h.items[i] = h.items[parent]
		h.items[i].heapIndex = i
		i = parent
		moved = true
	}

	if moved {
		h.items[i] = item
		item.heapIndex = i
	}
	return moved
}

// siftDown 將索引 i 的元素下沉到正確位置。
// 注意：expiration 可能被 addInternal 通過 SetExpiration 寫入（atomic），
// 因此必須使用 atomic 讀取以避免競態。
func (h *bucketHeap) siftDown(i int) {
	n := len(h.items)
	if n <= 1 {
		return
	}

	item := h.items[i]
	exp := atomic.LoadInt64(&item.expiration)

	for {
		// 計算第一個子節點索引
		firstChild := (i << 2) + 1 // 4*i + 1
		if firstChild >= n {
			break
		}

		// 找出最小的子節點
		minChild := firstChild
		minExp := atomic.LoadInt64(&h.items[firstChild].expiration)

		// 檢查其餘最多 3 個子節點
		lastChild := firstChild + 4
		if lastChild > n {
			lastChild = n
		}
		for j := firstChild + 1; j < lastChild; j++ {
			childExp := atomic.LoadInt64(&h.items[j].expiration)
			if childExp < minExp {
				minChild = j
				minExp = childExp
			}
		}

		// 若當前節點已是最小，停止
		if exp <= minExp {
			break
		}

		// 最小子節點上移
		h.items[i] = h.items[minChild]
		h.items[i].heapIndex = i
		i = minChild
	}

	h.items[i] = item
	item.heapIndex = i
}

// newDelayQueue 建立新的延遲佇列
func newDelayQueue(stopCh chan struct{}, clock Clock) *delayQueue {
	dq := &delayQueue{
		pq:       bucketHeap{items: make([]*twBucket, 0, 64)},
		wakeupCh: make(chan struct{}, 1),
		stopCh:   stopCh,
		clock:    clock,
		timer:    clock.NewTimer(time.Hour), // 初始設一個大值
	}
	// 清空初始 timer
	dq.timer.Stop()
	return dq
}

// Offer 將 Bucket 加入佇列
// 如果該 Bucket 成為新的最早過期項目，喚醒等待中的 goroutine
func (dq *delayQueue) Offer(b *twBucket, expiration int64) bool {
	dq.mu.Lock()
	defer dq.mu.Unlock()

	// 檢查 bucket 是否已在佇列中
	// 注意：expiration 可能被 run() goroutine 讀取（不在 dq.mu 鎖保護下），
	// 因此必須使用 atomic 操作
	currentExp := atomic.LoadInt64(&b.expiration)
	if currentExp != -1 && b.heapIndex >= 0 {
		// 已在佇列中，檢查是否需要更新
		if expiration < currentExp {
			atomic.StoreInt64(&b.expiration, expiration)
			dq.pq.Fix(b.heapIndex)
			dq.signalWakeup()
		}
		return false
	}

	atomic.StoreInt64(&b.expiration, expiration)
	dq.pq.Push(b)

	// 如果成為堆頂（最早過期），喚醒等待者
	if dq.pq.Len() > 0 && dq.pq.items[0] == b {
		dq.signalWakeup()
	}
	return true
}

// signalWakeup 發送非阻塞喚醒信號
func (dq *delayQueue) signalWakeup() {
	select {
	case dq.wakeupCh <- struct{}{}:
	default:
		// channel 已有信號，無需重複
	}
}

// Poll 阻塞等待直到有 Bucket 過期或收到停止信號
// 返回過期的 Bucket 及其原始過期時間；若收到停止信號則返回 (nil, 0)
// 注意：Pop 時會在持有 dq.mu 的情況下將 bucket.expiration 設為 -1，
// 避免 Offer() 在 Pop 與 expiration 重置之間的窗口內覆寫 expiration。
func (dq *delayQueue) Poll() (*twBucket, int64) {
	for {
		dq.mu.Lock()

		// 檢查停止信號
		select {
		case <-dq.stopCh:
			dq.mu.Unlock()
			return nil, 0
		default:
		}

		if dq.pq.Len() == 0 {
			// 佇列為空，等待新項目或停止
			dq.mu.Unlock()
			select {
			case <-dq.wakeupCh:
				continue
			case <-dq.stopCh:
				return nil, 0
			}
		}

		// 取堆頂
		b := dq.pq.items[0]
		// 注意：expiration 可能被其他 goroutine 通過 Offer 寫入，必須使用 atomic
		expiration := atomic.LoadInt64(&b.expiration)
		now := dq.clock.Now() // 使用單調時鐘
		delay := expiration - now

		if delay <= 0 {
			// 已過期，彈出並在持鎖狀態下標記 bucket 不在佇列中
			dq.pq.Pop()
			atomic.StoreInt64(&b.expiration, -1)
			dq.mu.Unlock()
			return b, expiration
		}

		// 尚未過期，精確睡眠
		dq.mu.Unlock()

		// 重用 Timer
		dq.timer.Reset(time.Duration(delay) * time.Millisecond)

		select {
		case <-dq.timer.C():
			// 時間到，重新檢查堆頂
		case <-dq.wakeupCh:
			// 有新項目入隊，可能更早過期
			dq.timer.Stop()
		case <-dq.stopCh:
			dq.timer.Stop()
			return nil, 0
		}
	}
}

// RemoveBucket 從佇列中移除指定的空 bucket（若存在）。
// 用於 Remove() 清空 bucket 後避免空喚醒。
// 安全性：持有 dq.mu 後再檢查 bucket.count，確保不會移除剛被重新使用的 bucket。
// 鎖序為 dq.mu → b.mu，不會與其他路徑（b.mu → release → dq.mu）產生死鎖。
func (dq *delayQueue) RemoveBucket(b *twBucket) {
	dq.mu.Lock()
	defer dq.mu.Unlock()

	if b.heapIndex < 0 {
		return // 不在佇列中
	}

	// 持有 bucket 鎖確認 count 仍為 0
	b.mu.Lock()
	empty := b.count == 0
	b.mu.Unlock()

	if !empty {
		return // 已有新項目加入，不移除
	}

	dq.pq.RemoveAt(b.heapIndex)
	atomic.StoreInt64(&b.expiration, -1)
}

// Len 返回佇列長度
func (dq *delayQueue) Len() int {
	dq.mu.Lock()
	defer dq.mu.Unlock()
	return dq.pq.Len()
}

// ============================================================================
// twBucket - 時間輪槽位（雙向鏈表 + epoch）
// ============================================================================

// twBucket 代表時間輪的一個槽位
type twBucket struct {
	mu         sync.Mutex
	head       *cacheEntry // 鏈表頭
	tail       *cacheEntry // 鏈表尾
	count      int64       // 項目數量
	expiration int64       // 過期時間（-1 表示不在 DelayQueue 中）
	heapIndex  int         // 在 DelayQueue 堆中的索引
	epoch      uint64      // 版本號，每次 Flush 遞增
}

// newTwBucket 建立新的 bucket
func newTwBucket() *twBucket {
	return &twBucket{
		expiration: -1,
		heapIndex:  -1,
	}
}

// Add 將項目加入 Bucket（頭插法，O(1)）
func (b *twBucket) Add(ce *cacheEntry) {
	b.mu.Lock()
	defer b.mu.Unlock()

	ce.twBucket.Store(b)
	ce.twEpoch = b.epoch
	ce.twPrev = nil
	ce.twNext = b.head

	if b.head != nil {
		b.head.twPrev = ce
	} else {
		b.tail = ce
	}
	b.head = ce

	atomic.AddInt64(&b.count, 1)
}

// Remove 從 Bucket 移除項目（O(1)）
func (b *twBucket) Remove(ce *cacheEntry) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	// 檢查是否屬於此 Bucket 且版本匹配
	if ce.twBucket.Load() != b || ce.twEpoch != b.epoch {
		return false
	}

	// 更新前後指標
	if ce.twPrev != nil {
		ce.twPrev.twNext = ce.twNext
	} else {
		b.head = ce.twNext
	}

	if ce.twNext != nil {
		ce.twNext.twPrev = ce.twPrev
	} else {
		b.tail = ce.twPrev
	}

	// 清空指標
	ce.twPrev = nil
	ce.twNext = nil
	ce.twBucket.Store(nil)

	atomic.AddInt64(&b.count, -1)
	return true
}

// Flush 取出所有項目並清空 Bucket
func (b *twBucket) Flush() *cacheEntry {
	b.mu.Lock()
	defer b.mu.Unlock()

	head := b.head

	// 清空每個 entry 的 bucket 指標
	for ce := head; ce != nil; ce = ce.twNext {
		ce.twBucket.Store(nil)
		ce.twPrev = nil // twNext 保留供遍歷
	}

	b.head = nil
	b.tail = nil
	atomic.StoreInt64(&b.count, 0)
	b.epoch++ // 遞增版本號

	return head
}

// SetExpiration 設定過期時間，返回是否為新設定
func (b *twBucket) SetExpiration(exp int64) bool {
	return atomic.SwapInt64(&b.expiration, exp) != exp
}

// Count 返回項目數量
func (b *twBucket) Count() int64 {
	return atomic.LoadInt64(&b.count)
}

// ============================================================================
// TimingWheel - 改進版時間輪
// ============================================================================

// TimingWheel 是改進版時間輪
type TimingWheel struct {
	// 基本配置
	tick      int64 // 每個槽的時間跨度（毫秒）
	wheelSize int64 // 槽的數量（2 的冪次方）
	wheelMask int64 // wheelSize - 1，用於位運算
	interval  int64 // tick * wheelSize，本輪總範圍

	// 當前時間（毫秒，保證單調遞增）
	currentTime int64

	// 槽位陣列
	buckets []*twBucket

	// 驅動佇列（只有根輪子持有實例，溢出輪共享）
	delayQueue *delayQueue

	// 時鐘介面（支援 MockClock）
	clock Clock

	// 溢出輪（延遲建立）
	overflowWheel atomic.Pointer[TimingWheel]
	overflowMu    sync.Mutex

	// 層級資訊
	level     int
	maxLevels int

	// 項目總數
	totalCount int64

	// 回調
	onExpired func(*cacheEntry)

	// 控制
	stopCh  chan struct{}
	wg      sync.WaitGroup
	running int32
}

// newTimingWheel 建立新的時間輪
func newTimingWheel(onExpired func(*cacheEntry)) *TimingWheel {
	return newTimingWheelWithConfig(defaultTick, defaultWheelSize, onExpired)
}

// newTimingWheelWithConfig 使用指定配置建立時間輪
func newTimingWheelWithConfig(tick time.Duration, wheelSize int64, onExpired func(*cacheEntry)) *TimingWheel {
	return newTimingWheelWithClock(tick, wheelSize, onExpired, defaultClock)
}

// newTimingWheelWithClock 使用指定時鐘建立時間輪（用於測試）
func newTimingWheelWithClock(tick time.Duration, wheelSize int64, onExpired func(*cacheEntry), clock Clock) *TimingWheel {
	// 自動對齊 wheelSize 為 2 的冪次方
	actualSize := nextPowerOfTwo(wheelSize)

	tickMs := int64(tick / time.Millisecond)
	if tickMs < 1 {
		tickMs = 1
	}

	startMs := clock.Now()
	startMs = startMs - (startMs % tickMs) // 對齊到 tick 邊界

	stopCh := make(chan struct{})
	tw := &TimingWheel{
		tick:        tickMs,
		wheelSize:   actualSize,
		wheelMask:   actualSize - 1,
		interval:    tickMs * actualSize,
		currentTime: startMs,
		buckets:     make([]*twBucket, actualSize),
		delayQueue:  newDelayQueue(stopCh, clock),
		clock:       clock,
		level:       0,
		maxLevels:   maxOverflowLevels,
		onExpired:   onExpired,
		stopCh:      stopCh,
	}

	for i := int64(0); i < actualSize; i++ {
		tw.buckets[i] = newTwBucket()
	}

	return tw
}

// newOverflowWheel 建立溢出輪（內部使用）
func (tw *TimingWheel) newOverflowWheel() *TimingWheel {
	overflow := &TimingWheel{
		tick:        tw.interval, // 新輪的 tick = 本輪的總範圍
		wheelSize:   tw.wheelSize,
		wheelMask:   tw.wheelMask,
		interval:    tw.interval * tw.wheelSize,
		currentTime: atomic.LoadInt64(&tw.currentTime),
		buckets:     make([]*twBucket, tw.wheelSize),
		delayQueue:  tw.delayQueue, // 共享 DelayQueue
		clock:       tw.clock,      // 共享 Clock
		level:       tw.level + 1,
		maxLevels:   tw.maxLevels,
		onExpired:   tw.onExpired,
		stopCh:      tw.stopCh, // 共享 stopCh
	}

	for i := int64(0); i < tw.wheelSize; i++ {
		overflow.buckets[i] = newTwBucket()
	}

	return overflow
}

// Start 啟動時間輪
func (tw *TimingWheel) Start() {
	if !atomic.CompareAndSwapInt32(&tw.running, 0, 1) {
		return
	}

	tw.wg.Add(1)
	go tw.run()
}

// Stop 停止時間輪
func (tw *TimingWheel) Stop() {
	if !atomic.CompareAndSwapInt32(&tw.running, 1, 0) {
		return
	}

	close(tw.stopCh)
	tw.wg.Wait()
}

// run 是時間輪的主循環
func (tw *TimingWheel) run() {
	defer tw.wg.Done()

	for {
		// Poll 內部監聽 stopCh，並在持鎖狀態下重置 expiration
		bucket, expiration := tw.delayQueue.Poll()
		if bucket == nil {
			return // 收到停止信號
		}

		// 推進時鐘
		tw.advanceClock(expiration)

		// 處理過期項目
		tw.expireBucket(bucket)
	}
}

// advanceClock 推進時鐘（保證單調遞增）
func (tw *TimingWheel) advanceClock(expiration int64) {
	for wheel := tw; wheel != nil; {
		currentTime := atomic.LoadInt64(&wheel.currentTime)
		if expiration < currentTime+wheel.tick {
			break
		}

		// 計算新的 currentTime（對齊到 tick 邊界）
		newTime := expiration - (expiration % wheel.tick)
		if newTime <= currentTime {
			break
		}

		// CAS 更新，確保單調遞增
		if atomic.CompareAndSwapInt64(&wheel.currentTime, currentTime, newTime) {
			wheel = wheel.overflowWheel.Load()
		}
		// CAS 失敗則重試當前層級
	}
}

// expireBucket 處理過期的 bucket
func (tw *TimingWheel) expireBucket(bucket *twBucket) {
	head := bucket.Flush()
	if head == nil {
		return // 空 bucket
	}

	for ce := head; ce != nil; {
		next := ce.twNext // 先保存 next，因為可能會被修改
		ce.twNext = nil   // 清理指標

		// 檢查是否已被刪除（不設置 deleted，讓 onExpired 回調處理）
		// P1 修復：不預先設置 ce.deleted，讓 handleExpired → removeEntry → em.Remove
		// 來競爭刪除權，確保 CAS 機制正確運作
		if atomic.LoadInt32(&ce.deleted) == 1 {
			// Remove() 已通過 twCountClaimed CAS 處理了 totalCount
			ce = next
			continue
		}

		// 檢查是否需要降級（cascade）或真正過期
		expiration := ce.getPriority() / int64(time.Millisecond)
		currentTime := atomic.LoadInt64(&tw.currentTime)

		if expiration > currentTime+tw.tick {
			// 還沒過期，需要重新加入（降級場景）
			// 不扣 totalCount，因為項目仍在時間輪中
			if !tw.addInternal(ce) {
				// 加入失敗（項目在過程中已過期），CAS 扣減計數
				tw.claimCount(ce)
				tw.safeOnExpired(ce)
			}
		} else {
			// 真正過期，CAS 確保 totalCount 只扣一次
			tw.claimCount(ce)
			tw.safeOnExpired(ce)
		}

		ce = next
	}
}

// safeOnExpired 安全執行回調（panic recovery）
func (tw *TimingWheel) safeOnExpired(ce *cacheEntry) {
	defer func() {
		if r := recover(); r != nil {
			// 記錄錯誤但不中斷
			// 可以考慮加入 logger
		}
	}()

	if tw.onExpired != nil {
		tw.onExpired(ce)
	}
}

// Add 加入項目到時間輪
// P2 修復：當項目的過期時間在當前 tick 內時，立即觸發過期回調，
// 而不是靜默丟棄。這確保短 TTL 項目能正確觸發清理。
//
// 返回值：
//   - true: 項目被安排在時間輪中等待過期
//   - false: 項目沒有被安排（已刪除或已被立即處理）
func (tw *TimingWheel) Add(ce *cacheEntry) bool {
	// 檢查是否已刪除
	if atomic.LoadInt32(&ce.deleted) == 1 {
		return false
	}

	// P2 修復：先檢查是否已過期，能正確處理 TTL=0 的情況
	// 注意：absoluteExpiration < 0 表示永不過期，應該跳過此檢查
	// 統一使用 tw.clock.Now()（毫秒），確保與時間輪其他部分使用相同時間基準，
	// 也讓 MockClock 測試能正確運作
	absExp := ce.getAbsoluteExpiration()
	if absExp >= 0 {
		absExpMs := absExp / int64(time.Millisecond)
		nowMs := tw.clock.Now()
		if absExpMs <= nowMs {
			// 已過期，未入隊，預先標記防止 claimCount 誤扣 totalCount
			atomic.StoreInt32(&ce.twCountClaimed, 1)
			tw.safeOnExpired(ce)
			return false
		}
	}

	// 初始化 TW 相關欄位
	ce.twPrev = nil
	ce.twNext = nil
	ce.twBucket.Store(nil)
	atomic.StoreInt32(&ce.twLevel, int32(tw.level))
	atomic.StoreInt32(&ce.twCountClaimed, 0)

	if tw.addInternal(ce) {
		atomic.AddInt64(&tw.totalCount, 1)
		return true
	}

	// P2 修復：addInternal 返回 false 表示項目已過期
	// 未入隊，預先標記防止 claimCount 誤扣 totalCount
	atomic.StoreInt32(&ce.twCountClaimed, 1)
	tw.safeOnExpired(ce)
	return false
}

// addInternal 內部加入邏輯
// P2 修復：區分「已過期」和「在當前 tick 內但未過期」兩種情況
func (tw *TimingWheel) addInternal(ce *cacheEntry) bool {
	expiration := ce.getPriority() / int64(time.Millisecond)
	currentTime := atomic.LoadInt64(&tw.currentTime)

	// P2 修復：只有真正已過期的才返回 false
	// 注意：Add 方法已經使用納秒級精度做了初步過濾，
	// 這裡的毫秒級檢查主要處理 cascade（降級）場景
	if expiration <= currentTime {
		// 已過期，返回 false（讓 Add 觸發過期回調）
		return false
	}

	if expiration < currentTime+tw.tick {
		// P2 修復：在當前 tick 內但還沒過期，放入第一個將要處理的槽位
		// 這確保短 TTL 項目（< tick）能被正確安排，而不是被靜默丟棄
		nextTickTime := (currentTime/tw.tick + 1) * tw.tick
		virtualID := nextTickTime / tw.tick
		bucketIdx := virtualID & tw.wheelMask
		bucket := tw.buckets[bucketIdx]

		bucket.Add(ce)
		atomic.StoreInt32(&ce.twLevel, int32(tw.level))

		// 設定 Bucket 過期時間並加入 DelayQueue
		if bucket.SetExpiration(nextTickTime) {
			tw.delayQueue.Offer(bucket, nextTickTime)
		}

		return true
	}

	if expiration < currentTime+tw.interval {
		// 在本輪範圍內
		virtualID := expiration / tw.tick
		bucketIdx := virtualID & tw.wheelMask // 位運算
		bucket := tw.buckets[bucketIdx]

		bucket.Add(ce)
		atomic.StoreInt32(&ce.twLevel, int32(tw.level))

		// 設定 Bucket 過期時間並加入 DelayQueue
		//
		// Level 0（最底層）維持現有語義：向上對齊到下一個 tick，
		// 避免在同一秒槽位內過早觸發。
		//
		// Overflow level（level > 0）使用槽位起點（virtualID * tick），
		// 讓 bucket 能在「接近到期前」被 flush，進而 cascade 到更低層，
		// 最終收斂到 Level 0 的秒級精度，而不是卡在高層 tick 造成大幅延遲。
		bucketExpiration := (virtualID + 1) * tw.tick
		if tw.level > 0 {
			bucketExpiration = virtualID * tw.tick
		}
		if bucket.SetExpiration(bucketExpiration) {
			tw.delayQueue.Offer(bucket, bucketExpiration)
		}

		return true
	}

	// 超出本輪範圍，委託給溢出輪
	overflow := tw.getOrCreateOverflowWheel()
	if overflow == nil {
		// 達到層級上限，放入本層最後一槽
		bucket := tw.buckets[tw.wheelSize-1]
		bucket.Add(ce)
		atomic.StoreInt32(&ce.twLevel, int32(tw.level))

		bucketExpiration := (currentTime/tw.tick + tw.wheelSize) * tw.tick
		if bucket.SetExpiration(bucketExpiration) {
			tw.delayQueue.Offer(bucket, bucketExpiration)
		}
		return true
	}

	return overflow.addInternal(ce)
}

// getOrCreateOverflowWheel 延遲建立溢出輪
func (tw *TimingWheel) getOrCreateOverflowWheel() *TimingWheel {
	// 檢查層級上限
	if tw.level >= tw.maxLevels-1 {
		return nil
	}

	// 快速路徑
	if overflow := tw.overflowWheel.Load(); overflow != nil {
		return overflow
	}

	tw.overflowMu.Lock()
	defer tw.overflowMu.Unlock()

	// Double-check
	if overflow := tw.overflowWheel.Load(); overflow != nil {
		return overflow
	}

	overflow := tw.newOverflowWheel()
	tw.overflowWheel.Store(overflow)
	return overflow
}

// claimCount CAS 扣減 totalCount，確保每個 entry 只扣一次。
func (tw *TimingWheel) claimCount(ce *cacheEntry) {
	if atomic.CompareAndSwapInt32(&ce.twCountClaimed, 0, 1) {
		atomic.AddInt64(&tw.totalCount, -1)
	}
}

// claimAndDetach 扣減 totalCount 並從 bucket 中移除項目。
func (tw *TimingWheel) claimAndDetach(ce *cacheEntry) {
	tw.claimCount(ce)

	bucket := ce.twBucket.Load()
	if bucket != nil && bucket.Remove(ce) {
		// 若 bucket 已清空，嘗試從 DelayQueue 移除以避免空喚醒
		if atomic.LoadInt64(&bucket.count) == 0 {
			tw.delayQueue.RemoveBucket(bucket)
		}
	}
}

// Remove 從時間輪移除項目
func (tw *TimingWheel) Remove(ce *cacheEntry) bool {
	// CAS 標記刪除
	if !atomic.CompareAndSwapInt32(&ce.deleted, 0, 1) {
		return false
	}

	tw.claimAndDetach(ce)
	return true
}

// RemoveMarked 移除已標記刪除的項目（由 ExpirationManager 呼叫）
func (tw *TimingWheel) RemoveMarked(ce *cacheEntry) {
	tw.claimAndDetach(ce)
}

// Count 返回項目總數
func (tw *TimingWheel) Count() int {
	return int(atomic.LoadInt64(&tw.totalCount))
}

// IsRunning 返回是否正在運行
func (tw *TimingWheel) IsRunning() bool {
	return atomic.LoadInt32(&tw.running) == 1
}

// Clear 清空所有項目
func (tw *TimingWheel) Clear() {
	for _, bucket := range tw.buckets {
		bucket.Flush()
	}
	atomic.StoreInt64(&tw.totalCount, 0)

	if overflow := tw.overflowWheel.Load(); overflow != nil {
		overflow.Clear()
	}
}

// ============================================================================
// 輔助函數
// ============================================================================

// nextPowerOfTwo 返回 >= n 的最小 2 的冪次
func nextPowerOfTwo(n int64) int64 {
	if n <= 1 {
		return 1
	}
	n--
	n |= n >> 1
	n |= n >> 2
	n |= n >> 4
	n |= n >> 8
	n |= n >> 16
	n |= n >> 32
	return n + 1
}

// ============================================================================
// 調試與測試工具
// ============================================================================

// TimingWheelMetrics 時間輪指標
type TimingWheelMetrics struct {
	Level           int     // 當前層級
	TotalCount      int64   // 項目總數
	BucketCounts    []int64 // 每個 bucket 的項目數
	NonEmptyBuckets int     // 非空 bucket 數量
	DelayQueueSize  int     // DelayQueue 堆大小
	OverflowLevels  int     // 溢出輪層級數
}

// GetMetrics 獲取時間輪指標
func (tw *TimingWheel) GetMetrics() TimingWheelMetrics {
	m := TimingWheelMetrics{
		Level:        tw.level,
		TotalCount:   atomic.LoadInt64(&tw.totalCount),
		BucketCounts: make([]int64, len(tw.buckets)),
	}

	for i, b := range tw.buckets {
		count := b.Count()
		m.BucketCounts[i] = count
		if count > 0 {
			m.NonEmptyBuckets++
		}
	}

	if tw.delayQueue != nil {
		m.DelayQueueSize = tw.delayQueue.Len()
	}

	// 計算溢出輪層級
	overflow := tw.overflowWheel.Load()
	for overflow != nil {
		m.OverflowLevels++
		overflow = overflow.overflowWheel.Load()
	}

	return m
}

// Validate 驗證時間輪完整性（用於測試）
func (tw *TimingWheel) Validate() error {
	for i, bucket := range tw.buckets {
		if err := bucket.Validate(); err != nil {
			return fmt.Errorf("bucket[%d]: %w", i, err)
		}
	}

	if overflow := tw.overflowWheel.Load(); overflow != nil {
		return overflow.Validate()
	}

	return nil
}

// Validate 驗證 bucket 鏈表完整性
func (b *twBucket) Validate() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	count := int64(0)
	var prev *cacheEntry

	for ce := b.head; ce != nil; ce = ce.twNext {
		count++

		if ce.twPrev != prev {
			return fmt.Errorf("broken backward link at entry %p", ce)
		}

		if ce.twBucket.Load() != b {
			return fmt.Errorf("entry %p has wrong bucket reference", ce)
		}

		prev = ce
	}

	if prev != b.tail {
		return fmt.Errorf("tail mismatch")
	}

	if count != atomic.LoadInt64(&b.count) {
		return fmt.Errorf("count mismatch: traversed %d, stored %d", count, b.count)
	}

	return nil
}

// WaitUntilIdle 等待所有處理完成（用於測試）
func (tw *TimingWheel) WaitUntilIdle(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		// 檢查是否有正在處理的項目
		if tw.delayQueue.Len() == 0 && tw.Count() == 0 {
			return nil
		}
		runtime.Gosched()
		time.Sleep(10 * time.Millisecond)
	}

	return fmt.Errorf("timeout waiting for idle")
}
