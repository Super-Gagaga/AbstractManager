package router

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Super-Gagaga/abstract-manager/service"
)

// ==================== WritedownRouterGroup:单条写入 ====================

func TestWritedownRouter_WriteSingle(t *testing.T) {
	flushRedis()
	r := newWritedownRouter()

	w, resp := doJSON(t, r, "POST", base+"/cache/write", map[string]interface{}{
		"key":                "user:1",
		"data":               alice(),
		"expiration_seconds": 60,
	})
	require.Equal(t, http.StatusOK, w.Code)
	assert.EqualValues(t, 0, resp["code"])
	assert.EqualValues(t, 1, resp["items_written"])

	ctx := context.Background()
	val, err := service.GetRedis().Get(ctx, "user:1").Result()
	require.NoError(t, err)
	assert.Contains(t, val, `"username":"alice"`)

	ttl, err := service.GetRedis().TTL(ctx, "user:1").Result()
	require.NoError(t, err)
	assert.Greater(t, ttl, time.Duration(0))
	assert.LessOrEqual(t, ttl, 60*time.Second)
}

func TestWritedownRouter_Write_Validation(t *testing.T) {
	flushRedis()
	r := newWritedownRouter()

	w, resp := doJSON(t, r, "POST", base+"/cache/write", map[string]interface{}{
		"data": alice(),
	})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, resp["message"], "key cannot be empty")

	w, resp = doJSON(t, r, "POST", base+"/cache/write", map[string]interface{}{
		"key": "user:1",
	})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, resp["message"], "either data or id must be provided")
}

func TestWritedownRouter_WriteNX_Conflict(t *testing.T) {
	flushRedis()
	seedCache(map[string]string{"user:1": `{"id":1,"username":"old"}`})
	r := newWritedownRouter()

	w, resp := doJSON(t, r, "POST", base+"/cache/write", map[string]interface{}{
		"key":  "user:1",
		"data": alice(),
		"nx":   true,
	})
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.EqualValues(t, 500, resp["code"])

	// NX 冲突时不应覆盖原值
	val, err := service.GetRedis().Get(context.Background(), "user:1").Result()
	require.NoError(t, err)
	assert.Contains(t, val, "old")
}

func TestWritedownRouter_WriteAsync(t *testing.T) {
	flushRedis()
	r := newWritedownRouter()

	w, resp := doJSON(t, r, "POST", base+"/cache/write", map[string]interface{}{
		"key":   "user:2",
		"data":  alice(),
		"async": true,
	})
	require.Equal(t, http.StatusOK, w.Code)
	assert.EqualValues(t, 0, resp["code"])

	// 异步写入由工作池完成,短暂等待后校验
	require.Eventually(t, func() bool {
		return service.GetRedis().Get(context.Background(), "user:2").Err() == nil
	}, 2*time.Second, 20*time.Millisecond)
}

// ==================== WritedownRouterGroup:版本化写入 ====================

func TestWritedownRouter_WriteVersion(t *testing.T) {
	flushRedis()
	seedCache(map[string]string{
		"user:3":         `{"id":3,"username":"old"}`,
		"user:3:version": "5",
	})
	r := newWritedownRouter()

	// 版本号不大于当前版本 → 冲突
	w, resp := doJSON(t, r, "POST", base+"/cache/write-version", map[string]interface{}{
		"key":     "user:3",
		"data":    map[string]interface{}{"id": 3, "username": "stale"},
		"version": 5,
	})
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, resp["message"], "version outdated")

	// 更高版本 → 成功并覆盖
	w, resp = doJSON(t, r, "POST", base+"/cache/write-version", map[string]interface{}{
		"key":     "user:3",
		"data":    map[string]interface{}{"id": 3, "username": "fresh"},
		"version": 6,
	})
	require.Equal(t, http.StatusOK, w.Code)
	assert.EqualValues(t, 0, resp["code"])

	val, err := service.GetRedis().Get(context.Background(), "user:3").Result()
	require.NoError(t, err)
	assert.Contains(t, val, "fresh")
}

// ==================== WritedownRouterGroup:批量写入 ====================

func TestWritedownRouter_BatchWrite(t *testing.T) {
	flushRedis()
	r := newWritedownRouter()

	w, resp := doJSON(t, r, "POST", base+"/cache/batch-write", map[string]interface{}{
		"key_template": "user:{id}",
		"data": []map[string]interface{}{
			{"id": 1, "username": "alice"},
			{"id": 2, "username": "bob"},
		},
		"expiration_seconds": 60,
	})
	require.Equal(t, http.StatusOK, w.Code)
	assert.EqualValues(t, 2, resp["items_written"])

	ctx := context.Background()
	for _, key := range []string{"user:1", "user:2"} {
		require.NoError(t, service.GetRedis().Get(ctx, key).Err(), key)
	}
}

func TestWritedownRouter_BatchWrite_Validation(t *testing.T) {
	w, resp := doJSON(t, newWritedownRouter(), "POST", base+"/cache/batch-write", map[string]interface{}{
		"data": []map[string]interface{}{{"id": 1}},
	})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, resp["message"], "key_template cannot be empty")
}

// ==================== LookupRouterGroup:批量查询 ====================

func TestLookupRouter_Lookup_WithFilters(t *testing.T) {
	flushRedis()
	seedCache(map[string]string{
		"user:1":         `{"id":1,"username":"alice","age":21,"status":"1"}`,
		"user:2":         `{"id":2,"username":"bob","age":30,"status":"1"}`,
		"user:9:version": "3", // 内部键应被排除
	})
	r := newLookupRouter()

	w, resp := doJSON(t, r, "POST", base+"/lookup/lookup", map[string]interface{}{
		"key_pattern": "user:*",
		"filters": []map[string]interface{}{
			{"field": "age", "operator": "<", "value": 25},
		},
	})
	require.Equal(t, http.StatusOK, w.Code)
	assert.EqualValues(t, 1, resp["count"])
	assert.Equal(t, []interface{}{"user:1"}, resp["keys"])
	data := resp["data"].(map[string]interface{})
	assert.Contains(t, data, "user:1")
}

func TestLookupRouter_Lookup_Empty(t *testing.T) {
	flushRedis()

	w, resp := doJSON(t, newLookupRouter(), "POST", base+"/lookup/lookup", map[string]interface{}{
		"key_pattern": "user:*",
	})
	require.Equal(t, http.StatusOK, w.Code)
	assert.EqualValues(t, 0, resp["count"])
}

func TestLookupRouter_Lookup_FallbackToDB(t *testing.T) {
	flushRedis()
	mock := injectDB(t)
	mock.ExpectQuery("SELECT count").WillReturnRows(countRows(1))
	mock.ExpectQuery("SELECT").WillReturnRows(userRows(alice()))

	w, resp := doJSON(t, newLookupRouter(), "POST", base+"/lookup/lookup", map[string]interface{}{
		"key_pattern": "user:*",
		"filters":     []map[string]interface{}{{"field": "age", "operator": ">=", "value": 18}},
		"fallback_db": true,
	})
	require.Equal(t, http.StatusOK, w.Code)
	assert.EqualValues(t, 1, resp["count"])
	// 回源数据按 "类型名小写:id" 规则回填缓存
	assert.Equal(t, []interface{}{"testuser:1"}, resp["keys"])
	require.NoError(t, service.GetRedis().Get(context.Background(), "testuser:1").Err())
	assert.NoError(t, mock.ExpectationsWereMet())
}

// ==================== LookupRouterGroup:单键 Cache Aside ====================

func TestLookupRouter_GetByKey_CacheHit(t *testing.T) {
	flushRedis()
	seedCache(map[string]string{
		"user:1": `{"id":1,"username":"alice","email":"alice@example.com","age":21,"status":"active"}`,
	})

	w, resp := doGet(t, newLookupRouter(), base+"/lookup/user:1")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, true, resp["cache_hit"])
	assert.Equal(t, "cache", resp["source"])
	data := resp["data"].(map[string]interface{})
	assert.Equal(t, "alice", data["username"])
}

func TestLookupRouter_GetByKey_CacheAside_Miss(t *testing.T) {
	flushRedis()
	mock := injectDB(t)
	mock.ExpectQuery("SELECT count").WillReturnRows(countRows(1))
	mock.ExpectQuery("SELECT").WillReturnRows(userRows(alice()))

	w, resp := doGet(t, newLookupRouter(), base+"/lookup/user:1")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, false, resp["cache_hit"])
	assert.Equal(t, "database", resp["source"])
	data := resp["data"].(map[string]interface{})
	assert.Equal(t, "alice", data["username"])

	// 回填后缓存中应有该键
	require.NoError(t, service.GetRedis().Get(context.Background(), "user:1").Err())
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestLookupRouter_GetByKey_NotFoundAnywhere(t *testing.T) {
	flushRedis()
	mock := injectDB(t)
	mock.ExpectQuery("SELECT count").WillReturnRows(countRows(0))
	mock.ExpectQuery("SELECT").WillReturnRows(userRows())

	w, resp := doGet(t, newLookupRouter(), base+"/lookup/user:999")
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.EqualValues(t, 404, resp["code"])
}

// ==================== LookupRouterGroup:计数与失效 ====================

func TestLookupRouter_Count(t *testing.T) {
	flushRedis()
	seedCache(map[string]string{
		"user:1":         `{"id":1,"age":21}`,
		"user:2":         `{"id":2,"age":30}`,
		"user:3":         `{"id":3,"age":19}`,
		"user:9:version": "3",
	})

	w, resp := doJSON(t, newLookupRouter(), "POST", base+"/lookup/count", map[string]interface{}{
		"key_pattern": "user:*",
		"filters": []map[string]interface{}{
			{"field": "age", "operator": ">=", "value": 21},
		},
	})
	require.Equal(t, http.StatusOK, w.Code)
	assert.EqualValues(t, 2, resp["count"])
}

func TestLookupRouter_Invalidate_Keys(t *testing.T) {
	flushRedis()
	seedCache(map[string]string{
		"user:1": `{"id":1}`,
		"user:2": `{"id":2}`,
	})
	r := newLookupRouter()

	w, resp := doJSON(t, r, "POST", base+"/lookup/invalidate", map[string]interface{}{
		"keys": []string{"user:1"},
	})
	require.Equal(t, http.StatusOK, w.Code)
	assert.EqualValues(t, 1, resp["count"])

	ctx := context.Background()
	assert.Error(t, service.GetRedis().Get(ctx, "user:1").Err())
	assert.NoError(t, service.GetRedis().Get(ctx, "user:2").Err())
}

func TestLookupRouter_Invalidate_Pattern(t *testing.T) {
	flushRedis()
	seedCache(map[string]string{
		"user:1": `{"id":1}`,
		"user:2": `{"id":2}`,
	})

	w, resp := doJSON(t, newLookupRouter(), "POST", base+"/lookup/invalidate", map[string]interface{}{
		"pattern": "user:*",
	})
	require.Equal(t, http.StatusOK, w.Code)
	assert.EqualValues(t, -1, resp["count"])

	keys, err := service.GetRedis().Keys(context.Background(), "user:*").Result()
	require.NoError(t, err)
	assert.Empty(t, keys)
}

func TestLookupRouter_Invalidate_Validation(t *testing.T) {
	w, resp := doJSON(t, newLookupRouter(), "POST", base+"/lookup/invalidate", map[string]interface{}{})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, resp["message"], "either keys or pattern")
}
