package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisManager Redis 管理器
type RedisManager struct {
	Client *redis.Client
}

var globalRedisManager *RedisManager

// InitRedis 初始化 Redis 连接（不变）
func InitRedis() (*RedisManager, error) {
	client := redis.NewClient(&redis.Options{
		Addr:         fmt.Sprintf("%s:%s", os.Getenv("REDIS_HOST"), os.Getenv("REDIS_PORT")),
		Password:     os.Getenv("REDIS_PASSWORD"),
		DB:           0,
		PoolSize:     50,
		MinIdleConns: 10,
		MaxRetries:   3,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect redis: %w", err)
	}

	globalRedisManager = &RedisManager{Client: client}
	return globalRedisManager, nil
}

// GetRedis 获取全局 Redis 实例（不变）
func GetRedis() *redis.Client {
	if globalRedisManager == nil {
		panic("redis not initialized, call InitRedis first")
	}
	return globalRedisManager.Client
}

// SetGlobalRedis 注入全局 Redis 客户端(主要用于测试替身或自定义初始化流程);
// 传 nil 恢复为未初始化状态
func SetGlobalRedis(client *redis.Client) {
	if client == nil {
		globalRedisManager = nil
		return
	}
	globalRedisManager = &RedisManager{Client: client}
}

// GetRedisManager 返回该实例使用的 RedisManager:优先实例注入,回退全局单例
func (sm *ServiceManager[T]) GetRedisManager() *RedisManager {
	if sm.redisClient != nil {
		return &RedisManager{Client: sm.redisClient}
	}
	if globalRedisManager == nil {
		panic("redis not initialized, call InitRedis first")
	}
	return globalRedisManager
}

// Close 关闭 Redis 连接（不变）
func (rm *RedisManager) Close() error {
	return rm.Client.Close()
}

// Set 设置缓存（带过期时间）—— 修改在这里
func (rm *RedisManager) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	var data []byte
	var err error

	// 如果传入的已经是 []byte，就直接用
	if b, ok := value.([]byte); ok {
		data = b
	} else {
		// 其他类型（结构体、map、基本类型等）统一 json 序列化
		data, err = json.Marshal(value)
		if err != nil {
			return fmt.Errorf("failed to json marshal value for key %s: %w", key, err)
		}
	}

	return rm.Client.Set(ctx, key, data, expiration).Err()
}

// Get 获取缓存（不变）
func (rm *RedisManager) Get(ctx context.Context, key string, dest interface{}) error {
	data, err := rm.Client.Get(ctx, key).Bytes()
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dest)
}

// Delete 删除缓存（不变）
func (rm *RedisManager) Delete(ctx context.Context, keys ...string) error {
	return rm.Client.Del(ctx, keys...).Err()
}

// Exists 检查键是否存在（不变）
func (rm *RedisManager) Exists(ctx context.Context, key string) (bool, error) {
	n, err := rm.Client.Exists(ctx, key).Result()
	return n > 0, err
}

// SetMultiple 批量设置缓存 —— 修改在这里
func (rm *RedisManager) SetMultiple(ctx context.Context, items map[string]interface{}, expiration time.Duration) error {
	pipe := rm.Client.Pipeline()

	for key, value := range items {
		var data []byte
		var err error

		// 同 Set 的逻辑：先转成 []byte
		if b, ok := value.([]byte); ok {
			data = b
		} else {
			data, err = json.Marshal(value)
			if err != nil {
				return fmt.Errorf("failed to json marshal value for key %s: %w", key, err)
			}
		}

		pipe.Set(ctx, key, data, expiration)
	}

	_, err := pipe.Exec(ctx)
	return err
}

// ScanKeys 使用 SCAN 命令安全遍历匹配的 key
// 生产环境必须使用此函数而非 KEYS 命令，避免阻塞 Redis 主线程
func ScanKeys(ctx context.Context, client *redis.Client, pattern string, batchSize int64) ([]string, error) {
	var allKeys []string
	var cursor uint64
	for {
		keys, nextCursor, err := client.Scan(ctx, cursor, pattern, batchSize).Result()
		if err != nil {
			return nil, fmt.Errorf("scan keys failed: %w", err)
		}
		allKeys = append(allKeys, keys...)
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
	return allKeys, nil
}

// GetMultiple 批量获取缓存（不变）
func (rm *RedisManager) GetMultiple(ctx context.Context, keys []string) (map[string][]byte, error) {
	pipe := rm.Client.Pipeline()
	cmds := make([]*redis.StringCmd, len(keys))

	for i, key := range keys {
		cmds[i] = pipe.Get(ctx, key)
	}

	_, err := pipe.Exec(ctx)
	if err != nil && err != redis.Nil {
		return nil, err
	}

	result := make(map[string][]byte)
	for i, cmd := range cmds {
		if data, err := cmd.Bytes(); err == nil {
			result[keys[i]] = data
		}
	}

	return result, nil
}
