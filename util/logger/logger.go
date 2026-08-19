// Package logger 提供 AbstractManager 的统一日志入口。
//
// 框架内部不直接打印,统一经由本包上报;宿主应用可通过 SetLogger
// 注入自己的 *slog.Logger(默认按环境变量 LOG_LEVEL / LOG_FORMAT 构造)。
package logger

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

type requestIDKey struct{}

var active atomic.Pointer[slog.Logger]

func init() {
	active.Store(NewDefault())
}

// NewDefault 根据环境变量构造默认 logger:
//   - LOG_LEVEL=debug|info|warn|error,未设置时默认 info
//   - LOG_FORMAT=text|json,未设置时默认 text
//   - 测试进程(testing.Testing)未显式设置 LOG_LEVEL 时降级为
//     warn 且丢弃输出,保证测试静默
func NewDefault() *slog.Logger {
	level := parseLevel(os.Getenv("LOG_LEVEL"))
	out := io.Writer(os.Stderr)
	format := strings.ToLower(strings.TrimSpace(os.Getenv("LOG_FORMAT")))

	if level == nil {
		if testing.Testing() {
			lvl := slog.LevelWarn
			level = &lvl
			out = io.Discard
		} else {
			lvl := slog.LevelInfo
			level = &lvl
		}
	}

	opts := &slog.HandlerOptions{Level: *level}
	if format == "json" {
		return slog.New(slog.NewJSONHandler(out, opts))
	}
	return slog.New(slog.NewTextHandler(out, opts))
}

func parseLevel(s string) *slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		lvl := slog.LevelDebug
		return &lvl
	case "info":
		lvl := slog.LevelInfo
		return &lvl
	case "warn", "warning":
		lvl := slog.LevelWarn
		return &lvl
	case "error":
		lvl := slog.LevelError
		return &lvl
	default:
		return nil
	}
}

// SetLogger 注入宿主 logger;传 nil 恢复默认。并发安全。
func SetLogger(l *slog.Logger) {
	if l == nil {
		l = NewDefault()
	}
	active.Store(l)
}

// L 返回当前生效的 logger,是框架内部日志的唯一入口。
func L() *slog.Logger {
	return active.Load()
}

// WithRequestID 把请求 ID 附加到 context,供 FromContext 提取。
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

// GetRequestID 从 context 读取请求 ID,不存在时返回空串。
func GetRequestID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(requestIDKey{}).(string); ok {
		return v
	}
	return ""
}

// FromContext 返回自动携带 request_id 字段的 logger;
// context 中没有请求 ID 时等价于 L()。
func FromContext(ctx context.Context) *slog.Logger {
	l := L()
	if id := GetRequestID(ctx); id != "" {
		return l.With(slog.String("request_id", id))
	}
	return l
}
