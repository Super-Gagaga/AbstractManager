package service

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Super-Gagaga/abstract-manager/util"
	"github.com/Super-Gagaga/abstract-manager/util/logger"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SetSingleOptions 单个设置配置选项
type SetSingleOptions struct {
	OnConflictUpdate bool // 冲突时是否更新
	InvalidateCache  bool // 是否使缓存失效
	ReturnUpdated    bool // 是否返回更新后的数据
}

// SetSingle 设置单个数据（新增或修改）
func (sm *ServiceManager[T]) SetSingle(
	ctx context.Context,
	data *T,
	opts *SetSingleOptions,
) error {
	// 设置默认选项
	if opts == nil {
		opts = &SetSingleOptions{
			OnConflictUpdate: true,
			InvalidateCache:  true,
			ReturnUpdated:    false,
		}
	}

	// 开启事务闭包
	ctx, cancel := util.EnsureTimeout(ctx, util.GetDefaultDBTimeout())
	defer cancel()
	err := sm.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		tx = sm.applyTableName(tx)

		if opts.OnConflictUpdate {
			// 使用 Upsert 操作
			return tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(data).Error
		}
		// 仅插入
		return tx.Create(data).Error
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})

	if err != nil {
		return fmt.Errorf("set single failed: %w", err)
	}

	// 使缓存失效
	if opts.InvalidateCache {
		if err := sm.invalidateCacheForSingle(ctx, data); err != nil {
			logger.FromContext(ctx).Warn("cache.invalidate_failed",
				"table", sm.TableName, "err", err)
		}
	}

	return nil
}

// Update 更新单个数据
func (sm *ServiceManager[T]) Update(
	ctx context.Context,
	updates map[string]interface{},
	queryFunc func(*gorm.DB) *gorm.DB,
) error {
	ctx, cancel := util.EnsureTimeout(ctx, util.GetDefaultDBTimeout())
	defer cancel()
	return sm.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		tx = sm.applyTableName(tx)

		if queryFunc != nil {
			tx = queryFunc(tx)
		}

		return tx.Model(&sm.Resource).Updates(updates).Error
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
}

// Save 保存单个数据（GORM 的 Save 方法，会保存所有字段）
func (sm *ServiceManager[T]) Save(ctx context.Context, data *T) error {
	ctx, cancel := util.EnsureTimeout(ctx, util.GetDefaultDBTimeout())
	defer cancel()
	return sm.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		tx = sm.applyTableName(tx)
		return tx.Save(data).Error
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
}

// Upsert 单个 Upsert 操作（插入或更新）
func (sm *ServiceManager[T]) Upsert(
	ctx context.Context,
	data *T,
	conflictColumns []string,
	updateColumns []string,
) error {
	ctx, cancel := util.EnsureTimeout(ctx, util.GetDefaultDBTimeout())
	defer cancel()
	return sm.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		tx = sm.applyTableName(tx)

		onConflict := clause.OnConflict{}
		for _, col := range conflictColumns {
			onConflict.Columns = append(onConflict.Columns, clause.Column{Name: col})
		}

		if len(updateColumns) > 0 {
			onConflict.DoUpdates = clause.AssignmentColumns(updateColumns)
		} else {
			onConflict.UpdateAll = true
		}

		return tx.Clauses(onConflict).Create(data).Error
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
}

// Delete 删除单个数据
func (sm *ServiceManager[T]) Delete(
	ctx context.Context,
	queryFunc func(*gorm.DB) *gorm.DB,
) error {
	ctx, cancel := util.EnsureTimeout(ctx, util.GetDefaultDBTimeout())
	defer cancel()
	return sm.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		tx = sm.applyTableName(tx)

		if queryFunc != nil {
			tx = queryFunc(tx)
		}

		return tx.Delete(&sm.Resource).Error
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
}

// Increment 增加字段值
func (sm *ServiceManager[T]) Increment(
	ctx context.Context,
	column string,
	value interface{},
	queryFunc func(*gorm.DB) *gorm.DB,
) error {
	ctx, cancel := util.EnsureTimeout(ctx, util.GetDefaultDBTimeout())
	defer cancel()
	return sm.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		tx = sm.applyTableName(tx)

		if queryFunc != nil {
			tx = queryFunc(tx)
		}

		return tx.Model(&sm.Resource).UpdateColumn(column, gorm.Expr(fmt.Sprintf("%s + ?", column), value)).Error
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
}

// Decrement 减少字段值
func (sm *ServiceManager[T]) Decrement(
	ctx context.Context,
	column string,
	value interface{},
	queryFunc func(*gorm.DB) *gorm.DB,
) error {
	ctx, cancel := util.EnsureTimeout(ctx, util.GetDefaultDBTimeout())
	defer cancel()
	return sm.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		tx = sm.applyTableName(tx)

		if queryFunc != nil {
			tx = queryFunc(tx)
		}

		return tx.Model(&sm.Resource).UpdateColumn(column, gorm.Expr(fmt.Sprintf("%s - ?", column), value)).Error
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
}

// --- 封装方法（逻辑不变，直接调用上述重构后的方法） ---

func (sm *ServiceManager[T]) Insert(ctx context.Context, data *T) error {
	return sm.SetSingle(ctx, data, &SetSingleOptions{OnConflictUpdate: false, InvalidateCache: false})
}

func (sm *ServiceManager[T]) UpdateByID(ctx context.Context, id interface{}, updates map[string]interface{}) error {
	return sm.Update(ctx, updates, func(db *gorm.DB) *gorm.DB {
		return db.Where("id = ?", id)
	})
}

func (sm *ServiceManager[T]) DeleteByID(ctx context.Context, id interface{}) error {
	return sm.Delete(ctx, func(db *gorm.DB) *gorm.DB {
		return db.Where("id = ?", id)
	})
}

func (sm *ServiceManager[T]) SoftDelete(ctx context.Context, queryFunc func(*gorm.DB) *gorm.DB) error {
	updates := map[string]interface{}{"deleted_at": gorm.Expr("NOW()")}
	return sm.Update(ctx, updates, queryFunc)
}

func (sm *ServiceManager[T]) SoftDeleteByID(ctx context.Context, id interface{}) error {
	return sm.SoftDelete(ctx, func(db *gorm.DB) *gorm.DB {
		return db.Where("id = ?", id)
	})
}

func (sm *ServiceManager[T]) IncrementByID(ctx context.Context, id interface{}, column string, value interface{}) error {
	return sm.Increment(ctx, column, value, func(db *gorm.DB) *gorm.DB {
		return db.Where("id = ?", id)
	})
}

func (sm *ServiceManager[T]) DecrementByID(ctx context.Context, id interface{}, column string, value interface{}) error {
	return sm.Decrement(ctx, column, value, func(db *gorm.DB) *gorm.DB {
		return db.Where("id = ?", id)
	})
}

func (sm *ServiceManager[T]) invalidateCacheForSingle(ctx context.Context, data *T) error {
	// 留给具体业务实现
	return nil
}
