package http_router

import (
	"fmt"
	"net/http"
	"time"

	"github.com/Super-Gagaga/abstract-manager/service"
	"github.com/Super-Gagaga/abstract-manager/util"
	"github.com/Super-Gagaga/abstract-manager/util/cache_key_builder"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ==================== 请求/响应结构 ====================

type WritedownSingleRequest[T any] struct {
	Key               string      `json:"key"`
	Data              *T          `json:"data,omitempty"`
	ID                interface{} `json:"id,omitempty"`
	ExpirationSeconds int64       `json:"expiration_seconds,omitempty"`
	Overwrite         bool        `json:"overwrite"`
	NX                bool        `json:"nx,omitempty"`
	XX                bool        `json:"xx,omitempty"`
	Async             bool        `json:"async,omitempty"`
}

type WritedownQueryRequest[T any] struct {
	Data        []T           `json:"data,omitempty"`
	IDs         []interface{} `json:"ids,omitempty"`
	LoadAll     bool          `json:"load_all,omitempty"`
	KeyTemplate string        `json:"key_template"`
	Expiration  int64         `json:"expiration_seconds,omitempty"`
	BatchSize   int           `json:"batch_size,omitempty"`
	Overwrite   bool          `json:"overwrite"`
	UsePipeline bool          `json:"use_pipeline,omitempty"`
	Incremental bool          `json:"incremental,omitempty"`
}

type WritedownWithLockRequest struct {
	Key         string      `json:"key"`
	ID          interface{} `json:"id"`
	Expiration  int64       `json:"expiration_seconds,omitempty"`
	LockTimeout int64       `json:"lock_timeout_seconds,omitempty"`
}

type WritedownWithVersionRequest[T any] struct {
	Key        string `json:"key"`
	Data       *T     `json:"data"`
	Version    int64  `json:"version"`
	Expiration int64  `json:"expiration_seconds,omitempty"`
}

type WarmupCacheRequest struct {
	KeyTemplate string `json:"key_template"`
	Limit       int    `json:"limit,omitempty"`
	OrderBy     string `json:"order_by,omitempty"`
	Expiration  int64  `json:"expiration_seconds,omitempty"`
}

type RefreshCacheRequest struct {
	Key        string      `json:"key"`
	ID         interface{} `json:"id"`
	Expiration int64       `json:"expiration_seconds,omitempty"`
}

type WritedownResponse[T any] struct {
	Code         int    `json:"code"`
	Message      string `json:"message"`
	ItemsWritten int    `json:"items_written,omitempty"`
	Data         *T     `json:"data,omitempty"`
}

// ==================== 路由组 ====================

type WritedownRouterGroup[T any] struct {
	RouterGroup *gin.RouterGroup
	Service     *service.ServiceManager[T]
	KeyBuilder  cache_key_builder.KeyBuilder[T]
}

type WritedownRouterConfig[T any] struct {
	KeyBuilder cache_key_builder.KeyBuilder[T]
}

func NewWritedownRouterGroup[T any](rg *gin.RouterGroup, svc *service.ServiceManager[T], cfg ...*WritedownRouterConfig[T]) *WritedownRouterGroup[T] {
	wdg := &WritedownRouterGroup[T]{RouterGroup: rg, Service: svc}
	if len(cfg) > 0 && cfg[0] != nil && cfg[0].KeyBuilder != nil {
		wdg.KeyBuilder = cfg[0].KeyBuilder
	}
	return wdg
}

func (wdg *WritedownRouterGroup[T]) SetKeyBuilder(builder cache_key_builder.KeyBuilder[T]) {
	wdg.KeyBuilder = builder
}

func (wdg *WritedownRouterGroup[T]) RegisterRoutes(basePath string) {
	r := wdg.RouterGroup
	r.POST(basePath+"/write", wdg.HandleWritedownSingle)
	r.POST(basePath+"/write-lock", wdg.HandleWritedownWithLock)
	r.POST(basePath+"/write-version", wdg.HandleWritedownWithVersion)
	r.POST(basePath+"/refresh", wdg.HandleRefreshCache)
	r.POST(basePath+"/batch-write", wdg.HandleWritedownQuery)
	r.POST(basePath+"/warmup", wdg.HandleWarmupCache)
}

// ==================== 公共辅助 (逻辑更新点) ====================

// getEnvCacheTime 获取环境变量中的默认过期时间（秒）
func getEnvCacheTime() int64 {
	ttl := util.GetCacheAsideTTL()
	return int64(ttl.Seconds())
}

// parseExpiration 优先使用 customSec，如果为0则尝试环境变量，最后使用 fallbackDefault
func parseExpiration(fallbackDefault int64, customSec int64) time.Duration {
	// 1. 如果请求体自带了时间，优先使用
	if customSec > 0 {
		return time.Duration(customSec) * time.Second
	}
	// 2. 如果 fallbackDefault 是标准默认值(3600)，说明调用方希望使用“全局默认值”
	if fallbackDefault == 3600 {
		return time.Duration(getEnvCacheTime()) * time.Second
	}
	// 3. 否则使用传入的特定 fallback（例如 LockTimeout 的默认值）
	return time.Duration(fallbackDefault) * time.Second
}

func respondError[T any](c *gin.Context, code int, msg string) {
	c.JSON(code, WritedownResponse[T]{Code: code, Message: msg})
}

func respondSuccess[T any](c *gin.Context, items int, data *T) {
	c.JSON(http.StatusOK, WritedownResponse[T]{Code: 0, Message: "success", ItemsWritten: items, Data: data})
}

func (wdg *WritedownRouterGroup[T]) getKeyFunc(template string) func(*T) string {
	if wdg.KeyBuilder != nil {
		return cache_key_builder.BuildKeyFunc(wdg.KeyBuilder)
	}
	builder := cache_key_builder.NewTemplateKeyBuilder[T](template)
	return cache_key_builder.BuildKeyFunc[T](builder)
}

// ==================== 单个写入 ====================

func (wdg *WritedownRouterGroup[T]) HandleWritedownSingle(c *gin.Context) {
	var req WritedownSingleRequest[T]
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError[T](c, http.StatusBadRequest, fmt.Sprintf("invalid request: %v", err))
		return
	}
	if req.Key == "" {
		respondError[T](c, http.StatusBadRequest, "key cannot be empty")
		return
	}

	// 使用全局默认逻辑
	expiration := parseExpiration(3600, req.ExpirationSeconds)

	var data *T
	var err error
	if req.Data != nil {
		data = req.Data
	} else if req.ID != nil {
		data, err = wdg.Service.GetSingle(c.Request.Context(), func(db *gorm.DB) *gorm.DB {
			return db.Where("id = ?", req.ID)
		}, nil)
		if err != nil {
			respondError[T](c, http.StatusInternalServerError, fmt.Sprintf("failed to load data: %v", err))
			return
		}
	} else {
		respondError[T](c, http.StatusBadRequest, "either data or id must be provided")
		return
	}

	opts := &service.WritedownSingleOptions{
		Expiration: expiration,
		Overwrite:  req.Overwrite,
		NX:         req.NX,
		XX:         req.XX,
	}

	if req.Async {
		wdg.Service.WritedownSingleAsync(c.Request.Context(), req.Key, data, expiration)
		c.JSON(http.StatusOK, WritedownResponse[T]{Code: 0, Message: "async write initiated"})
		return
	}

	if err := wdg.Service.WritedownSingle(c.Request.Context(), req.Key, data, opts); err != nil {
		respondError[T](c, http.StatusInternalServerError, fmt.Sprintf("writedown failed: %v", err))
		return
	}

	respondSuccess[T](c, 1, nil)
}

func (wdg *WritedownRouterGroup[T]) HandleWritedownWithLock(c *gin.Context) {
	var req WritedownWithLockRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError[T](c, http.StatusBadRequest, fmt.Sprintf("invalid request: %v", err))
		return
	}

	// 缓存过期使用全局默认，分布式锁超时保持较短的硬编码默认(5s)
	expiration := parseExpiration(3600, req.Expiration)
	lockTimeout := parseExpiration(5, req.LockTimeout)

	queryFunc := func(db *gorm.DB) *gorm.DB { return db.Where("id = ?", req.ID) }

	data, err := wdg.Service.WritedownSingleWithLock(c.Request.Context(), req.Key, queryFunc, expiration, lockTimeout)
	if err != nil {
		respondError[T](c, http.StatusInternalServerError, fmt.Sprintf("writedown with lock failed: %v", err))
		return
	}

	respondSuccess(c, 1, data)
}

func (wdg *WritedownRouterGroup[T]) HandleWritedownWithVersion(c *gin.Context) {
	var req WritedownWithVersionRequest[T]
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError[T](c, http.StatusBadRequest, fmt.Sprintf("invalid request: %v", err))
		return
	}
	if req.Key == "" || req.Data == nil {
		respondError[T](c, http.StatusBadRequest, "key and data cannot be empty")
		return
	}

	expiration := parseExpiration(3600, req.Expiration)
	if err := wdg.Service.WritedownSingleWithVersion(c.Request.Context(), req.Key, req.Data, req.Version, expiration); err != nil {
		respondError[T](c, http.StatusInternalServerError, fmt.Sprintf("writedown with version failed: %v", err))
		return
	}

	respondSuccess[T](c, 1, nil)
}

func (wdg *WritedownRouterGroup[T]) HandleRefreshCache(c *gin.Context) {
	var req RefreshCacheRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError[T](c, http.StatusBadRequest, fmt.Sprintf("invalid request: %v", err))
		return
	}
	if req.Key == "" || req.ID == nil {
		respondError[T](c, http.StatusBadRequest, "key and id cannot be empty")
		return
	}

	expiration := parseExpiration(3600, req.Expiration)
	queryFunc := func(db *gorm.DB) *gorm.DB { return db.Where("id = ?", req.ID) }

	if err := wdg.Service.RefreshSingleCacheFromDB(c.Request.Context(), req.Key, queryFunc, expiration); err != nil {
		respondError[T](c, http.StatusInternalServerError, fmt.Sprintf("refresh cache failed: %v", err))
		return
	}

	respondSuccess[T](c, 1, nil)
}

// ==================== 批量写入 ====================
func (wdg *WritedownRouterGroup[T]) HandleWritedownQuery(c *gin.Context) {
	var req WritedownQueryRequest[T]
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError[T](c, http.StatusBadRequest, fmt.Sprintf("invalid request: %v", err))
		return
	}
	if req.KeyTemplate == "" {
		respondError[T](c, http.StatusBadRequest, "key_template cannot be empty")
		return
	}

	expiration := parseExpiration(3600, req.Expiration)
	if req.BatchSize == 0 {
		req.BatchSize = 100
	}
	buildKey := wdg.getKeyFunc(req.KeyTemplate)

	if len(req.Data) > 0 {
		writeBatch(c, wdg, req.Data, buildKey, expiration, req.BatchSize, req.Overwrite, req.UsePipeline, req.Incremental)
		return
	} else if len(req.IDs) > 0 {
		if err := wdg.Service.WritedownQueryByIDs(c.Request.Context(), req.IDs, buildKey, &service.WritedownQueryOptions{
			Expiration: expiration,
			BatchSize:  req.BatchSize,
			Overwrite:  req.Overwrite,
		}); err != nil {
			respondError[T](c, http.StatusInternalServerError, fmt.Sprintf("writedown by ids failed: %v", err))
			return
		}
		respondSuccess[T](c, len(req.IDs), nil)
		return
	} else if req.LoadAll {
		if err := wdg.Service.WritedownAllToCache(c.Request.Context(), buildKey, &service.WritedownQueryOptions{
			Expiration: expiration,
			BatchSize:  req.BatchSize,
			Overwrite:  req.Overwrite,
		}); err != nil {
			respondError[T](c, http.StatusInternalServerError, fmt.Sprintf("writedown all failed: %v", err))
			return
		}
		respondSuccess[T](c, 0, nil)
		return
	}
}

func writeBatch[T any](c *gin.Context, wdg *WritedownRouterGroup[T], data []T, buildKey func(*T) string, expiration time.Duration, batchSize int, overwrite, usePipeline, incremental bool) {
	opts := &service.WritedownQueryOptions{
		Expiration: expiration,
		BatchSize:  batchSize,
		Overwrite:  overwrite,
	}
	var err error
	if usePipeline {
		err = wdg.Service.WritedownWithPipeline(c.Request.Context(), data, buildKey, opts)
	} else if incremental {
		err = wdg.Service.WritedownIncremental(c.Request.Context(), data, buildKey, nil, opts)
	} else {
		err = wdg.Service.WritedownQuery(c.Request.Context(), data, buildKey, opts)
	}
	if err != nil {
		respondError[T](c, http.StatusInternalServerError, fmt.Sprintf("writedown query failed: %v", err))
		return
	}
	respondSuccess[T](c, len(data), nil)
}

func (wdg *WritedownRouterGroup[T]) HandleWarmupCache(c *gin.Context) {
	var req WarmupCacheRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError[T](c, http.StatusBadRequest, fmt.Sprintf("invalid request: %v", err))
		return
	}
	if req.KeyTemplate == "" {
		respondError[T](c, http.StatusBadRequest, "key_template cannot be empty")
		return
	}
	if req.Expiration == 0 {
		req.Expiration = 3600
	}
	if req.Limit == 0 {
		req.Limit = 1000
	}
	if req.OrderBy == "" {
		req.OrderBy = "id"
	}

	buildKey := wdg.getKeyFunc(req.KeyTemplate)
	queryFunc := func(db *gorm.DB) *gorm.DB { return db.Order(fmt.Sprintf("%s DESC", req.OrderBy)).Limit(req.Limit) }

	if err := wdg.Service.WarmupCache(c.Request.Context(), queryFunc, buildKey, parseExpiration(3600, req.Expiration)); err != nil {
		respondError[T](c, http.StatusInternalServerError, fmt.Sprintf("warmup cache failed: %v", err))
		return
	}
	respondSuccess[T](c, 0, nil)
}
