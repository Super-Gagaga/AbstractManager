package http_router

import (
	"fmt"
	"net/http"

	"github.com/Super-Gagaga/AbstractManager/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ========== 写操作请求/响应结构 ==========

// SetSingleRequest 单个设置请求
type SetSingleRequest[T any] struct {
	Data             *T   `json:"data"`
	OnConflictUpdate bool `json:"on_conflict_update"` // 默认 true
	InvalidateCache  bool `json:"invalidate_cache"`   // 默认 true
}

// SetQueryRequest 批量设置请求
type SetQueryRequest[T any] struct {
	Data             []T  `json:"data"`
	BatchSize        int  `json:"batch_size"`         // 默认 100
	OnConflictUpdate bool `json:"on_conflict_update"` // 默认 true
	InvalidateCache  bool `json:"invalidate_cache"`   // 默认 true
}

// UpdateRequest 更新请求
type UpdateRequest struct {
	ID      interface{}            `json:"id"`      // 可选,如果提供则按ID更新
	Updates map[string]interface{} `json:"updates"` // 更新字段
}

// BatchUpdateRequest 批量更新请求
type BatchUpdateRequest struct {
	Updates map[string]interface{} `json:"updates"`           // 更新字段
	Filters []interface{}          `json:"filters,omitempty"` // 过滤条件(预留)
}

// DeleteRequest 删除请求
type DeleteRequest struct {
	ID   interface{} `json:"id"`   // 可选,如果提供则按ID删除
	Soft bool        `json:"soft"` // 是否软删除
}

// BatchDeleteRequest 批量删除请求
type BatchDeleteRequest struct {
	IDs     []interface{} `json:"ids,omitempty"`     // ID列表
	Soft    bool          `json:"soft"`              // 是否软删除
	Filters []interface{} `json:"filters,omitempty"` // 过滤条件(预留)
}

// UpsertRequest Upsert请求
type UpsertRequest[T any] struct {
	Data            *T       `json:"data"`
	ConflictColumns []string `json:"conflict_columns"` // 冲突字段
	UpdateColumns   []string `json:"update_columns"`   // 更新字段(为空则全部更新)
}

// BatchUpsertRequest 批量Upsert请求
type BatchUpsertRequest[T any] struct {
	Data            []T      `json:"data"`
	ConflictColumns []string `json:"conflict_columns"` // 冲突字段
	UpdateColumns   []string `json:"update_columns"`   // 更新字段(为空则全部更新)
	BatchSize       int      `json:"batch_size"`       // 批次大小,默认100
}

// IncrementRequest 增量请求
type IncrementRequest struct {
	ID     interface{} `json:"id"`                // 可选,如果提供则按ID操作
	Column string      `json:"column"`            // 字段名
	Value  interface{} `json:"value"`             // 增量值
	IsDecr bool        `json:"is_decr,omitempty"` // 是否为减量操作
}

// BatchIncrementRequest 批量增量请求
type BatchIncrementRequest struct {
	IDs     []interface{} `json:"ids,omitempty"`     // ID列表
	Column  string        `json:"column"`            // 字段名
	Value   interface{}   `json:"value"`             // 增量值
	IsDecr  bool          `json:"is_decr,omitempty"` // 是否为减量操作
	Filters []interface{} `json:"filters,omitempty"` // 过滤条件(预留)
}

// WriteResponse 写操作响应
type WriteResponse struct {
	Code         int    `json:"code"`
	Message      string `json:"message"`
	RowsAffected int64  `json:"rows_affected,omitempty"` // 影响的行数
}

// ========== 写操作路由组 ==========

type WriteRouterGroup[T any] struct {
	RouterGroup *gin.RouterGroup
	Service     *service.ServiceManager[T]
}

func NewWriteRouterGroup[T any](
	rg *gin.RouterGroup,
	service *service.ServiceManager[T],
) *WriteRouterGroup[T] {
	return &WriteRouterGroup[T]{
		RouterGroup: rg,
		Service:     service,
	}
}

// ========== 路由注册 ==========

func (wrg *WriteRouterGroup[T]) RegisterRoutes(basePath string) {
	// 单个操作
	wrg.RouterGroup.POST(basePath+"/set", wrg.HandleSetSingle)
	wrg.RouterGroup.POST(basePath+"/insert", wrg.HandleInsert)
	wrg.RouterGroup.PUT(basePath+"/update", wrg.HandleUpdate)
	wrg.RouterGroup.DELETE(basePath+"/delete", wrg.HandleDelete)
	wrg.RouterGroup.POST(basePath+"/upsert", wrg.HandleUpsert)
	wrg.RouterGroup.POST(basePath+"/increment", wrg.HandleIncrement)

	// 批量操作
	wrg.RouterGroup.POST(basePath+"/batch/set", wrg.HandleSetQuery)
	wrg.RouterGroup.POST(basePath+"/batch/insert", wrg.HandleBatchInsert)
	wrg.RouterGroup.PUT(basePath+"/batch/update", wrg.HandleBatchUpdate)
	wrg.RouterGroup.DELETE(basePath+"/batch/delete", wrg.HandleBatchDelete)
	wrg.RouterGroup.POST(basePath+"/batch/upsert", wrg.HandleBatchUpsert)
	wrg.RouterGroup.POST(basePath+"/batch/increment", wrg.HandleBatchIncrement)
}

// ========== 单个操作处理器 ==========

// HandleSetSingle 处理单个设置(新增或更新)
func (wrg *WriteRouterGroup[T]) HandleSetSingle(c *gin.Context) {
	var req SetSingleRequest[T]
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, WriteResponse{
			Code:    400,
			Message: fmt.Sprintf("invalid request: %v", err),
		})
		return
	}

	if req.Data == nil {
		c.JSON(http.StatusBadRequest, WriteResponse{
			Code:    400,
			Message: "data cannot be nil",
		})
		return
	}

	opts := &service.SetSingleOptions{
		OnConflictUpdate: req.OnConflictUpdate,
		InvalidateCache:  req.InvalidateCache,
	}

	if err := wrg.Service.SetSingle(c.Request.Context(), req.Data, opts); err != nil {
		c.JSON(http.StatusInternalServerError, WriteResponse{
			Code:    500,
			Message: fmt.Sprintf("set single failed: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, WriteResponse{
		Code:    0,
		Message: "success",
	})
}

// HandleInsert 处理插入(不冲突更新)
func (wrg *WriteRouterGroup[T]) HandleInsert(c *gin.Context) {
	var req SetSingleRequest[T]
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, WriteResponse{
			Code:    400,
			Message: fmt.Sprintf("invalid request: %v", err),
		})
		return
	}

	if req.Data == nil {
		c.JSON(http.StatusBadRequest, WriteResponse{
			Code:    400,
			Message: "data cannot be nil",
		})
		return
	}

	if err := wrg.Service.Insert(c.Request.Context(), req.Data); err != nil {
		c.JSON(http.StatusInternalServerError, WriteResponse{
			Code:    500,
			Message: fmt.Sprintf("insert failed: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, WriteResponse{
		Code:    0,
		Message: "success",
	})
}

// HandleUpdate 处理更新
func (wrg *WriteRouterGroup[T]) HandleUpdate(c *gin.Context) {
	var req UpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, WriteResponse{
			Code:    400,
			Message: fmt.Sprintf("invalid request: %v", err),
		})
		return
	}

	if len(req.Updates) == 0 {
		c.JSON(http.StatusBadRequest, WriteResponse{
			Code:    400,
			Message: "updates cannot be empty",
		})
		return
	}

	var err error
	if req.ID != nil {
		err = wrg.Service.UpdateByID(c.Request.Context(), req.ID, req.Updates)
	} else {
		err = wrg.Service.Update(c.Request.Context(), req.Updates, nil)
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, WriteResponse{
			Code:    500,
			Message: fmt.Sprintf("update failed: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, WriteResponse{
		Code:    0,
		Message: "success",
	})
}

// HandleDelete 处理删除
func (wrg *WriteRouterGroup[T]) HandleDelete(c *gin.Context) {
	var req DeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, WriteResponse{
			Code:    400,
			Message: fmt.Sprintf("invalid request: %v", err),
		})
		return
	}

	if req.ID == nil {
		c.JSON(http.StatusBadRequest, WriteResponse{
			Code:    400,
			Message: "id cannot be nil",
		})
		return
	}

	var err error
	if req.Soft {
		err = wrg.Service.SoftDeleteByID(c.Request.Context(), req.ID)
	} else {
		err = wrg.Service.DeleteByID(c.Request.Context(), req.ID)
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, WriteResponse{
			Code:    500,
			Message: fmt.Sprintf("delete failed: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, WriteResponse{
		Code:    0,
		Message: "success",
	})
}

// HandleUpsert 处理Upsert
func (wrg *WriteRouterGroup[T]) HandleUpsert(c *gin.Context) {
	var req UpsertRequest[T]
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, WriteResponse{
			Code:    400,
			Message: fmt.Sprintf("invalid request: %v", err),
		})
		return
	}

	if req.Data == nil {
		c.JSON(http.StatusBadRequest, WriteResponse{
			Code:    400,
			Message: "data cannot be nil",
		})
		return
	}

	if len(req.ConflictColumns) == 0 {
		c.JSON(http.StatusBadRequest, WriteResponse{
			Code:    400,
			Message: "conflict_columns cannot be empty",
		})
		return
	}

	err := wrg.Service.Upsert(
		c.Request.Context(),
		req.Data,
		req.ConflictColumns,
		req.UpdateColumns,
	)

	if err != nil {
		c.JSON(http.StatusInternalServerError, WriteResponse{
			Code:    500,
			Message: fmt.Sprintf("upsert failed: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, WriteResponse{
		Code:    0,
		Message: "success",
	})
}

// HandleIncrement 处理增量/减量
func (wrg *WriteRouterGroup[T]) HandleIncrement(c *gin.Context) {
	var req IncrementRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, WriteResponse{
			Code:    400,
			Message: fmt.Sprintf("invalid request: %v", err),
		})
		return
	}

	if req.Column == "" {
		c.JSON(http.StatusBadRequest, WriteResponse{
			Code:    400,
			Message: "column cannot be empty",
		})
		return
	}

	if req.ID == nil {
		c.JSON(http.StatusBadRequest, WriteResponse{
			Code:    400,
			Message: "id cannot be nil",
		})
		return
	}

	var err error
	if req.IsDecr {
		err = wrg.Service.DecrementByID(c.Request.Context(), req.ID, req.Column, req.Value)
	} else {
		err = wrg.Service.IncrementByID(c.Request.Context(), req.ID, req.Column, req.Value)
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, WriteResponse{
			Code:    500,
			Message: fmt.Sprintf("increment/decrement failed: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, WriteResponse{
		Code:    0,
		Message: "success",
	})
}

// ========== 批量操作处理器 ==========

// HandleSetQuery 处理批量设置
func (wrg *WriteRouterGroup[T]) HandleSetQuery(c *gin.Context) {
	var req SetQueryRequest[T]
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, WriteResponse{
			Code:    400,
			Message: fmt.Sprintf("invalid request: %v", err),
		})
		return
	}

	if len(req.Data) == 0 {
		c.JSON(http.StatusBadRequest, WriteResponse{
			Code:    400,
			Message: "data cannot be empty",
		})
		return
	}

	opts := &service.SetQueryOptions{
		BatchSize:        req.BatchSize,
		OnConflictUpdate: req.OnConflictUpdate,
		InvalidateCache:  req.InvalidateCache,
	}

	if err := wrg.Service.SetQuery(c.Request.Context(), req.Data, opts); err != nil {
		c.JSON(http.StatusInternalServerError, WriteResponse{
			Code:    500,
			Message: fmt.Sprintf("set query failed: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, WriteResponse{
		Code:         0,
		Message:      "success",
		RowsAffected: int64(len(req.Data)),
	})
}

// HandleBatchInsert 处理批量插入
func (wrg *WriteRouterGroup[T]) HandleBatchInsert(c *gin.Context) {
	var req SetQueryRequest[T]
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, WriteResponse{
			Code:    400,
			Message: fmt.Sprintf("invalid request: %v", err),
		})
		return
	}

	if len(req.Data) == 0 {
		c.JSON(http.StatusBadRequest, WriteResponse{
			Code:    400,
			Message: "data cannot be empty",
		})
		return
	}

	batchSize := req.BatchSize
	if batchSize <= 0 {
		batchSize = 100
	}

	if err := wrg.Service.BatchInsert(c.Request.Context(), req.Data, batchSize); err != nil {
		c.JSON(http.StatusInternalServerError, WriteResponse{
			Code:    500,
			Message: fmt.Sprintf("batch insert failed: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, WriteResponse{
		Code:         0,
		Message:      "success",
		RowsAffected: int64(len(req.Data)),
	})
}

// HandleBatchUpdate 处理批量更新
func (wrg *WriteRouterGroup[T]) HandleBatchUpdate(c *gin.Context) {
	var req BatchUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, WriteResponse{
			Code:    400,
			Message: fmt.Sprintf("invalid request: %v", err),
		})
		return
	}

	if len(req.Updates) == 0 {
		c.JSON(http.StatusBadRequest, WriteResponse{
			Code:    400,
			Message: "updates cannot be empty",
		})
		return
	}

	rowsAffected, err := wrg.Service.BatchUpdate(c.Request.Context(), req.Updates, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, WriteResponse{
			Code:    500,
			Message: fmt.Sprintf("batch update failed: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, WriteResponse{
		Code:         0,
		Message:      "success",
		RowsAffected: rowsAffected,
	})
}

// HandleBatchDelete 处理批量删除
func (wrg *WriteRouterGroup[T]) HandleBatchDelete(c *gin.Context) {
	var req BatchDeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, WriteResponse{
			Code:    400,
			Message: fmt.Sprintf("invalid request: %v", err),
		})
		return
	}

	if len(req.IDs) == 0 {
		c.JSON(http.StatusBadRequest, WriteResponse{
			Code:    400,
			Message: "ids cannot be empty",
		})
		return
	}

	var rowsAffected int64
	var err error

	if req.Soft {
		rowsAffected, err = wrg.Service.BatchSoftDelete(c.Request.Context(), func(db *gorm.DB) *gorm.DB {
			return db.Where("id IN ?", req.IDs)
		})
	} else {
		rowsAffected, err = wrg.Service.BatchDelete(c.Request.Context(), func(db *gorm.DB) *gorm.DB {
			return db.Where("id IN ?", req.IDs)
		})
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, WriteResponse{
			Code:    500,
			Message: fmt.Sprintf("batch delete failed: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, WriteResponse{
		Code:         0,
		Message:      "success",
		RowsAffected: rowsAffected,
	})
}

// HandleBatchUpsert 处理批量Upsert
func (wrg *WriteRouterGroup[T]) HandleBatchUpsert(c *gin.Context) {
	var req BatchUpsertRequest[T]
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, WriteResponse{
			Code:    400,
			Message: fmt.Sprintf("invalid request: %v", err),
		})
		return
	}

	if len(req.Data) == 0 {
		c.JSON(http.StatusBadRequest, WriteResponse{
			Code:    400,
			Message: "data cannot be empty",
		})
		return
	}

	if len(req.ConflictColumns) == 0 {
		c.JSON(http.StatusBadRequest, WriteResponse{
			Code:    400,
			Message: "conflict_columns cannot be empty",
		})
		return
	}

	batchSize := req.BatchSize
	if batchSize <= 0 {
		batchSize = 100
	}

	err := wrg.Service.BatchUpsert(
		c.Request.Context(),
		req.Data,
		req.ConflictColumns,
		req.UpdateColumns,
		batchSize,
	)

	if err != nil {
		c.JSON(http.StatusInternalServerError, WriteResponse{
			Code:    500,
			Message: fmt.Sprintf("batch upsert failed: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, WriteResponse{
		Code:         0,
		Message:      "success",
		RowsAffected: int64(len(req.Data)),
	})
}

// HandleBatchIncrement 处理批量增量/减量
func (wrg *WriteRouterGroup[T]) HandleBatchIncrement(c *gin.Context) {
	var req BatchIncrementRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, WriteResponse{
			Code:    400,
			Message: fmt.Sprintf("invalid request: %v", err),
		})
		return
	}

	if req.Column == "" {
		c.JSON(http.StatusBadRequest, WriteResponse{
			Code:    400,
			Message: "column cannot be empty",
		})
		return
	}

	if len(req.IDs) == 0 {
		c.JSON(http.StatusBadRequest, WriteResponse{
			Code:    400,
			Message: "ids cannot be empty",
		})
		return
	}

	var rowsAffected int64
	var err error

	queryFunc := func(db *gorm.DB) *gorm.DB {
		return db.Where("id IN ?", req.IDs)
	}

	if req.IsDecr {
		rowsAffected, err = wrg.Service.BatchDecrement(c.Request.Context(), req.Column, req.Value, queryFunc)
	} else {
		rowsAffected, err = wrg.Service.BatchIncrement(c.Request.Context(), req.Column, req.Value, queryFunc)
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, WriteResponse{
			Code:    500,
			Message: fmt.Sprintf("batch increment/decrement failed: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, WriteResponse{
		Code:         0,
		Message:      "success",
		RowsAffected: rowsAffected,
	})
}
