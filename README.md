# carrot

[![GitHub](https://img.shields.io/github/license/mashape/apistatus.svg)](https://github.com/jiansoft/carrot)
[![Go Report Card](https://goreportcard.com/badge/github.com/jiansoft/carrot)](https://goreportcard.com/report/github.com/jiansoft/carrot)
[![build-test](https://github.com/jiansoft/carrot/actions/workflows/go.yml/badge.svg)](https://github.com/jiansoft/carrot/actions/workflows/go.yml)
[![](https://img.shields.io/github/tag/jiansoft/carrot.svg)](https://github.com/jiansoft/carrot/releases)

[繁體中文](README.zh-TW.md)

A high-performance, thread-safe in-memory cache library for Go, with 100%+ feature parity with .NET MemoryCache.

## Features

- **Multiple expiration policies** - Absolute time, sliding expiration, or never expire
- **Thread-safe** - Uses `sync.Map` and `sync.Mutex` for concurrent access
- **Priority queue** - Efficient automatic cleanup of expired items using min-heap
- **Cache statistics** - Track hit/miss rates and usage counts
- **Singleton & custom instances** - Use the default global instance or create your own
- **GetOrCreate pattern** - Atomic get-or-create operations (similar to .NET's GetOrCreate)
- **Size limits** - Configure maximum cache size with automatic eviction
- **Priority-based eviction** - Low/Normal/High/NeverRemove priority levels
- **Eviction callbacks** - Get notified when items are evicted
- **Expiration tokens** - Cancel cache entries using context cancellation
- **Generics support** - Type-safe cache operations with Go generics

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
    carrot.Default.Expire("key", "value", time.Second)

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
| `Expire(key, val any, ttl time.Duration)` | Store an item that expires after the specified duration. Use negative duration for never expire |
| `Until(key, val any, until time.Time)` | Store an item that expires at the specified time |
| `Inactive(key, val any, inactive time.Duration)` | Store an item with sliding expiration (resets on each access) |
| `Set(key, val any, options EntryOptions)` | Store an item with full control over all options |

### Read Operations

| Method | Description |
|--------|-------------|
| `Read(key any) (any, bool)` | Get the value if it exists and hasn't expired |
| `Have(key any) bool` | Check if the key exists and hasn't expired |
| `Keys() []any` | Get all non-expired keys in the cache |
| `Count() int` | Get the number of items in the cache |

### GetOrCreate Operations

| Method | Description |
|--------|-------------|
| `GetOrCreate(key, val any, ttl time.Duration) (any, bool)` | Get existing value or create with default value |
| `GetOrCreateFunc(key any, ttl time.Duration, factory func() (any, error)) (any, bool, error)` | Get existing value or create using factory function |
| `GetOrCreateWithOptions(key, val any, options EntryOptions) (any, bool)` | Get existing value or create with full options |

### Delete Operations

| Method | Description |
|--------|-------------|
| `Forget(key any)` | Remove a specific item |
| `Reset()` | Clear all items |
| `Compact(percentage float64)` | Remove a percentage of low-priority items (0.0-1.0) |

### Configuration & Statistics

| Method | Description |
|--------|-------------|
| `SetScanFrequency(frequency time.Duration) bool` | Set how often to scan for expired items (default: 1 minute) |
| `SetSizeLimit(limit int64)` | Set maximum cache size (0 to disable) |
| `GetSizeLimit() int64` | Get current size limit |
| `GetCurrentSize() int64` | Get current total size of all entries |
| `Statistics() CacheStatistics` | Get cache statistics (hits, misses, counts) |

## EntryOptions

Full control over cache entry behavior:

```go
type EntryOptions struct {
    TimeToLive           time.Duration         // Expiration duration
    SlidingExpiration    time.Duration         // Sliding expiration duration
    Priority             CachePriority         // Eviction priority
    Size                 int64                 // Entry size for size limit
    PostEvictionCallback PostEvictionCallback  // Callback on eviction
    ExpirationToken      context.Context       // Cancellation token
}
```

### Cache Priority Levels

```go
const (
    PriorityLow         // Evicted first
    PriorityNormal      // Default priority
    PriorityHigh        // Evicted last
    PriorityNeverRemove // Never automatically evicted
)
```

### Eviction Reasons

```go
const (
    EvictionReasonNone         // Not evicted
    EvictionReasonRemoved      // Manually removed via Forget()
    EvictionReasonReplaced     // Replaced by new value
    EvictionReasonExpired      // Expired by time
    EvictionReasonCapacity     // Evicted due to size limit
    EvictionReasonTokenExpired // Evicted due to token cancellation
)
```

## Advanced Usage

### GetOrCreate Pattern

Atomic get-or-create operations to avoid race conditions:

```go
// Simple get or create
val, existed := cache.GetOrCreate("user:123", defaultUser, time.Hour)

// With factory function
val, existed, err := cache.GetOrCreateFunc("user:123", time.Hour, func() (any, error) {
    return fetchUserFromDB(123)
})

// With full options
val, existed := cache.GetOrCreateWithOptions("user:123", defaultUser, EntryOptions{
    TimeToLive: time.Hour,
    Priority:   PriorityHigh,
})
```

### Size Limits & Eviction

Configure maximum cache size with automatic eviction:

```go
cache := carrot.New[string, []byte]()
cache.SetSizeLimit(1000) // Maximum total size

// Items with size
cache.Set("large-data", data, carrot.EntryOptions{
    Size:       100,
    TimeToLive: time.Hour,
    Priority:   carrot.PriorityLow, // Evicted first when size exceeded
})

// Manual compaction
cache.Compact(0.2) // Remove 20% of low-priority items
```

### Eviction Callbacks

Get notified when items are evicted:

```go
cache.Set("session", sessionData, EntryOptions{
    TimeToLive: 30 * time.Minute,
    PostEvictionCallback: func(key, value any, reason EvictionReason) {
        switch reason {
        case EvictionReasonExpired:
            log.Printf("Session %v expired", key)
        case EvictionReasonRemoved:
            log.Printf("Session %v manually removed", key)
        case EvictionReasonCapacity:
            log.Printf("Session %v evicted due to capacity", key)
        }
    },
})
```

### Expiration Tokens

Cancel cache entries using context:

```go
ctx, cancel := context.WithCancel(context.Background())

cache.Set("operation-result", result, EntryOptions{
    TimeToLive:      time.Hour,
    ExpirationToken: ctx,
})

// Later, cancel to immediately expire the entry
cancel()
```

### Type-Safe Generics (Recommended)

Use the generic API for type safety. This is the recommended way to use carrot:

```go
// Create a typed cache - recommended way
userCache := carrot.New[string, *User]()

// All operations are type-safe
userCache.Expire("user:123", &User{ID: 123, Name: "Alice"}, time.Hour)

user, ok := userCache.Read("user:123")
if ok {
    fmt.Println(user.Name) // No type assertion needed
}

// GetOrCreate with typed factory
user, existed, err := userCache.GetOrCreateFunc("user:456", time.Hour, func() (*User, error) {
    return fetchUser(456)
})

// Get all keys as typed slice
keys := userCache.Keys() // []string
```

### Shared Underlying Storage

Multiple typed caches can share the same underlying storage:

```go
// Create a base untyped cache
baseCache := carrot.NewCache()

// Create multiple typed views sharing the same storage
intCache := carrot.From[string, int](baseCache)
strCache := carrot.From[string, string](baseCache)

// Store values through different typed views
intCache.Expire("count", 42, time.Hour)
strCache.Expire("name", "Alice", time.Hour)

// Both share the same underlying storage
fmt.Println(intCache.Count()) // 2
fmt.Println(strCache.Count()) // 2

// Access underlying cache if needed
underlying := intCache.Underlying()
```

## Expiration Policies

### Absolute Expiration

Items expire at a fixed time, regardless of access.

```go
// Expires after 5 minutes
carrot.Default.Expire("session", sessionData, 5*time.Minute)

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
carrot.Default.Expire("settings", settings, -time.Second)
```

## Using Custom Instances

Create separate cache instances for different purposes.

```go
// Create typed cache instances (recommended)
sessionCache := carrot.New[string, *Session]()
sessionCache.SetScanFrequency(30 * time.Second)
sessionCache.SetSizeLimit(10000)

dataCache := carrot.New[string, QueryResult]()
dataCache.SetScanFrequency(5 * time.Minute)

// Use independently with type safety
sessionCache.Inactive("user:123", session, 30*time.Minute)
dataCache.Expire("query:result", result, time.Hour)

// Or create untyped cache for backward compatibility
untypedCache := carrot.NewCache()
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
- `sync.RWMutex` protects priority queue operations (allows concurrent reads)
- `atomic` operations for cache entry fields (priority, expiration, statistics)
- Lock-free reads and updates using atomic compare-and-swap
- Background cleanup runs asynchronously without blocking read/write operations

## Performance

Carrot is optimized for high-performance scenarios, comparable to .NET MemoryCache:

| Operation | Performance | Allocations |
|-----------|-------------|-------------|
| Read | ~35-52 ns/op | 0 |
| Read (parallel) | ~35 ns/op | 0 |
| Write (Expire) | ~476 ns/op | 4 |
| GetOrCreate | ~32-52 ns/op | 1 |
| Forget | ~655 ns/op | 0 |
| High Contention Read | ~35 ns/op | 0 |

### Comparison with .NET MemoryCache

| Feature | .NET MemoryCache | Carrot (Go) |
|---------|------------------|-------------|
| Read latency | ~20-50 ns | ~35-52 ns |
| Write latency | ~50-200 ns | ~476 ns* |
| GetOrCreate | ~50-100 ns | ~32-52 ns |
| Concurrent scaling | Excellent | Excellent |
| Memory overhead | Low | Low |

\* Write latency includes priority queue maintenance for automatic expiration

### Running Benchmarks

```bash
go test -bench=. -benchmem
```

## .NET MemoryCache Compatibility

Carrot provides 100%+ feature parity with .NET MemoryCache:

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
| `AbsoluteExpiration` | `TimeToLive` in options |
| `SlidingExpiration` | `SlidingExpiration` in options |
| `Priority` | `Priority` in options |
| `PostEvictionCallbacks` | `PostEvictionCallback` in options |
| `ExpirationTokens` | `ExpirationToken` in options |

## Example

See the [example directory](https://github.com/jiansoft/carrot/blob/main/example/main.go) for a complete working example.

## License

Copyright (c) 2023

Released under the MIT license:

- [www.opensource.org/licenses/MIT](http://www.opensource.org/licenses/MIT)
