// db_example 演示框架的数据库侧能力:
//
//	WriteRouterGroup  → 12 个数据库写入端点(单条/批量、Upsert、原子增减、软删除)
//	QueryRouterGroup  → 方法化分页查询(list / active_list / search / 自定义)、按 ID 查询、计数
//
// 缓存侧(WritedownRouterGroup / LookupRouterGroup)见 cache_example;
// 缓存与数据库的定时同步见 dataConsistency_db_cache_example。
package main

import (
	"context"
	"log"
	"os"

	"github.com/Super-Gagaga/AbstractManager/example/db_example/model"
	"github.com/Super-Gagaga/AbstractManager/http_router"
	"github.com/Super-Gagaga/AbstractManager/service"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"gorm.io/gorm"
)

////////////////////////////////////////////////////////////////////////////////
//                          环境与基础设施初始化层
// 与其他示例一致:加载 .env → 连接 MySQL / Redis(缓存池虽不直接使用,
// 但 WriteRouterGroup 的 invalidate_cache 选项会操作缓存)
////////////////////////////////////////////////////////////////////////////////

func initEnv() {
	_ = godotenv.Load()
}

func initInfra() (*service.DBManager, *service.RedisManager) {
	db, err := service.InitDB()
	if err != nil {
		log.Fatalf("Failed to init database: %v", err)
	}
	redis, err := service.InitRedis()
	if err != nil {
		log.Fatalf("Failed to init redis: %v", err)
	}
	return db, redis
}

////////////////////////////////////////////////////////////////////////////////
//                            Service 初始化层
////////////////////////////////////////////////////////////////////////////////

func initServices() *service.ServiceManager[model.Product] {
	productSvc := service.NewServiceManager(model.Product{})

	// AutoMigrate 建表;生产环境建议用独立 migration 工具
	if err := productSvc.Create(context.Background(), &service.CreateOptions{
		IfNotExists: true,
	}); err != nil {
		log.Printf("Warning: failed to ensure product table: %v", err)
	}

	return productSvc
}

// seedProducts 启动时写入一批示例数据(OnConflictUpdate 保证重复启动幂等)
func seedProducts(productSvc *service.ServiceManager[model.Product]) {
	seed := []model.Product{
		{Name: "T-Shirt", Category: "apparel", Price: 59.9, Stock: 120, Status: "active"},
		{Name: "Jeans", Category: "apparel", Price: 199.0, Stock: 80, Status: "active"},
		{Name: "Sneakers", Category: "footwear", Price: 399.0, Stock: 45, Status: "active"},
		{Name: "Sandals", Category: "footwear", Price: 89.0, Stock: 0, Status: "inactive"},
		{Name: "Coffee Beans", Category: "food", Price: 45.5, Stock: 300, Status: "active"},
		{Name: "Chocolate", Category: "food", Price: 12.9, Stock: 500, Status: "inactive"},
	}

	err := productSvc.SetQuery(context.Background(), seed, &service.SetQueryOptions{
		BatchSize:        100,
		OnConflictUpdate: true, // 名字冲突时更新其余字段
		InvalidateCache:  false,
	})
	if err != nil {
		log.Printf("Warning: seed products failed: %v", err)
	} else {
		log.Printf("Seeded %d products", len(seed))
	}
}

////////////////////////////////////////////////////////////////////////////////
//                            Router 初始化层
// 一个 ServiceManager 可以同时挂多个路由组;Write 与 Query 的端点路径
// 互不冲突,因此都直接注册在 /api/v1/products 之下
////////////////////////////////////////////////////////////////////////////////

func initRouter(productSvc *service.ServiceManager[model.Product]) *gin.Engine {
	r := gin.Default()
	group := r.Group("/api/v1/products")

	// ---- 数据库写入:POST /set、PUT /update、DELETE /delete、
	// POST /batch/set 等 12 个端点(完整清单见 README「数据库写入」一节)
	writeRg := http_router.NewWriteRouterGroup(group, productSvc)
	writeRg.RegisterRoutes("")

	// ---- 数据库查询:POST /query、GET /:id、POST /count
	queryRg := http_router.NewQueryRouterGroup(group, productSvc)

	// 注册内置查询方法:按 created_at 倒序、每页 20 条
	//   list         全量列表
	//   active_list  仅 status = 'active' 且未软删
	//   search       关键字搜索
	queryRg.RegisterCommonMethods(20)

	// 注册自定义查询方法:预置 FilterFunc + 排序,请求时按名称调用
	// FilterFunc 与请求里的 filters 是 AND 关系,适合放"业务固定条件"
	queryRg.RegisterMethod(
		"cheap", // POST /query {"method": "cheap", ...}
		20,
		func(db *gorm.DB) *gorm.DB {
			return db.Where("price <= ?", 100)
		},
		"price", "ASC",
	)

	queryRg.RegisterRoutes("")

	return r
}

////////////////////////////////////////////////////////////////////////////////
//                                    main
////////////////////////////////////////////////////////////////////////////////

func main() {
	initEnv()

	db, redis := initInfra()
	defer func() {
		if err := db.Close(); err != nil {
			log.Printf("Failed to close database: %v", err)
		}
		if err := redis.Close(); err != nil {
			log.Printf("Failed to close redis: %v", err)
		}
	}()

	productSvc := initServices()
	seedProducts(productSvc)

	addr := ":" + getEnvOrDefault("PORT", "8080")

	// 启动时打印一份 curl 速查表,方便直接复制体验
	const cheatSheet = `
  curl -X POST localhost:8080/api/v1/products/set \
     -H 'Content-Type: application/json' \
     -d '{"data":{"name":"Cap","category":"apparel","price":35,"stock":50,"status":"active"}}'

  curl -X POST localhost:8080/api/v1/products/query -d '{"method":"cheap","page":1}'
  curl -X POST localhost:8080/api/v1/products/query -d '{"method":"list","page":1,"filters":[{"field":"category","operator":"=","value":"food"}]}'
  curl localhost:8080/api/v1/products/1
  curl -X POST localhost:8080/api/v1/products/count -d '{"filters":[{"field":"status","operator":"=","value":"active"}]}'
  curl -X POST localhost:8080/api/v1/products/increment -d '{"id":1,"column":"stock","value":10}'
  curl -X DELETE 'localhost:8080/api/v1/products/delete?id=6&soft=true'
`
	log.Println("==== DB example: WriteRouterGroup + QueryRouterGroup ====")
	log.Print("Try:" + cheatSheet + "\n")
	log.Printf("Server starting on http://localhost%s", addr)

	if err := initRouter(productSvc).Run(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
