package service

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/Super-Gagaga/abstract-manager/util"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// DBManager 数据库管理器
type DBManager struct {
	DB *gorm.DB
}

var globalDBManager *DBManager

// InitDB 初始化数据库连接
func InitDB() (*DBManager, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		os.Getenv("DB_USER"),
		os.Getenv("DB_PASSWORD"),
		os.Getenv("DB_HOST"),
		os.Getenv("DB_PORT"),
		os.Getenv("DB_NAME"),
	)

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		// SQL 日志桥接到 slog(LOG_LEVEL 控制,DB_SLOW_QUERY_MS 控制慢查询阈值)
		Logger: NewSlogGormLogger(
			time.Duration(util.GetEnvIntOrDefault("DB_SLOW_QUERY_MS", 500)) * time.Millisecond,
		),
		// 准备语句执行，提高性能
		PrepareStmt: true,
		// 命名策略
		NamingStrategy: nil,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to connect database: %w", err)
	}

	// 获取底层的 *sql.DB
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get sql.DB: %w", err)
	}

	// 设置连接池参数
	sqlDB.SetMaxOpenConns(100)          // 最大打开连接数
	sqlDB.SetMaxIdleConns(10)           // 最大空闲连接数
	sqlDB.SetConnMaxLifetime(time.Hour) // 连接最大生命周期

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := sqlDB.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	globalDBManager = &DBManager{DB: db}
	return globalDBManager, nil
}

// GetDB 获取全局数据库实例
func GetDB() *gorm.DB {
	if globalDBManager == nil {
		panic("database not initialized, call InitDB first")
	}
	return globalDBManager.DB
}

// Close 关闭数据库连接
func (dm *DBManager) Close() error {
	sqlDB, err := dm.DB.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
