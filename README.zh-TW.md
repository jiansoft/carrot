# carrot

[![GitHub](https://img.shields.io/github/license/mashape/apistatus.svg)](https://github.com/jiansoft/carrot)
[![Go Report Card](https://goreportcard.com/badge/github.com/jiansoft/carrot)](https://goreportcard.com/report/github.com/jiansoft/carrot)
[![build-test](https://github.com/jiansoft/carrot/actions/workflows/go.yml/badge.svg)](https://github.com/jiansoft/carrot/actions/workflows/go.yml)
[![](https://img.shields.io/github/tag/jiansoft/carrot.svg)](https://github.com/jiansoft/carrot/releases)

[English](README.md)

一個高效能、執行緒安全的 Go 記憶體快取套件，類似於 C# 的 MemoryCache。

## 功能特色

- **多種過期策略** - 絕對時間過期、滑動過期、永不過期
- **執行緒安全** - 使用 `sync.Map` 和 `sync.Mutex` 確保並發存取安全
- **優先級佇列** - 使用最小堆積實現高效的過期項目自動清理
- **快取統計** - 追蹤命中率、未命中率及使用量
- **單例與自訂實例** - 可使用預設全域實例或建立獨立實例

## 安裝

```bash
go get github.com/jiansoft/carrot
```

需要 Go 1.19 或更高版本。

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
    carrot.Default.Delay("key", "value", time.Second)

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
| `Delay(key, val any, ttl time.Duration)` | 儲存指定時間後過期的項目。使用負數表示永不過期 |
| `Until(key, val any, until time.Time)` | 儲存在指定時間點過期的項目 |
| `Inactive(key, val any, inactive time.Duration)` | 儲存滑動過期的項目（每次存取會重置過期時間） |

### 讀取操作

| 方法 | 說明 |
|------|------|
| `Read(key any) (any, bool)` | 取得值（如果存在且未過期） |
| `Have(key any) bool` | 檢查鍵是否存在且未過期 |

### 刪除操作

| 方法 | 說明 |
|------|------|
| `Forget(key any)` | 移除指定項目 |
| `Reset()` | 清空所有項目 |

### 設定與統計

| 方法 | 說明 |
|------|------|
| `SetScanFrequency(frequency time.Duration) bool` | 設定過期掃描頻率（預設：1 分鐘） |
| `Statistics() CacheStatistics` | 取得快取統計資訊（命中、未命中、數量） |

## 過期策略

### 絕對過期

項目在固定時間後過期，與存取次數無關。

```go
// 5 分鐘後過期
carrot.Default.Delay("session", sessionData, 5*time.Minute)

// 在午夜過期
midnight := time.Now().Truncate(24*time.Hour).Add(24*time.Hour)
carrot.Default.Until("daily-cache", data, midnight)
```

### 滑動過期

項目在一段時間未被存取後過期。每次讀取都會重置過期計時器。

```go
// 最後一次存取後 10 分鐘過期
carrot.Default.Inactive("user-session", userData, 10*time.Minute)

// 每次讀取都會重置 10 分鐘的計時器
val, _ := carrot.Default.Read("user-session")
```

### 永不過期

項目會一直保留在快取中，直到手動移除。

```go
// 永不過期
carrot.Default.Forever("config", configData)

// 同樣永不過期（負數時間）
carrot.Default.Delay("settings", settings, -time.Second)
```

## 使用自訂實例

為不同用途建立獨立的快取實例。

```go
// 建立新的快取實例
sessionCache := carrot.New()
sessionCache.SetScanFrequency(30 * time.Second)

dataCache := carrot.New()
dataCache.SetScanFrequency(5 * time.Minute)

// 獨立使用
sessionCache.Inactive("user:123", session, 30*time.Minute)
dataCache.Delay("query:result", result, time.Hour)
```

## 快取統計

使用內建統計功能監控快取效能。

```go
stats := carrot.Default.Statistics()

fmt.Printf("總命中次數: %d\n", stats.TotalHits())
fmt.Printf("總未命中次數: %d\n", stats.TotalMisses())
fmt.Printf("目前項目數: %d\n", stats.UsageCount())
fmt.Printf("佇列大小: %d\n", stats.PqCount())

// 計算命中率
total := stats.TotalHits() + stats.TotalMisses()
if total > 0 {
    hitRate := float64(stats.TotalHits()) / float64(total) * 100
    fmt.Printf("命中率: %.2f%%\n", hitRate)
}
```

## 執行緒安全

Carrot 專為並發使用而設計：

- `sync.Map` 用於無鎖的快取項目儲存
- `sync.Mutex` 保護優先級佇列操作
- `atomic` 操作用於快取項目欄位（優先級、過期時間、統計資訊）
- 使用 atomic compare-and-swap 實現無鎖讀取和更新
- 背景清理非同步執行，不會阻塞讀寫操作

## 範例

完整範例請參考 [example 目錄](https://github.com/jiansoft/carrot/blob/main/exemple/main.go)。

## 授權條款

Copyright (c) 2023

以 MIT 授權條款釋出：

- [www.opensource.org/licenses/MIT](http://www.opensource.org/licenses/MIT)
