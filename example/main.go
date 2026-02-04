package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/jiansoft/carrot"
)

func main() {
	log.Println("=== Carrot Cache Example ===")
	log.Println()

	// 1. Basic usage with New[K,V]() and Default singleton
	demonstrateBasicUsage()

	// 2. Expiration policies
	demonstrateExpirationPolicies()

	// 3. GetOrCreate patterns
	demonstrateGetOrCreate()

	// 4. Size limit and Compact
	demonstrateSizeLimitAndCompact()

	// 5. Priority levels
	demonstratePriorityLevels()

	// 6. Post eviction callback
	demonstrateEvictionCallback()

	// 7. Expiration token (context cancellation)
	demonstrateExpirationToken()

	// 8. Shared cache with From[K,V]()
	demonstrateSharedCache()

	// 9. Statistics
	demonstrateStatistics()

	log.Println()
	log.Println("=== All examples completed ===")
}

// demonstrateBasicUsage shows basic cache operations with generic API
func demonstrateBasicUsage() {
	log.Println("--- 1. Basic Usage (Generic API) ---")

	// Create a new typed cache instance - recommended way
	cache := carrot.New[string, string]()

	// Set scan frequency (default is 1 minute)
	cache.SetScanFrequency(time.Second)

	// Store and read values - type safe!
	cache.Forever("key1", "value1")
	val, ok := cache.Read("key1") // val is string, no type assertion needed
	log.Printf("Read key1: value=%v, found=%v", val, ok)

	// Check if key exists
	exists := cache.Have("key1")
	log.Printf("Have key1: %v", exists)

	// Remove a key
	cache.Forget("key1")
	log.Printf("After Forget, Have key1: %v", cache.Have("key1"))

	// Use Default singleton (untyped, for backward compatibility)
	carrot.Default.Forever("singleton_key", "singleton_value")
	sval, ok := carrot.Default.Read("singleton_key")
	log.Printf("Default singleton Read: value=%v, found=%v", sval, ok)
	carrot.Default.Forget("singleton_key")

	log.Println()
}

// demonstrateExpirationPolicies shows different expiration strategies
func demonstrateExpirationPolicies() {
	log.Println("--- 2. Expiration Policies ---")

	cache := carrot.New[string, string]()
	cache.SetScanFrequency(100 * time.Millisecond)

	// Forever - never expires
	cache.Forever("forever_key", "I never expire")
	log.Printf("Forever key exists: %v", cache.Have("forever_key"))

	// Expire - expires after duration (negative means never)
	cache.Expire("expire_key", "I expire in 200ms", 200*time.Millisecond)
	cache.Expire("never_expire", "Negative TTL means forever", -time.Second)
	log.Printf("Expire key exists: %v", cache.Have("expire_key"))

	// Until - expires at specific time
	cache.Until("until_key", "I expire at specific time", time.Now().Add(200*time.Millisecond))
	log.Printf("Until key exists: %v", cache.Have("until_key"))

	// Inactive (sliding expiration) - expires after period of inactivity
	cache.Inactive("inactive_key", "I expire if not accessed", 200*time.Millisecond)
	log.Printf("Inactive key exists: %v", cache.Have("inactive_key"))

	// Wait for expiration
	time.Sleep(300 * time.Millisecond)
	log.Printf("After 300ms - expire_key exists: %v", cache.Have("expire_key"))
	log.Printf("After 300ms - never_expire exists: %v", cache.Have("never_expire"))
	log.Printf("After 300ms - forever_key exists: %v", cache.Have("forever_key"))

	// Reset clears all
	cache.Reset()
	log.Printf("After Reset - forever_key exists: %v", cache.Have("forever_key"))

	log.Println()
}

// demonstrateGetOrCreate shows atomic get-or-create patterns
func demonstrateGetOrCreate() {
	log.Println("--- 3. GetOrCreate Patterns ---")

	cache := carrot.New[string, string]()

	// GetOrCreate - returns existing or creates new
	val1, existed := cache.GetOrCreate("goc_key", "default_value", time.Hour)
	log.Printf("GetOrCreate (first call): value=%v, existed=%v", val1, existed)

	val2, existed := cache.GetOrCreate("goc_key", "another_value", time.Hour)
	log.Printf("GetOrCreate (second call): value=%v, existed=%v", val2, existed)

	// GetOrCreateFunc - with typed factory function
	callCount := 0
	factory := func() (string, error) {
		callCount++
		return fmt.Sprintf("factory_value_%d", callCount), nil
	}

	val3, existed, err := cache.GetOrCreateFunc("gocf_key", time.Hour, factory)
	log.Printf("GetOrCreateFunc (first): value=%v, existed=%v, err=%v", val3, existed, err)

	val4, existed, err := cache.GetOrCreateFunc("gocf_key", time.Hour, factory)
	log.Printf("GetOrCreateFunc (second): value=%v, existed=%v, err=%v", val4, existed, err)
	log.Printf("Factory was called %d time(s)", callCount)

	// GetOrCreateFunc with error
	errorFactory := func() (string, error) {
		return "", errors.New("factory error")
	}
	_, _, err = cache.GetOrCreateFunc("error_key", time.Hour, errorFactory)
	log.Printf("GetOrCreateFunc with error: err=%v", err)

	// GetOrCreateWithOptions - with full options
	val5, existed := cache.GetOrCreateWithOptions("goco_key", "options_value", carrot.EntryOptions{
		TimeToLive: time.Hour,
		Priority:   carrot.PriorityHigh,
	})
	log.Printf("GetOrCreateWithOptions: value=%v, existed=%v", val5, existed)

	log.Println()
}

// demonstrateSizeLimitAndCompact shows size management
func demonstrateSizeLimitAndCompact() {
	log.Println("--- 4. Size Limit and Compact ---")

	cache := carrot.New[string, string]()

	// Set size limit
	cache.SetSizeLimit(100)
	log.Printf("Size limit set to: %d", cache.GetSizeLimit())

	// Add items with sizes
	for i := 0; i < 5; i++ {
		cache.Set(fmt.Sprintf("sized_key_%d", i), fmt.Sprintf("value_%d", i), carrot.EntryOptions{
			Size:       30,
			TimeToLive: time.Hour,
			Priority:   carrot.PriorityLow,
		})
	}

	log.Printf("After adding 5 items (size=30 each)")
	log.Printf("Current size: %d", cache.GetCurrentSize())
	log.Printf("Count: %d", cache.Count())

	// Keys returns all non-expired keys (typed!)
	keys := cache.Keys() // []string
	log.Printf("Keys in cache: %v", keys)

	// Compact removes percentage of low-priority items
	cache.Compact(0.5) // Remove 50%
	log.Printf("After Compact(0.5) - Count: %d", cache.Count())

	log.Println()
}

// demonstratePriorityLevels shows priority-based eviction
func demonstratePriorityLevels() {
	log.Println("--- 5. Priority Levels ---")

	cache := carrot.New[string, string]()
	cache.SetSizeLimit(50)

	// Add items with different priorities
	cache.Set("low_priority", "low", carrot.EntryOptions{
		Size:       20,
		TimeToLive: time.Hour,
		Priority:   carrot.PriorityLow,
	})

	cache.Set("normal_priority", "normal", carrot.EntryOptions{
		Size:       20,
		TimeToLive: time.Hour,
		Priority:   carrot.PriorityNormal,
	})

	cache.Set("high_priority", "high", carrot.EntryOptions{
		Size:       20,
		TimeToLive: time.Hour,
		Priority:   carrot.PriorityHigh,
	})

	cache.Set("never_remove", "protected", carrot.EntryOptions{
		Size:       20,
		TimeToLive: time.Hour,
		Priority:   carrot.PriorityNeverRemove,
	})

	log.Printf("Added 4 items with different priorities")
	log.Printf("Current size: %d (limit: %d)", cache.GetCurrentSize(), cache.GetSizeLimit())

	// Low priority items are evicted first when size limit exceeded
	log.Printf("low_priority exists: %v", cache.Have("low_priority"))
	log.Printf("normal_priority exists: %v", cache.Have("normal_priority"))
	log.Printf("high_priority exists: %v", cache.Have("high_priority"))
	log.Printf("never_remove exists: %v", cache.Have("never_remove"))

	log.Println()
}

// demonstrateEvictionCallback shows post-eviction callbacks
func demonstrateEvictionCallback() {
	log.Println("--- 6. Post Eviction Callback ---")

	cache := carrot.New[string, string]()
	cache.SetScanFrequency(50 * time.Millisecond)

	// Set with callback
	cache.Set("callback_key", "callback_value", carrot.EntryOptions{
		TimeToLive: 100 * time.Millisecond,
		PostEvictionCallback: func(key, value any, reason carrot.EvictionReason) {
			reasonStr := ""
			switch reason {
			case carrot.EvictionReasonExpired:
				reasonStr = "Expired"
			case carrot.EvictionReasonRemoved:
				reasonStr = "Removed"
			case carrot.EvictionReasonReplaced:
				reasonStr = "Replaced"
			case carrot.EvictionReasonCapacity:
				reasonStr = "Capacity"
			case carrot.EvictionReasonTokenExpired:
				reasonStr = "TokenExpired"
			default:
				reasonStr = "None"
			}
			log.Printf("  [Callback] Key=%v evicted, Reason=%s", key, reasonStr)
		},
	})

	log.Printf("Item set with eviction callback, waiting for expiration...")
	time.Sleep(200 * time.Millisecond)

	// Also test manual removal callback
	cache.Set("manual_remove", "value", carrot.EntryOptions{
		TimeToLive: time.Hour,
		PostEvictionCallback: func(key, value any, reason carrot.EvictionReason) {
			log.Printf("  [Callback] Key=%v manually removed", key)
		},
	})
	cache.Forget("manual_remove")
	time.Sleep(50 * time.Millisecond) // Wait for async callback

	log.Println()
}

// demonstrateExpirationToken shows context-based cancellation
func demonstrateExpirationToken() {
	log.Println("--- 7. Expiration Token (Context Cancellation) ---")

	cache := carrot.New[string, string]()

	// Create a cancellable context
	ctx, cancel := context.WithCancel(context.Background())

	cache.Set("token_key", "token_value", carrot.EntryOptions{
		TimeToLive:      time.Hour, // Long TTL
		ExpirationToken: ctx,
		PostEvictionCallback: func(key, value any, reason carrot.EvictionReason) {
			if reason == carrot.EvictionReasonTokenExpired {
				log.Printf("  [Callback] Key=%v expired due to token cancellation", key)
			}
		},
	})

	log.Printf("token_key exists before cancel: %v", cache.Have("token_key"))

	// Cancel the context to expire the item
	cancel()

	// Need to access the item to trigger expiration check
	exists := cache.Have("token_key")
	log.Printf("token_key exists after cancel: %v", exists)

	log.Println()
}

// demonstrateSharedCache shows sharing underlying storage between typed caches
func demonstrateSharedCache() {
	log.Println("--- 8. Shared Cache with From[K,V]() ---")

	// Create a base untyped cache
	baseCache := carrot.NewCache()

	// Create multiple typed views sharing the same storage
	intCache := carrot.From[string, int](baseCache)
	strCache := carrot.From[string, string](baseCache)

	// Store values through different typed views
	intCache.Expire("count", 42, time.Hour)
	strCache.Expire("name", "Alice", time.Hour)

	// Read back
	count, ok := intCache.Read("count")
	log.Printf("intCache.Read(\"count\"): value=%d, found=%v", count, ok)

	name, ok := strCache.Read("name")
	log.Printf("strCache.Read(\"name\"): value=%s, found=%v", name, ok)

	// Both share the same underlying storage
	log.Printf("intCache.Count(): %d", intCache.Count())
	log.Printf("strCache.Count(): %d", strCache.Count())
	log.Printf("baseCache.Count(): %d", baseCache.Count())

	// Access underlying cache directly
	underlying := intCache.Underlying()
	log.Printf("Underlying is same as baseCache: %v", underlying == baseCache)

	// Keys are typed
	intKeys := intCache.Keys() // []string
	log.Printf("intCache.Keys(): %v (type: []string)", intKeys)

	log.Println()
}

// demonstrateStatistics shows cache statistics
func demonstrateStatistics() {
	log.Println("--- 9. Statistics ---")

	cache := carrot.New[string, string]()

	// Generate some hits and misses
	cache.Forever("stat_key", "stat_value")

	// Hits
	for i := 0; i < 5; i++ {
		cache.Read("stat_key")
	}

	// Misses
	for i := 0; i < 3; i++ {
		cache.Read("nonexistent")
	}

	stats := cache.Statistics()
	log.Printf("Statistics:")
	log.Printf("  Total Hits: %d", stats.TotalHits())
	log.Printf("  Total Misses: %d", stats.TotalMisses())
	log.Printf("  Usage Count: %d", stats.UsageCount())
	log.Printf("  PQ Count: %d", stats.PqCount())

	// Calculate hit rate
	total := stats.TotalHits() + stats.TotalMisses()
	if total > 0 {
		hitRate := float64(stats.TotalHits()) / float64(total) * 100
		log.Printf("  Hit Rate: %.2f%%", hitRate)
	}

	log.Println()
}
