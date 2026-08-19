package http_router

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/Super-Gagaga/abstract-manager/util/logger"

	"github.com/gin-gonic/gin"
)

// RequestIDHeader 请求 ID 的透传/响应头
const RequestIDHeader = "X-Request-ID"

// RequestLogger 请求日志中间件:
//  1. 优先透传上游传入的 X-Request-ID,否则生成 16 位随机 ID
//  2. 把请求 ID 写入 context,后续 service 层日志自动携带 request_id 字段
//  3. 请求结束时输出一条结构化访问日志(http.request)
//
// 使用方式:r.Use(http_router.RequestLogger())
func RequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(RequestIDHeader)
		if id == "" {
			id = newRequestID()
		}
		c.Header(RequestIDHeader, id)
		ctx := logger.WithRequestID(c.Request.Context(), id)
		c.Request = c.Request.WithContext(ctx)

		start := time.Now()
		c.Next()

		logger.FromContext(ctx).Info("http.request",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"latency_ms", float64(time.Since(start).Microseconds())/1000,
			"bytes", c.Writer.Size(),
		)
	}
}

// Recovery panic 恢复中间件,输出到 slog(替代 gin.Recovery),
// panic 日志携带请求 ID 与堆栈。
func Recovery() gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, rec interface{}) {
		logger.FromContext(c.Request.Context()).Error("http.panic",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"panic", rec,
			"stack", string(debug.Stack()),
		)
		c.AbortWithStatus(http.StatusInternalServerError)
	})
}

// New 返回装配了请求日志与 panic 恢复的 gin 引擎(替代 gin.Default())。
// 与 gin.Default 的区别:访问日志走 slog 结构化输出,并注入请求 ID。
func New() *gin.Engine {
	r := gin.New()
	r.Use(RequestLogger(), Recovery())
	return r
}

func newRequestID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// 随机源不可用时退化为时间戳,保证 ID 总有值
		return time.Now().UTC().Format("20060102150405.000000000")
	}
	return hex.EncodeToString(b[:])
}
