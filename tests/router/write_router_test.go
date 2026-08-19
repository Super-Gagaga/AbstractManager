package router

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Super-Gagaga/abstract-manager/tests/testutil"
)

const base = "/api/v1/users"

func alice() testutil.TestUser {
	return testutil.TestUser{ID: 1, Username: "alice", Email: "alice@example.com", Age: 21, Status: "active"}
}

// ==================== 参数校验(不触达 DB) ====================

func TestWriteRouter_Validation(t *testing.T) {
	tests := []struct {
		name    string
		method  string
		path    string
		body    interface{}
		wantMsg string
	}{
		{"set: nil data", "POST", base + "/set", map[string]interface{}{}, "data cannot be nil"},
		{"update: empty updates", "PUT", base + "/update", map[string]interface{}{"id": 1}, "updates cannot be empty"},
		{"delete: nil id", "DELETE", base + "/delete", map[string]interface{}{}, "id cannot be nil"},
		{"upsert: no conflict columns", "POST", base + "/upsert", map[string]interface{}{"data": alice()}, "conflict_columns cannot be empty"},
		{"increment: empty column", "POST", base + "/increment", map[string]interface{}{"id": 1, "value": 1}, "column cannot be empty"},
		{"increment: nil id", "POST", base + "/increment", map[string]interface{}{"column": "age", "value": 1}, "id cannot be nil"},
		{"batch/set: empty data", "POST", base + "/batch/set", map[string]interface{}{}, "data cannot be empty"},
		{"batch/insert: empty data", "POST", base + "/batch/insert", map[string]interface{}{}, "data cannot be empty"},
		{"batch/update: empty updates", "PUT", base + "/batch/update", map[string]interface{}{}, "updates cannot be empty"},
		{"batch/delete: empty ids", "DELETE", base + "/batch/delete", map[string]interface{}{}, "ids cannot be empty"},
		{"batch/upsert: no conflict columns", "POST", base + "/batch/upsert", map[string]interface{}{"data": []testutil.TestUser{alice()}}, "conflict_columns cannot be empty"},
		{"batch/increment: empty column", "POST", base + "/batch/increment", map[string]interface{}{"ids": []int{1}, "value": 1}, "column cannot be empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, resp := doJSON(t, newWriteRouter(), tt.method, tt.path, tt.body)
			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.EqualValues(t, 400, resp["code"])
			assert.Contains(t, resp["message"], tt.wantMsg)
		})
	}
}

func TestWriteRouter_InvalidJSON(t *testing.T) {
	r := newWriteRouter()
	w := postRaw(t, r, base+"/set", `{not json`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ==================== 成功路径(sqlmock) ====================

func TestWriteRouter_SetSingle_Success(t *testing.T) {
	mock := injectDB(t)
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	w, resp := doJSON(t, newWriteRouter(), "POST", base+"/set", map[string]interface{}{
		"data":               alice(),
		"on_conflict_update": true,
		"invalidate_cache":   false,
	})
	require.Equal(t, http.StatusOK, w.Code)
	assert.EqualValues(t, 0, resp["code"])
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestWriteRouter_SetSingle_DBError(t *testing.T) {
	mock := injectDB(t)
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO").WillReturnError(errors.New("boom"))
	mock.ExpectRollback()

	w, resp := doJSON(t, newWriteRouter(), "POST", base+"/set", map[string]interface{}{
		"data": alice(), "invalidate_cache": false,
	})
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.EqualValues(t, 500, resp["code"])
	assert.Contains(t, resp["message"], "set single failed")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestWriteRouter_UpdateByID_Success(t *testing.T) {
	mock := injectDB(t)
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	w, resp := doJSON(t, newWriteRouter(), "PUT", base+"/update", map[string]interface{}{
		"id":      1,
		"updates": map[string]interface{}{"age": 22},
	})
	require.Equal(t, http.StatusOK, w.Code)
	assert.EqualValues(t, 0, resp["code"])
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestWriteRouter_DeleteByID_Success(t *testing.T) {
	mock := injectDB(t)
	mock.ExpectBegin()
	mock.ExpectExec("DELETE").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	w, resp := doJSON(t, newWriteRouter(), "DELETE", base+"/delete", map[string]interface{}{"id": 1})
	require.Equal(t, http.StatusOK, w.Code)
	assert.EqualValues(t, 0, resp["code"])
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestWriteRouter_Upsert_Success(t *testing.T) {
	mock := injectDB(t)
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	w, resp := doJSON(t, newWriteRouter(), "POST", base+"/upsert", map[string]interface{}{
		"data":             alice(),
		"conflict_columns": []string{"id"},
	})
	require.Equal(t, http.StatusOK, w.Code)
	assert.EqualValues(t, 0, resp["code"])
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestWriteRouter_Increment_Success(t *testing.T) {
	mock := injectDB(t)
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	w, resp := doJSON(t, newWriteRouter(), "POST", base+"/increment", map[string]interface{}{
		"id": 1, "column": "age", "value": 1,
	})
	require.Equal(t, http.StatusOK, w.Code)
	assert.EqualValues(t, 0, resp["code"])
	assert.NoError(t, mock.ExpectationsWereMet())
}

// SEC: 恶意列名在触达数据库前被 service 层校验拦截(无需 DB 期望)
func TestWriteRouter_Increment_MaliciousColumn(t *testing.T) {
	w, resp := doJSON(t, newWriteRouter(), "POST", base+"/increment", map[string]interface{}{
		"id": 1, "column": "age; DROP TABLE users;--", "value": 1,
	})
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, resp["message"], "invalid column name")
}

func TestWriteRouter_BatchSet_Success(t *testing.T) {
	mock := injectDB(t)
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO").WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	w, resp := doJSON(t, newWriteRouter(), "POST", base+"/batch/set", map[string]interface{}{
		"data":             []testutil.TestUser{alice(), {ID: 2, Username: "bob"}},
		"invalidate_cache": false,
	})
	require.Equal(t, http.StatusOK, w.Code)
	assert.EqualValues(t, 0, resp["code"])
	assert.EqualValues(t, 2, resp["rows_affected"])
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestWriteRouter_BatchDelete_Success(t *testing.T) {
	mock := injectDB(t)
	mock.ExpectBegin()
	mock.ExpectExec("DELETE").WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	w, resp := doJSON(t, newWriteRouter(), "DELETE", base+"/batch/delete", map[string]interface{}{
		"ids": []int{1, 2},
	})
	require.Equal(t, http.StatusOK, w.Code)
	assert.EqualValues(t, 2, resp["rows_affected"])
	assert.NoError(t, mock.ExpectationsWereMet())
}

// ==================== 辅助 ====================

// postRaw issues a request with a raw (possibly malformed) string body.
func postRaw(t *testing.T, r *gin.Engine, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}
