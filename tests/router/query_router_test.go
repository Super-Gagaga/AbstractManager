package router

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Super-Gagaga/abstract-manager/tests/testutil"
)

// ==================== 参数校验 ====================

func TestQueryRouter_UnknownMethod(t *testing.T) {
	w, resp := doJSON(t, newQueryRouter(), "POST", base+"/query", map[string]interface{}{
		"method": "nope",
	})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, resp["message"], "unknown method")
}

func TestQueryRouter_InvalidFilter(t *testing.T) {
	w, resp := doJSON(t, newQueryRouter(), "POST", base+"/query", map[string]interface{}{
		"method": "list",
		"filters": []map[string]interface{}{
			{"field": "", "operator": "=", "value": 1},
		},
	})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, resp["message"], "invalid filters")
}

// ==================== 查询路径 ====================

func TestQueryRouter_List_Success(t *testing.T) {
	mock := injectDB(t)
	mock.ExpectQuery("SELECT count").WillReturnRows(countRows(2))
	mock.ExpectQuery("SELECT").WillReturnRows(userRows(alice(), testutil.TestUser{ID: 2, Username: "bob", Age: 30}))

	w, resp := doJSON(t, newQueryRouter(), "POST", base+"/query", map[string]interface{}{
		"method": "list",
		"page":   1,
	})
	require.Equal(t, http.StatusOK, w.Code)
	assert.EqualValues(t, 0, resp["code"])
	assert.EqualValues(t, 2, resp["total"])
	data, ok := resp["data"].([]interface{})
	require.True(t, ok, "data should be an array")
	assert.Len(t, data, 2)
	assert.EqualValues(t, 1, resp["page"])
	assert.EqualValues(t, 20, resp["page_size"])
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestQueryRouter_List_WithFilters(t *testing.T) {
	mock := injectDB(t)
	mock.ExpectQuery("SELECT count").WillReturnRows(countRows(1))
	mock.ExpectQuery("SELECT").WillReturnRows(userRows(alice()))

	w, resp := doJSON(t, newQueryRouter(), "POST", base+"/query", map[string]interface{}{
		"method": "list",
		"filters": []map[string]interface{}{
			{"field": "age", "operator": ">=", "value": 21},
		},
	})
	require.Equal(t, http.StatusOK, w.Code)
	assert.EqualValues(t, 1, resp["total"])
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestQueryRouter_List_DBError(t *testing.T) {
	mock := injectDB(t)
	mock.ExpectQuery("SELECT count").WillReturnError(errors.New("db down"))

	w, resp := doJSON(t, newQueryRouter(), "POST", base+"/query", map[string]interface{}{
		"method": "list",
	})
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.EqualValues(t, 500, resp["code"])
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestQueryRouter_GetByID_Found(t *testing.T) {
	mock := injectDB(t)
	mock.ExpectQuery("SELECT").WillReturnRows(userRows(alice()))

	w, resp := doGet(t, newQueryRouter(), base+"/1")
	require.Equal(t, http.StatusOK, w.Code)
	data, ok := resp["data"].(map[string]interface{})
	require.True(t, ok, "data should be an object")
	assert.EqualValues(t, 1, data["id"])
	assert.Equal(t, "alice", data["username"])
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestQueryRouter_GetByID_NotFound(t *testing.T) {
	mock := injectDB(t)
	mock.ExpectQuery("SELECT").WillReturnRows(userRows())

	w, resp := doGet(t, newQueryRouter(), base+"/999")
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.EqualValues(t, 404, resp["code"])
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestQueryRouter_Count_Success(t *testing.T) {
	mock := injectDB(t)
	mock.ExpectQuery("SELECT count").WillReturnRows(countRows(5))

	w, resp := doJSON(t, newQueryRouter(), "POST", base+"/count", map[string]interface{}{})
	require.Equal(t, http.StatusOK, w.Code)
	assert.EqualValues(t, 5, resp["count"])
	assert.NoError(t, mock.ExpectationsWereMet())
}
