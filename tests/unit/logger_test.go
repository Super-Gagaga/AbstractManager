package unit

import (
	"context"
	"log/slog"
	"testing"

	"github.com/Super-Gagaga/abstract-manager/tests/testutil"
	"github.com/Super-Gagaga/abstract-manager/util"
	"github.com/Super-Gagaga/abstract-manager/util/logger"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDefault_ExplicitLevel(t *testing.T) {
	t.Setenv("LOG_LEVEL", "debug")
	l := logger.NewDefault()
	assert.True(t, l.Enabled(context.Background(), slog.LevelDebug))
}

func TestNewDefault_TestProcessQuietByDefault(t *testing.T) {
	t.Setenv("LOG_LEVEL", "")
	l := logger.NewDefault()
	// 测试进程未显式配置:warn 生效、info 静默
	assert.True(t, l.Enabled(context.Background(), slog.LevelWarn))
	assert.False(t, l.Enabled(context.Background(), slog.LevelInfo))
}

func TestSetLogger_InjectAndReset(t *testing.T) {
	custom := slog.New(slog.NewTextHandler(&nullWriter{}, nil))
	logger.SetLogger(custom)
	t.Cleanup(func() { logger.SetLogger(nil) })
	assert.Same(t, custom, logger.L())

	logger.SetLogger(nil)
	assert.NotSame(t, custom, logger.L())
}

type nullWriter struct{}

func (nullWriter) Write(p []byte) (int, error) { return len(p), nil }

func TestRequestID_ContextRoundtrip(t *testing.T) {
	ctx := context.Background()
	assert.Empty(t, logger.GetRequestID(ctx))

	ctx = logger.WithRequestID(ctx, "req-42")
	assert.Equal(t, "req-42", logger.GetRequestID(ctx))
}

func TestFromContext_AttachesRequestID(t *testing.T) {
	h := testutil.InstallCaptureLogger(t)
	ctx := logger.WithRequestID(context.Background(), "req-42")

	logger.FromContext(ctx).Warn("some.event", "key", "user:1")

	rec, ok := h.Find("some.event")
	require.True(t, ok, "event should be logged")
	v, ok := testutil.Attr(rec, "request_id")
	require.True(t, ok, "request_id attr should be present")
	assert.Equal(t, "req-42", v.String())
}

func TestFromContext_NoRequestID(t *testing.T) {
	h := testutil.InstallCaptureLogger(t)

	logger.FromContext(context.Background()).Warn("plain.event")

	rec, ok := h.Find("plain.event")
	require.True(t, ok)
	_, hasAttr := testutil.Attr(rec, "request_id")
	assert.False(t, hasAttr, "no request_id attr when context has none")
}

func TestGetEnvIntOrDefault(t *testing.T) {
	t.Setenv("SOME_INT", "42")
	assert.Equal(t, 42, util.GetEnvIntOrDefault("SOME_INT", 7))

	t.Setenv("SOME_INT", "not-a-number")
	assert.Equal(t, 7, util.GetEnvIntOrDefault("SOME_INT", 7))

	assert.Equal(t, 7, util.GetEnvIntOrDefault("SOME_INT_UNSET", 7))
}
