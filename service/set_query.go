package service

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Super-Gagaga/abstract-manager/util"
	"github.com/Super-Gagaga/abstract-manager/util/filter_translator"
	"github.com/Super-Gagaga/abstract-manager/util/logger"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SetQueryOptions 批量设置配置选项
type SetQueryOptions struct {
	BatchSize        int  // 批次大小
	OnConflictUpdate bool // 冲突时是否更新
	InvalidateCache  bool // 是否使缓存失效
}

// SetQuery 批量设置数据（新增或修改）
func (sm *ServiceManager[T]) SetQuery(
	ctx context.Context,
	data []T,
	opts *SetQueryOptions,
) error {
	if len(data) == 0 {
		return nil
	}

	// 设置默认选项
	if opts == nil {
		opts = &SetQueryOptions{
			BatchSize:        100,
			OnConflictUpdate: true,
			InvalidateCache:  true,
		}
	}

	// 使用 Transaction 闭包自动管理提交和回滚
	ctx, cancel := util.EnsureTimeout(ctx, util.GetDefaultDBTimeout())
	defer cancel()
	err := GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		tx = sm.applyTableName(tx)

		batchSize := opts.BatchSize
		if batchSize <= 0 {
			batchSize = 100
		}

		for i := 0; i < len(data); i += batchSize {
			end := i + batchSize
			if end > len(data) {
				end = len(data)
			}
			batch := data[i:end]

			if opts.OnConflictUpdate {
				if err := tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&batch).Error; err != nil {
					return err // 触发自动回滚
				}
			} else {
				if err := tx.Create(&batch).Error; err != nil {
					return err
				}
			}
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})

	if err != nil {
		return fmt.Errorf("set query failed: %w", err)
	}

	// 使缓存失效
	if opts.InvalidateCache {
		if err := sm.invalidateCacheForBatch(ctx, data); err != nil {
			logger.FromContext(ctx).Warn("cache.invalidate_failed",
				"table", sm.TableName, "count", len(data), "err", err)
		}
	}

	return nil
}

// BatchUpdate 批量更新数据
func (sm *ServiceManager[T]) BatchUpdate(
	ctx context.Context,
	updates map[string]interface{},
	queryFunc func(*gorm.DB) *gorm.DB,
) (int64, error) {
	ctx, cancel := util.EnsureTimeout(ctx, util.GetDefaultDBTimeout())
	defer cancel()

	var rowsAffected int64
	err := GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		tx = sm.applyTableName(tx)
		if queryFunc != nil {
			tx = queryFunc(tx)
		}

		result := tx.Model(&sm.Resource).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		rowsAffected = result.RowsAffected
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})

	return rowsAffected, err
}

// BatchUpsert 批量 Upsert 操作
func (sm *ServiceManager[T]) BatchUpsert(
	ctx context.Context,
	data []T,
	conflictColumns []string,
	updateColumns []string,
	batchSize int,
) error {
	if len(data) == 0 {
		return nil
	}

	ctx, cancel := util.EnsureTimeout(ctx, util.GetDefaultDBTimeout())
	defer cancel()
	return GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		tx = sm.applyTableName(tx)
		if batchSize <= 0 {
			batchSize = 100
		}

		for i := 0; i < len(data); i += batchSize {
			end := i + batchSize
			if end > len(data) {
				end = len(data)
			}

			onConflict := clause.OnConflict{}
			for _, col := range conflictColumns {
				onConflict.Columns = append(onConflict.Columns, clause.Column{Name: col})
			}
			if len(updateColumns) > 0 {
				onConflict.DoUpdates = clause.AssignmentColumns(updateColumns)
			} else {
				onConflict.UpdateAll = true
			}

			if err := tx.Clauses(onConflict).Create(data[i:end]).Error; err != nil {
				return err
			}
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
}

// BatchDelete 批量删除数据
func (sm *ServiceManager[T]) BatchDelete(
	ctx context.Context,
	queryFunc func(*gorm.DB) *gorm.DB,
) (int64, error) {
	var rowsAffected int64
	ctx, cancel := util.EnsureTimeout(ctx, util.GetDefaultDBTimeout())
	defer cancel()
	err := GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		tx = sm.applyTableName(tx)
		if queryFunc != nil {
			tx = queryFunc(tx)
		}

		result := tx.Delete(&sm.Resource)
		if result.Error != nil {
			return result.Error
		}
		rowsAffected = result.RowsAffected
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})

	return rowsAffected, err
}

// BatchIncrement 批量增加字段值
func (sm *ServiceManager[T]) BatchIncrement(
	ctx context.Context,
	column string,
	value interface{},
	queryFunc func(*gorm.DB) *gorm.DB,
) (int64, error) {
	// 校验列名，防止 SQL 注入
	if err := filter_translator.ValidateSQLIdentifier(column); err != nil {
		return 0, fmt.Errorf("invalid column name %q: %w", column, err)
	}

	var rowsAffected int64
	ctx, cancel := util.EnsureTimeout(ctx, util.GetDefaultDBTimeout())
	defer cancel()
	err := GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		tx = sm.applyTableName(tx)
		if queryFunc != nil {
			tx = queryFunc(tx)
		}

		result := tx.Model(&sm.Resource).UpdateColumn(column, gorm.Expr(fmt.Sprintf("%s + ?", column), value))
		if result.Error != nil {
			return result.Error
		}
		rowsAffected = result.RowsAffected
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})

	return rowsAffected, err
}

// BatchDecrement 批量减少字段值 (复用 Increment 逻辑)
func (sm *ServiceManager[T]) BatchDecrement(
	ctx context.Context,
	column string,
	value interface{},
	queryFunc func(*gorm.DB) *gorm.DB,
) (int64, error) {
	// 校验列名，防止 SQL 注入
	if err := filter_translator.ValidateSQLIdentifier(column); err != nil {
		return 0, fmt.Errorf("invalid column name %q: %w", column, err)
	}

	// 减量可以直接调用加量传入负值，或者保持原样
	var rowsAffected int64
	ctx, cancel := util.EnsureTimeout(ctx, util.GetDefaultDBTimeout())
	defer cancel()
	err := GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		tx = sm.applyTableName(tx)
		if queryFunc != nil {
			tx = queryFunc(tx)
		}

		result := tx.Model(&sm.Resource).UpdateColumn(column, gorm.Expr(fmt.Sprintf("%s - ?", column), value))
		if result.Error != nil {
			return result.Error
		}
		rowsAffected = result.RowsAffected
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})

	return rowsAffected, err
}

// --- 以下为未变动的辅助方法 ---

func (sm *ServiceManager[T]) BatchInsert(ctx context.Context, data []T, batchSize int) error {
	return sm.SetQuery(ctx, data, &SetQueryOptions{BatchSize: batchSize, OnConflictUpdate: false, InvalidateCache: false})
}

func (sm *ServiceManager[T]) BatchSoftDelete(ctx context.Context, queryFunc func(*gorm.DB) *gorm.DB) (int64, error) {
	updates := map[string]interface{}{"deleted_at": gorm.Expr("NOW()")}
	return sm.BatchUpdate(ctx, updates, queryFunc)
}

func (sm *ServiceManager[T]) invalidateCacheForBatch(ctx context.Context, data []T) error {
	pattern := fmt.Sprintf("%s:*", sm.CacheKeyName)
	return sm.InvalidateCacheByPattern(ctx, pattern)
}
