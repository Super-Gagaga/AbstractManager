package main

import (
	"context"
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
	pass := os.Getenv("DB_PASSWORD")
	host := os.Getenv("DB_HOST")
	port := os.Getenv("DB_PORT")
	name := os.Getenv("DB_NAME")

	log.Printf("DB_USER: %s", user)
	log.Printf("DB_PASSWORD: %s", pass) // 生产环境不要打印密码
	log.Printf("DB_HOST: %s", host)
	log.Printf("DB_PORT: %s", port)
	log.Printf("DB_NAME: %s", name)
}

////////////////////////////////////////////////////////////////////////////////
//                            基础设施初始化层（Infra）
// 负责数据库、缓存等“外部资源”的创建与销毁
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
// 负责把 Service “暴露”为 HTTP API
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
// 这是一个“可插拔”的业务函数：
// - Router 只负责调用
// - 业务逻辑完全独立
// //////////////////////////////////////////////////////////////////////////////
// activeUserFilter 筛选出 status == 1 的活跃用户 key
// 功能：从传入的一堆 Redis key 中，找出那些 status 字段值为 "1" 的 key（代表活跃用户）
// 参数：
//   - ctx: 上下文，用于超时控制和取消
//   - client: Redis 客户端
//   - keys: 要检查的 Redis key 列表（通常是 "user:123" 这种格式）
func activeUserFilter(
	ctx context.Context,
	client *redis.Client,
	keys []string,
) ([]string, error) {

	// 如果传入的 key 列表为空，直接返回 nil, nil
	// 避免无意义的后续操作，也是一种防御性编程
	if len(keys) == 0 {
		return nil, nil
	}

	// 创建一个 Redis Pipeline（管道），用于批量发送命令，减少网络往返次数
	// 这是性能优化的关键：原本要发 N 次请求，现在只发一次 + 一次接收结果
	pipe := client.Pipeline()

	// 用 map 来保存每个 key 对应的 *redis.StringCmd 对象
	// 后面可以通过 key 找到对应命令的结果
	// 容量预设为 len(keys)，避免 map 扩容带来的性能损耗
	statusCmds := make(map[string]*redis.StringCmd, len(keys))

	// 遍历所有传入的 key
	for _, key := range keys {
		// 在 pipeline 中加入一条 HGet 命令：从 Hash 结构中获取 key 对应的 "status" 字段
		// 返回的是 *redis.StringCmd 对象（命令对象，还没执行）
		// 相当于说：“等会儿一起执行时，把这个 key 的 status 取出来”
		statusCmds[key] = pipe.HGet(ctx, key, "status")
	}

	// 执行整个 pipeline（一次性把所有 HGet 命令发给 Redis）
	// _, err := ... 这里的 _ 是忽略返回的命令结果切片，因为我们后面会通过 cmd.Result() 逐个取
	// 如果执行出错，且不是 redis.Nil（redis.Nil 表示某个 key 没找到，但我们允许这种情况）
	// 就返回错误
	_, err := pipe.Exec(ctx)
	if err != nil && err != redis.Nil {
		// 包装错误，带上上下文信息，方便定位问题
		return nil, fmt.Errorf("pipeline exec failed: %w", err)
	}

	// 准备结果切片，用于收集符合条件的活跃用户 key
	var activeKeys []string

	// 遍历刚才保存的每个命令对象
	for key, cmd := range statusCmds {
		// 获取这条 HGet 命令的执行结果
		// status 是字段值（字符串），err 是执行这条命令时的错误
		status, err := cmd.Result()

		if err == redis.Nil {
			// 情况1：key 存在，但 Hash 里没有 "status" 这个字段
			// 我们选择跳过，不认为它是活跃用户
			// （也可以根据业务定义为非活跃或抛异常，这里按宽松处理）
			continue
		}

		if err != nil {
			// 情况2：执行这条命令时出了其他错误（比如网络问题、类型断言失败等）
			// 打印警告日志，但不中断整个函数（容错设计）
			log.Printf("warn: failed to get status for %s: %v", key, err)
			continue
		}

		// 情况3：正常拿到值，且值为 "1" → 说明是活跃用户
		if status == "1" {
			// 把这个 key 加入结果列表
			activeKeys = append(activeKeys, key)
		}
		// 其他值（比如 "0"、"2" 或其他）就直接忽略
	}

	// 最后返回筛选出的活跃用户 key 列表，和 nil 错误
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
// main 只负责“组织流程”，不承载任何具体实现细节
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
