package unit

import (
	"os"
	"testing"
	"time"

	"github.com/Super-Gagaga/abstract-manager/util"

	"github.com/stretchr/testify/assert"
)

// =============================================================================
// UT-101 ~ UT-106: GetCacheAsideTTL
// =============================================================================

func setEnv(t *testing.T, key, value string) {
	t.Helper()
	t.Setenv(key, value)
}

func unsetEnv(t *testing.T, key string) {
	t.Helper()
	os.Unsetenv(key)
}

func TestGetCacheAsideTTL_Default(t *testing.T) {
	// Ensure both env vars are unset
	os.Unsetenv("CACHE_ASIDE_TTL")
	os.Unsetenv("CACHE_TTL_SECONDS")

	result := util.GetCacheAsideTTL()
	assert.Equal(t, 1*time.Hour, result)
}

func TestGetCacheAsideTTL_CACHE_ASIDE_TTL(t *testing.T) {
	t.Setenv("CACHE_ASIDE_TTL", "60")
	os.Unsetenv("CACHE_TTL_SECONDS")

	result := util.GetCacheAsideTTL()
	assert.Equal(t, 60*time.Second, result)
}

func TestGetCacheAsideTTL_CACHE_TTL_SECONDS(t *testing.T) {
	os.Unsetenv("CACHE_ASIDE_TTL")
	t.Setenv("CACHE_TTL_SECONDS", "120")

	result := util.GetCacheAsideTTL()
	assert.Equal(t, 120*time.Second, result)
}

func TestGetCacheAsideTTL_Both_Priority(t *testing.T) {
	// CACHE_ASIDE_TTL should take priority when both are set
	t.Setenv("CACHE_ASIDE_TTL", "30")
	t.Setenv("CACHE_TTL_SECONDS", "120")

	result := util.GetCacheAsideTTL()
	assert.Equal(t, 30*time.Second, result)
}

func TestGetCacheAsideTTL_Invalid(t *testing.T) {
	t.Setenv("CACHE_ASIDE_TTL", "abc")
	os.Unsetenv("CACHE_TTL_SECONDS")

	result := util.GetCacheAsideTTL()
	assert.Equal(t, 1*time.Hour, result, "invalid value should return default")
}

func TestGetCacheAsideTTL_Negative(t *testing.T) {
	t.Setenv("CACHE_ASIDE_TTL", "-5")
	os.Unsetenv("CACHE_TTL_SECONDS")

	result := util.GetCacheAsideTTL()
	assert.Equal(t, 1*time.Hour, result, "negative value should return default")
}

func TestGetCacheAsideTTL_Zero(t *testing.T) {
	t.Setenv("CACHE_ASIDE_TTL", "0")
	os.Unsetenv("CACHE_TTL_SECONDS")

	result := util.GetCacheAsideTTL()
	assert.Equal(t, 1*time.Hour, result, "zero should return default")
}

func TestGetCacheAsideTTL_LegacyVar_Invalid(t *testing.T) {
	// CACHE_ASIDE_TTL not set, legacy var has garbage
	os.Unsetenv("CACHE_ASIDE_TTL")
	t.Setenv("CACHE_TTL_SECONDS", "notanumber")

	result := util.GetCacheAsideTTL()
	assert.Equal(t, 1*time.Hour, result)
}

// =============================================================================
// UT-107 ~ UT-108: GetCacheHitRefresh
// =============================================================================

func TestGetCacheHitRefresh_True(t *testing.T) {
	t.Setenv("CACHE_HIT_REFRESH", "true")

	result := util.GetCacheHitRefresh()
	assert.True(t, result)
}

func TestGetCacheHitRefresh_False_Default(t *testing.T) {
	os.Unsetenv("CACHE_HIT_REFRESH")

	result := util.GetCacheHitRefresh()
	assert.False(t, result)
}

func TestGetCacheHitRefresh_RandomString(t *testing.T) {
	t.Setenv("CACHE_HIT_REFRESH", "yes")

	result := util.GetCacheHitRefresh()
	assert.False(t, result, "only exactly 'true' yields true")
}

// =============================================================================
// UT-109 ~ UT-110: GetEnvOrDefault
// =============================================================================

func TestGetEnvOrDefault_Found(t *testing.T) {
	t.Setenv("PORT", "9090")

	result := util.GetEnvOrDefault("PORT", "8080")
	assert.Equal(t, "9090", result)
}

func TestGetEnvOrDefault_Default(t *testing.T) {
	os.Unsetenv("PORT")

	result := util.GetEnvOrDefault("PORT", "8080")
	assert.Equal(t, "8080", result)
}

func TestGetEnvOrDefault_EmptyString(t *testing.T) {
	t.Setenv("PORT", "")

	result := util.GetEnvOrDefault("PORT", "8080")
	// os.Getenv returns "" for empty value, which is != ""? No, "" == "".
	// So the function should return defaultValue for empty strings too.
	assert.Equal(t, "8080", result)
}
