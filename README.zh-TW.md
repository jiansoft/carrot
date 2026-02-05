# carrot

[![GitHub](https://img.shields.io/github/license/mashape/apistatus.svg)](https://github.com/jiansoft/carrot)
[![Go Report Card](https://goreportcard.com/badge/github.com/jiansoft/carrot)](https://goreportcard.com/report/github.com/jiansoft/carrot)
[![build-test](https://github.com/jiansoft/carrot/actions/workflows/go.yml/badge.svg)](https://github.com/jiansoft/carrot/actions/workflows/go.yml)
[![](https://img.shields.io/github/tag/jiansoft/carrot.svg)](https://github.com/jiansoft/carrot/releases)

[English](README.md)

一個高效能、執行緒安全的 Go 記憶體快取套件，與 .NET MemoryCache 100%+ 功能對等。

## 功能特色

- **多種過期策略** - 絕對時間過期、滑動過期、永不過期
- **執行緒安全** - 使用 `sync.Map` 和 `sync.Mutex` 確保並發存取安全
- **混合型過期管理** - 短 TTL 使用時間輪（批次過期）+ 長 TTL 使用分片優先佇列（高效單項操作）
- **快取統計** - 追蹤命中率、未命中率及使用量
- **單例與自訂實例** - 可使用預設全域實例或建立獨立實例
- **GetOrCreate 模式** - 原子性的取得或建立操作（類似 .NET 的 GetOrCreate）
- **大小限制** - 設定最大快取大小，自動驅逐項目
- **優先級驅逐** - Low/Normal/High/NeverRemove 優先級等級
- **驅逐回調** - 項目被驅逐時收到通知
- **過期令牌** - 使用 context 取消機制使快取項目過期
- **泛型支援** - 使用 Go 泛型實現型別安全的快取操作

## 安裝

```bash
go get github.com/jiansoft/carrot
```

需要 Go 1.22 或更高版本。

## 快速開始

```go
package main

import (
    "fmt"
    "time"

    "github.com/jiansoft/carrot"
)

func main() {
    // 儲存一個 1 秒後過期的項目
    carrot.Default.Expire("key", "value", time.Second)

    // 讀取值
    val, ok := carrot.Default.Read("key")
    if ok {
        fmt.Println(val) // 輸出: value
    }

    // 檢查鍵是否存在
    exists := carrot.Default.Have("key")

    // 移除項目
    carrot.Default.Forget("key")

    // 清空所有項目
    carrot.Default.Reset()
}
```

## API 參考

### 寫入操作

| 方法 | 說明 |
|------|------|
| `Forever(key, val any)` | 儲存永不過期的項目 |
| `Expire(key, val any, ttl time.Duration)` | 儲存指定時間後過期的項目。使用負數表示永不過期 |
| `Until(key, val any, until time.Time)` | 儲存在指定時間點過期的項目 |
| `Sliding(key, val any, sliding time.Duration)` | 儲存滑動過期的項目（每次存取會重置過期時間）。duration ≤ 0 時不執行任何操作 |
| `Set(key, val any, options EntryOptions)` | 使用完整選項儲存項目 |

### 讀取操作

| 方法 | 說明 |
|------|------|
| `Read(key any) (any, bool)` | 取得值（如果存在且未過期） |
| `Have(key any) bool` | 檢查鍵是否存在且未過期 |
| `Keys() []any` | 取得快取中所有未過期的鍵 |
| `Count() int` | 取得快取中的項目數量 |

### GetOrCreate 操作

| 方法 | 說明 |
|------|------|
| `GetOrCreate(key, val any, ttl time.Duration) (any, bool)` | 取得現有值或使用預設值建立 |
| `GetOrCreateFunc(key any, ttl time.Duration, factory func() (any, error)) (any, bool, error)` | 取得現有值或使用工廠函數建立 |
| `GetOrCreateWithOptions(key, val any, options EntryOptions) (any, bool)` | 取得現有值或使用完整選項建立 |

### 刪除操作

| 方法 | 說明 |
|------|------|
| `Forget(key any)` | 移除指定項目 |
| `Reset()` | 清空所有項目 |
| `Compact(percentage float64)` | 移除指定百分比的低優先級項目 (0.0-1.0) |

### 設定與統計

| 方法 | 說明 |
|------|------|
| `SetScanFrequency(frequency time.Duration) bool` | 設定過期掃描頻率（預設：1 分鐘） |
| `SetSizeLimit(limit int64)` | 設定最大快取大小（0 表示無限制） |
| `GetSizeLimit() int64` | 取得目前的大小限制 |
| `GetCurrentSize() int64` | 取得所有項目的目前總大小 |
| `Statistics() CacheStatistics` | 取得快取統計資訊（命中、未命中、數量） |
| `SetExpirationStrategy(s ExpirationStrategy)` | 設定過期策略（Auto/TimingWheel/PriorityQueue） |
| `SetShortTTLThreshold(d time.Duration)` | 設定短 TTL 閾值（預設：1 小時） |
| `ExpirationStats() ExpirationManagerStats` | 取得詳細的過期統計資訊 |
| `ShrinkExpirationQueue()` | 整理內部優先佇列的記憶體碎片 |
| `Stop()` | 停止背景過期 goroutine（使用完畢時呼叫） |

## EntryOptions

完整控制快取項目行為：

```go
type EntryOptions struct {
    TimeToLive           time.Duration         // 過期時間
    SlidingExpiration    time.Duration         // 滑動過期時間
    Priority             CachePriority         // 驅逐優先級
    Size                 int64                 // 項目大小（用於大小限制計算）
    PostEvictionCallback PostEvictionCallback  // 驅逐時的回調函數
    ExpirationToken      context.Context       // 取消令牌
}
```

### 快取優先級等級

```go
const (
    PriorityLow         // 最先被驅逐
    PriorityNormal      // 預設優先級
    PriorityHigh        // 最後被驅逐
    PriorityNeverRemove // 永不自動驅逐
)
```

### 驅逐原因

```go
const (
    EvictionReasonNone         // 未被驅逐
    EvictionReasonRemoved      // 透過 Forget() 手動移除
    EvictionReasonReplaced     // 被新值取代
    EvictionReasonExpired      // 時間過期
    EvictionReasonCapacity     // 因大小限制被驅逐
    EvictionReasonTokenExpired // 因令牌取消被驅逐
)
```

## 進階用法

### GetOrCreate 模式

原子性的取得或建立操作，避免競爭條件：

```go
// 簡單的取得或建立
val, existed := cache.GetOrCreate("user:123", defaultUser, time.Hour)

// 使用工廠函數
val, existed, err := cache.GetOrCreateFunc("user:123", time.Hour, func() (any, error) {
    return fetchUserFromDB(123)
})

// 使用完整選項
val, existed := cache.GetOrCreateWithOptions("user:123", defaultUser, EntryOptions{
    TimeToLive: time.Hour,
    Priority:   PriorityHigh,
})
```

### 大小限制與驅逐

設定最大快取大小，自動驅逐：

```go
cache := carrot.New[string, []byte]()
cache.SetSizeLimit(1000) // 最大總大小

// 設定項目大小
cache.Set("large-data", data, carrot.EntryOptions{
    Size:       100,
    TimeToLive: time.Hour,
    Priority:   carrot.PriorityLow, // 超過大小時優先被驅逐
})

// 手動壓縮
cache.Compact(0.2) // 移除 20% 的低優先級項目
```

### 驅逐回調

項目被驅逐時收到通知：

```go
cache.Set("session", sessionData, EntryOptions{
    TimeToLive: 30 * time.Minute,
    PostEvictionCallback: func(key, value any, reason EvictionReason) {
        switch reason {
        case EvictionReasonExpired:
            log.Printf("Session %v 已過期", key)
        case EvictionReasonRemoved:
            log.Printf("Session %v 被手動移除", key)
        case EvictionReasonCapacity:
            log.Printf("Session %v 因容量限制被驅逐", key)
        }
    },
})
```

### 過期令牌

使用 context 取消快取項目：

```go
ctx, cancel := context.WithCancel(context.Background())

cache.Set("operation-result", result, EntryOptions{
    TimeToLive:      time.Hour,
    ExpirationToken: ctx,
})

// 之後呼叫 cancel 立即使項目過期
cancel()
```

### 型別安全的泛型（建議使用）

使用泛型 API 實現型別安全。這是使用 carrot 的建議方式：

```go
// 建立型別化的快取 - 建議方式
userCache := carrot.New[string, *User]()

// 所有操作都是型別安全的
userCache.Expire("user:123", &User{ID: 123, Name: "Alice"}, time.Hour)

user, ok := userCache.Read("user:123")
if ok {
    fmt.Println(user.Name) // 不需要型別斷言
}

// 使用型別化工廠的 GetOrCreate
user, existed, err := userCache.GetOrCreateFunc("user:456", time.Hour, func() (*User, error) {
    return fetchUser(456)
})

// 取得所有鍵（型別化的切片）
keys := userCache.Keys() // []string
```

### 共享底層儲存空間

多個型別化快取可以共享相同的底層儲存空間：

```go
// 建立一個底層非型別化快取
baseCache := carrot.NewCache()

// 建立多個共享相同儲存空間的型別化視圖
intCache := carrot.From[string, int](baseCache)
strCache := carrot.From[string, string](baseCache)

// 透過不同的型別化視圖儲存值
intCache.Expire("count", 42, time.Hour)
strCache.Expire("name", "Alice", time.Hour)

// 兩者共享相同的底層儲存空間
fmt.Println(intCache.Count()) // 2
fmt.Println(strCache.Count()) // 2

// 需要時可存取底層快取
underlying := intCache.Underlying()
```

## 過期策略

### 絕對過期

項目在固定時間後過期，與存取次數無關。

```go
// 5 分鐘後過期
carrot.Default.Expire("session", sessionData, 5*time.Minute)

// 在午夜過期
midnight := time.Now().Truncate(24*time.Hour).Add(24*time.Hour)
carrot.Default.Until("daily-cache", data, midnight)
```

### 滑動過期

項目在一段時間未被存取後過期。每次讀取都會重置過期計時器。

```go
// 最後一次存取後 10 分鐘過期
carrot.Default.Sliding("user-session", userData, 10*time.Minute)

// 每次讀取都會重置 10 分鐘的計時器
val, _ := carrot.Default.Read("user-session")
```

### 永不過期

項目會一直保留在快取中，直到手動移除。

```go
// 永不過期
carrot.Default.Forever("config", configData)

// 同樣永不過期（負數時間）
carrot.Default.Expire("settings", settings, -time.Second)
```

## 過期管理

Carrot 使用混合型方式管理過期：

| 資料結構 | TTL 範圍 | 特性 |
|----------|----------|------|
| **時間輪 (TimingWheel)** | ≤ 閾值（預設：1 小時） | O(1) 批次過期，適合高頻率短 TTL |
| **分片優先佇列 (ShardedPriorityQueue)** | > 閾值 | O(log n) 單項操作，適合長 TTL |
| **無** | 永久 | 項目不放入任何過期佇列 |

### 設定過期策略

```go
// 選項 1：調整閾值（預設：1 小時）
// TTL ≤ 5 分鐘 → 時間輪，TTL > 5 分鐘 → 分片優先佇列
cache.SetShortTTLThreshold(5 * time.Minute)

// 選項 2：強制使用特定策略
cache.SetExpirationStrategy(carrot.StrategyTimingWheel)    // 所有項目使用時間輪
cache.SetExpirationStrategy(carrot.StrategyPriorityQueue)  // 所有項目使用分片優先佇列
cache.SetExpirationStrategy(carrot.StrategyAuto)           // 根據 TTL 自動選擇（預設）

// 查看統計
emStats := cache.ExpirationStats()
fmt.Printf("短 TTL 比例: %.2f%%\n",
    float64(emStats.TwAddCount) / float64(emStats.TwAddCount + emStats.SpqAddCount) * 100)
```

### 建議的閾值設定

| 使用場景 | 建議閾值 | 說明 |
|----------|----------|------|
| 一般用途 | 1 小時（預設） | 平衡效能 |
| 高頻率短 TTL | 5-15 分鐘 | 更多項目使用時間輪 |
| 主要為長 TTL | 24 小時 | 幾乎全部使用分片優先佇列 |
| 超短 TTL | 1 分鐘 | 只有非常短的 TTL 使用時間輪 |

### 資源清理

使用完快取後，呼叫 `Stop()` 釋放背景 goroutine：

```go
cache := carrot.NewCache()
defer cache.Stop()  // 釋放時間輪 goroutine
```

## 使用自訂實例

為不同用途建立獨立的快取實例。

```go
// 建立型別化的快取實例（建議）
sessionCache := carrot.New[string, *Session]()
sessionCache.SetScanFrequency(30 * time.Second)
sessionCache.SetSizeLimit(10000)

dataCache := carrot.New[string, QueryResult]()
dataCache.SetScanFrequency(5 * time.Minute)

// 以型別安全的方式獨立使用
sessionCache.Sliding("user:123", session, 30*time.Minute)
dataCache.Expire("query:result", result, time.Hour)

// 或建立非型別化的快取以保持向後相容
untypedCache := carrot.NewCache()
```

## 快取統計

使用內建統計功能監控快取效能。

```go
stats := carrot.Default.Statistics()

fmt.Printf("總命中次數: %d\n", stats.TotalHits())
fmt.Printf("總未命中次數: %d\n", stats.TotalMisses())
fmt.Printf("目前項目數: %d\n", stats.UsageCount())
fmt.Printf("時間輪項目數: %d\n", stats.TwCount())    // 短 TTL 項目
fmt.Printf("優先佇列項目數: %d\n", stats.PqCount())  // 長 TTL 項目

// 計算命中率
total := stats.TotalHits() + stats.TotalMisses()
if total > 0 {
    hitRate := float64(stats.TotalHits()) / float64(total) * 100
    fmt.Printf("命中率: %.2f%%\n", hitRate)
}

// 詳細的過期統計
emStats := carrot.Default.ExpirationStats()
fmt.Printf("時間輪過期數: %d\n", emStats.TwExpireCount)
fmt.Printf("優先佇列過期數: %d\n", emStats.SpqExpireCount)
fmt.Printf("永久項目數: %d\n", emStats.ForeverCount)
```

## 執行緒安全

Carrot 專為並發使用而設計：

- `sync.Map` 用於無鎖的快取項目儲存
- 分片優先佇列使用每分片獨立鎖，提高並發性能
- 時間輪使用槽位級別的鎖，實現高效的批次過期
- `atomic` 操作用於快取項目欄位（優先級、過期時間、統計資訊）
- CAS（Compare-And-Swap）確保回調只執行一次
- 背景清理非同步執行，不會阻塞讀寫操作

## 效能

Carrot 針對高效能場景進行了優化，效能可與 .NET MemoryCache 比肩：

| 操作 | 效能 | 記憶體分配 |
|------|------|-----------|
| 讀取 | ~35-52 ns/op | 0 |
| 讀取（並行） | ~35 ns/op | 0 |
| 寫入（Expire） | ~350-476 ns/op | 4 |
| GetOrCreate | ~32-52 ns/op | 1 |
| Forget | ~185-655 ns/op | 0 |
| 高競爭讀取 | ~35 ns/op | 0 |
| 批次過期（時間輪） | ~34 μs/1000 項目 | - |

### 與 .NET MemoryCache 的比較

| 功能 | .NET MemoryCache | Carrot (Go) |
|------|------------------|-------------|
| 讀取延遲 | ~20-50 ns | ~35-52 ns |
| 寫入延遲 | ~50-200 ns | ~476 ns* |
| GetOrCreate | ~50-100 ns | ~32-52 ns |
| 並發擴展性 | 優秀 | 優秀 |
| 記憶體開銷 | 低 | 低 |

\* 寫入延遲包含用於自動過期的優先級佇列維護

### 執行基準測試

```bash
go test -bench=. -benchmem
```

## .NET MemoryCache 相容性

Carrot 提供與 .NET MemoryCache 100%+ 功能對等：

| .NET MemoryCache | Carrot |
|------------------|--------|
| `Set(key, value, policy)` | `Set(key, val, options)` |
| `GetOrCreate()` | `GetOrCreate()`, `GetOrCreateFunc()` |
| `TryGetValue()` | `Read()` |
| `Remove()` | `Forget()` |
| `Clear()` | `Reset()` |
| `Count` | `Count()` |
| `Compact()` | `Compact()` |
| `SizeLimit` | `SetSizeLimit()`, `GetSizeLimit()` |
| `AbsoluteExpiration` | options 中的 `TimeToLive` |
| `SlidingExpiration` | options 中的 `SlidingExpiration` |
| `Priority` | options 中的 `Priority` |
| `PostEvictionCallbacks` | options 中的 `PostEvictionCallback` |
| `ExpirationTokens` | options 中的 `ExpirationToken` |

## 範例

完整範例請參考 [example 目錄](https://github.com/jiansoft/carrot/blob/main/example/main.go)。

## 授權條款

Copyright (c) 2023

以 MIT 授權條款釋出：

- [www.opensource.org/licenses/MIT](http://www.opensource.org/licenses/MIT)
