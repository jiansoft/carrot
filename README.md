# carrot

[![GitHub](https://img.shields.io/github/license/mashape/apistatus.svg)](https://github.com/jiansoft/carrot)
[![Go Report Card](https://goreportcard.com/badge/github.com/jiansoft/carrot)](https://goreportcard.com/report/github.com/jiansoft/carrot)
[![build-test](https://github.com/jiansoft/carrot/actions/workflows/go.yml/badge.svg)](https://github.com/jiansoft/carrot/actions/workflows/go.yml)
[![](https://img.shields.io/github/tag/jiansoft/carrot.svg)](https://github.com/jiansoft/carrot/releases)

[繁體中文](README.zh-TW.md)

A high-performance, thread-safe in-memory cache library for Go, similar to C# MemoryCache.

## Features

- **Multiple expiration policies** - Absolute time, sliding expiration, or never expire
- **Thread-safe** - Uses `sync.Map` and `sync.Mutex` for concurrent access
- **Priority queue** - Efficient automatic cleanup of expired items using min-heap
- **Cache statistics** - Track hit/miss rates and usage counts
- **Singleton & custom instances** - Use the default global instance or create your own

## Installation

```bash
go get github.com/jiansoft/carrot
```

Requires Go 1.19 or later.

## Quick Start

```go
package main

import (
    "fmt"
    "time"

    "github.com/jiansoft/carrot"
)

func main() {
    // Store an item that expires after 1 second
    carrot.Default.Delay("key", "value", time.Second)

    // Read the value
    val, ok := carrot.Default.Read("key")
    if ok {
        fmt.Println(val) // Output: value
    }

    // Check if key exists
    exists := carrot.Default.Have("key")

    // Remove an item
    carrot.Default.Forget("key")

    // Clear all items
    carrot.Default.Reset()
}
```

## API Reference

### Write Operations

| Method | Description |
|--------|-------------|
| `Forever(key, val any)` | Store an item that never expires |
| `Delay(key, val any, ttl time.Duration)` | Store an item that expires after the specified duration. Use negative duration for never expire |
| `Until(key, val any, until time.Time)` | Store an item that expires at the specified time |
| `Inactive(key, val any, inactive time.Duration)` | Store an item with sliding expiration (resets on each access) |

### Read Operations

| Method | Description |
|--------|-------------|
| `Read(key any) (any, bool)` | Get the value if it exists and hasn't expired |
| `Have(key any) bool` | Check if the key exists and hasn't expired |

### Delete Operations

| Method | Description |
|--------|-------------|
| `Forget(key any)` | Remove a specific item |
| `Reset()` | Clear all items |

### Configuration & Statistics

| Method | Description |
|--------|-------------|
| `SetScanFrequency(frequency time.Duration) bool` | Set how often to scan for expired items (default: 1 minute) |
| `Statistics() CacheStatistics` | Get cache statistics (hits, misses, counts) |

## Expiration Policies

### Absolute Expiration

Items expire at a fixed time, regardless of access.

```go
// Expires after 5 minutes
carrot.Default.Delay("session", sessionData, 5*time.Minute)

// Expires at midnight
midnight := time.Now().Truncate(24*time.Hour).Add(24*time.Hour)
carrot.Default.Until("daily-cache", data, midnight)
```

### Sliding Expiration

Items expire after a period of inactivity. Each read resets the expiration timer.

```go
// Expires 10 minutes after last access
carrot.Default.Inactive("user-session", userData, 10*time.Minute)

// Each read resets the 10-minute timer
val, _ := carrot.Default.Read("user-session")
```

### Never Expire

Items remain in cache until manually removed.

```go
// Never expires
carrot.Default.Forever("config", configData)

// Also never expires (negative duration)
carrot.Default.Delay("settings", settings, -time.Second)
```

## Using Custom Instances

Create separate cache instances for different purposes.

```go
// Create a new cache instance
sessionCache := carrot.New()
sessionCache.SetScanFrequency(30 * time.Second)

dataCache := carrot.New()
dataCache.SetScanFrequency(5 * time.Minute)

// Use independently
sessionCache.Inactive("user:123", session, 30*time.Minute)
dataCache.Delay("query:result", result, time.Hour)
```

## Cache Statistics

Monitor cache performance with built-in statistics.

```go
stats := carrot.Default.Statistics()

fmt.Printf("Total Hits: %d\n", stats.TotalHits())
fmt.Printf("Total Misses: %d\n", stats.TotalMisses())
fmt.Printf("Current Items: %d\n", stats.UsageCount())
fmt.Printf("Queue Size: %d\n", stats.PqCount())

// Calculate hit rate
total := stats.TotalHits() + stats.TotalMisses()
if total > 0 {
    hitRate := float64(stats.TotalHits()) / float64(total) * 100
    fmt.Printf("Hit Rate: %.2f%%\n", hitRate)
}
```

## Thread Safety

Carrot is designed for concurrent use:

- `sync.Map` for lock-free cache entry storage
- `sync.Mutex` protects priority queue operations
- `atomic` operations for cache entry fields (priority, expiration, statistics)
- Lock-free reads and updates using atomic compare-and-swap
- Background cleanup runs asynchronously without blocking read/write operations

## Example

See the [example directory](https://github.com/jiansoft/carrot/blob/main/exemple/main.go) for a complete working example.

## License

Copyright (c) 2023

Released under the MIT license:

- [www.opensource.org/licenses/MIT](http://www.opensource.org/licenses/MIT)
