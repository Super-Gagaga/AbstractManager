package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/Super-Gagaga/abstract-manager/util/logger"

	gormlogger "gorm.io/gorm/logger"
)

const (
	// DefaultSlowQueryThreshold 慢查询默认阈值(毫秒)
	DefaultSlowQueryThreshold = 500 * time.Millisecond

	// maxSQLLogLen 单条日志中 SQL 的最大长度,超长截断(避免大 IN 列表刷屏)
	maxSQLLogLen = 2000
)

// dsnPasswordPattern 匹配 DSN 中 "user:password@tcp(...)" 形式的密码段,
// 用于清洗驱动报错中可能内嵌的连接串
var dsnPasswordPattern = regexp.MustCompile(`:[^:@/\s]+@`)

// redactDSN 清理文本中可能泄露的 DSN 密码
func redactDSN(s string) string {
	return dsnPasswordPattern.ReplaceAllString(s, ":***@")
}

// slogGormLogger 将 GORM 日志桥接到 util/logger(slog):
//   - SQL 错误     → Error  event=db.error
//   - 慢查询       → Warn   event=db.slow_query(阈值由 NewSlogGormLogger 传入)
//   - 常规 SQL     → Debug  event=db.sql(默认级别下不输出)
//   - 记录不存在   → Debug(上层已按业务语义处理,如 404)
//
// 级别过滤完全交给 slog(LOG_LEVEL 控制),LogMode 为 no-op。
type slogGormLogger struct {
	slowThreshold time.Duration
}

// NewSlogGormLogger 创建桥接到 slog 的 GORM 日志器
func NewSlogGormLogger(slowThreshold time.Duration) gormlogger.Interface {
	return &slogGormLogger{slowThreshold: slowThreshold}
}

// LogMode no-op:级别由 slog 的 LOG_LEVEL 统一控制
func (s *slogGormLogger) LogMode(_ gormlogger.LogLevel) gormlogger.Interface {
	return s
}

func (s *slogGormLogger) Info(ctx context.Context, msg string, args ...interface{}) {
	logger.FromContext(ctx).Debug(fmt.Sprintf(msg, args...))
}

func (s *slogGormLogger) Warn(ctx context.Context, msg string, args ...interface{}) {
	logger.FromContext(ctx).Warn(fmt.Sprintf(msg, args...))
}

func (s *slogGormLogger) Error(ctx context.Context, msg string, args ...interface{}) {
	logger.FromContext(ctx).Error(fmt.Sprintf(msg, args...))
}

func (s *slogGormLogger) Trace(
	ctx context.Context,
	begin time.Time,
	fc func() (sql string, rowsAffected int64),
	err error,
) {
	elapsed := time.Since(begin)
	l := logger.FromContext(ctx)

	switch {
	case err != nil && !errors.Is(err, gormlogger.ErrRecordNotFound):
		sql, rows := fc()
		l.Error("db.error",
			"sql", truncateSQL(sql),
			"rows", rows,
			"elapsed_ms", float64(elapsed.Microseconds())/1000,
			"err", redactDSN(err.Error()),
		)
	case errors.Is(err, gormlogger.ErrRecordNotFound):
		sql, _ := fc()
		l.Debug("db.not_found",
			"sql", truncateSQL(sql),
			"elapsed_ms", float64(elapsed.Microseconds())/1000,
		)
	case s.slowThreshold > 0 && elapsed > s.slowThreshold:
		sql, rows := fc()
		l.Warn("db.slow_query",
			"sql", truncateSQL(sql),
			"rows", rows,
			"elapsed_ms", float64(elapsed.Microseconds())/1000,
			"threshold_ms", float64(s.slowThreshold.Microseconds())/1000,
		)
	default:
		sql, rows := fc()
		l.Debug("db.sql",
			"sql", truncateSQL(sql),
			"rows", rows,
			"elapsed_ms", float64(elapsed.Microseconds())/1000,
		)
	}
}

func truncateSQL(sql string) string {
	if len(sql) <= maxSQLLogLen {
		return sql
	}
	return sql[:maxSQLLogLen] + "...(truncated)"
}
