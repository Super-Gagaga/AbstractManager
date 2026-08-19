package service

import (
	"context"
	"fmt"

	"github.com/Super-Gagaga/AbstractManager/util"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SingleQueryOptions 单个查询配置选项
type SingleQueryOptions struct {
	Preload   []string // 预加载关联
	Select    []string // 指定查询字段
	ForUpdate bool     // 是否加锁查询（用于后续更新）
}

// GetSingle 查询单个记录
// queryFunc: 用于构建查询条件的 lambda 函数
func (sm *ServiceManager[T]) GetSingle(
	ctx context.Context,
	queryFunc func(*gorm.DB) *gorm.DB,
	opts *SingleQueryOptions,
) (*T, error) {
	ctx, cancel := util.EnsureTimeout(ctx, util.GetDefaultDBTimeout())
	defer cancel()

	db := GetDB().WithContext(ctx)

	// 如果需要加锁，使用更高的事务隔离级别
	if opts != nil && opts.ForUpdate {
		// 开启事务并加锁
		db = db.Begin()
		defer func() {
			if r := recover(); r != nil {
				db.Rollback()
			}
		}()
	}

	// 应用表名
	db = sm.applyTableName(db)

	// 应用查询条件
	if queryFunc != nil {
		db = queryFunc(db)
	}

	// 应用单个查询选项
	db = sm.applySingleQueryOptions(db, opts)

	var result T
	err := db.First(&result).Error

	if err != nil {
		if opts != nil && opts.ForUpdate {
			db.Rollback()
		}
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("record not found")
		}
		return nil, fmt.Errorf("failed to query record: %w", err)
	}

	// 如果是加锁查询，不提交事务（让调用者处理）
	if opts != nil && opts.ForUpdate {
		// 返回结果，但保持事务打开
		// 注意：这里需要调用者在使用完数据后手动提交或回滚
		return &result, nil
	}

	return &result, nil
}

// GetSingleByID 根据主键 ID 查询单个记录
func (sm *ServiceManager[T]) GetSingleByID(
	ctx context.Context,
	id interface{},
	opts *SingleQueryOptions,
) (*T, error) {
	return sm.GetSingle(ctx, func(db *gorm.DB) *gorm.DB {
		return db.Where("id = ?", id)
	}, opts)
}

// GetSingleOrCreate 查询单个记录，不存在则创建
func (sm *ServiceManager[T]) GetSingleOrCreate(
	ctx context.Context,
	queryFunc func(*gorm.DB) *gorm.DB,
	createData *T,
) (*T, bool, error) {
	ctx, cancel := util.EnsureTimeout(ctx, util.GetDefaultDBTimeout())
	defer cancel()

	db := GetDB().WithContext(ctx)

	// 开启 REPEATABLE READ 事务
	db = db.Begin()
	defer func() {
		if r := recover(); r != nil {
			db.Rollback()
		}
	}()

	// 应用表名
	db = sm.applyTableName(db)

	// 应用查询条件
	if queryFunc != nil {
		db = queryFunc(db)
	}

	var result T
	err := db.First(&result).Error

	if err == nil {
		// 记录存在
		if err := db.Commit().Error; err != nil {
			return nil, false, fmt.Errorf("failed to commit transaction: %w", err)
		}
		return &result, false, nil
	}

	if err != gorm.ErrRecordNotFound {
		db.Rollback()
		return nil, false, fmt.Errorf("failed to query record: %w", err)
	}

	// 记录不存在，创建新记录
	if err := db.Create(createData).Error; err != nil {
		db.Rollback()
		return nil, false, fmt.Errorf("failed to create record: %w", err)
	}

	if err := db.Commit().Error; err != nil {
		return nil, false, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return createData, true, nil
}

// GetSingleWithLock 加锁查询单个记录（用于后续更新）
func (sm *ServiceManager[T]) GetSingleWithLock(
	ctx context.Context,
	queryFunc func(*gorm.DB) *gorm.DB,
) (*T, *gorm.DB, error) {
	ctx, cancel := util.EnsureTimeout(ctx, util.GetDefaultDBTimeout())
	defer cancel()

	db := GetDB().WithContext(ctx)

	// 开启事务
	txDB := db.Begin()

	// 应用表名
	txDB = sm.applyTableName(txDB)

	// 应用查询条件
	if queryFunc != nil {
		txDB = queryFunc(txDB)
	}

	// 加行锁
	txDB = txDB.Clauses(clause.Locking{Strength: "UPDATE"})

	var result T
	if err := txDB.First(&result).Error; err != nil {
		txDB.Rollback()
		if err == gorm.ErrRecordNotFound {
			return nil, nil, fmt.Errorf("record not found")
		}
		return nil, nil, fmt.Errorf("failed to query record with lock: %w", err)
	}

	// 返回结果和事务 DB，由调用者负责提交或回滚
	return &result, txDB, nil
}

// applySingleQueryOptions 应用单个查询选项
func (sm *ServiceManager[T]) applySingleQueryOptions(db *gorm.DB, opts *SingleQueryOptions) *gorm.DB {
	if opts == nil {
		return db
	}

	// 应用字段选择
	if len(opts.Select) > 0 {
		db = db.Select(opts.Select)
	}

	// 应用预加载
	for _, preload := range opts.Preload {
		db = db.Preload(preload)
	}

	// 应用行锁
	if opts.ForUpdate {
		db = db.Clauses(clause.Locking{Strength: "UPDATE"})
	}

	return db
}

// GetFirst 查询第一条记录（按创建时间）
func (sm *ServiceManager[T]) GetFirst(
	ctx context.Context,
	queryFunc func(*gorm.DB) *gorm.DB,
) (*T, error) {
	ctx, cancel := util.EnsureTimeout(ctx, util.GetDefaultDBTimeout())
	defer cancel()

	db := GetDB().WithContext(ctx)

	// 应用表名
	db = sm.applyTableName(db)

	// 应用查询条件
	if queryFunc != nil {
		db = queryFunc(db)
	}

	var result T
	if err := db.Order("created_at ASC").First(&result).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("record not found")
		}
		return nil, fmt.Errorf("failed to query first record: %w", err)
	}

	return &result, nil
}

// GetLast 查询最后一条记录（按创建时间）
func (sm *ServiceManager[T]) GetLast(
	ctx context.Context,
	queryFunc func(*gorm.DB) *gorm.DB,
) (*T, error) {
	ctx, cancel := util.EnsureTimeout(ctx, util.GetDefaultDBTimeout())
	defer cancel()

	db := GetDB().WithContext(ctx)

	// 应用表名
	db = sm.applyTableName(db)

	// 应用查询条件
	if queryFunc != nil {
		db = queryFunc(db)
	}

	var result T
	if err := db.Order("created_at DESC").First(&result).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("record not found")
		}
		return nil, fmt.Errorf("failed to query last record: %w", err)
	}

	return &result, nil
}
