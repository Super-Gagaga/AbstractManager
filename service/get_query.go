package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/Super-Gagaga/abstract-manager/util"
	"github.com/Super-Gagaga/abstract-manager/util/filter_translator"

	"gorm.io/gorm"
)

// HavingCondition 结构化的 HAVING 条件（支持运算符，类型安全）
type HavingCondition struct {
	Field    string      // 聚合字段/表达式名称（如 "COUNT(*)"、"SUM(amount)"）
	Operator string      // 运算符: "=", ">", ">=", "<", "<=", "!="
	Value    interface{} // 比较值
}

// QueryOptions 查询配置选项
type QueryOptions struct {
	Page             int                    // 页码（从1开始）
	PageSize         int                    // 每页数量
	OrderBy          string                 // 排序字段
	Order            string                 // 排序方向（ASC/DESC）
	Preload          []string               // 预加载关联
	Select           []string               // 指定查询字段
	Distinct         bool                   // 是否去重
	Group            string                 // 分组字段
	Having           map[string]interface{} // Having 条件（简易写法：仅支持 = 运算符）
	HavingConditions []HavingCondition      // Having 条件（结构化写法：支持全部运算符，推荐）
}

// QueryResult 查询结果
type QueryResult[T any] struct {
	Data       []T   // 数据列表
	Total      int64 // 总数
	Page       int   // 当前页
	PageSize   int   // 每页数量
	TotalPages int   // 总页数
}

// GetQuery 条件查询（支持分页）
// queryFunc: 用于构建查询条件的 lambda 函数
// 注意：纯只读 SELECT 不使用事务，直接复用 GetQueryWithoutTransaction
func (sm *ServiceManager[T]) GetQuery(
	ctx context.Context,
	queryFunc func(*gorm.DB) *gorm.DB,
	opts *QueryOptions,
) (*QueryResult[T], error) {
	return sm.GetQueryWithoutTransaction(ctx, queryFunc, opts)
}

// GetQueryWithoutTransaction 无事务的条件查询（用于高并发只读场景）
func (sm *ServiceManager[T]) GetQueryWithoutTransaction(
	ctx context.Context,
	queryFunc func(*gorm.DB) *gorm.DB,
	opts *QueryOptions,
) (*QueryResult[T], error) {
	ctx, cancel := util.EnsureTimeout(ctx, util.GetDefaultDBTimeout())
	defer cancel()

	db := sm.DB().WithContext(ctx)

	// 应用表名
	db = sm.applyTableName(db)

	// 应用查询条件
	if queryFunc != nil {
		db = queryFunc(db)
	}

	// 统计总数
	var total int64
	countDB := db
	if err := countDB.Model(&sm.Resource).Count(&total).Error; err != nil {
		return nil, fmt.Errorf("failed to count records: %w", err)
	}

	// 应用查询选项
	db = sm.applyQueryOptions(db, opts)

	// 执行查询
	var results []T
	if err := db.Find(&results).Error; err != nil {
		return nil, fmt.Errorf("failed to query records: %w", err)
	}

	// 构建返回结果
	result := &QueryResult[T]{
		Data:  results,
		Total: total,
	}

	if opts != nil && opts.PageSize > 0 {
		result.Page = opts.Page
		result.PageSize = opts.PageSize
		result.TotalPages = int((total + int64(opts.PageSize) - 1) / int64(opts.PageSize))
	}

	return result, nil
}

// applyTableName 应用表名
func (sm *ServiceManager[T]) applyTableName(db *gorm.DB) *gorm.DB {
	tableName := sm.TableName
	if sm.Schema != "" && sm.Schema != "public" {
		tableName = fmt.Sprintf("%s.%s", sm.Schema, sm.TableName)
	}
	return db.Table(tableName)
}

// applyQueryOptions 应用查询选项
func (sm *ServiceManager[T]) applyQueryOptions(db *gorm.DB, opts *QueryOptions) *gorm.DB {
	if opts == nil {
		return db
	}

	// 应用字段选择
	if len(opts.Select) > 0 {
		db = db.Select(opts.Select)
	}

	// 应用去重
	if opts.Distinct {
		db = db.Distinct()
	}

	// 应用分组（校验列名防注入）
	if opts.Group != "" {
		if err := filter_translator.ValidateSQLIdentifier(opts.Group); err != nil {
			db.AddError(fmt.Errorf("invalid Group field: %w", err))
			return db
		}
		db = db.Group(opts.Group)
	}

	// 应用 Having 条件 - 简易写法（仅支持 = 运算符）
	if len(opts.Having) > 0 {
		for key, value := range opts.Having {
			if err := filter_translator.ValidateSQLIdentifier(key); err != nil {
				db.AddError(fmt.Errorf("invalid Having key: %w", err))
				return db
			}
			db = db.Having(key, value)
		}
	}

	// 应用 Having 条件 - 结构化写法（支持全部运算符）
	for _, hc := range opts.HavingConditions {
		if err := filter_translator.ValidateSQLIdentifier(hc.Field); err != nil {
			db.AddError(fmt.Errorf("invalid HavingCondition field: %w", err))
			return db
		}
		// 校验运算符白名单
		switch strings.ToUpper(hc.Operator) {
		case "=", "!=", ">", ">=", "<", "<=":
			db = db.Having(fmt.Sprintf("%s %s ?", hc.Field, hc.Operator), hc.Value)
		default:
			db.AddError(fmt.Errorf("invalid HavingCondition operator: %s", hc.Operator))
			return db
		}
	}

	// 应用排序（校验列名防注入）
	if opts.OrderBy != "" {
		if err := filter_translator.ValidateSQLIdentifier(opts.OrderBy); err != nil {
			db.AddError(fmt.Errorf("invalid OrderBy field: %w", err))
			return db
		}
		order := "ASC"
		if opts.Order != "" {
			order = opts.Order
		}
		// 校验排序方向，只允许 ASC 或 DESC
		upperOrder := strings.ToUpper(order)
		if upperOrder != "ASC" && upperOrder != "DESC" {
			db.AddError(fmt.Errorf("invalid Order direction: %s (must be ASC or DESC)", order))
			return db
		}
		db = db.Order(fmt.Sprintf("%s %s", opts.OrderBy, upperOrder))
	}

	// 应用预加载
	for _, preload := range opts.Preload {
		db = db.Preload(preload)
	}

	// 应用分页
	if opts.PageSize > 0 {
		page := opts.Page
		if page < 1 {
			page = 1
		}
		offset := (page - 1) * opts.PageSize
		db = db.Offset(offset).Limit(opts.PageSize)
	}

	return db
}

// CountQuery 条件计数
func (sm *ServiceManager[T]) CountQuery(
	ctx context.Context,
	queryFunc func(*gorm.DB) *gorm.DB,
) (int64, error) {
	ctx, cancel := util.EnsureTimeout(ctx, util.GetDefaultDBTimeout())
	defer cancel()

	db := sm.DB().WithContext(ctx)

	// 应用表名
	db = sm.applyTableName(db)

	// 应用查询条件
	if queryFunc != nil {
		db = queryFunc(db)
	}

	var count int64
	if err := db.Model(&sm.Resource).Count(&count).Error; err != nil {
		return 0, fmt.Errorf("failed to count records: %w", err)
	}

	return count, nil
}

// ExistsQuery 检查是否存在满足条件的记录
func (sm *ServiceManager[T]) ExistsQuery(
	ctx context.Context,
	queryFunc func(*gorm.DB) *gorm.DB,
) (bool, error) {
	count, err := sm.CountQuery(ctx, queryFunc)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
