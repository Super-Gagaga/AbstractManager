package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Super-Gagaga/abstract-manager/util"

	"gorm.io/gorm"
)

// LookupQueryOptions 缓存查询配置选项
type LookupQueryOptions struct {
	KeyPattern   string        // 键模式（用于批量查询）
	CacheExpire  time.Duration // 缓存过期时间
	FallbackToDB bool          // 缓存未命中时是否回源数据库
}

// LookupQuery 从缓存中查询系列数据
// keys: 要查询的缓存键列表
func (sm *ServiceManager[T]) LookupQuery(
	ctx context.Context,
	keys []string,
	opts *LookupQueryOptions,
) (map[string]*T, error) {
	redis := GetRedis()

	if len(keys) == 0 {
		return make(map[string]*T), nil
	}

	ctx, cancel := util.EnsureTimeout(ctx, util.GetDefaultRedisTimeout())
	defer cancel()

	// 批量获取缓存
	dataMap, err := redis.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get multiple cache: %w", err)
	}

	result := make(map[string]*T)
	missedKeys := []string{}

	// 解析缓存数据
	for i, key := range keys {
		data := dataMap[i]

		// MGet 对于不存在的 key 会返回 nil
		if data == nil {
			missedKeys = append(missedKeys, key)
			continue
		}

		// go-redis 的 MGet 返回值如果是字符串，通常需要断言为 string 或 []byte
		strData, ok := data.(string)
		if !ok {
			// 有些配置下可能会直接返回 []byte，可以根据实际测试调整
			missedKeys = append(missedKeys, key)
			continue
		}

		var item T
		if err := json.Unmarshal([]byte(strData), &item); err != nil {
			return nil, fmt.Errorf("failed to unmarshal cache data for key %s: %w", key, err)
		}
		result[key] = &item
	}

	// 如果有缓存未命中且需要回源
	if len(missedKeys) > 0 && opts != nil && opts.FallbackToDB {
		dbResults, err := sm.lookupFromDB(ctx, missedKeys, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to fallback to database: %w", err)
		}

		// 合并数据库结果
		for key, item := range dbResults {
			result[key] = item
		}
	}

	return result, nil
}

// LookupQueryByPattern 根据键模式从缓存中查询数据
// LookupQueryByPattern 改进版：使用 SCAN 代替 KEYS
func (sm *ServiceManager[T]) LookupQueryByPattern(
	ctx context.Context,
	pattern string,
	opts *LookupQueryOptions,
) (map[string]*T, error) {
	redis := GetRedis()
	ctx, cancel := util.EnsureTimeout(ctx, util.GetDefaultRedisTimeout())
	defer cancel()
	var allKeys []string
	var cursor uint64

	// 内部使用 SCAN，对 Redis 更友好
	for {
		keys, nextCursor, err := redis.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return nil, fmt.Errorf("scan keys failed: %w", err)
		}
		allKeys = append(allKeys, keys...)
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	if len(allKeys) == 0 {
		return make(map[string]*T), nil
	}

	return sm.LookupQuery(ctx, allKeys, opts)
}

// LookupQueryWithRefresh 从缓存查询数据，如果缓存不存在则从数据库加载并刷新缓存
func (sm *ServiceManager[T]) LookupQueryWithRefresh(
	ctx context.Context,
	keys []string,
	queryFunc func(*gorm.DB, []string) *gorm.DB, // 自定义数据库查询函数
	buildKeyFunc func(*T) string, // 根据数据生成缓存键的函数
	expiration time.Duration,
) (map[string]*T, error) {
	// 先从缓存查询
	result, err := sm.LookupQuery(ctx, keys, &LookupQueryOptions{
		FallbackToDB: false,
	})
	if err != nil {
		return nil, err
	}

	// 找出缓存未命中的键
	missedKeys := []string{}
	for _, key := range keys {
		if _, exists := result[key]; !exists {
			missedKeys = append(missedKeys, key)
		}
	}

	// 如果有缓存未命中，从数据库查询
	if len(missedKeys) > 0 {
		db := GetDB().WithContext(ctx)
		db = sm.applyTableName(db)

		if queryFunc != nil {
			db = queryFunc(db, missedKeys)
		}

		var dbResults []T
		if err := db.Find(&dbResults).Error; err != nil {
			return nil, fmt.Errorf("failed to query from database: %w", err)
		}

		// 将数据库结果写入缓存并添加到返回结果
		redis := GetRedis()
		for i := range dbResults {
			item := &dbResults[i]
			key := buildKeyFunc(item)

			// 写入缓存
			if err := redis.Set(ctx, key, item, expiration); err != nil {
				// 记录错误但不中断流程
				fmt.Printf("warning: failed to cache item with key %s: %v\n", key, err)
			}

			result[key] = item
		}
	}

	return result, nil
}

// extractID 从结构体 T 中提取 uint ID 字段（通过 JSON 序列化/反序列化）
func extractID[T any](item *T) (uint, bool) {
	jsonData, err := json.Marshal(item)
	if err != nil {
		return 0, false
	}
	var tempMap map[string]interface{}
	if err := json.Unmarshal(jsonData, &tempMap); err != nil {
		return 0, false
	}
	id, ok := tempMap["id"].(float64)
	if !ok {
		return 0, false
	}
	return uint(id), true
}

// extractIDFromKey 从缓存键 "prefix:123" 中提取 uint ID
func extractIDFromKey(key string) (uint, error) {
	parts := strings.Split(key, ":")
	if len(parts) < 2 {
		return 0, fmt.Errorf("invalid key format: %s (expected prefix:id)", key)
	}
	id, err := strconv.ParseUint(parts[len(parts)-1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse ID from key %s: %w", key, err)
	}
	return uint(id), nil
}

// lookupFromDB 从数据库查询缓存未命中的数据
func (sm *ServiceManager[T]) lookupFromDB(
	ctx context.Context,
	keys []string,
	opts *LookupQueryOptions,
) (map[string]*T, error) {
	db := GetDB().WithContext(ctx)
	db = sm.applyTableName(db)

	// 从缓存键中解析 ID 列表
	var ids []interface{}
	for _, key := range keys {
		id, err := extractIDFromKey(key)
		if err != nil {
			continue // 跳过格式不正确的 key
		}
		ids = append(ids, id)
	}

	if len(ids) == 0 {
		return make(map[string]*T), nil
	}

	var results []T
	if err := db.Where("id IN ?", ids).Find(&results).Error; err != nil {
		return nil, fmt.Errorf("failed to query from database: %w", err)
	}

	// 构建结果映射并写入缓存
	resultMap := make(map[string]*T)
	redis := GetRedis()

	for i := range results {
		item := &results[i]

		// 从 item 中提取 ID 字段，构建正确的 key
		id, ok := extractID(item)
		if !ok {
			continue
		}
		key := sm.buildCacheKey(id)

		// 写入缓存
		expiration := 1 * time.Hour
		if opts != nil && opts.CacheExpire > 0 {
			expiration = opts.CacheExpire
		}

		if err := redis.Set(ctx, key, item, expiration); err != nil {
			fmt.Printf("warning: failed to cache item: %v\n", err)
		}

		resultMap[key] = item
	}

	return resultMap, nil
}

// RefreshCache 刷新缓存（从数据库重新加载）
func (sm *ServiceManager[T]) RefreshCache(
	ctx context.Context,
	keys []string,
	queryFunc func(*gorm.DB, []string) *gorm.DB,
	buildKeyFunc func(*T) string,
	expiration time.Duration,
) error {
	db := GetDB().WithContext(ctx)
	db = sm.applyTableName(db)

	if queryFunc != nil {
		db = queryFunc(db, keys)
	}

	var results []T
	if err := db.Find(&results).Error; err != nil {
		return fmt.Errorf("failed to query from database: %w", err)
	}

	// 批量写入缓存
	redis := GetRedis()
	ctx, cancel := util.EnsureTimeout(ctx, util.GetDefaultRedisTimeout())
	defer cancel()
	cacheItems := make(map[string]interface{})

	for i := range results {
		item := &results[i]
		key := buildKeyFunc(item)
		cacheItems[key] = item
	}

	pipe := redis.Pipeline()
	for key, item := range cacheItems {
		data, _ := json.Marshal(item)
		pipe.Set(ctx, key, data, expiration)
	}
	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to refresh cache: %w", err)
	}

	return nil
}

// InvalidateCache 使缓存失效
func (sm *ServiceManager[T]) InvalidateCache(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}

	redis := GetRedis()
	ctx, cancel := util.EnsureTimeout(ctx, util.GetDefaultRedisTimeout())
	defer cancel()
	if err := redis.Del(ctx, keys...).Err(); err != nil {
		return fmt.Errorf("failed to invalidate cache: %w", err)
	}

	return nil
}

// InvalidateCacheByPattern 根据模式使缓存失效
func (sm *ServiceManager[T]) InvalidateCacheByPattern(ctx context.Context, pattern string) error {
	redis := GetRedis()
	ctx, cancel := util.EnsureTimeout(ctx, util.GetDefaultRedisTimeout())
	defer cancel()

	// 使用 SCAN 安全遍历匹配的键（避免 KEYS 阻塞 Redis）
	keys, err := ScanKeys(ctx, redis, pattern, 100)
	if err != nil {
		return fmt.Errorf("failed to scan keys with pattern %s: %w", pattern, err)
	}

	if len(keys) == 0 {
		return nil
	}

	// 批量删除
	if err := redis.Del(ctx, keys...).Err(); err != nil {
		return fmt.Errorf("failed to invalidate cache by pattern: %w", err)
	}

	return nil
}
