package http_router

import (
	"context"
	"fmt"
	"net/http"

	"github.com/Super-Gagaga/abstract-manager/service"
	"github.com/Super-Gagaga/abstract-manager/util/filter_translator"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ========== 预定义查询方法 ==========

type PaginatedQueryMethod[T any] struct {
	Name       string
	PageSize   int
	Service    *service.ServiceManager[T]
	FilterFunc func(*gorm.DB) *gorm.DB
	OrderBy    string
	Order      string
}

func (m *PaginatedQueryMethod[T]) Execute(ctx context.Context, page int, filters []filter_translator.GormFilter) (*service.QueryResult[T], error) {
	queryFunc := func(db *gorm.DB) *gorm.DB {
		if m.FilterFunc != nil {
			db = m.FilterFunc(db)
		}

		// 【变更1】使用示例中的 ApplyGormFilters 替代原有的 ApplyFilters
		db = filter_translator.ApplyGormFilters(db, filters)

		return db
	}

	opts := &service.QueryOptions{
		Page:     page,
		PageSize: m.PageSize,
		OrderBy:  m.OrderBy,
		Order:    m.Order,
	}

	return m.Service.GetQuery(ctx, queryFunc, opts)
}

// ========== 查询方法注册表 (保持不变) ==========

type QueryMethodRegistry[T any] struct {
	methods map[string]*PaginatedQueryMethod[T]
}

func NewQueryMethodRegistry[T any]() *QueryMethodRegistry[T] {
	return &QueryMethodRegistry[T]{
		methods: make(map[string]*PaginatedQueryMethod[T]),
	}
}

func (r *QueryMethodRegistry[T]) Register(method *PaginatedQueryMethod[T]) {
	r.methods[method.Name] = method
}

func (r *QueryMethodRegistry[T]) Get(name string) (*PaginatedQueryMethod[T], bool) {
	method, ok := r.methods[name]
	return method, ok
}

// ========== Gin 路由组 ==========

type QueryRouterGroup[T any] struct {
	RouterGroup    *gin.RouterGroup
	Service        *service.ServiceManager[T]
	MethodRegistry *QueryMethodRegistry[T]
	// 【变更2】类型改为 GormTranslatorRegistry (根据示例推断)
	TranslatorRegistry *filter_translator.GormTranslatorRegistry
}

func NewQueryRouterGroup[T any](
	rg *gin.RouterGroup,
	service *service.ServiceManager[T],
) *QueryRouterGroup[T] {
	return &QueryRouterGroup[T]{
		RouterGroup:    rg,
		Service:        service,
		MethodRegistry: NewQueryMethodRegistry[T](),
		// 【变更3】使用示例中的 DefaultGormRegistry
		TranslatorRegistry: filter_translator.DefaultGormRegistry,
	}
}

// ========== 注册预定义查询方法 (保持不变) ==========

func (qrg *QueryRouterGroup[T]) RegisterMethod(name string, pageSize int, filterFunc func(*gorm.DB) *gorm.DB, orderBy string, order string) {
	method := &PaginatedQueryMethod[T]{
		Name:       name,
		PageSize:   pageSize,
		Service:    qrg.Service,
		FilterFunc: filterFunc,
		OrderBy:    orderBy,
		Order:      order,
	}
	qrg.MethodRegistry.Register(method)
}

func (qrg *QueryRouterGroup[T]) RegisterListMethod(pageSize int) {
	qrg.RegisterMethod("list", pageSize, nil, "created_at", "DESC")
}

func (qrg *QueryRouterGroup[T]) RegisterActiveListMethod(pageSize int) {
	qrg.RegisterMethod("active_list", pageSize, func(db *gorm.DB) *gorm.DB {
		return db.Where("status = ?", "active").Where("deleted_at IS NULL")
	}, "created_at", "DESC")
}

func (qrg *QueryRouterGroup[T]) RegisterSearchMethod(pageSize int, searchFields []string) {
	qrg.RegisterMethod("search", pageSize, nil, "created_at", "DESC")
}

// ========== 路由注册 ==========

func (qrg *QueryRouterGroup[T]) RegisterRoutes(basePath string) {
	qrg.RouterGroup.POST(basePath+"/query", qrg.HandleQuery)
	qrg.RouterGroup.GET(basePath+"/:id", qrg.HandleGetByID)
	qrg.RouterGroup.POST(basePath+"/count", qrg.HandleCount)
}

// ========== 请求/响应结构 (保持不变) ==========

type QueryRequest struct {
	Method  string                          `json:"method"`
	Page    int                             `json:"page"`
	Filters []filter_translator.FilterParam `json:"filters"`
}

type QueryResponse[T any] struct {
	Code       int    `json:"code"`
	Message    string `json:"message"`
	Data       []T    `json:"data"`
	Total      int64  `json:"total"`
	Page       int    `json:"page"`
	PageSize   int    `json:"page_size"`
	TotalPages int    `json:"total_pages"`
}

type CountRequest struct {
	Filters []filter_translator.FilterParam `json:"filters"`
}

type CountResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Count   int64  `json:"count"`
}

// ========== 处理器 ==========

func (qrg *QueryRouterGroup[T]) HandleQuery(c *gin.Context) {
	var req QueryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "invalid request", "error": err.Error()})
		return
	}

	method, ok := qrg.MethodRegistry.Get(req.Method)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": fmt.Sprintf("unknown method: %s", req.Method)})
		return
	}

	// 【变更4】TranslateBatch 返回的是 []filter_translator.BaseFilter (与示例同步)
	filters, err := qrg.TranslatorRegistry.TranslateBatch(req.Filters)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "invalid filters", "error": err.Error()})
		return
	}

	result, err := method.Execute(c.Request.Context(), req.Page, filters)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "query failed", "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, QueryResponse[T]{
		Code:       0,
		Message:    "success",
		Data:       result.Data,
		Total:      result.Total,
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalPages: result.TotalPages,
	})
}

func (qrg *QueryRouterGroup[T]) HandleGetByID(c *gin.Context) {
	id := c.Param("id")
	result, err := qrg.Service.GetSingleByID(c.Request.Context(), id, nil)
	if err != nil {
		if err.Error() == "record not found" {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "record not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "query failed", "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "success", "data": result})
}

func (qrg *QueryRouterGroup[T]) HandleCount(c *gin.Context) {
	var req CountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "invalid request", "error": err.Error()})
		return
	}

	filters, err := qrg.TranslatorRegistry.TranslateBatch(req.Filters)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "invalid filters", "error": err.Error()})
		return
	}

	queryFunc := func(db *gorm.DB) *gorm.DB {
		// 【变更5】这里同样使用 ApplyGormFilters
		return filter_translator.ApplyGormFilters(db, filters)
	}

	count, err := qrg.Service.CountQuery(c.Request.Context(), queryFunc)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "count failed", "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, CountResponse{Code: 0, Message: "success", Count: count})
}

func (qrg *QueryRouterGroup[T]) RegisterCommonMethods(defaultPageSize int) {
	qrg.RegisterListMethod(defaultPageSize)
	qrg.RegisterActiveListMethod(defaultPageSize)
	qrg.RegisterSearchMethod(defaultPageSize, nil)
}
