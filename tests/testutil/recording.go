// 捕获 slog 记录的测试夹具,用于对日志输出做断言。
package testutil

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/Super-Gagaga/abstract-manager/util/logger"
)

// RecordingHandler 捕获所有写入的 slog 记录(不过滤级别)。
// WithAttrs 产生的属性(如 request_id)会附加到后续每条记录。
type RecordingHandler struct {
	store *recordStore
	attrs []slog.Attr
}

type recordStore struct {
	mu      sync.Mutex
	records []slog.Record
}

func NewRecordingHandler() *RecordingHandler {
	return &RecordingHandler{store: &recordStore{}}
}

func (h *RecordingHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (h *RecordingHandler) Handle(_ context.Context, r slog.Record) error {
	clone := r.Clone()
	if len(h.attrs) > 0 {
		clone.AddAttrs(h.attrs...)
	}
	h.store.mu.Lock()
	defer h.store.mu.Unlock()
	h.store.records = append(h.store.records, clone)
	return nil
}

func (h *RecordingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	merged := make([]slog.Attr, 0, len(h.attrs)+len(attrs))
	merged = append(merged, h.attrs...)
	merged = append(merged, attrs...)
	return &RecordingHandler{store: h.store, attrs: merged}
}

func (h *RecordingHandler) WithGroup(string) slog.Handler { return h }

// Records 返回已捕获记录的副本
func (h *RecordingHandler) Records() []slog.Record {
	h.store.mu.Lock()
	defer h.store.mu.Unlock()
	out := make([]slog.Record, len(h.store.records))
	copy(out, h.store.records)
	return out
}

// Find 返回第一条消息匹配的记录
func (h *RecordingHandler) Find(msg string) (slog.Record, bool) {
	for _, r := range h.Records() {
		if r.Message == msg {
			return r, true
		}
	}
	return slog.Record{}, false
}

// Attr 在记录中查找指定属性,不存在时返回零值
func Attr(r slog.Record, key string) (slog.Value, bool) {
	var found slog.Value
	ok := false
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == key {
			found = a.Value
			ok = true
			return false
		}
		return true
	})
	return found, ok
}

// InstallCaptureLogger 把 util/logger 切换为捕获 handler,测试结束后恢复默认
func InstallCaptureLogger(t *testing.T) *RecordingHandler {
	t.Helper()
	h := NewRecordingHandler()
	logger.SetLogger(slog.New(h))
	t.Cleanup(func() { logger.SetLogger(nil) })
	return h
}
