package service

import (
	"context"
	"reflect"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// asyncCacheTask 异步缓存写入任务
type asyncCacheTask[T any] struct {
	ctx        context.Context
	key        string
	data       *T
	expiration time.Duration
}

type ServiceManager[T any] struct {
	Resource     T      // 被管理的资源
	ResourceName string // 资源名称
	TableName    string // 表名
	Schema       string // 数据库模式
	CacheKeyType string // 缓存键
	CacheKeyName string // 缓存键名称

	// 可选的实例级依赖注入;为 nil 时回退到全局单例(InitDB/InitRedis)
	db          *gorm.DB
	redisClient *redis.Client

	// 异步写入 worker pool
	asyncTasks    chan asyncCacheTask[T] // 任务队列
	asyncWg       sync.WaitGroup         // 等待所有 worker 完成
	asyncShutdown chan struct{}          // 通知 worker 退出
	asyncStarted  bool                   // 是否已启动 worker
	asyncMu       sync.Mutex             // 保护 asyncStarted
}

// WithDB 为该实例注入独立的数据库句柄(测试替身、多数据源场景)。
// 不注入时使用全局单例(InitDB)。
func (sm *ServiceManager[T]) WithDB(db *gorm.DB) *ServiceManager[T] {
	sm.db = db
	return sm
}

// WithRedis 为该实例注入独立的 Redis 客户端(测试替身、多实例场景)。
// 不注入时使用全局单例(InitRedis)。
func (sm *ServiceManager[T]) WithRedis(client *redis.Client) *ServiceManager[T] {
	sm.redisClient = client
	return sm
}

// DB 返回该实例使用的数据库句柄:优先实例注入,回退全局单例
func (sm *ServiceManager[T]) DB() *gorm.DB {
	if sm.db != nil {
		return sm.db
	}
	return GetDB()
}

// Redis 返回该实例使用的 Redis 客户端:优先实例注入,回退全局单例
func (sm *ServiceManager[T]) Redis() *redis.Client {
	if sm.redisClient != nil {
		return sm.redisClient
	}
	return GetRedis()
}

func getTypeName[T any](value T) string {
	var zero T
	t := reflect.TypeOf(zero)
	// 如果 T 是指针，剥一层
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t.Name()
}

// NewServiceManager 创建一个新的 ServiceManager 实例
// 通过reflect获取名字自动赋值给ResourceName和TableName还有keyname
func NewServiceManager[T any](resource T) *ServiceManager[T] {
	return &ServiceManager[T]{
		Resource:      resource,
		ResourceName:  getTypeName(resource),
		TableName:     getTypeName(resource),
		Schema:        "public",
		CacheKeyType:  "none",
		CacheKeyName:  getTypeName(resource) + "_key",
		asyncTasks:    make(chan asyncCacheTask[T], 256),
		asyncShutdown: make(chan struct{}),
	}
}
