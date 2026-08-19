// Package router holds HTTP-layer tests for the four http_router router
// groups: cache-backed routes run against miniredis, DB-backed routes run
// against a sqlmock ghost database injected via service.SetGlobalDB.
//
// Run with: go test -v ./tests/router/
package router

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/Super-Gagaga/abstract-manager/http_router"
	"github.com/Super-Gagaga/abstract-manager/service"
	"github.com/Super-Gagaga/abstract-manager/tests/testutil"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

var testMR *miniredis.Miniredis

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)

	mr, err := miniredis.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "miniredis.Run failed: %v\n", err)
		os.Exit(1)
	}
	testMR = mr

	host, port, err := net.SplitHostPort(mr.Addr())
	if err != nil {
		fmt.Fprintf(os.Stderr, "SplitHostPort failed: %v\n", err)
		os.Exit(1)
	}
	os.Setenv("REDIS_HOST", host)
	os.Setenv("REDIS_PORT", port)
	os.Setenv("REDIS_PASSWORD", "")

	rm, err := service.InitRedis()
	if err != nil {
		fmt.Fprintf(os.Stderr, "InitRedis failed: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()

	rm.Close()
	mr.Close()
	os.Exit(code)
}

// newSM builds a ServiceManager over the shared test model.
func newSM() *service.ServiceManager[testutil.TestUser] {
	return service.NewServiceManager(testutil.TestUser{})
}

// injectDB swaps the global DB for a fresh sqlmock-backed gorm instance and
// returns the mock so the test can program expectations.
func injectDB(t *testing.T) sqlmock.Sqlmock {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	gormDB, err := gorm.Open(mysql.New(mysql.Config{
		Conn:                      sqlDB,
		SkipInitializeWithVersion: true,
	}), &gorm.Config{
		// 测试中的 gorm 实例同样走 slog 适配器,保证输出可控
		Logger: service.NewSlogGormLogger(0),
	})
	require.NoError(t, err)
	service.SetGlobalDB(gormDB)
	t.Cleanup(func() { sqlDB.Close() })
	return mock
}

func flushRedis() {
	testMR.FlushAll()
}

// seedCache writes raw key→JSON entries straight into miniredis.
func seedCache(data map[string]string) {
	ctx := context.Background()
	rdb := service.GetRedis()
	for k, v := range data {
		if err := rdb.Set(ctx, k, v, 0).Err(); err != nil {
			panic(fmt.Sprintf("seedCache %s: %v", k, err))
		}
	}
}

// doJSON performs a JSON request and decodes the JSON response body.
func doJSON(t *testing.T, r *gin.Engine, method, path string, body interface{}) (*httptest.ResponseRecorder, map[string]interface{}) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w, decodeBody(w)
}

func doGet(t *testing.T, r *gin.Engine, path string) (*httptest.ResponseRecorder, map[string]interface{}) {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w, decodeBody(w)
}

func decodeBody(w *httptest.ResponseRecorder) map[string]interface{} {
	resp := map[string]interface{}{}
	if w.Body.Len() > 0 {
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
	}
	return resp
}

// userRow builds one sqlmock row shaped for testutil.TestUser.
func userRows(rows ...testutil.TestUser) *sqlmock.Rows {
	r := sqlmock.NewRows([]string{"id", "username", "email", "age", "status"})
	for _, u := range rows {
		r.AddRow(u.ID, u.Username, u.Email, u.Age, u.Status)
	}
	return r
}

func countRows(n int64) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"count(*)"}).AddRow(n)
}

// ==================== 路由引擎构造 ====================

func newWriteRouter() *gin.Engine {
	r := gin.New()
	g := r.Group("/api/v1/users")
	http_router.NewWriteRouterGroup(g, newSM()).RegisterRoutes("")
	return r
}

func newQueryRouter() *gin.Engine {
	r := gin.New()
	g := r.Group("/api/v1/users")
	qrg := http_router.NewQueryRouterGroup(g, newSM())
	qrg.RegisterCommonMethods(20)
	qrg.RegisterRoutes("")
	return r
}

func newWritedownRouter() *gin.Engine {
	r := gin.New()
	g := r.Group("/api/v1/users")
	http_router.NewWritedownRouterGroup(g, newSM()).RegisterRoutes("/cache")
	return r
}

func newLookupRouter() *gin.Engine {
	r := gin.New()
	g := r.Group("/api/v1/users")
	lrg := http_router.NewLookupRouterGroup(g, newSM())
	lrg.SetDefaults("user:*", time.Hour)
	lrg.RegisterRoutes("/lookup")
	return r
}
