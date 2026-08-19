package unit

import (
	"context"
	"testing"

	"github.com/Super-Gagaga/abstract-manager/tests/testutil"
	"github.com/Super-Gagaga/abstract-manager/util/filter_translator"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// =============================================================================
// UT-001 ~ UT-007: ValidateSQLIdentifier
// =============================================================================

func TestValidateSQLIdentifier_Valid(t *testing.T) {
	assert.NoError(t, filter_translator.ValidateSQLIdentifier("user_name"))
	assert.NoError(t, filter_translator.ValidateSQLIdentifier("id"))
	assert.NoError(t, filter_translator.ValidateSQLIdentifier("a"))
}

func TestValidateSQLIdentifier_StartsWithNumber(t *testing.T) {
	err := filter_translator.ValidateSQLIdentifier("1user")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid SQL identifier")
}

func TestValidateSQLIdentifier_SQLInjection_Drop(t *testing.T) {
	err := filter_translator.ValidateSQLIdentifier("id;DROP TABLE")
	assert.Error(t, err)
}

func TestValidateSQLIdentifier_SQLInjection_Comment(t *testing.T) {
	err := filter_translator.ValidateSQLIdentifier("id--")
	assert.Error(t, err)
}

func TestValidateSQLIdentifier_Empty(t *testing.T) {
	err := filter_translator.ValidateSQLIdentifier("")
	assert.Error(t, err)
}

func TestValidateSQLIdentifier_SpecialChars(t *testing.T) {
	err := filter_translator.ValidateSQLIdentifier("id@x")
	assert.Error(t, err)

	err = filter_translator.ValidateSQLIdentifier("user space")
	assert.Error(t, err)
}

func TestValidateSQLIdentifier_UnderscorePrefix(t *testing.T) {
	// _private is a valid SQL identifier (starts with _ which matches [a-zA-Z_])
	assert.NoError(t, filter_translator.ValidateSQLIdentifier("_private"))
}

// =============================================================================
// UT-008 ~ UT-013: toFloat64（通过 Redis 过滤器间接测试）
// =============================================================================

// newRedisGtFilter helper: creates a RedisGreaterThanFilter for the given field/value.
func newRedisGtFilter(field string, value interface{}) *filter_translator.RedisGreaterThanFilter {
	return &filter_translator.RedisGreaterThanFilter{
		GenericFilter: &filter_translator.GenericFilter{
			Field:    field,
			Operator: ">",
			Value:    value,
		},
	}
}

func TestToFloat64_Int(t *testing.T) {
	client, mr := testutil.SetupMiniRedis(t)
	defer mr.Close()
	ctx := context.Background()
	testutil.SeedRedis(ctx, t, client, map[string]string{
		"k:1": `{"id":1,"age":25}`,
		"k:2": `{"id":2,"age":35}`,
	})

	f := newRedisGtFilter("age", 30) // int → toFloat64 succeeds
	keys := []string{"k:1", "k:2"}
	result, err := f.ApplyRedis(ctx, client, keys)
	require.NoError(t, err)
	assert.Equal(t, []string{"k:2"}, result) // only age 35 > 30
}

func TestToFloat64_String(t *testing.T) {
	client, mr := testutil.SetupMiniRedis(t)
	defer mr.Close()
	ctx := context.Background()
	testutil.SeedRedis(ctx, t, client, map[string]string{
		"k:1": `{"id":1,"age":25}`,
		"k:2": `{"id":2,"age":35}`,
	})

	f := newRedisGtFilter("age", "30.5") // string → toFloat64 parses
	keys := []string{"k:1", "k:2"}
	result, err := f.ApplyRedis(ctx, client, keys)
	require.NoError(t, err)
	assert.Equal(t, []string{"k:2"}, result)
}

func TestToFloat64_StringInvalid(t *testing.T) {
	client, mr := testutil.SetupMiniRedis(t)
	defer mr.Close()
	ctx := context.Background()
	testutil.SeedRedis(ctx, t, client, map[string]string{
		"k:1": `{"id":1,"age":25}`,
	})

	// "abc" cannot be parsed by toFloat64 → ApplyRedis returns error
	f := newRedisGtFilter("age", "abc")
	_, err := f.ApplyRedis(ctx, client, []string{"k:1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid value")
}

func TestToFloat64_Bool(t *testing.T) {
	client, mr := testutil.SetupMiniRedis(t)
	defer mr.Close()
	ctx := context.Background()
	testutil.SeedRedis(ctx, t, client, map[string]string{
		"k:1": `{"id":1,"age":25}`,
	})

	f := newRedisGtFilter("age", true) // bool → "unknown type"
	_, err := f.ApplyRedis(ctx, client, []string{"k:1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid value")
}

func TestToFloat64_Int64(t *testing.T) {
	client, mr := testutil.SetupMiniRedis(t)
	defer mr.Close()
	ctx := context.Background()
	testutil.SeedRedis(ctx, t, client, map[string]string{
		"k:1": `{"id":1,"count":100}`,
		"k:2": `{"id":2,"count":50}`,
	})

	f := newRedisGtFilter("count", int64(75))
	keys := []string{"k:1", "k:2"}
	result, err := f.ApplyRedis(ctx, client, keys)
	require.NoError(t, err)
	assert.Equal(t, []string{"k:1"}, result)
}

func TestToFloat64_Nil(t *testing.T) {
	client, mr := testutil.SetupMiniRedis(t)
	defer mr.Close()
	ctx := context.Background()
	testutil.SeedRedis(ctx, t, client, map[string]string{
		"k:1": `{"id":1,"age":25}`,
	})

	f := newRedisGtFilter("age", nil) // nil → "unknown type"
	_, err := f.ApplyRedis(ctx, client, []string{"k:1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid value")
}

// =============================================================================
// UT-014 ~ UT-015: GORM Filter ApplyGorm
// =============================================================================

// setupMockGormDB returns a *gorm.DB backed by sqlmock for testing clause building.
func setupMockGormDB(t *testing.T) *gorm.DB {
	t.Helper()
	sqlDB, _, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { sqlDB.Close() })

	gormDB, err := gorm.Open(mysql.New(mysql.Config{
		Conn:                      sqlDB,
		SkipInitializeWithVersion: true,
	}), &gorm.Config{})
	require.NoError(t, err)
	return gormDB
}

func TestGormEqualFilter_ApplyGorm(t *testing.T) {
	db := setupMockGormDB(t)

	f := &filter_translator.GormEqualFilter{
		GenericFilter: &filter_translator.GenericFilter{
			Field:    "age",
			Operator: "=",
			Value:    25,
		},
	}

	result := f.ApplyGorm(db)
	// Valid field + value → no error added
	assert.NoError(t, result.Error)
}

func TestGormFilter_SQLInjection(t *testing.T) {
	db := setupMockGormDB(t)

	f := &filter_translator.GormEqualFilter{
		GenericFilter: &filter_translator.GenericFilter{
			Field:    "id;DROP TABLE users--",
			Operator: "=",
			Value:    1,
		},
	}

	result := f.ApplyGorm(db)
	// SQL injection field name → AddError called
	assert.Error(t, result.Error)
	assert.Contains(t, result.Error.Error(), "invalid filter field")
}

// =============================================================================
// UT-016 ~ UT-017: GormTranslatorRegistry
// =============================================================================

func TestGormTranslatorRegistry_AllOperators(t *testing.T) {
	registry := filter_translator.NewGormTranslatorRegistry()
	operators := registry.GetSupportedOperators()

	// All 11 operators should be registered
	expected := []string{"=", "!=", ">", ">=", "<", "<=", "like", "in", "between", "isnull", "isnotnull"}
	for _, op := range expected {
		assert.Contains(t, operators, op, "missing operator: %s", op)
	}
}

func TestGormTranslatorRegistry_UnsupportedOperator(t *testing.T) {
	registry := filter_translator.NewGormTranslatorRegistry()

	_, err := registry.Translate(filter_translator.FilterParam{
		Field:    "name",
		Operator: "unknown_op",
		Value:    "test",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported operator")
}

func TestGormTranslatorRegistry_ValidateBatch(t *testing.T) {
	registry := filter_translator.NewGormTranslatorRegistry()

	// Batch with an invalid operator should fail
	_, err := registry.TranslateBatch([]filter_translator.FilterParam{
		{Field: "age", Operator: ">", Value: 18},
		{Field: "name", Operator: "bad_operator", Value: "x"},
	})
	assert.Error(t, err)
}

// =============================================================================
// UT-018 ~ UT-020: Redis Filter ApplyRedis
// =============================================================================

func TestRedisGreaterThanFilter_ApplyRedis(t *testing.T) {
	client, mr := testutil.SetupMiniRedis(t)
	defer mr.Close()
	ctx := context.Background()

	testutil.SeedRedis(ctx, t, client, map[string]string{
		"user:1": `{"id":1,"name":"Alice","age":25}`,
		"user:2": `{"id":2,"name":"Bob","age":35}`,
		"user:3": `{"id":3,"name":"Charlie","age":20}`,
	})

	f := &filter_translator.RedisGreaterThanFilter{
		GenericFilter: &filter_translator.GenericFilter{
			Field:    "age",
			Operator: ">",
			Value:    30,
		},
	}

	keys := []string{"user:1", "user:2", "user:3"}
	result, err := f.ApplyRedis(ctx, client, keys)
	require.NoError(t, err)
	assert.Equal(t, []string{"user:2"}, result)
}

func TestRedisGreaterThanFilter_Invalid(t *testing.T) {
	client, mr := testutil.SetupMiniRedis(t)
	defer mr.Close()
	ctx := context.Background()

	testutil.SeedRedis(ctx, t, client, map[string]string{
		"user:1": `{"id":1,"age":25}`,
	})

	f := &filter_translator.RedisGreaterThanFilter{
		GenericFilter: &filter_translator.GenericFilter{
			Field:    "age",
			Operator: ">",
			Value:    "not_a_number",
		},
	}

	_, err := f.ApplyRedis(ctx, client, []string{"user:1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid value")
}

func TestApplyRedisFilters_Multiple(t *testing.T) {
	client, mr := testutil.SetupMiniRedis(t)
	defer mr.Close()
	ctx := context.Background()

	testutil.SeedRedis(ctx, t, client, map[string]string{
		"user:1": `{"id":1,"name":"Alice","age":25,"status":"active"}`,
		"user:2": `{"id":2,"name":"Bob","age":35,"status":"inactive"}`,
		"user:3": `{"id":3,"name":"Charlie","age":30,"status":"active"}`,
	})

	// Chain: age > 20 AND status = active
	gtFilter := &filter_translator.RedisGreaterThanFilter{
		GenericFilter: &filter_translator.GenericFilter{
			Field: "age", Operator: ">", Value: 20,
		},
	}
	eqFilter := &filter_translator.RedisEqualFilter{
		GenericFilter: &filter_translator.GenericFilter{
			Field: "status", Operator: "=", Value: "active",
		},
	}

	keys := []string{"user:1", "user:2", "user:3"}
	result, err := filter_translator.ApplyRedisFilters(ctx, client, keys, []filter_translator.RedisFilter{gtFilter, eqFilter})
	require.NoError(t, err)

	// Alice (age 25, active) and Charlie (age 30, active) pass
	// Bob (age 35, inactive) fails because status != active
	assert.Equal(t, []string{"user:1", "user:3"}, result)
}

// =============================================================================
// Additional translator tests
// =============================================================================

func TestRedisEqualFilter_ApplyRedis(t *testing.T) {
	client, mr := testutil.SetupMiniRedis(t)
	defer mr.Close()
	ctx := context.Background()

	testutil.SeedRedis(ctx, t, client, map[string]string{
		"u:1": `{"id":1,"status":"active"}`,
		"u:2": `{"id":2,"status":"inactive"}`,
		"u:3": `{"id":3,"status":"active"}`,
	})

	f := &filter_translator.RedisEqualFilter{
		GenericFilter: &filter_translator.GenericFilter{
			Field: "status", Operator: "=", Value: "active",
		},
	}

	result, err := f.ApplyRedis(ctx, client, []string{"u:1", "u:2", "u:3"})
	require.NoError(t, err)
	assert.Equal(t, []string{"u:1", "u:3"}, result)
}

func TestRedisNotEqualFilter_ApplyRedis(t *testing.T) {
	client, mr := testutil.SetupMiniRedis(t)
	defer mr.Close()
	ctx := context.Background()

	testutil.SeedRedis(ctx, t, client, map[string]string{
		"u:1": `{"id":1,"status":"active"}`,
		"u:2": `{"id":2,"status":"inactive"}`,
	})

	f := &filter_translator.RedisNotEqualFilter{
		GenericFilter: &filter_translator.GenericFilter{
			Field: "status", Operator: "!=", Value: "active",
		},
	}

	result, err := f.ApplyRedis(ctx, client, []string{"u:1", "u:2"})
	require.NoError(t, err)
	assert.Equal(t, []string{"u:2"}, result)
}

func TestRedisInFilter_ApplyRedis(t *testing.T) {
	client, mr := testutil.SetupMiniRedis(t)
	defer mr.Close()
	ctx := context.Background()

	testutil.SeedRedis(ctx, t, client, map[string]string{
		"u:1": `{"id":1,"role":"admin"}`,
		"u:2": `{"id":2,"role":"user"}`,
		"u:3": `{"id":3,"role":"moderator"}`,
		"u:4": `{"id":4,"role":"user"}`,
	})

	f := &filter_translator.RedisInFilter{
		GenericInFilter: &filter_translator.GenericInFilter{
			Field: "role", Operator: "in", Values: []interface{}{"admin", "moderator"},
		},
	}

	result, err := f.ApplyRedis(ctx, client, []string{"u:1", "u:2", "u:3", "u:4"})
	require.NoError(t, err)
	assert.Equal(t, []string{"u:1", "u:3"}, result)
}

func TestRedisBetweenFilter_ApplyRedis(t *testing.T) {
	client, mr := testutil.SetupMiniRedis(t)
	defer mr.Close()
	ctx := context.Background()

	testutil.SeedRedis(ctx, t, client, map[string]string{
		"p:1": `{"id":1,"price":10}`,
		"p:2": `{"id":2,"price":25}`,
		"p:3": `{"id":3,"price":50}`,
		"p:4": `{"id":4,"price":5}`,
	})

	f := &filter_translator.RedisBetweenFilter{
		GenericBetweenFilter: &filter_translator.GenericBetweenFilter{
			Field: "price", Operator: "between", Min: 10, Max: 30,
		},
	}

	result, err := f.ApplyRedis(ctx, client, []string{"p:1", "p:2", "p:3", "p:4"})
	require.NoError(t, err)
	assert.Equal(t, []string{"p:1", "p:2"}, result)
}

func TestRedisBetweenFilter_InvalidMin(t *testing.T) {
	client, mr := testutil.SetupMiniRedis(t)
	defer mr.Close()
	ctx := context.Background()
	testutil.SeedRedis(ctx, t, client, map[string]string{"k:1": `{"id":1,"val":10}`})

	f := &filter_translator.RedisBetweenFilter{
		GenericBetweenFilter: &filter_translator.GenericBetweenFilter{
			Field: "val", Operator: "between", Min: "invalid", Max: 30,
		},
	}

	_, err := f.ApplyRedis(ctx, client, []string{"k:1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid min value")
}

func TestRedisLikeFilter_ApplyRedis(t *testing.T) {
	client, mr := testutil.SetupMiniRedis(t)
	defer mr.Close()
	ctx := context.Background()

	testutil.SeedRedis(ctx, t, client, map[string]string{
		"u:1": `{"id":1,"name":"Alice"}`,
		"u:2": `{"id":2,"name":"Bob"}`,
		"u:3": `{"id":3,"name":"Alicia"}`,
	})

	f := &filter_translator.RedisLikeFilter{
		GenericFilter: &filter_translator.GenericFilter{
			Field: "name", Operator: "like", Value: "Ali",
		},
	}

	result, err := f.ApplyRedis(ctx, client, []string{"u:1", "u:2", "u:3"})
	require.NoError(t, err)
	assert.Equal(t, []string{"u:1", "u:3"}, result)
}

func TestRedisIsNullFilter_ApplyRedis(t *testing.T) {
	client, mr := testutil.SetupMiniRedis(t)
	defer mr.Close()
	ctx := context.Background()

	// applyRedisBatchFilter requires the field to exist in JSON.
	// "deleted_at":null is detected as null by the filter;
	// a missing field is skipped (exists=false).
	testutil.SeedRedis(ctx, t, client, map[string]string{
		"u:1": `{"id":1,"name":"Alice","deleted_at":null}`,
		"u:2": `{"id":2,"name":"Bob","deleted_at":"2024-01-01"}`,
	})

	f := &filter_translator.RedisIsNullFilter{
		GenericFilter: &filter_translator.GenericFilter{
			Field: "deleted_at", Operator: "isnull", Value: nil,
		},
	}

	result, err := f.ApplyRedis(ctx, client, []string{"u:1", "u:2"})
	require.NoError(t, err)
	assert.Equal(t, []string{"u:1"}, result)
}

func TestRedisTranslatorRegistry_Translate(t *testing.T) {
	registry := filter_translator.NewRedisTranslatorRegistry()

	f, err := registry.Translate(filter_translator.FilterParam{
		Field: "age", Operator: ">", Value: 18,
	})
	require.NoError(t, err)
	assert.NotNil(t, f)
}

func TestRedisTranslatorRegistry_Unsupported(t *testing.T) {
	registry := filter_translator.NewRedisTranslatorRegistry()

	_, err := registry.Translate(filter_translator.FilterParam{
		Field: "x", Operator: "no_such_op", Value: 1,
	})
	assert.Error(t, err)
}

func TestApplyRedisFilters_EmptyKeys(t *testing.T) {
	client, mr := testutil.SetupMiniRedis(t)
	defer mr.Close()
	ctx := context.Background()

	f := &filter_translator.RedisEqualFilter{
		GenericFilter: &filter_translator.GenericFilter{
			Field: "x", Operator: "=", Value: 1,
		},
	}

	result, err := filter_translator.ApplyRedisFilters(ctx, client, []string{}, []filter_translator.RedisFilter{f})
	require.NoError(t, err)
	assert.Empty(t, result)
}
