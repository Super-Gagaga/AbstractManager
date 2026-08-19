package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/Super-Gagaga/abstract-manager/util"

	"gorm.io/gorm"
)

// CreateOptions 创建表的配置选项
type CreateOptions struct {
	IfNotExists  bool // 如果表已存在则跳过
	DropIfExists bool // 如果表存在则删除后重建
}

// Create 创建数据表
func (sm *ServiceManager[T]) Create(ctx context.Context, opts *CreateOptions) error {
	ctx, cancel := util.EnsureTimeout(ctx, util.GetDefaultDDLTimeout())
	defer cancel()
	db := sm.DB().WithContext(ctx)

	if opts == nil {
		opts = &CreateOptions{IfNotExists: true}
	}

	// 设置表名和 schema
	if sm.Schema != "" && sm.Schema != "public" {
		db = db.Table(fmt.Sprintf("%s.%s", sm.Schema, sm.TableName))
	} else {
		db = db.Table(sm.TableName)
	}

	// 如果需要删除已存在的表
	if opts.DropIfExists {
		if err := db.Migrator().DropTable(&sm.Resource); err != nil {
			return fmt.Errorf("failed to drop table %s: %w", sm.TableName, err)
		}
	}

	// 创建表
	if opts.IfNotExists {
		if db.Migrator().HasTable(&sm.Resource) {
			return nil // 表已存在，跳过
		}
	}

	if err := db.AutoMigrate(&sm.Resource); err != nil {
		return fmt.Errorf("failed to create table %s: %w", sm.TableName, err)
	}

	return nil
}

// CreateWithIndexes 创建数据表并添加索引
func (sm *ServiceManager[T]) CreateWithIndexes(ctx context.Context, opts *CreateOptions, indexes []Index) error {
	// 先创建表
	if err := sm.Create(ctx, opts); err != nil {
		return err
	}

	db := sm.DB().WithContext(ctx)

	// 添加索引
	for _, idx := range indexes {
		if err := sm.createIndex(db, idx); err != nil {
			return fmt.Errorf("failed to create index %s: %w", idx.Name, err)
		}
	}

	return nil
}

// Index 索引定义
type Index struct {
	Name    string   // 索引名称
	Columns []string // 索引字段
	Unique  bool     // 是否唯一索引
}

// createIndex 创建索引
func (sm *ServiceManager[T]) createIndex(db *gorm.DB, idx Index) error {
	tableName := sm.TableName
	if sm.Schema != "" && sm.Schema != "public" {
		tableName = fmt.Sprintf("%s.%s", sm.Schema, sm.TableName)
	}

	columns := strings.Join(idx.Columns, ", ")

	if idx.Unique {
		sql := fmt.Sprintf("CREATE UNIQUE INDEX %s ON %s (%s)", idx.Name, tableName, columns)
		return db.Exec(sql).Error
	}
	sql := fmt.Sprintf("CREATE INDEX %s ON %s (%s)", idx.Name, tableName, columns)
	return db.Exec(sql).Error
}

// DropTable 删除数据表
func (sm *ServiceManager[T]) DropTable(ctx context.Context) error {
	ctx, cancel := util.EnsureTimeout(ctx, util.GetDefaultDDLTimeout())
	defer cancel()
	db := sm.DB().WithContext(ctx)

	tableName := sm.TableName
	if sm.Schema != "" && sm.Schema != "public" {
		tableName = fmt.Sprintf("%s.%s", sm.Schema, sm.TableName)
	}

	if err := db.Table(tableName).Migrator().DropTable(&sm.Resource); err != nil {
		return fmt.Errorf("failed to drop table %s: %w", tableName, err)
	}

	return nil
}

// HasTable 检查表是否存在
func (sm *ServiceManager[T]) HasTable(ctx context.Context) (bool, error) {
	ctx, cancel := util.EnsureTimeout(ctx, util.GetDefaultDBTimeout())
	defer cancel()
	db := sm.DB().WithContext(ctx)

	tableName := sm.TableName
	if sm.Schema != "" && sm.Schema != "public" {
		tableName = fmt.Sprintf("%s.%s", sm.Schema, sm.TableName)
	}

	return db.Table(tableName).Migrator().HasTable(&sm.Resource), nil
}
