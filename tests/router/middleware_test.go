package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Super-Gagaga/abstract-manager/http_router"
	"github.com/Super-Gagaga/abstract-manager/tests/testutil"
	"github.com/Super-Gagaga/abstract-manager/util/logger"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// probeEngine 构造带 RequestLogger 的引擎,handler 把收到的请求 ID 回显
func probeEngine(extra ...gin.HandlerFunc) *gin.Engine {
	r := gin.New()
	r.Use(append([]gin.HandlerFunc{http_router.RequestLogger()}, extra...)...)
	r.GET("/probe", func(c *gin.Context) {
		c.String(http.StatusOK, logger.GetRequestID(c.Request.Context()))
	})
	return r
}

func TestRequestLogger_GeneratesID(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/probe", nil)
	probeEngine().ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	id := w.Body.String()
	assert.Len(t, id, 16, "generated ID should be 16-char hex")
	assert.Equal(t, id, w.Header().Get(http_router.RequestIDHeader), "response should echo the ID")
}

func TestRequestLogger_PassthroughUpstreamID(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/probe", nil)
	req.Header.Set(http_router.RequestIDHeader, "upstream-42")
	probeEngine().ServeHTTP(w, req)

	assert.Equal(t, "upstream-42", w.Body.String(), "upstream ID should be reused")
	assert.Equal(t, "upstream-42", w.Header().Get(http_router.RequestIDHeader))
}

func TestRequestLogger_AccessLog(t *testing.T) {
	h := testutil.InstallCaptureLogger(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/probe", nil)
	probeEngine().ServeHTTP(w, req)

	rec, ok := h.Find("http.request")
	require.True(t, ok, "access log should be emitted")
	for key, want := range map[string]interface{}{
		"method": "GET", "path": "/probe", "status": int64(200),
	} {
		v, ok := testutil.Attr(rec, key)
		require.True(t, ok, "attr %s missing", key)
		assert.EqualValues(t, want, v.Any())
	}
	_, ok = testutil.Attr(rec, "request_id")
	assert.True(t, ok, "access log should carry request_id")
}

func TestRecovery_PanicBecomes500(t *testing.T) {
	h := testutil.InstallCaptureLogger(t)

	r := http_router.New()
	r.GET("/boom", func(c *gin.Context) { panic("kaboom") })

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/boom", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	rec, ok := h.Find("http.panic")
	require.True(t, ok, "panic should be logged")
	v, ok := testutil.Attr(rec, "request_id")
	require.True(t, ok)
	assert.Len(t, v.String(), 16, "panic log should carry the request ID")
}
