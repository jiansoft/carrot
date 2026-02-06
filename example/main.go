// Package main provides examples demonstrating all features of the carrot cache library.
// main 套件提供了 carrot 快取函式庫所有功能的使用範例。
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/jiansoft/carrot"
)

// User represents a user entity for cache examples.
// User 代表快取範例中的使用者實體。
type User struct {
	ID       int
	Name     string
	Email    string
	IsActive bool
}

// Product represents a product entity for cache examples.
// Product 代表快取範例中的產品實體。
type Product struct {
	SKU      string
	Name     string
	Price    float64
	Quantity int
}

// OrderID is a custom type for order identifiers.
// OrderID 是訂單識別碼的自定義類型。
type OrderID int64

// assertionErrors counts the number of failed assertions.
// assertionErrors 記錄失敗的斷言數量。
var assertionErrors int

// assert checks if a condition is true, logs error if not.
// assert 檢查條件是否為真，若否則記錄錯誤。
func assert(condition bool, msg string) {
	if !condition {
		assertionErrors++
		log.Printf("  ❌ ASSERTION FAILED: %s", msg)
	}
}

// assertEqual checks if two values are equal using interface comparison.
// assertEqual 使用介面比較檢查兩個值是否相等。
func assertEqual(actual, expected any, msg string) {
	if actual != expected {
		assertionErrors++
		log.Printf("  ❌ ASSERTION FAILED: %s (expected=%v, actual=%v)", msg, expected, actual)
	}
}

// Order represents an order entity for cache examples.
// Order 代表快取範例中的訂單實體。
type Order struct {
	ID        OrderID
	UserID    int
	Items     []string
	Total     float64
	CreatedAt time.Time
}

func main() {
	log.Println("=== Carrot Cache Example / Carrot 快取範例 ===")
	log.Println()

	// 1. Basic usage with New[K,V]() and Default singleton
	// 1. 使用 New[K,V]() 和 Default 單例的基本用法
	demonstrateBasicUsage()

	// 2. Expiration policies
	// 2. 過期策略
	demonstrateExpirationPolicies()

	// 3. GetOrCreate patterns
	// 3. GetOrCreate 模式
	demonstrateGetOrCreate()

	// 4. Size limit and Compact
	// 4. 大小限制與壓縮
	demonstrateSizeLimitAndCompact()

	// 5. Priority levels
	// 5. 優先順序等級
	demonstratePriorityLevels()

	// 6. Post eviction callback
	// 6. 驅逐後回呼
	demonstrateEvictionCallback()

	// 7. Expiration token (context cancellation)
	// 7. 過期令牌（context 取消）
	demonstrateExpirationToken()

	// 8. Shared cache with From[K,V]()
	// 8. 使用 From[K,V]() 共享快取
	demonstrateSharedCache()

	// 9. Statistics
	// 9. 統計資訊
	demonstrateStatistics()

	// 10. Default singleton comprehensive usage
	// 10. Default 單例的完整用法
	demonstrateDefaultSingleton()

	log.Println()
	if assertionErrors == 0 {
		log.Println("=== All examples completed successfully / 所有範例已成功完成 ===")
	} else {
		log.Printf("=== Examples completed with %d assertion errors / 範例完成，有 %d 個斷言失敗 ===", assertionErrors, assertionErrors)
	}
}

// demonstrateBasicUsage shows basic cache operations with generic API.
// demonstrateBasicUsage 展示使用泛型 API 的基本快取操作。
func demonstrateBasicUsage() {
	log.Println("--- 1. Basic Usage (Generic API) / 基本用法（泛型 API）---")

	// Cache with int key and User struct value
	// 使用 int 作為鍵，User 結構體作為值的快取
	userCache := carrot.New[int, User]()

	// Store and read User structs - type safe!
	// 儲存和讀取 User 結構體 - 型別安全！
	user1 := User{ID: 1, Name: "Alice", Email: "alice@example.com", IsActive: true}
	userCache.Forever(1, user1)
	val, ok := userCache.Read(1) // val is User, no type assertion needed / val 是 User，無需型別斷言
	log.Printf("Read user 1: %+v, found=%v", val, ok)
	assert(ok, "Forever item should be readable")
	assertEqual(val.Name, "Alice", "User name should match")

	// Check if key exists
	// 檢查鍵是否存在
	exists := userCache.Have(1)
	log.Printf("Have user 1: %v", exists)
	assert(exists, "Forever item should exist")

	// Remove a key
	// 移除一個鍵
	userCache.Forget(1)
	log.Printf("After Forget, Have user 1: %v", userCache.Have(1))
	assert(!userCache.Have(1), "Forgotten item should not exist")

	// Use Default singleton (untyped, for backward compatibility)
	// See section 10 for comprehensive Default usage examples
	// 使用 Default 單例（無型別，用於向後相容）
	// 完整的 Default 用法範例請參見第 10 節
	carrot.Default.Forever("config:timezone", "Asia/Taipei")
	sval, ok := carrot.Default.Read("config:timezone")
	log.Printf("Default singleton Read: value=%v, found=%v", sval, ok)
	assert(ok, "Default singleton should be readable")
	assertEqual(sval, "Asia/Taipei", "Default singleton value should match")
	carrot.Default.Forget("config:timezone")
	assert(!carrot.Default.Have("config:timezone"), "Forgotten Default item should not exist")

	log.Println()
}

// demonstrateExpirationPolicies shows different expiration strategies.
// demonstrateExpirationPolicies 展示不同的過期策略。
func demonstrateExpirationPolicies() {
	log.Println("--- 2. Expiration Policies / 過期策略 ---")

	// Cache with string key and Product struct value
	// 使用 string 作為鍵，Product 結構體作為值的快取
	productCache := carrot.New[string, Product]()

	// Forever - never expires
	// Forever - 永不過期
	featuredProduct := Product{SKU: "FEATURED-001", Name: "Premium Widget", Price: 99.99, Quantity: 100}
	productCache.Forever("featured", featuredProduct)
	log.Printf("Forever product exists: %v", productCache.Have("featured"))
	assert(productCache.Have("featured"), "Forever product should exist")

	// Expire - expires after duration (negative means never)
	// Expire - 在指定時間後過期（負值表示永不過期）
	flashSale := Product{SKU: "FLASH-001", Name: "Flash Sale Item", Price: 19.99, Quantity: 50}
	productCache.Expire("flash-sale", flashSale, 200*time.Millisecond)
	permanentItem := Product{SKU: "PERM-001", Name: "Permanent Item", Price: 49.99, Quantity: 200}
	productCache.Expire("permanent", permanentItem, -time.Second) // Never expires / 永不過期
	log.Printf("Flash sale product exists: %v", productCache.Have("flash-sale"))
	assert(productCache.Have("flash-sale"), "Flash sale should exist before expiration")
	assert(productCache.Have("permanent"), "Permanent (negative TTL) should exist")

	// Until - expires at specific time
	// Until - 在指定時間點過期
	limitedOffer := Product{SKU: "LIMITED-001", Name: "Limited Time Offer", Price: 29.99, Quantity: 10}
	productCache.Until("limited-offer", limitedOffer, time.Now().Add(200*time.Millisecond))
	log.Printf("Limited offer exists: %v", productCache.Have("limited-offer"))
	assert(productCache.Have("limited-offer"), "Limited offer should exist before expiration")

	// Sliding - expires after period of inactivity, each access resets the timer
	// Sliding - 閒置一段時間後過期，每次存取會重設計時器
	recentlyViewed := Product{SKU: "VIEWED-001", Name: "Recently Viewed", Price: 39.99, Quantity: 75}
	productCache.Sliding("recently-viewed", recentlyViewed, 200*time.Millisecond)
	log.Printf("Recently viewed exists: %v", productCache.Have("recently-viewed"))
	assert(productCache.Have("recently-viewed"), "Sliding item should exist initially")

	// Demonstrate sliding expiration behavior
	// 展示滑動過期的行為
	time.Sleep(150 * time.Millisecond)
	// Access the key to reset the sliding timer
	// 存取鍵以重設滑動計時器
	if product, ok := productCache.Read("recently-viewed"); ok {
		log.Printf("Recently viewed accessed at 150ms (SKU: %s), timer reset", product.SKU)
	}

	// Wait for expiration
	// 等待過期
	time.Sleep(300 * time.Millisecond)
	log.Printf("After 450ms total - flash-sale exists: %v", productCache.Have("flash-sale"))
	log.Printf("After 450ms total - permanent exists: %v", productCache.Have("permanent"))
	log.Printf("After 450ms total - featured exists: %v", productCache.Have("featured"))
	log.Printf("After 450ms total - recently-viewed exists (should be false): %v", productCache.Have("recently-viewed"))
	assert(!productCache.Have("flash-sale"), "Expired flash-sale should not exist")
	assert(productCache.Have("permanent"), "Permanent (negative TTL) should still exist")
	assert(productCache.Have("featured"), "Forever item should still exist")
	assert(!productCache.Have("recently-viewed"), "Sliding item should expire after 300ms without access")

	// Reset clears all
	// Reset 清除所有項目
	productCache.Reset()
	log.Printf("After Reset - featured exists: %v", productCache.Have("featured"))
	assert(!productCache.Have("featured"), "Reset should clear all items")

	log.Println()
}

// demonstrateGetOrCreate shows atomic get-or-create patterns.
// demonstrateGetOrCreate 展示原子性的 get-or-create 模式。
func demonstrateGetOrCreate() {
	log.Println("--- 3. GetOrCreate Patterns / GetOrCreate 模式 ---")

	// Cache with OrderID (custom type) key and Order struct value
	// 使用 OrderID（自定義類型）作為鍵，Order 結構體作為值的快取
	orderCache := carrot.New[OrderID, Order]()

	// GetOrCreate - returns existing or creates new
	// GetOrCreate - 傳回現有值或建立新值
	defaultOrder := Order{
		ID:        1001,
		UserID:    42,
		Items:     []string{"item-a", "item-b"},
		Total:     150.00,
		CreatedAt: time.Now(),
	}
	order1, existed := orderCache.GetOrCreate(OrderID(1001), defaultOrder, time.Hour)
	log.Printf("GetOrCreate (first call): OrderID=%d, existed=%v", order1.ID, existed)
	assert(!existed, "First GetOrCreate should return existed=false")
	assertEqual(order1.UserID, 42, "First GetOrCreate should return the provided value")

	anotherOrder := Order{ID: 1001, UserID: 99, Items: []string{"different"}, Total: 999.99}
	order2, existed := orderCache.GetOrCreate(OrderID(1001), anotherOrder, time.Hour)
	log.Printf("GetOrCreate (second call): OrderID=%d, UserID=%d (original), existed=%v", order2.ID, order2.UserID, existed)
	assert(existed, "Second GetOrCreate should return existed=true")
	assertEqual(order2.UserID, 42, "Second GetOrCreate should return original value, not the new one")

	// GetOrCreateFunc - with typed factory function
	// GetOrCreateFunc - 使用型別化的工廠函式
	callCount := 0
	orderFactory := func() (Order, error) {
		callCount++
		return Order{
			ID:        1002,
			UserID:    callCount,
			Items:     []string{fmt.Sprintf("factory-item-%d", callCount)},
			Total:     float64(callCount) * 100,
			CreatedAt: time.Now(),
		}, nil
	}

	order3, existed, err := orderCache.GetOrCreateFunc(OrderID(1002), time.Hour, orderFactory)
	if err != nil {
		log.Printf("GetOrCreateFunc (first) error: %v", err)
	} else {
		log.Printf("GetOrCreateFunc (first): OrderID=%d, existed=%v", order3.ID, existed)
		assert(!existed, "First GetOrCreateFunc should return existed=false")
	}

	order4, existed, err := orderCache.GetOrCreateFunc(OrderID(1002), time.Hour, orderFactory)
	if err != nil {
		log.Printf("GetOrCreateFunc (second) error: %v", err)
	} else {
		log.Printf("GetOrCreateFunc (second): OrderID=%d, UserID=%d (cached), existed=%v", order4.ID, order4.UserID, existed)
		assert(existed, "Second GetOrCreateFunc should return existed=true")
	}
	log.Printf("Factory was called %d time(s)", callCount)
	assertEqual(callCount, 1, "Factory should be called exactly once")

	// GetOrCreateFunc with error
	// GetOrCreateFunc 帶有錯誤
	errorFactory := func() (Order, error) {
		return Order{}, errors.New("database connection failed")
	}
	_, _, err = orderCache.GetOrCreateFunc(OrderID(9999), time.Hour, errorFactory)
	log.Printf("GetOrCreateFunc with error: err=%v", err)
	assert(err != nil, "GetOrCreateFunc with error factory should return error")
	assert(!orderCache.Have(OrderID(9999)), "Failed GetOrCreateFunc should not create entry")

	// GetOrCreateWithOptions - with full options
	// GetOrCreateWithOptions - 使用完整選項
	priorityOrder := Order{ID: 1003, UserID: 1, Items: []string{"priority-item"}, Total: 500.00}
	order5, existed := orderCache.GetOrCreateWithOptions(OrderID(1003), priorityOrder, carrot.EntryOptions{
		TimeToLive: time.Hour,
		Priority:   carrot.PriorityHigh,
	})
	log.Printf("GetOrCreateWithOptions: OrderID=%d, existed=%v", order5.ID, existed)

	log.Println()
}

// demonstrateSizeLimitAndCompact shows size management.
// demonstrateSizeLimitAndCompact 展示大小管理。
func demonstrateSizeLimitAndCompact() {
	log.Println("--- 4. Size Limit and Compact / 大小限制與壓縮 ---")

	// Cache with int key and slice value
	// 使用 int 作為鍵，切片作為值的快取
	dataCache := carrot.New[int, []byte]()

	// Set size limit
	// 設定大小限制
	dataCache.SetSizeLimit(100)
	log.Printf("Size limit set to: %d", dataCache.GetSizeLimit())

	// Add items with sizes (simulating byte data)
	// 新增帶有大小的項目（模擬位元組資料）
	for i := 0; i < 5; i++ {
		data := make([]byte, 30) // 30 bytes each / 每個 30 位元組
		for j := range data {
			data[j] = byte(i)
		}
		dataCache.Set(i, data, carrot.EntryOptions{
			Size:       30,
			TimeToLive: time.Hour,
			Priority:   carrot.PriorityLow,
		})
	}

	log.Printf("After adding 5 items (size=30 each)")
	log.Printf("Current size: %d", dataCache.GetCurrentSize())
	log.Printf("Count: %d", dataCache.Count())

	// Keys returns all non-expired keys (typed!)
	// Keys 傳回所有未過期的鍵（有型別！）
	keys := dataCache.Keys() // []int
	log.Printf("Keys in cache ([]int): %v", keys)

	// Compact removes percentage of low-priority items
	// Compact 移除指定百分比的低優先順序項目
	dataCache.Compact(0.5) // Remove 50% / 移除 50%
	log.Printf("After Compact(0.5) - Count: %d", dataCache.Count())

	log.Println()
}

// demonstratePriorityLevels shows priority-based eviction.
// demonstratePriorityLevels 展示基於優先順序的驅逐。
func demonstratePriorityLevels() {
	log.Println("--- 5. Priority Levels / 優先順序等級 ---")

	// Cache with string key and map value
	// 使用 string 作為鍵，map 作為值的快取
	configCache := carrot.New[string, map[string]any]()
	configCache.SetSizeLimit(50)

	// Add items with different priorities
	// 新增不同優先順序的項目
	configCache.Set("temp-config", map[string]any{"debug": true, "level": 1}, carrot.EntryOptions{
		Size:       20,
		TimeToLive: time.Hour,
		Priority:   carrot.PriorityLow,
	})

	configCache.Set("app-config", map[string]any{"name": "MyApp", "version": "1.0"}, carrot.EntryOptions{
		Size:       20,
		TimeToLive: time.Hour,
		Priority:   carrot.PriorityNormal,
	})

	configCache.Set("db-config", map[string]any{"host": "localhost", "port": 5432}, carrot.EntryOptions{
		Size:       20,
		TimeToLive: time.Hour,
		Priority:   carrot.PriorityHigh,
	})

	configCache.Set("security-config", map[string]any{"secret": "***", "algorithm": "AES256"}, carrot.EntryOptions{
		Size:       20,
		TimeToLive: time.Hour,
		Priority:   carrot.PriorityNeverRemove,
	})

	log.Printf("Added 4 config items with different priorities")
	log.Printf("Current size: %d (limit: %d)", configCache.GetCurrentSize(), configCache.GetSizeLimit())

	// Low priority items are evicted first when size limit exceeded
	// 當超過大小限制時，低優先順序的項目會先被驅逐
	log.Printf("temp-config (Low) exists: %v", configCache.Have("temp-config"))
	log.Printf("app-config (Normal) exists: %v", configCache.Have("app-config"))
	log.Printf("db-config (High) exists: %v", configCache.Have("db-config"))
	log.Printf("security-config (NeverRemove) exists: %v", configCache.Have("security-config"))

	log.Println()
}

// demonstrateEvictionCallback shows post-eviction callbacks.
// demonstrateEvictionCallback 展示驅逐後的回呼。
func demonstrateEvictionCallback() {
	log.Println("--- 6. Post Eviction Callback / 驅逐後回呼 ---")

	// Cache with int64 key and User struct value
	// 使用 int64 作為鍵，User 結構體作為值的快取
	sessionCache := carrot.New[int64, User]()

	// Set with callback
	// 設定帶有回呼的項目
	sessionUser := User{ID: 100, Name: "SessionUser", Email: "session@example.com", IsActive: true}
	sessionCache.Set(int64(12345), sessionUser, carrot.EntryOptions{
		TimeToLive: 100 * time.Millisecond,
		PostEvictionCallback: func(key, value any, reason carrot.EvictionReason) {
			reasonStr := ""
			switch reason {
			case carrot.EvictionReasonExpired:
				reasonStr = "Expired / 已過期"
			case carrot.EvictionReasonRemoved:
				reasonStr = "Removed / 已移除"
			case carrot.EvictionReasonReplaced:
				reasonStr = "Replaced / 已取代"
			case carrot.EvictionReasonCapacity:
				reasonStr = "Capacity / 容量限制"
			case carrot.EvictionReasonTokenExpired:
				reasonStr = "TokenExpired / 令牌過期"
			default:
				reasonStr = "None / 無"
			}
			if user, ok := value.(User); ok {
				log.Printf("  [Callback] Session %v (User: %s) evicted, Reason=%s", key, user.Name, reasonStr)
			}
		},
	})

	log.Printf("Session set with eviction callback, waiting for expiration...")
	log.Printf("工作階段已設定驅逐回呼，等待過期...")
	time.Sleep(200 * time.Millisecond)

	// Also test manual removal callback
	// 同時測試手動移除的回呼
	manualUser := User{ID: 200, Name: "ManualUser", Email: "manual@example.com"}
	sessionCache.Set(int64(67890), manualUser, carrot.EntryOptions{
		TimeToLive: time.Hour,
		PostEvictionCallback: func(key, value any, reason carrot.EvictionReason) {
			if user, ok := value.(User); ok {
				log.Printf("  [Callback] Session %v (User: %s) manually removed / 手動移除", key, user.Name)
			}
		},
	})
	sessionCache.Forget(int64(67890))
	time.Sleep(50 * time.Millisecond) // Wait for async callback / 等待非同步回呼

	log.Println()
}

// demonstrateExpirationToken shows context-based cancellation.
// demonstrateExpirationToken 展示基於 context 的取消機制。
func demonstrateExpirationToken() {
	log.Println("--- 7. Expiration Token (Context Cancellation) / 過期令牌（Context 取消）---")

	// Cache with string key and pointer to struct value
	// 使用 string 作為鍵，結構體指標作為值的快取
	taskCache := carrot.New[string, *Order]()

	// Create a cancellable context
	// 建立一個可取消的 context
	ctx, cancel := context.WithCancel(context.Background())

	longRunningOrder := &Order{
		ID:        5001,
		UserID:    999,
		Items:     []string{"long-running-task"},
		Total:     1000.00,
		CreatedAt: time.Now(),
	}

	taskCache.Set("long-task", longRunningOrder, carrot.EntryOptions{
		TimeToLive:      time.Hour, // Long TTL / 長 TTL
		ExpirationToken: ctx,
		PostEvictionCallback: func(key, value any, reason carrot.EvictionReason) {
			if reason == carrot.EvictionReasonTokenExpired {
				if order, ok := value.(*Order); ok {
					log.Printf("  [Callback] Task %v (OrderID: %d) cancelled / 已取消", key, order.ID)
				}
			}
		},
	})

	log.Printf("long-task exists before cancel: %v", taskCache.Have("long-task"))
	assert(taskCache.Have("long-task"), "Task should exist before context cancel")

	// Cancel the context to expire the item
	// 取消 context 以使項目過期
	cancel()

	// Need to access the item to trigger expiration check
	// 需要存取項目以觸發過期檢查
	exists := taskCache.Have("long-task")
	log.Printf("long-task exists after cancel: %v", exists)
	assert(!exists, "Task should not exist after context cancel")

	log.Println()
}

// demonstrateSharedCache shows sharing underlying storage between typed caches.
// demonstrateSharedCache 展示在型別快取之間共享底層儲存。
func demonstrateSharedCache() {
	log.Println("--- 8. Shared Cache with From[K,V]() / 使用 From[K,V]() 共享快取 ---")

	// Create a base untyped cache
	// 建立一個基礎的無型別快取
	baseCache := carrot.NewCache()

	// Create multiple typed views sharing the same storage
	// 建立多個共享相同儲存的型別視圖
	userCache := carrot.From[int, User](baseCache)
	productCache := carrot.From[string, Product](baseCache)
	counterCache := carrot.From[string, int64](baseCache)

	// Store values through different typed views
	// 透過不同的型別視圖儲存值
	userCache.Expire(1, User{ID: 1, Name: "SharedUser", Email: "shared@example.com"}, time.Hour)
	productCache.Expire("shared-product", Product{SKU: "SHARED-001", Name: "Shared Product", Price: 59.99}, time.Hour)
	counterCache.Expire("visit-count", int64(12345), time.Hour)

	// Read back with correct types
	// 以正確的型別讀取回來
	user, ok := userCache.Read(1)
	log.Printf("userCache.Read(1): User=%+v, found=%v", user, ok)
	assert(ok, "User should be readable from typed cache")
	assertEqual(user.Name, "SharedUser", "User name should match")

	product, ok := productCache.Read("shared-product")
	log.Printf("productCache.Read(\"shared-product\"): Product=%+v, found=%v", product, ok)
	assert(ok, "Product should be readable from typed cache")
	assertEqual(product.SKU, "SHARED-001", "Product SKU should match")

	count, ok := counterCache.Read("visit-count")
	log.Printf("counterCache.Read(\"visit-count\"): count=%d, found=%v", count, ok)
	assert(ok, "Counter should be readable from typed cache")
	assertEqual(count, int64(12345), "Counter value should match")

	// All share the same underlying storage
	// 全部共享相同的底層儲存
	log.Printf("userCache.Count(): %d", userCache.Count())
	log.Printf("productCache.Count(): %d", productCache.Count())
	log.Printf("counterCache.Count(): %d", counterCache.Count())
	log.Printf("baseCache.Count(): %d", baseCache.Count())
	assertEqual(userCache.Count(), 3, "All typed caches should see 3 items")
	assertEqual(baseCache.Count(), 3, "Base cache should have 3 items")

	// Access underlying cache directly
	// 直接存取底層快取
	underlying := userCache.Underlying()
	log.Printf("Underlying is same as baseCache: %v", underlying == baseCache)
	assert(underlying == baseCache, "Underlying should be same as baseCache")

	// Keys are typed according to each view
	// 鍵根據每個視圖有不同的型別
	userKeys := userCache.Keys() // []int
	log.Printf("userCache.Keys() ([]int): %v", userKeys)

	productKeys := productCache.Keys() // []string
	log.Printf("productCache.Keys() ([]string): %v", productKeys)

	log.Println()
}

// demonstrateStatistics shows cache statistics.
// demonstrateStatistics 展示快取統計資訊。
func demonstrateStatistics() {
	log.Println("--- 9. Statistics / 統計資訊 ---")

	// Cache with int key and float64 value (e.g., for metrics)
	// 使用 int 作為鍵，float64 作為值的快取（例如用於指標）
	metricsCache := carrot.New[int, float64]()

	// Add some metrics data
	// 新增一些指標資料
	metricsCache.Forever(1, 0.95) // CPU usage / CPU 使用率
	metricsCache.Forever(2, 0.75) // Memory usage / 記憶體使用率
	metricsCache.Forever(3, 0.50) // Disk usage / 磁碟使用率

	// Hits - read existing metrics
	// 命中 - 讀取現有指標
	for i := 0; i < 5; i++ {
		metricsCache.Read(1) // CPU metric
	}

	// Misses - read non-existent metrics
	// 未命中 - 讀取不存在的指標
	for i := 0; i < 3; i++ {
		metricsCache.Read(999) // Non-existent metric
	}

	stats := metricsCache.Statistics()
	log.Printf("Statistics / 統計資訊:")
	log.Printf("  Total Hits / 總命中: %d", stats.TotalHits())
	log.Printf("  Total Misses / 總未命中: %d", stats.TotalMisses())
	log.Printf("  Usage Count / 使用計數: %d", stats.UsageCount())
	log.Printf("  TW Count / 時間輪計數: %d", stats.TwCount())
	assertEqual(int(stats.TotalHits()), 5, "Should have 5 hits")
	assertEqual(int(stats.TotalMisses()), 3, "Should have 3 misses")
	assertEqual(stats.UsageCount(), 3, "Should have 3 items in cache")

	// Calculate hit rate
	// 計算命中率
	total := stats.TotalHits() + stats.TotalMisses()
	if total > 0 {
		hitRate := float64(stats.TotalHits()) / float64(total) * 100
		log.Printf("  Hit Rate / 命中率: %.2f%%", hitRate)
		assert(hitRate > 60 && hitRate < 65, "Hit rate should be around 62.5%")
	}

	log.Println()
}

// demonstrateDefaultSingleton shows comprehensive usage of the Default singleton.
// demonstrateDefaultSingleton 展示 Default 單例的完整用法。
//
// carrot.Default is a global, untyped cache instance that accepts any key and value types.
// It's useful for quick prototyping or when type safety is not critical.
//
// carrot.Default 是一個全域的無型別快取實例，可接受任何鍵和值的類型。
// 適用於快速原型開發或不需要嚴格型別安全的場景。
func demonstrateDefaultSingleton() {
	log.Println("--- 10. Default Singleton / Default 單例 ---")

	// Reset Default to start fresh
	// 重設 Default 以從頭開始
	carrot.Default.Reset()

	// ====== Various Key Types / 各種鍵類型 ======
	log.Println("  [Key Types / 鍵類型]")

	// String key (most common)
	// 字串鍵（最常見）
	carrot.Default.Forever("config:app-name", "MyApplication")
	if val, ok := carrot.Default.Read("config:app-name"); ok {
		log.Printf("    string key: \"config:app-name\" → %v", val)
		assertEqual(val, "MyApplication", "String key should work")
	}

	// Int key
	// 整數鍵
	carrot.Default.Forever(42, "The answer to everything")
	if val, ok := carrot.Default.Read(42); ok {
		log.Printf("    int key: 42 → %v", val)
		assertEqual(val, "The answer to everything", "Int key should work")
	}

	// Int64 key
	// Int64 鍵
	carrot.Default.Forever(int64(9876543210), "Large ID value")
	if val, ok := carrot.Default.Read(int64(9876543210)); ok {
		log.Printf("    int64 key: 9876543210 → %v", val)
		assertEqual(val, "Large ID value", "Int64 key should work")
	}

	// Custom type key (OrderID)
	// 自定義類型鍵 (OrderID)
	carrot.Default.Forever(OrderID(1001), "Order #1001 data")
	if val, ok := carrot.Default.Read(OrderID(1001)); ok {
		log.Printf("    OrderID key: OrderID(1001) → %v", val)
		assertEqual(val, "Order #1001 data", "Custom type key should work")
	}

	// Struct key (must be comparable)
	// 結構體鍵（必須是可比較的）
	type CacheKey struct {
		Namespace string
		ID        int
	}
	structKey := CacheKey{Namespace: "users", ID: 123}
	carrot.Default.Forever(structKey, "User 123 in users namespace")
	if val, ok := carrot.Default.Read(structKey); ok {
		log.Printf("    struct key: {users, 123} → %v", val)
		assertEqual(val, "User 123 in users namespace", "Struct key should work")
	}

	log.Println()

	// ====== Various Value Types with Type Assertion / 各種值類型與型別斷言 ======
	log.Println("  [Value Types & Type Assertion / 值類型與型別斷言]")
	log.Println("    Note: carrot.Default.Read() returns (any, bool), type assertion is required.")
	log.Println("    注意：carrot.Default.Read() 回傳 (any, bool)，需要進行型別斷言。")
	log.Println()

	// String value with type assertion
	// 字串值與型別斷言
	carrot.Default.Expire("val:string", "Hello, 世界!", time.Hour)
	if val, ok := carrot.Default.Read("val:string"); ok {
		// Type assertion: val.(string)
		// 型別斷言：val.(string)
		strVal := val.(string) // Direct assertion (panics if wrong type) / 直接斷言（類型錯誤會 panic）
		log.Printf("    string: val.(string) = %q", strVal)
		assertEqual(strVal, "Hello, 世界!", "String value should work")
	}

	// Int value with safe type assertion
	// 整數值與安全型別斷言
	carrot.Default.Expire("val:int", 12345, time.Hour)
	if val, ok := carrot.Default.Read("val:int"); ok {
		// Safe type assertion with comma-ok idiom
		// 使用 comma-ok 慣用法的安全型別斷言
		if intVal, ok := val.(int); ok {
			log.Printf("    int: val.(int) = %d", intVal)
			assertEqual(intVal, 12345, "Int value should work")
		}
	}

	// Float64 value
	// 浮點數值
	carrot.Default.Expire("val:float64", 3.14159265359, time.Hour)
	if val, ok := carrot.Default.Read("val:float64"); ok {
		floatVal := val.(float64)
		log.Printf("    float64: val.(float64) = %f", floatVal)
	}

	// Bool value
	// 布林值
	carrot.Default.Expire("val:bool", true, time.Hour)
	if val, ok := carrot.Default.Read("val:bool"); ok {
		boolVal := val.(bool)
		log.Printf("    bool: val.(bool) = %v", boolVal)
		assertEqual(boolVal, true, "Bool value should work")
	}

	// Struct value - requires type assertion to access fields
	// 結構體值 - 需要型別斷言才能存取欄位
	user := User{ID: 999, Name: "DefaultUser", Email: "default@example.com", IsActive: true}
	carrot.Default.Expire("val:struct", user, time.Hour)
	if val, ok := carrot.Default.Read("val:struct"); ok {
		// Without assertion: val.Name would cause compile error
		// 沒有斷言：val.Name 會導致編譯錯誤
		userVal := val.(User)
		log.Printf("    struct: val.(User) = {ID:%d, Name:%q, Email:%q}", userVal.ID, userVal.Name, userVal.Email)
		assertEqual(userVal.Name, "DefaultUser", "Struct value should work")
	}

	// Pointer value - assert to pointer type
	// 指標值 - 斷言為指標類型
	product := &Product{SKU: "PTR-001", Name: "Pointer Product", Price: 99.99, Quantity: 10}
	carrot.Default.Expire("val:pointer", product, time.Hour)
	if val, ok := carrot.Default.Read("val:pointer"); ok {
		ptrVal := val.(*Product) // Assert to *Product / 斷言為 *Product
		log.Printf("    pointer: val.(*Product) = {SKU:%q, Price:%.2f}", ptrVal.SKU, ptrVal.Price)
		assertEqual(ptrVal.SKU, "PTR-001", "Pointer value should work")
	}

	// Slice value - assert to specific slice type
	// 切片值 - 斷言為特定切片類型
	tags := []string{"go", "cache", "carrot", "高效能"}
	carrot.Default.Expire("val:slice", tags, time.Hour)
	if val, ok := carrot.Default.Read("val:slice"); ok {
		sliceVal := val.([]string) // Assert to []string / 斷言為 []string
		log.Printf("    slice: val.([]string) = %v", sliceVal)
		assertEqual(len(sliceVal), 4, "Slice value should have 4 elements")
	}

	// Map value - assert to map type, then access nested values
	// Map 值 - 斷言為 map 類型，然後存取巢狀值
	metadata := map[string]any{
		"version":   "1.0.0",
		"buildDate": "2024-01-15",
		"features":  []string{"expiration", "sliding", "callback"},
		"count":     42,
	}
	carrot.Default.Expire("val:map", metadata, time.Hour)
	if val, ok := carrot.Default.Read("val:map"); ok {
		mapVal := val.(map[string]any) // Assert to map[string]any / 斷言為 map[string]any
		// Nested values also need assertion / 巢狀值也需要斷言
		version := mapVal["version"].(string)
		features := mapVal["features"].([]string)
		count := mapVal["count"].(int)
		log.Printf("    map: val.(map[string]any)[\"version\"].(string) = %q", version)
		log.Printf("         val.(map[string]any)[\"features\"].([]string) = %v", features)
		log.Printf("         val.(map[string]any)[\"count\"].(int) = %d", count)
	}

	// Byte slice value
	// 位元組切片值
	binaryData := []byte{0x48, 0x65, 0x6c, 0x6c, 0x6f} // "Hello" in ASCII
	carrot.Default.Expire("val:bytes", binaryData, time.Hour)
	if val, ok := carrot.Default.Read("val:bytes"); ok {
		bytesVal := val.([]byte)
		log.Printf("    []byte: val.([]byte) = %v → string(%s)", bytesVal, string(bytesVal))
		assertEqual(string(bytesVal), "Hello", "Byte slice value should work")
	}

	// Time value
	// 時間值
	now := time.Now()
	carrot.Default.Expire("val:time", now, time.Hour)
	if val, ok := carrot.Default.Read("val:time"); ok {
		timeVal := val.(time.Time)
		log.Printf("    time.Time: val.(time.Time) = %v", timeVal.Format(time.RFC3339))
	}

	// ====== Type Switch Example / 型別切換範例 ======
	log.Println()
	log.Println("  [Type Switch / 型別切換]")
	log.Println("    Use type switch when value type is unknown at compile time.")
	log.Println("    當編譯時不知道值的類型時，使用型別切換。")

	// Store mixed types / 儲存混合類型
	carrot.Default.Expire("mixed:1", "I am a string", time.Hour)
	carrot.Default.Expire("mixed:2", 42, time.Hour)
	carrot.Default.Expire("mixed:3", User{ID: 1, Name: "TypeSwitch"}, time.Hour)

	for _, key := range []string{"mixed:1", "mixed:2", "mixed:3"} {
		if val, ok := carrot.Default.Read(key); ok {
			// Type switch handles multiple types / 型別切換處理多種類型
			switch v := val.(type) {
			case string:
				log.Printf("    %s → string: %q", key, v)
			case int:
				log.Printf("    %s → int: %d", key, v)
			case User:
				log.Printf("    %s → User: {ID:%d, Name:%q}", key, v.ID, v.Name)
			default:
				log.Printf("    %s → unknown type: %T", key, v)
			}
		}
	}

	log.Println()

	// ====== Expiration Policies with Default / 使用 Default 的過期策略 ======
	log.Println("  [Expiration Policies / 過期策略]")

	// Forever - never expires
	// Forever - 永不過期
	carrot.Default.Forever("policy:forever", "This never expires / 這永不過期")
	log.Printf("    Forever: exists=%v", carrot.Default.Have("policy:forever"))

	// Expire - relative TTL
	// Expire - 相對 TTL
	carrot.Default.Expire("policy:expire", "Expires in 1 hour / 1 小時後過期", time.Hour)
	log.Printf("    Expire(1h): exists=%v", carrot.Default.Have("policy:expire"))

	// Until - absolute time
	// Until - 絕對時間
	carrot.Default.Until("policy:until", "Expires at specific time / 在指定時間過期", time.Now().Add(2*time.Hour))
	log.Printf("    Until(+2h): exists=%v", carrot.Default.Have("policy:until"))

	// Sliding - resets on access
	// Sliding - 存取時重設
	carrot.Default.Sliding("policy:sliding", "Sliding expiration / 滑動過期", 30*time.Minute)
	log.Printf("    Sliding(30m): exists=%v", carrot.Default.Have("policy:sliding"))

	// Delay (alias for Expire)
	// Delay（Expire 的別名）
	carrot.Default.Delay("policy:delay", "Delay is alias for Expire / Delay 是 Expire 的別名", time.Hour)
	log.Printf("    Delay(1h): exists=%v", carrot.Default.Have("policy:delay"))

	log.Println()

	// ====== Other Operations / 其他操作 ======
	log.Println("  [Other Operations / 其他操作]")

	// GetOrCreate - atomic get or create
	// GetOrCreate - 原子性的取得或建立
	val1, existed := carrot.Default.GetOrCreate("atomic:key", "initial value", time.Hour)
	log.Printf("    GetOrCreate (1st): value=%v, existed=%v", val1, existed)
	assert(!existed, "First GetOrCreate should return existed=false")

	val2, existed := carrot.Default.GetOrCreate("atomic:key", "different value", time.Hour)
	log.Printf("    GetOrCreate (2nd): value=%v, existed=%v", val2, existed)
	assert(existed, "Second GetOrCreate should return existed=true")
	assertEqual(val2, "initial value", "Second GetOrCreate should return original value")

	// Count and Keys
	// Count 和 Keys
	count := carrot.Default.Count()
	keys := carrot.Default.Keys()
	log.Printf("    Count: %d", count)
	log.Printf("    Keys (sample): %v...", keys[:min(3, len(keys))])

	// Statistics
	// 統計資訊
	stats := carrot.Default.Statistics()
	log.Printf("    Statistics: Hits=%d, Misses=%d, UsageCount=%d",
		stats.TotalHits(), stats.TotalMisses(), stats.UsageCount())

	// Forget - remove specific key
	// Forget - 移除特定鍵
	carrot.Default.Forget("val:string")
	log.Printf("    After Forget(\"val:string\"): exists=%v", carrot.Default.Have("val:string"))
	assert(!carrot.Default.Have("val:string"), "Forgotten key should not exist")

	// Have - check existence
	// Have - 檢查是否存在
	log.Printf("    Have(\"policy:forever\"): %v", carrot.Default.Have("policy:forever"))
	log.Printf("    Have(\"nonexistent\"): %v", carrot.Default.Have("nonexistent"))

	log.Println()

	// ====== Type Safety Note / 型別安全注意事項 ======
	log.Println("  [Type Safety Note / 型別安全注意事項]")
	log.Println("    carrot.Default uses any for keys and values, so type assertions are needed.")
	log.Println("    carrot.Default 使用 any 作為鍵和值，因此需要型別斷言。")
	log.Println("    For type-safe caching, use carrot.New[K,V]() instead.")
	log.Println("    若需要型別安全的快取，請改用 carrot.New[K,V]()。")

	// Cleanup
	// 清理
	carrot.Default.Reset()
	log.Printf("    After Reset: Count=%d", carrot.Default.Count())
	assertEqual(carrot.Default.Count(), 0, "Reset should clear all items")

	log.Println()
}
