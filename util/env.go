package util

import (
	"os"
	"strconv"
	"time"
)

// GetCacheAsideTTL 从环境变量获取 Cache Aside TTL（秒）
// 优先读取 CACHE_ASIDE_TTL，兼容旧变量名 CACHE_TTL_SECONDS
func GetCacheAsideTTL() time.Duration {
	if ttlStr := os.Getenv("CACHE_ASIDE_TTL"); ttlStr != "" {
		if ttl, err := strconv.Atoi(ttlStr); err == nil && ttl > 0 {
			return time.Duration(ttl) * time.Second
		}
	}
	// 兼容旧变量名
	if ttlStr := os.Getenv("CACHE_TTL_SECONDS"); ttlStr != "" {
		if ttl, err := strconv.Atoi(ttlStr); err == nil && ttl > 0 {
			return time.Duration(ttl) * time.Second
		}
	}
	return 1 * time.Hour // 默认1小时
}

// GetCacheHitRefresh 从环境变量获取缓存命中时是否刷新 TTL
func GetCacheHitRefresh() bool {
	return os.Getenv("CACHE_HIT_REFRESH") == "true"
}

// GetEnvOrDefault 读取环境变量，不存在时返回默认值
func GetEnvOrDefault(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

// GetEnvIntOrDefault 读取整型环境变量，缺失或非法时返回默认值
func GetEnvIntOrDefault(key string, defaultValue int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return defaultValue
}
