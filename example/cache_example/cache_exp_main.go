package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/Super-Gagaga/AbstractManager/example/cache_example/model"
	"github.com/Super-Gagaga/AbstractManager/http_router"
	"github.com/Super-Gagaga/AbstractManager/service"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
)

////////////////////////////////////////////////////////////////////////////////
//                               环境初始化层
// 只负责加载运行环境（如 .env），不参与任何业务或基础设施构建
////////////////////////////////////////////////////////////////////////////////

func initEnv() {
	// 开发环境下加载 .env 文件
	// 生产环境通常由系统环境变量注入
	_ = godotenv.Load()
	user := os.Getenv("DB_USER")
	host := os.Getenv("DB_HOST")
	port := os.Getenv("DB_PORT")
	name := os.Getenv("DB_NAME")

	// 只打印非敏感配置;密码等凭据任何环境都不应输出到日志
	log.Printf("DB_USER: %s", user)
	log.Printf("DB_HOST: %s", host)
	log.Printf("DB_PORT: %s", port)
	log.Printf("DB_NAME: %s", name)
}

////////////////////////////////////////////////////////////////////////////////
//                            基础设施初始化层（Infra）
// 负责数据库、缓存等"外部资源"的创建与销毁
// ❗ 不包含任何业务逻辑
////////////////////////////////////////////////////////////////////////////////

func initInfra() (*service.DBManager, *service.RedisManager) {
	// 初始化数据库连接管理器
	dbManager, err := service.InitDB()
	if err != nil {
		log.Fatalf("Failed to init database: %v", err)
	}

	// 初始化 Redis 管理器
	redisManager, err := service.InitRedis()
	if err != nil {
		log.Fatalf("Failed to init redis: %v", err)
	}

	return dbManager, redisManager
}

// 统一关闭基础设施资源，保证程序退出时不泄漏连接
func closeInfra(db *service.DBManager, redis *service.RedisManager) {
	if db != nil {
		if err := db.Close(); err != nil {
			log.Printf("Failed to close database: %v", err)
		}
	}

	if redis != nil {
		if err := redis.Close(); err != nil {
			log.Printf("Failed to close redis: %v", err)
		}
	}
}

////////////////////////////////////////////////////////////////////////////////
//                              Service 初始化层
// Service 是业务核心：
// - 知道 Model
// - 不知道 HTTP
// - 不知道 Gin / Router
////////////////////////////////////////////////////////////////////////////////

func initServices() *service.ServiceManager[model.User] {
	// 创建 User 对应的 ServiceManager
	userSvc := service.NewServiceManager(model.User{})

	// 示例中直接确保表存在
	// 实际生产环境建议用 migration 工具
	ctx := context.Background()
	if err := userSvc.Create(ctx, &service.CreateOptions{
		IfNotExists: true,
	}); err != nil {
		log.Printf("Warning: failed to ensure user table: %v", err)
	}

	return userSvc
}

////////////////////////////////////////////////////////////////////////////////
//                               Router 初始化层
// 负责把 Service "暴露"为 HTTP API
// 这里是示例代码最重要的阅读入口
////////////////////////////////////////////////////////////////////////////////

func initRouter(userSvc *service.ServiceManager[model.User]) *gin.Engine {
	// Gin Engine 是整个 HTTP 层的根
	r := gin.Default()

	// 注册用户相关的写入接口
	registerUserWriteRoutes(r, userSvc)

	// 注册用户相关的查询接口
	registerUserLookupRoutes(r, userSvc)

	return r
}

////////////////////////////////////////////////////////////////////////////////
//                          写入路由（Writedown）
// 典型用途：
// - 创建
// - 更新
// - 删除
// - 写缓存
////////////////////////////////////////////////////////////////////////////////

func registerUserWriteRoutes(
	r *gin.Engine,
	userSvc *service.ServiceManager[model.User],
) {
	// 用户 API 的统一前缀
	group := r.Group("/api/v1/users")

	// 创建写入路由组（绑定 Service）
	writedownRg := http_router.NewWritedownRouterGroup(
		group,
		userSvc,
	)

	// 注册写入相关路由，最终路径形如：
	// POST /api/v1/users/cache/xxx
	writedownRg.RegisterRoutes("/cache")
}

////////////////////////////////////////////////////////////////////////////////
//                          查询路由（Lookup）
// 典型用途：
// - 单条查询
// - 条件查询
// - 缓存聚合查询
////////////////////////////////////////////////////////////////////////////////

func registerUserLookupRoutes(
	r *gin.Engine,
	userSvc *service.ServiceManager[model.User],
) {
	group := r.Group("/api/v1/users")

	// 创建查询路由组
	lookupRg := http_router.NewLookupRouterGroup(group, userSvc)

	// 配置默认值和自定义过滤器
	lookupRg.
		SetDefaults("user:*", 24*time.Hour). // 设置默认 key 模式和缓存时间
		SetCustomFilter(activeUserFilter)    // 设置活跃用户过滤器

	// 注册路由
	lookupRg.RegisterRoutes("/lookup")
}

// //////////////////////////////////////////////////////////////////////////////
//
//	业务回调示例：活跃用户筛选逻辑
//
// 这是一个"可插拔"的业务函数：
// - Router 只负责调用
// - 业务逻辑完全独立
// //////////////////////////////////////////////////////////////////////////////
// activeUserFilter 筛选出 status == "active" 的活跃用户 key
// 参数：
//   - ctx: 上下文，用于超时控制和取消
//   - client: Redis 客户端
//   - keys: 要检查的 Redis key 列表（通常是 "user:123" 这种格式）
//
// 注意：WritedownRouterGroup 写入缓存时用的是 SET(JSON 字符串)，
// 所以这里要用 GET 拿到 JSON 后在内存中反序列化筛选，
// 而不是 HGET——对 string 类型的 key 执行 HGET 会返回 WRONGTYPE 错误
func activeUserFilter(
	ctx context.Context,
	client *redis.Client,
	keys []string,
) ([]string, error) {
	if len(keys) == 0 {
		return nil, nil
	}

	// Pipeline 批量提交所有 GET 命令：原本 N 次网络往返合并为 1 次
	pipe := client.Pipeline()
	getCmds := make(map[string]*redis.StringCmd, len(keys))
	for _, key := range keys {
		getCmds[key] = pipe.Get(ctx, key)
	}

	// 某些 key 不存在时 Exec 会返回 redis.Nil，属于正常情况，放行
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, fmt.Errorf("pipeline exec failed: %w", err)
	}

	var activeKeys []string
	for key, cmd := range getCmds {
		raw, err := cmd.Bytes()
		if err != nil {
			// key 不存在或已过期，跳过
			continue
		}

		var user model.User
		if err := json.Unmarshal(raw, &user); err != nil {
			log.Printf("warn: failed to unmarshal %s: %v", key, err)
			continue
		}

		if user.Status == "active" {
			activeKeys = append(activeKeys, key)
		}
	}

	return activeKeys, nil
}

////////////////////////////////////////////////////////////////////////////////
//                         Gin Server 生命周期管理
// 使用 Gin 的 Run 作为示例入口，降低理解成本，这里仅作示例而不是生产用的代码
////////////////////////////////////////////////////////////////////////////////

func runGinServer(r *gin.Engine) {
	addr := ":" + getEnvOrDefault("PORT", "8080")
	log.Printf("Server starting on http://localhost%s", addr)

	// 直接运行，Ctrl+C 会自动尝试优雅关闭
	if err := r.Run(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

////////////////////////////////////////////////////////////////////////////////
//                                    main
// main 只负责"组织流程"，不承载任何具体实现细节
////////////////////////////////////////////////////////////////////////////////

func main() {
	// 1. 加载环境
	initEnv()

	// 2. 初始化基础设施
	dbManager, redisManager := initInfra()
	defer closeInfra(dbManager, redisManager)

	// 3. 初始化业务 Service
	userSvc := initServices()

	// 4. 构建 Router
	router := initRouter(userSvc)

	// 5. 启动 HTTP Server
	runGinServer(router)
}

////////////////////////////////////////////////////////////////////////////////
//                               工具函数
////////////////////////////////////////////////////////////////////////////////

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
