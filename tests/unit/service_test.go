package unit

import (
	"context"
	"testing"

	"github.com/Super-Gagaga/AbstractManager/service"
	"github.com/Super-Gagaga/AbstractManager/tests/testutil"
	"github.com/Super-Gagaga/AbstractManager/util/filter_translator"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// =============================================================================
// UT-301 ~ UT-303: ServiceManager construction & type resolution
// =============================================================================

func TestNewServiceManager_Defaults(t *testing.T) {
	sm := service.NewServiceManager(testutil.TestUser{})

	assert.NotNil(t, sm)
	assert.Equal(t, "TestUser", sm.ResourceName)
	assert.Equal(t, "TestUser", sm.TableName)
	assert.Equal(t, "public", sm.Schema)
	assert.Equal(t, "none", sm.CacheKeyType)
	assert.Equal(t, "TestUser_key", sm.CacheKeyName)
}

func TestNewServiceManager_PointerType(t *testing.T) {
	sm := service.NewServiceManager(&testutil.TestUser{})

	// Pointer type should be unwrapped to get the element name
	assert.Equal(t, "TestUser", sm.ResourceName)
	assert.Equal(t, "TestUser", sm.TableName)
}

func TestNewServiceManager_PrimitiveType(t *testing.T) {
	sm := service.NewServiceManager(42)

	// For primitive types, getTypeName returns the type name (e.g. "int")
	assert.NotEmpty(t, sm.ResourceName)
}

// =============================================================================
// UT-304 ~ UT-309: extractID / extractIDFromKey (test through exported API)
// =============================================================================

// buildCacheKey is unexported — tested indirectly via LookupSingleByID
// when integration tests with miniredis are available.
// Direct tests for extractID/extractIDFromKey are in service/extract_test.go

// =============================================================================
// UT-310 ~ UT-311: HavingCondition / applyQueryOptions
// =============================================================================

// Tested via the exported GetQueryWithoutTransaction with sqlmock.
// Direct white-box tests in service/query_options_test.go

// =============================================================================
// Filter Translator: GORM Translator validation tests
// =============================================================================

func TestGormEqualTranslator_Validate(t *testing.T) {
	translator := &filter_translator.GormEqualTranslator{}

	// Valid: field + value present
	err := translator.Validate(filter_translator.FilterParam{
		Field: "age", Operator: "=", Value: 25,
	})
	assert.NoError(t, err)

	// Invalid: empty field
	err = translator.Validate(filter_translator.FilterParam{
		Field: "", Operator: "=", Value: 25,
	})
	assert.Error(t, err)

	// Invalid: nil value
	err = translator.Validate(filter_translator.FilterParam{
		Field: "age", Operator: "=", Value: nil,
	})
	assert.Error(t, err)
}

func TestGormInTranslator_Validate_EmptyArray(t *testing.T) {
	translator := &filter_translator.GormInTranslator{}

	// Empty array should fail
	err := translator.Validate(filter_translator.FilterParam{
		Field: "id", Operator: "in", Value: []interface{}{},
	})
	assert.Error(t, err)

	// Non-array value should fail
	err = translator.Validate(filter_translator.FilterParam{
		Field: "id", Operator: "in", Value: "not_array",
	})
	assert.Error(t, err)
}

func TestGormBetweenTranslator_Validate_WrongArrayLen(t *testing.T) {
	translator := &filter_translator.GormBetweenTranslator{}

	// Only 1 element (need 2)
	err := translator.Validate(filter_translator.FilterParam{
		Field: "price", Operator: "between", Value: []interface{}{10},
	})
	assert.Error(t, err)

	// 3 elements (need 2)
	err = translator.Validate(filter_translator.FilterParam{
		Field: "price", Operator: "between", Value: []interface{}{10, 20, 30},
	})
	assert.Error(t, err)
}

func TestGormLikeTranslator_Validate_NonString(t *testing.T) {
	translator := &filter_translator.GormLikeTranslator{}

	// LIKE requires string value
	err := translator.Validate(filter_translator.FilterParam{
		Field: "name", Operator: "like", Value: 123,
	})
	assert.Error(t, err)
}

// =============================================================================
// GORM Filter ApplyGorm with mock DB — extended tests
// =============================================================================

func setupTestGormDB(t *testing.T) *gorm.DB {
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

func TestGormGreaterThanFilter_ApplyGorm(t *testing.T) {
	db := setupTestGormDB(t)

	f := &filter_translator.GormGreaterThanFilter{
		GenericFilter: &filter_translator.GenericFilter{
			Field: "age", Operator: ">", Value: 18,
		},
	}

	result := f.ApplyGorm(db)
	assert.NoError(t, result.Error)
}

func TestGormInFilter_ApplyGorm(t *testing.T) {
	db := setupTestGormDB(t)

	f := &filter_translator.GormInFilter{
		GenericInFilter: &filter_translator.GenericInFilter{
			Field: "id", Operator: "in", Values: []interface{}{1, 2, 3},
		},
	}

	result := f.ApplyGorm(db)
	assert.NoError(t, result.Error)
}

func TestGormInFilter_SQLInjectionField(t *testing.T) {
	db := setupTestGormDB(t)

	f := &filter_translator.GormInFilter{
		GenericInFilter: &filter_translator.GenericInFilter{
			Field: "id;DELETE FROM users", Operator: "in", Values: []interface{}{1},
		},
	}

	result := f.ApplyGorm(db)
	assert.Error(t, result.Error)
}

func TestGormBetweenFilter_ApplyGorm(t *testing.T) {
	db := setupTestGormDB(t)

	f := &filter_translator.GormBetweenFilter{
		GenericBetweenFilter: &filter_translator.GenericBetweenFilter{
			Field: "price", Operator: "between", Min: 10, Max: 100,
		},
	}

	result := f.ApplyGorm(db)
	assert.NoError(t, result.Error)
}

func TestGormIsNullFilter_ApplyGorm(t *testing.T) {
	db := setupTestGormDB(t)

	f := &filter_translator.GormIsNullFilter{
		GenericFilter: &filter_translator.GenericFilter{
			Field: "deleted_at", Operator: "isnull", Value: nil,
		},
	}

	result := f.ApplyGorm(db)
	assert.NoError(t, result.Error)
}

func TestGormIsNotNullFilter_ApplyGorm(t *testing.T) {
	db := setupTestGormDB(t)

	f := &filter_translator.GormIsNotNullFilter{
		GenericFilter: &filter_translator.GenericFilter{
			Field: "email", Operator: "isnotnull", Value: nil,
		},
	}

	result := f.ApplyGorm(db)
	assert.NoError(t, result.Error)
}

func TestGormLikeFilter_ApplyGorm(t *testing.T) {
	db := setupTestGormDB(t)

	f := &filter_translator.GormLikeFilter{
		GenericFilter: &filter_translator.GenericFilter{
			Field: "name", Operator: "like", Value: "test",
		},
	}

	result := f.ApplyGorm(db)
	assert.NoError(t, result.Error)
}

func TestApplyGormFilters_Multiple(t *testing.T) {
	db := setupTestGormDB(t)

	filters := []filter_translator.GormFilter{
		&filter_translator.GormEqualFilter{
			GenericFilter: &filter_translator.GenericFilter{Field: "status", Operator: "=", Value: "active"},
		},
		&filter_translator.GormGreaterThanFilter{
			GenericFilter: &filter_translator.GenericFilter{Field: "age", Operator: ">", Value: 18},
		},
	}

	result := filter_translator.ApplyGormFilters(db, filters)
	assert.NoError(t, result.Error)
}

// =============================================================================
// Translator registry — translate batch success
// =============================================================================

func TestGormTranslatorRegistry_TranslateBatch_Success(t *testing.T) {
	registry := filter_translator.NewGormTranslatorRegistry()

	params := []filter_translator.FilterParam{
		{Field: "age", Operator: ">", Value: 18},
		{Field: "status", Operator: "=", Value: "active"},
		{Field: "id", Operator: "in", Value: []interface{}{1, 2, 3}},
	}

	filters, err := registry.TranslateBatch(params)
	require.NoError(t, err)
	assert.Len(t, filters, 3)
}

func TestGormTranslatorRegistry_MutuallyExclusiveOperators(t *testing.T) {
	registry := filter_translator.NewGormTranslatorRegistry()

	// isnull should not require a value
	f, err := registry.Translate(filter_translator.FilterParam{
		Field: "deleted_at", Operator: "isnull",
	})
	require.NoError(t, err)
	assert.NotNil(t, f)

	// isnotnull should not require a value
	f2, err := registry.Translate(filter_translator.FilterParam{
		Field: "email", Operator: "isnotnull",
	})
	require.NoError(t, err)
	assert.NotNil(t, f2)
}

// =============================================================================
// Redis translator — additional tests
// =============================================================================

func TestRedisLessThanFilter_ApplyRedis(t *testing.T) {
	client, mr := testutil.SetupMiniRedis(t)
	defer mr.Close()
	ctx := context.Background()

	testutil.SeedRedis(ctx, t, client, map[string]string{
		"k:1": `{"id":1,"score":50}`,
		"k:2": `{"id":2,"score":90}`,
		"k:3": `{"id":3,"score":30}`,
	})

	f := &filter_translator.RedisLessThanFilter{
		GenericFilter: &filter_translator.GenericFilter{
			Field: "score", Operator: "<", Value: 80,
		},
	}

	result, err := f.ApplyRedis(ctx, client, []string{"k:1", "k:2", "k:3"})
	require.NoError(t, err)
	assert.Equal(t, []string{"k:1", "k:3"}, result)
}

func TestRedisLessThanOrEqualFilter_ApplyRedis(t *testing.T) {
	client, mr := testutil.SetupMiniRedis(t)
	defer mr.Close()
	ctx := context.Background()

	testutil.SeedRedis(ctx, t, client, map[string]string{
		"k:1": `{"id":1,"score":50}`,
		"k:2": `{"id":2,"score":80}`,
	})

	f := &filter_translator.RedisLessThanOrEqualFilter{
		GenericFilter: &filter_translator.GenericFilter{
			Field: "score", Operator: "<=", Value: 80,
		},
	}

	result, err := f.ApplyRedis(ctx, client, []string{"k:1", "k:2"})
	require.NoError(t, err)
	// Both 50 <= 80 and 80 <= 80 should pass
	assert.ElementsMatch(t, []string{"k:1", "k:2"}, result)
}

func TestRedisGreaterThanOrEqualFilter_ApplyRedis(t *testing.T) {
	client, mr := testutil.SetupMiniRedis(t)
	defer mr.Close()
	ctx := context.Background()

	testutil.SeedRedis(ctx, t, client, map[string]string{
		"k:1": `{"id":1,"score":50}`,
		"k:2": `{"id":2,"score":80}`,
	})

	f := &filter_translator.RedisGreaterThanOrEqualFilter{
		GenericFilter: &filter_translator.GenericFilter{
			Field: "score", Operator: ">=", Value: 80,
		},
	}

	result, err := f.ApplyRedis(ctx, client, []string{"k:1", "k:2"})
	require.NoError(t, err)
	assert.Equal(t, []string{"k:2"}, result)
}

func TestRedisIsNotNullFilter_ApplyRedis(t *testing.T) {
	client, mr := testutil.SetupMiniRedis(t)
	defer mr.Close()
	ctx := context.Background()

	testutil.SeedRedis(ctx, t, client, map[string]string{
		"k:1": `{"id":1,"name":"Alice"}`,
		"k:2": `{"id":2}`,
	})

	f := &filter_translator.RedisIsNotNullFilter{
		GenericFilter: &filter_translator.GenericFilter{
			Field: "name", Operator: "isnotnull",
		},
	}

	result, err := f.ApplyRedis(ctx, client, []string{"k:1", "k:2"})
	require.NoError(t, err)
	assert.Equal(t, []string{"k:1"}, result)
}

func TestApplyRedisFilters_ChainNarrowsResults(t *testing.T) {
	client, mr := testutil.SetupMiniRedis(t)
	defer mr.Close()
	ctx := context.Background()

	testutil.SeedRedis(ctx, t, client, map[string]string{
		"p:1": `{"id":1,"price":50,"category":"books"}`,
		"p:2": `{"id":2,"price":150,"category":"books"}`,
		"p:3": `{"id":3,"price":75,"category":"electronics"}`,
		"p:4": `{"id":4,"price":200,"category":"books"}`,
	})

	// Chain: price > 60 AND category = books
	filters := []filter_translator.RedisFilter{
		&filter_translator.RedisGreaterThanFilter{
			GenericFilter: &filter_translator.GenericFilter{Field: "price", Operator: ">", Value: 60},
		},
		&filter_translator.RedisEqualFilter{
			GenericFilter: &filter_translator.GenericFilter{Field: "category", Operator: "=", Value: "books"},
		},
	}

	keys := []string{"p:1", "p:2", "p:3", "p:4"}
	result, err := filter_translator.ApplyRedisFilters(ctx, client, keys, filters)
	require.NoError(t, err)
	// p:2 (price 150, books) and p:4 (price 200, books)
	assert.ElementsMatch(t, []string{"p:2", "p:4"}, result)
}
