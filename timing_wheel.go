package carrot

import (
	"sync"
	"sync/atomic"
	"time"
)

// ============================================================================
// 時間輪 (Timing Wheel) 實作說明
// ============================================================================
//
// 時間輪是一種高效的定時器管理資料結構，概念類似時鐘：
//
//     ┌───┐
// ┌───┤ 0 │◄── 當前指針 (tick)
// │   └───┘
// │   ┌───┐
// │   │ 1 │──► [entry1] → [entry2]  (該時間點過期的項目鏈表)
// │   └───┘
// │   ┌───┐
// │   │ 2 │──► [entry3]
// │   └───┘
// │     ⋮
// │   ┌───┐
// └───│59 │──► [entry4]
//     └───┘
//
// 運作原理：
// 1. 時間輪有固定數量的「槽」(slots)，每個槽代表一個時間單位
// 2. 指針每隔 tick 時間前進一格
// 3. 新增項目時，根據過期時間計算放入哪個槽
// 4. 指針指到的槽，該槽內所有項目即為過期項目
//
// 層級時間輪 (Hierarchical Timing Wheel)：
// - 類似時鐘的「秒/分/時」概念
// - 第 0 層：秒級精度 (60 slots × 1秒 = 1分鐘範圍)
// - 第 1 層：分鐘級精度 (60 slots × 1分鐘 = 1小時範圍)
// - 第 2 層：小時級精度 (24 slots × 1小時 = 24小時範圍)
// - 當高層級槽被觸發時，該槽內的項目會「降級」到低層級重新排程
//
// 優點：
// - Add: O(1) - 直接放入對應槽
// - Remove: O(1) - 從槽中移除
// - 過期檢查: O(1) - 只需處理當前槽
//
// 缺點：
// - 精度受限於 tick 間隔（本實作為 1 秒）
// - 超過最大範圍的項目需要特殊處理
// ============================================================================

const (
	// tickDuration 是時間輪的基本時間單位（精度）
	// 每隔這段時間，時間輪指針會前進一格
	tickDuration = time.Second

	// levelCount 是時間輪的層級數量
	// 3 層可涵蓋：60秒 + 60分鐘 + 24小時 = 超過 24 小時的範圍
	levelCount = 3
)

// levelConfig 定義每個層級的配置
// sizes: 該層級的槽數量
// tickMs: 該層級每個槽代表的毫秒數
var levelConfig = struct {
	sizes  [levelCount]int64
	tickMs [levelCount]int64
}{
	sizes:  [levelCount]int64{60, 60, 24},              // 秒(60) / 分(60) / 時(24)
	tickMs: [levelCount]int64{1000, 60_000, 3_600_000}, // 1秒 / 1分鐘 / 1小時 (毫秒)
}

// TimingWheel 是層級時間輪的主結構
// 它將過期時間相近的項目分組，實現 O(1) 的過期檢查
type TimingWheel struct {
	// levels 儲存各層級的時間輪
	levels [levelCount]*wheelLevel

	// startTime 是時間輪啟動的時間點（用於計算相對時間）
	startTime int64

	// currentTick 是當前的 tick 計數（原子操作）
	currentTick int64

	// onExpired 是項目過期時的回調函數
	onExpired func(*cacheEntry)

	// stopCh 用於通知背景 goroutine 停止
	stopCh chan struct{}

	// wg 用於等待背景 goroutine 結束
	wg sync.WaitGroup

	// running 標記時間輪是否正在運行（原子操作）
	running int32

	// totalCount 追蹤總項目數（原子操作）
	totalCount int64
}

// wheelLevel 代表時間輪的單一層級
type wheelLevel struct {
	// slots 是該層級的所有槽
	slots []*twSlot

	// size 是槽的數量
	size int64

	// tickMs 是每個槽代表的毫秒數
	tickMs int64

	// current 是當前槽的位置（原子操作）
	current int64
}

// twSlot 是時間輪的單一槽，儲存在該時間點過期的所有項目
type twSlot struct {
	// mu 保護該槽的並發訪問
	mu sync.Mutex

	// entries 儲存該槽內的所有項目
	// 使用 map 實現 O(1) 的新增和移除
	entries map[*cacheEntry]struct{}
}

// newTimingWheel 建立新的層級時間輪
//
// 參數：
//   - onExpired: 項目過期時的回調函數，會在背景 goroutine 中調用
//
// 回傳：
//   - 初始化完成的 TimingWheel 指標
//
// 實作細節：
//   - 根據 levelConfig 初始化各層級
//   - 每個槽預先分配 map 以減少後續分配
func newTimingWheel(onExpired func(*cacheEntry)) *TimingWheel {
	tw := &TimingWheel{
		startTime: time.Now().UnixNano(),
		onExpired: onExpired,
		stopCh:    make(chan struct{}),
	}

	// 初始化各層級
	// Level 0: 60 個槽 × 1秒 = 可處理 60 秒內的過期
	// Level 1: 60 個槽 × 1分鐘 = 可處理 60 分鐘內的過期
	// Level 2: 24 個槽 × 1小時 = 可處理 24 小時內的過期
	for i := 0; i < levelCount; i++ {
		size := levelConfig.sizes[i]
		tw.levels[i] = &wheelLevel{
			slots:  make([]*twSlot, size),
			size:   size,
			tickMs: levelConfig.tickMs[i],
		}

		// 初始化每個槽
		for j := int64(0); j < size; j++ {
			tw.levels[i].slots[j] = &twSlot{
				entries: make(map[*cacheEntry]struct{}, 8), // 預分配小容量
			}
		}
	}

	return tw
}

// Start 啟動時間輪的背景 goroutine
//
// 這個方法會啟動一個 goroutine，每隔 tickDuration 時間：
// 1. 將指針前進一格
// 2. 處理當前槽中的所有過期項目
// 3. 必要時觸發層級進位（類似時鐘的進位）
//
// 注意：這個方法是非阻塞的，會立即返回
func (tw *TimingWheel) Start() {
	// 使用 CAS 確保只啟動一次
	if !atomic.CompareAndSwapInt32(&tw.running, 0, 1) {
		return
	}

	tw.wg.Add(1)
	go tw.run()
}

// Stop 停止時間輪的背景 goroutine
//
// 這個方法會：
// 1. 發送停止信號給背景 goroutine
// 2. 等待 goroutine 完全結束
//
// 注意：調用後應該不再使用此時間輪
func (tw *TimingWheel) Stop() {
	if !atomic.CompareAndSwapInt32(&tw.running, 1, 0) {
		return
	}

	close(tw.stopCh)
	tw.wg.Wait()
}

// run 是時間輪的主循環
//
// 運作流程：
// 1. 建立 ticker，每 tickDuration 觸發一次
// 2. 每次觸發時調用 advance() 前進一格
// 3. 收到停止信號時退出
func (tw *TimingWheel) run() {
	defer tw.wg.Done()

	ticker := time.NewTicker(tickDuration)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			tw.advance()
		case <-tw.stopCh:
			return
		}
	}
}

// advance 將時間輪前進一格
//
// 這是時間輪的核心邏輯：
//
// 1. 增加 tick 計數
// 2. 第 0 層指針前進，處理該槽的過期項目
// 3. 如果第 0 層完成一圈（回到 0），觸發第 1 層進位
// 4. 以此類推，類似時鐘的秒→分→時進位
//
// 時間複雜度：O(k)，k 是當前槽中的項目數
func (tw *TimingWheel) advance() {
	atomic.AddInt64(&tw.currentTick, 1)

	// 處理第 0 層（秒級）
	level := tw.levels[0]
	idx := atomic.AddInt64(&level.current, 1) % level.size

	// 過期該槽中的所有項目
	tw.expireSlot(level.slots[idx])

	// 檢查是否需要進位到下一層
	// 當 idx == 0 時，表示該層已轉完一圈
	if idx == 0 {
		tw.cascade(1)
	}
}

// cascade 處理層級進位
//
// 當低層級轉完一圈時，高層級需要前進一格。
// 高層級槽中的項目會被「降級」到低層級重新排程。
//
// 範例：
// - 假設有個項目設定 65 秒後過期
// - 初始放入：Level 1（分鐘層）的第 1 槽
// - 60 秒後：Level 0 轉完一圈，觸發 Level 1 進位
// - 該項目被取出，重新計算後放入 Level 0 的第 5 槽
// - 再過 5 秒後：Level 0 指向第 5 槽，項目過期
//
// 參數：
//   - levelIdx: 要進位的層級索引
func (tw *TimingWheel) cascade(levelIdx int) {
	if levelIdx >= levelCount {
		return
	}

	level := tw.levels[levelIdx]
	idx := atomic.AddInt64(&level.current, 1) % level.size

	// 取出該槽的所有項目
	slot := level.slots[idx]
	slot.mu.Lock()
	entries := slot.entries
	slot.entries = make(map[*cacheEntry]struct{}, len(entries))
	slot.mu.Unlock()

	// 將項目降級到較低層級重新排程
	// 這些項目的剩餘過期時間現在可以用更精確的低層級表示
	for ce := range entries {
		if atomic.LoadInt32(&ce.deleted) == 0 {
			tw.addToWheel(ce)
		} else {
			atomic.AddInt64(&tw.totalCount, -1)
		}
	}

	// 繼續檢查更高層級的進位
	if idx == 0 {
		tw.cascade(levelIdx + 1)
	}
}

// expireSlot 處理槽中所有過期的項目
//
// 精確計數架構（Section 9.2.4）：
// 1. 取出槽時立即批量預扣 totalCount
// 2. Sliding 項目 reschedule 成功時補回 totalCount
// 3. em.Remove 不操作 TW count（只負責 CAS）
//
// 這個方法會：
// 1. 鎖定槽並取出所有項目
// 2. 批量預扣 totalCount（entries 離開槽位）
// 3. 標記項目已被取出（twLevel = -1）
// 4. 對每個項目：
//   - 如果 deleted=1，跳過（計數已預扣）
//   - 如果是 Sliding 項目且尚未過期，重新排程（reschedule 會補回計數）
//   - 否則調用過期回調（不操作計數，已預扣）
//
// 參數：
//   - slot: 要處理的槽
func (tw *TimingWheel) expireSlot(slot *twSlot) {
	slot.mu.Lock()
	entries := slot.entries
	slot.entries = make(map[*cacheEntry]struct{}, 8)
	count := len(entries)
	slot.mu.Unlock()

	// 精確計數：entries 離開槽位時立即批量扣減
	// 這確保 tw.Count() 反映「正在槽位中等待」的真實數量
	if count > 0 {
		atomic.AddInt64(&tw.totalCount, -int64(count))
	}

	now := time.Now().UnixNano()

	for ce := range entries {
		// 標記項目已被從槽中取出
		// 這樣 Remove 就知道不需要再從槽中刪除（已被取出且計數已扣）
		atomic.StoreInt32(&ce.twLevel, -1)

		// 已標記刪除，直接跳過
		// 注意：totalCount 已在上面批量扣減，這裡不需要再扣
		if atomic.LoadInt32(&ce.deleted) == 1 {
			continue
		}

		// Sliding Expiration 的 Lazy 檢查
		if ce.isSliding() {
			lastAccessed := ce.getLastAccessed()
			actualExpireAt := lastAccessed + int64(ce.slidingExpiration)
			if actualExpireAt > now {
				// 還沒真正過期，重新放入時間輪
				// reschedule 成功時會 totalCount++（entry 重新進入輪子）
				ce.setPriority(actualExpireAt)
				ce.setAbsoluteExpiration(actualExpireAt)
				tw.reschedule(ce)
				continue
			}
		}

		// 真的過期了，調用回調
		// 注意：這裡「不做 CAS」，CAS 權限收斂在 removeEntry -> em.Remove
		// em.Remove 只負責 CAS，不操作 TW totalCount（已在上面扣過）
		tw.onExpired(ce)
	}
}

// reschedule 將項目重新放入時間輪（expireSlot 中的重排場景）
//
// 精確計數架構（Section 9.2.4）：
// - expireSlot 已經批量預扣了 totalCount
// - 這裡成功放入新槽時需要補回 totalCount
// - 如果 deleted=1 或已過期，不補回（視為真正離開輪子）
//
// 競態安全性說明：
// 在 expireSlot 檢查 deleted 到呼叫 reschedule 之間，
// 可能有其他 goroutine 呼叫 em.Remove() 並設定 deleted=1。
// 此時應該放棄重排，不補回計數。
//
// 參數：
//   - ce: 要重新排程的快取項目
func (tw *TimingWheel) reschedule(ce *cacheEntry) {
	expireAt := ce.getPriority()
	now := time.Now().UnixNano()
	delayMs := (expireAt - now) / int64(time.Millisecond)

	if delayMs <= 0 {
		// 已經過期，調用回調（不做 CAS，由 removeEntry 處理）
		// 注意：totalCount 已在 expireSlot 取出時扣過，這裡不需要再扣
		tw.onExpired(ce)
		return
	}

	// Double-check: 放入新槽之前檢查 deleted
	// 如果已被 em.Remove() 刪除，直接返回
	// 注意：totalCount 已在 expireSlot 取出時扣過，這裡不需要再扣
	if atomic.LoadInt32(&ce.deleted) == 1 {
		return
	}

	levelIdx, offset := tw.calculateSlot(delayMs)

	level := tw.levels[levelIdx]
	current := atomic.LoadInt64(&level.current)
	targetSlot := (current + offset) % level.size
	if targetSlot < 0 {
		targetSlot += level.size
	}

	slot := level.slots[targetSlot]
	slot.mu.Lock()
	// 在持有鎖的情況下最終檢查
	if atomic.LoadInt32(&ce.deleted) == 1 {
		slot.mu.Unlock()
		// totalCount 已在 expireSlot 取出時扣過，這裡不需要再扣
		return
	}
	slot.entries[ce] = struct{}{}
	slot.mu.Unlock()

	// 精確計數：entry 重新進入輪子，totalCount++
	// 這補回 expireSlot 預扣的計數
	atomic.AddInt64(&tw.totalCount, 1)

	atomic.StoreInt32(&ce.twLevel, int32(levelIdx))
	atomic.StoreInt32(&ce.twSlot, int32(targetSlot))
}

// Add 將項目加入時間輪排程
//
// 這個方法會根據項目的過期時間，計算應該放入哪個層級的哪個槽。
// 永不過期的項目（absoluteExpiration < 0）不會被加入時間輪。
//
// 時間複雜度：O(1)
//
// 參數：
//   - ce: 要加入的快取項目
//
// 回傳：
//   - true: 項目已加入時間輪
//   - false: 項目為永不過期，未加入時間輪
func (tw *TimingWheel) Add(ce *cacheEntry) bool {
	// 永不過期的項目不需要放入時間輪
	// absoluteExpiration < 0 表示永不過期（如 Forever 設定的 -1）
	if ce.getAbsoluteExpiration() < 0 {
		return false
	}

	// 只有真正入槽才計數
	// addToWheel 回傳 false 表示已過期，直接觸發回調而未入槽
	if tw.addToWheel(ce) {
		atomic.AddInt64(&tw.totalCount, 1)
	}
	return true
}

// addToWheel 將項目放入適當的槽
//
// 計算邏輯：
// 1. 計算項目的延遲時間（過期時間 - 現在時間）
// 2. 如果已過期，直接調用回調並回傳 false
// 3. 否則根據延遲時間選擇適當的層級和槽，回傳 true
//
// 槽位計算：
// - 對於 delayMs 毫秒後過期的項目
// - 找到最小能容納此延遲的層級
// - 槽位 = (當前位置 + 延遲/tickMs) % size
//
// 參數：
//   - ce: 要加入的快取項目
//
// 回傳：
//   - true: 成功放入槽位
//   - false: 已過期，直接觸發回調而未入槽
func (tw *TimingWheel) addToWheel(ce *cacheEntry) bool {
	// 取得過期時間（Unix 納秒）
	expireAt := ce.getPriority()
	now := time.Now().UnixNano()
	delayMs := (expireAt - now) / int64(time.Millisecond)

	// 已過期，直接處理，不入槽
	if delayMs <= 0 {
		if atomic.LoadInt32(&ce.deleted) == 0 {
			tw.onExpired(ce)
		}
		return false
	}

	// 計算應該放入哪個層級和槽
	levelIdx, offset := tw.calculateSlot(delayMs)

	level := tw.levels[levelIdx]
	current := atomic.LoadInt64(&level.current)

	// 計算目標槽位：當前位置 + 偏移量，對槽數取模
	targetSlot := (current + offset) % level.size
	if targetSlot < 0 {
		targetSlot += level.size
	}

	slot := level.slots[targetSlot]
	slot.mu.Lock()
	slot.entries[ce] = struct{}{}
	slot.mu.Unlock()

	// 記錄項目所在位置，方便後續移除
	atomic.StoreInt32(&ce.twLevel, int32(levelIdx))
	atomic.StoreInt32(&ce.twSlot, int32(targetSlot))
	return true
}

// calculateSlot 計算延遲時間對應的層級和槽偏移
//
// 演算法：
// 1. 從最低層級（秒級）開始檢查
// 2. 如果延遲時間在該層級範圍內（< tickMs × size），使用該層級
// 3. 否則嘗試更高層級
// 4. 如果超過最大範圍，放入最高層級的最後一槽
//
// 範例：
// - 30 秒 → Level 0, offset 30
// - 90 秒 → Level 1, offset 1 (1.5分鐘 → 分鐘層第1槽)
// - 3700 秒 → Level 2, offset 1 (約1小時 → 小時層第1槽)
//
// 參數：
//   - delayMs: 延遲時間（毫秒）
//
// 回傳：
//   - levelIdx: 層級索引
//   - offset: 相對於當前位置的槽偏移
func (tw *TimingWheel) calculateSlot(delayMs int64) (levelIdx int, offset int64) {
	for i := 0; i < levelCount; i++ {
		level := tw.levels[i]
		levelRange := level.tickMs * level.size

		if delayMs < levelRange {
			// 延遲時間在此層級範圍內
			// 偏移量 = 延遲時間 / 每槽時間
			// +1 是因為當前槽可能馬上就要被處理
			offset = delayMs/level.tickMs + 1
			if offset >= level.size {
				offset = level.size - 1
			}
			return i, offset
		}
	}

	// 超過最大範圍，放入最高層級的最後一槽
	return levelCount - 1, tw.levels[levelCount-1].size - 1
}

// Remove 從時間輪中移除項目
//
// 這個方法使用「標記刪除」策略：
// 1. 將項目標記為已刪除
// 2. 實際移除會在槽被處理時進行
//
// 這種策略的好處是 O(1) 時間複雜度，不需要遍歷查找。
//
// 參數：
//   - ce: 要移除的快取項目
func (tw *TimingWheel) Remove(ce *cacheEntry) {
	// 標記為已刪除
	if !atomic.CompareAndSwapInt32(&ce.deleted, 0, 1) {
		return // 已經被刪除了
	}

	tw.removeFromSlot(ce)
}

// RemoveMarked 從時間輪中移除已被標記刪除的項目
//
// 這個方法假設 ce.deleted 已經被設為 1（由 ExpirationManager.Remove 執行 CAS）
// 只需要從槽中物理移除並更新計數
//
// 參數：
//   - ce: 已被標記刪除的快取項目
func (tw *TimingWheel) RemoveMarked(ce *cacheEntry) {
	tw.removeFromSlot(ce)
}

// removeFromSlot 從槽中物理移除項目並更新計數
//
// 精確計數架構（Section 9.2.4）：
// - 如果 entry 還在槽位中 → 移除並 totalCount--
// - 如果 entry 已被 expireSlot 取出（twLevel == -1）→ 不操作 totalCount
//
// 這樣設計確保每個 entry 只會被扣減一次：
// - 被 expireSlot 取出的 → 在取出時批量扣減
// - 被手動 Forget 的 → 在 Remove 時扣減
func (tw *TimingWheel) removeFromSlot(ce *cacheEntry) {
	levelIdx := atomic.LoadInt32(&ce.twLevel)

	// 檢查是否已被 expireSlot 取出（twLevel = -1）
	if levelIdx == -1 {
		// 項目已被 expireSlot 取出，totalCount 已在 expireSlot 批量扣過
		// 不在這裡重複扣減，避免雙重扣減
		return
	}

	// 嘗試從槽中移除
	slotIdx := atomic.LoadInt32(&ce.twSlot)

	if levelIdx >= 0 && levelIdx < int32(levelCount) {
		level := tw.levels[levelIdx]
		if slotIdx >= 0 && slotIdx < int32(level.size) {
			slot := level.slots[slotIdx]
			slot.mu.Lock()
			if _, exists := slot.entries[ce]; exists {
				delete(slot.entries, ce)
				slot.mu.Unlock()
				// entry 還在槽位中，需要扣減 totalCount
				atomic.AddInt64(&tw.totalCount, -1)
				return
			}
			slot.mu.Unlock()
		}
	}
	// 如果項目不在預期的槽中，說明已經被 expireSlot 取出了，totalCount 已扣過
}

// Count 回傳時間輪中的項目總數
//
// 時間複雜度：O(1)
func (tw *TimingWheel) Count() int {
	return int(atomic.LoadInt64(&tw.totalCount))
}

// IsRunning 回傳時間輪是否正在運行
func (tw *TimingWheel) IsRunning() bool {
	return atomic.LoadInt32(&tw.running) == 1
}

// Clear 清空時間輪中的所有項目
//
// 這個方法會遍歷所有層級的所有槽，清空其中的項目。
// 主要用於重置或關閉時清理資源。
func (tw *TimingWheel) Clear() {
	for _, level := range tw.levels {
		for _, slot := range level.slots {
			slot.mu.Lock()
			slot.entries = make(map[*cacheEntry]struct{}, 8)
			slot.mu.Unlock()
		}
		atomic.StoreInt64(&level.current, 0)
	}
	atomic.StoreInt64(&tw.totalCount, 0)
	atomic.StoreInt64(&tw.currentTick, 0)
}
