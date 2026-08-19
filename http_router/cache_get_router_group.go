package http_router

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Super-Gagaga/abstract-manager/service"
	"github.com/Super-Gagaga/abstract-manager/util"
	"github.com/Super-Gagaga/abstract-manager/util/filter_translator"
	"github.com/Super-Gagaga/abstract-manager/util/logger"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// deriveKeyPrefix 从泛型 T 推导缓存 key 前缀（如 User → "user", Product → "product"）
func deriveKeyPrefix[T any]() string {
	var zero T
	typeName := fmt.Sprintf("%T", zero)
	// 去掉可能的指针前缀 *
	typeName = strings.TrimPrefix(typeName, "*")
	// 只取最后一段（去掉包名前缀）
	if idx := strings.LastIndex(typeName, "."); idx != -1 {
		typeName = typeName[idx+1:]
	}
	return strings.ToLower(typeName)
}

var (
	_ = deriveKeyPrefix[int] // 编译器占位，消除 unused warning
)

// ========== Lookup 路由组 ==========

type LookupRouterGroup[T any] struct {
	RouterGroup        *gin.RouterGroup
	Service            *service.ServiceManager[T]
	TranslatorRegistry *filter_translator.RedisTranslatorRegistry

	// 预定义的查询方法配置
	defaultKeyPattern  string
	defaultCacheExpire time.Duration
	customFilterFunc   func(context.Context, *redis.Client, []string) ([]string, error)

	// Cache Aside 配置
	cacheAsideTTL   time.Duration     // 从DB加载后的缓存TTL
	cacheHitRefresh bool              // 是否在缓存命中时刷新TTL
	buildKeyFromID  func(uint) string // 从ID构建Redis key的函数
}

func NewLookupRouterGroup[T any](
	rg *gin.RouterGroup,
	service *service.ServiceManager[T],
) *LookupRouterGroup[T] {
	return &LookupRouterGroup[T]{
		RouterGroup:        rg,
		Service:            service,
		TranslatorRegistry: filter_translator.DefaultRedisRegistry,
		defaultCacheExpire: util.GetCacheAsideTTL(),
		cacheAsideTTL:      util.GetCacheAsideTTL(),
		cacheHitRefresh:    util.GetCacheHitRefresh(),
	}
}

// ========== 配置方法（在注册路由前调用）==========

// SetDefaults 设置默认的 key 模式和缓存过期时间
func (lrg *LookupRouterGroup[T]) SetDefaults(keyPattern string, cacheExpire time.Duration) *LookupRouterGroup[T] {
	lrg.defaultKeyPattern = keyPattern
	lrg.defaultCacheExpire = cacheExpire
	return lrg
}

// SetCacheAsideConfig 设置 Cache Aside 模式配置
func (lrg *LookupRouterGroup[T]) SetCacheAsideConfig(ttl time.Duration, refreshOnHit bool) *LookupRouterGroup[T] {
	lrg.cacheAsideTTL = ttl
	lrg.cacheHitRefresh = refreshOnHit
	return lrg
}

// SetKeyBuilder 设置从ID构建key的函数
func (lrg *LookupRouterGroup[T]) SetKeyBuilder(builder func(uint) string) *LookupRouterGroup[T] {
	lrg.buildKeyFromID = builder
	return lrg
}

// SetCustomFilter 设置自定义过滤函数（如活跃用户过滤）
func (lrg *LookupRouterGroup[T]) SetCustomFilter(
	filterFunc func(context.Context, *redis.Client, []string) ([]string, error),
) *LookupRouterGroup[T] {
	lrg.customFilterFunc = filterFunc
	return lrg
}

// ========== 路由注册 ==========

func (lrg *LookupRouterGroup[T]) RegisterRoutes(basePath string) {
	lrg.RouterGroup.POST(basePath+"/lookup", lrg.HandleLookup)
	lrg.RouterGroup.GET(basePath+"/:key", lrg.HandleGetByKey)
	lrg.RouterGroup.POST(basePath+"/count", lrg.HandleCount)
	lrg.RouterGroup.POST(basePath+"/invalidate", lrg.HandleInvalidate)
}

// ========== 请求/响应结构 ==========

type LookupRequest struct {
	KeyPattern      string                          `json:"key_pattern"`       // 可选，覆盖默认 key 模式
	Filters         []filter_translator.FilterParam `json:"filters"`           // 过滤条件
	UseCustomFilter bool                            `json:"use_custom_filter"` // 是否使用自定义过滤器
	FallbackToDB    bool                            `json:"fallback_db"`       // 是否回源数据库
}

type LookupResponse[T any] struct {
	Code    int           `json:"code"`
	Message string        `json:"message"`
	Data    map[string]*T `json:"data"`
	Keys    []string      `json:"keys"`
	Count   int           `json:"count"`
}

type LookupCountRequest struct {
	KeyPattern      string                          `json:"key_pattern"`
	Filters         []filter_translator.FilterParam `json:"filters"`
	UseCustomFilter bool                            `json:"use_custom_filter"`
}

type LookupCountResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Count   int    `json:"count"`
}

type InvalidateRequest struct {
	Keys    []string `json:"keys"`    // 精确键列表
	Pattern string   `json:"pattern"` // 或使用模式
}

type InvalidateResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Count   int    `json:"count"` // 删除的键数量
}

// ========== 核心查询逻辑 ==========

func (lrg *LookupRouterGroup[T]) executeLookup(
	ctx context.Context,
	keyPattern string,
	filters []filter_translator.FilterParam,
	useCustomFilter bool,
	fallbackToDB bool,
) (map[string]*T, []string, error) {

	ctx, cancel := util.EnsureTimeout(ctx, util.GetDefaultRedisTimeout())
	defer cancel()

	// 1. 获取所有匹配的键（使用 SCAN 避免阻塞 Redis）
	redisClient := service.GetRedis()
	allKeys, err := service.ScanKeys(ctx, redisClient, keyPattern, 100)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to scan keys: %w", err)
	}

	// 2. 应用自定义过滤（如果启用）
	if useCustomFilter && lrg.customFilterFunc != nil {
		allKeys, err = lrg.customFilterFunc(ctx, redisClient, allKeys)
		if err != nil {
			return nil, nil, fmt.Errorf("custom filter failed: %w", err)
		}
	}

	// 3. 翻译并应用通用过滤器
	if len(filters) > 0 {
		redisFilters, err := lrg.TranslatorRegistry.TranslateBatch(filters)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid filters: %w", err)
		}

		allKeys, err = filter_translator.ApplyRedisFilters(ctx, redisClient, allKeys, redisFilters)
		if err != nil {
			return nil, nil, fmt.Errorf("filter application failed: %w", err)
		}
	}

	// 只保留普通对象 key
	filteredKeys := make([]string, 0, len(allKeys))
	for _, k := range allKeys {
		if !strings.HasSuffix(k, ":version") && !strings.HasSuffix(k, ":meta") {
			filteredKeys = append(filteredKeys, k)
		}
	}
	allKeys = filteredKeys

	// 如果 Redis 没有数据
	// 1. 有 filters 时，总是从 DB 查询（因为可能缓存中没有符合条件的数据）
	// 2. 无 filters 且 fallback_db=true 时，从 DB 加载所有数据
	if len(allKeys) == 0 {
		if len(filters) > 0 || fallbackToDB {
			return lrg.loadFromDBAndCache(ctx, keyPattern, filters)
		}
		return make(map[string]*T), []string{}, nil
	}

	// 4. 从缓存查询数据
	opts := &service.LookupQueryOptions{
		KeyPattern:   keyPattern,
		CacheExpire:  lrg.defaultCacheExpire,
		FallbackToDB: fallbackToDB,
	}

	result, err := lrg.Service.LookupQuery(ctx, allKeys, opts)
	if err != nil {
		return nil, nil, fmt.Errorf("lookup query failed: %w", err)
	}

	return result, allKeys, nil
}

// loadFromDBAndCache 从数据库加载数据并写入缓存（支持条件查询）
func (lrg *LookupRouterGroup[T]) loadFromDBAndCache(
	ctx context.Context,
	keyPattern string,
	filters []filter_translator.FilterParam,
) (map[string]*T, []string, error) {
	ctx, cancel := util.EnsureTimeout(ctx, util.GetDefaultDBTimeout())
	defer cancel()

	// 将 Redis filters 转换为 GORM 查询条件
	var queryFunc func(*gorm.DB) *gorm.DB

	if len(filters) > 0 {
		gormFilters, err := filter_translator.DefaultGormRegistry.TranslateBatch(filters)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid gorm filters: %w", err)
		}

		queryFunc = func(db *gorm.DB) *gorm.DB {
			return filter_translator.ApplyGormFilters(db, gormFilters)
		}
	}
	// 从数据库查询数据
	queryResult, err := lrg.Service.GetQueryWithoutTransaction(ctx, queryFunc, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query from database: %w", err)
	}

	if len(queryResult.Data) == 0 {
		return make(map[string]*T), []string{}, nil
	}

	// 批量写入缓存
	rdb := service.GetRedis()
	pipe := rdb.Pipeline()

	resultMap := make(map[string]*T)
	keys := make([]string, 0, len(queryResult.Data))

	for i := range queryResult.Data {
		item := &queryResult.Data[i]

		// 序列化为 JSON
		jsonData, err := json.Marshal(item)
		if err != nil {
			continue
		}

		// 从 JSON 中提取 ID（通用方法）
		var tempMap map[string]interface{}
		if err := json.Unmarshal(jsonData, &tempMap); err != nil {
			continue
		}

		id, ok := tempMap["id"].(float64) // JSON 数字默认是 float64
		if !ok {
			continue
		}

		key := fmt.Sprintf("%s:%d", deriveKeyPrefix[T](), uint(id))

		// 写入 Pipeline
		pipe.Set(ctx, key, jsonData, lrg.cacheAsideTTL)

		resultMap[key] = item
		keys = append(keys, key)
	}

	// 执行批量写入
	if len(keys) > 0 {
		if _, err := pipe.Exec(ctx); err != nil {
			// 即使缓存失败，也返回数据库数据
			return resultMap, keys, nil
		}
	}

	return resultMap, keys, nil
}

// ========== Cache Aside 模式核心逻辑 ==========

// extractIDFromKey 从 Redis key 中提取 ID (例如 "user:123" -> 123)
func (lrg *LookupRouterGroup[T]) extractIDFromKey(key string) (uint, error) {
	parts := strings.Split(key, ":")
	if len(parts) < 2 {
		return 0, fmt.Errorf("invalid key format: %s", key)
	}
	id, err := strconv.ParseUint(parts[len(parts)-1], 10, 32)
	if err != nil {
		return 0, fmt.Errorf("failed to parse ID from key %s: %w", key, err)
	}
	return uint(id), nil
}

// getByKeyCacheAside 实现 Cache Aside 模式的单个键查询
// 1. 先查 Redis
// 2. 如果命中：根据配置决定是否刷新 TTL
// 3. 如果未命中：从 DB 查询，转为 JSON，写入 Redis，设置 TTL
func (lrg *LookupRouterGroup[T]) getByKeyCacheAside(ctx context.Context, key string) (*T, bool, error) {
	redisClient := service.GetRedis()
	ctx, cancel := util.EnsureTimeout(ctx, util.GetDefaultRedisTimeout())
	defer cancel()

	// Step 1: 尝试从 Redis 获取
	var result T
	val, err := redisClient.Get(ctx, key).Result()

	if err == nil {
		// Cache Hit
		if err := json.Unmarshal([]byte(val), &result); err != nil {
			return nil, false, fmt.Errorf("failed to unmarshal cached data: %w", err)
		}

		// 根据配置决定是否刷新 TTL
		if lrg.cacheHitRefresh {
			if err := redisClient.Expire(ctx, key, lrg.cacheAsideTTL).Err(); err != nil {
				// TTL 刷新失败不影响返回数据
				logger.FromContext(ctx).Warn("cache.ttl_refresh_failed", "key", key, "err", err)
			}
		}

		return &result, true, nil
	}

	if err != redis.Nil {
		// Redis 错误（非 key 不存在）
		return nil, false, fmt.Errorf("redis get error: %w", err)
	}

	// Step 2: Cache Miss - 从数据库查询
	id, err := lrg.extractIDFromKey(key)
	if err != nil {
		return nil, false, err
	}

	// 使用 ServiceManager 的 GetQueryWithoutTransaction 查询单条数据
	queryResult, err := lrg.Service.GetQueryWithoutTransaction(
		ctx,
		func(db *gorm.DB) *gorm.DB {
			return db.Where("id = ?", id)
		},
		nil,
	)

	if err != nil {
		return nil, false, fmt.Errorf("failed to query from database: %w", err)
	}

	if len(queryResult.Data) == 0 {
		// 数据库中也不存在
		return nil, false, fmt.Errorf("record not found for key: %s", key)
	}

	result = queryResult.Data[0]

	// Step 3: 将数据转为 JSON 并写入 Redis
	jsonData, err := json.Marshal(result)
	if err != nil {
		return &result, false, fmt.Errorf("failed to marshal data: %w", err)
	}

	// 写入 Redis 并设置 TTL
	err = redisClient.Set(ctx, key, jsonData, lrg.cacheAsideTTL).Err()
	if err != nil {
		// 即使写入 Redis 失败，也返回数据库中的数据
		return &result, false, fmt.Errorf("failed to cache data (returned DB data): %w", err)
	}

	return &result, false, nil
}

// ========== HTTP 处理器 ==========

func (lrg *LookupRouterGroup[T]) HandleLookup(c *gin.Context) {
	var req LookupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "invalid request",
			"error":   err.Error(),
		})
		return
	}

	// 使用请求中的 key pattern，如果没有则使用默认值
	keyPattern := req.KeyPattern
	if keyPattern == "" {
		keyPattern = lrg.defaultKeyPattern
	}

	if keyPattern == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "key_pattern is required (or set default via SetDefaults)",
		})
		return
	}

	// 执行查询
	result, keys, err := lrg.executeLookup(
		c.Request.Context(),
		keyPattern,
		req.Filters,
		req.UseCustomFilter,
		req.FallbackToDB,
	)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "lookup failed",
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, LookupResponse[T]{
		Code:    0,
		Message: "success",
		Data:    result,
		Keys:    keys,
		Count:   len(result),
	})
}

// HandleGetByKey 使用 Cache Aside 模式处理单个键查询
func (lrg *LookupRouterGroup[T]) HandleGetByKey(c *gin.Context) {
	key := c.Param("key")
	ctx := c.Request.Context()

	result, cacheHit, err := lrg.getByKeyCacheAside(ctx, key)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    404,
				"message": "key not found",
				"error":   err.Error(),
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "lookup failed",
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":      0,
		"message":   "success",
		"data":      result,
		"cache_hit": cacheHit,
		"source":    getSource(cacheHit),
	})
}

func (lrg *LookupRouterGroup[T]) HandleCount(c *gin.Context) {
	var req LookupCountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "invalid request",
			"error":   err.Error(),
		})
		return
	}

	keyPattern := req.KeyPattern
	if keyPattern == "" {
		keyPattern = lrg.defaultKeyPattern
	}

	if keyPattern == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "key_pattern is required",
		})
		return
	}

	// 执行查询（只需要 keys，不需要数据）
	_, keys, err := lrg.executeLookup(
		c.Request.Context(),
		keyPattern,
		req.Filters,
		req.UseCustomFilter,
		false, // 计数不需要回源
	)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "count failed",
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, LookupCountResponse{
		Code:    0,
		Message: "success",
		Count:   len(keys),
	})
}

func (lrg *LookupRouterGroup[T]) HandleInvalidate(c *gin.Context) {
	var req InvalidateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "invalid request",
			"error":   err.Error(),
		})
		return
	}

	ctx := c.Request.Context()
	var deletedCount int

	if req.Pattern != "" {
		// 按模式删除
		if err := lrg.Service.InvalidateCacheByPattern(ctx, req.Pattern); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    500,
				"message": "invalidate failed",
				"error":   err.Error(),
			})
			return
		}
		deletedCount = -1 // -1 表示按模式删除，无法精确统计
	} else if len(req.Keys) > 0 {
		// 按键列表删除
		if err := lrg.Service.InvalidateCache(ctx, req.Keys...); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    500,
				"message": "invalidate failed",
				"error":   err.Error(),
			})
			return
		}
		deletedCount = len(req.Keys)
	} else {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "either keys or pattern must be provided",
		})
		return
	}

	c.JSON(http.StatusOK, InvalidateResponse{
		Code:    0,
		Message: "success",
		Count:   deletedCount,
	})
}

// ========== 辅助函数 ==========

// getSource 返回数据来源描述
func getSource(cacheHit bool) string {
	if cacheHit {
		return "cache"
	}
	return "database"
}
