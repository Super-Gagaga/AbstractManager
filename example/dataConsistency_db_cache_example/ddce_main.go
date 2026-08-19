package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Super-Gagaga/AbstractManager/example/dataConsistency_db_cache_example/model"
	"github.com/Super-Gagaga/AbstractManager/http_router"
	"github.com/Super-Gagaga/AbstractManager/service"
	"github.com/Super-Gagaga/AbstractManager/util"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

// --- 环境与基础设施初始化 ---

func initEnv() {
	if err := godotenv.Load(); err != nil {
		log.Printf("WARNING: .env file not loaded: %v (using system env vars only)", err)
	}
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

func initServices() *service.ServiceManager[model.User] {
	userSvc := service.NewServiceManager(model.User{})
	if err := userSvc.Create(context.Background(), &service.CreateOptions{IfNotExists: true}); err != nil {
		log.Printf("WARNING: auto-create table failed (may already exist): %v", err)
	}
	return userSvc
}

// --- Router 注册 ---

func initRouter(userSvc *service.ServiceManager[model.User]) *gin.Engine {
	r := gin.Default()
	group := r.Group("/api/v1/users")

	// Writedown 路由
	http_router.NewWritedownRouterGroup(group, userSvc).RegisterRoutes("/cache")

	// Lookup 路由（Cache Aside 模式）
	lookupRg := http_router.NewLookupRouterGroup(group, userSvc)
	lookupRg.SetDefaults("user:*", getCacheAsideTTL())
	lookupRg.SetCacheAsideConfig(getCacheAsideTTL(), getCacheHitRefresh())
	lookupRg.RegisterRoutes("/lookup")

	return r
}

// --- 批量同步处理器 ---

type CacheToDBRequest struct {
	KeyPattern       string `json:"key_pattern" binding:"required"`
	BatchSize        int    `json:"batch_size"`
	ConflictStrategy string `json:"conflict_strategy"`
	RecacheAfterSync bool   `json:"recache_after_sync"`
}

type CacheToDBResult struct {
	TotalScanned  int           `json:"total_scanned"`
	TotalSynced   int           `json:"total_synced"`
	RecachedItems int           `json:"recached_items"`
	Duration      time.Duration `json:"duration"`
	Mode          string        `json:"mode"`
}

// --- 核心同步逻辑 ---
func syncCacheToDatabase(
	ctx context.Context,
	userSvc *service.ServiceManager[model.User],
	req *CacheToDBRequest,
) (*CacheToDBResult, error) {
	startTime := time.Now()
	if req.BatchSize <= 0 {
		req.BatchSize = 500
	}

	// Step 1: 从 Redis 批量获取数据
	userMap, err := userSvc.LookupQueryByPattern(ctx, req.KeyPattern, &service.LookupQueryOptions{
		FallbackToDB: false,
	})
	if err != nil {
		return nil, fmt.Errorf("lookup failed: %w", err)
	}

	users := make([]model.User, 0, len(userMap))
	for _, u := range userMap {
		if u != nil {
			users = append(users, *u)
		}
	}

	if len(users) == 0 {
		return &CacheToDBResult{
			Duration: time.Since(startTime),
			Mode:     "no_data",
		}, nil
	}

	// Step 2: 批量写入数据库
	err = userSvc.SetQuery(ctx, users, &service.SetQueryOptions{
		BatchSize:        req.BatchSize,
		OnConflictUpdate: req.ConflictStrategy != "skip",
		InvalidateCache:  false,
	})
	if err != nil {
		return nil, fmt.Errorf("db write failed: %w", err)
	}

	result := &CacheToDBResult{
		TotalScanned: len(userMap),
		TotalSynced:  len(users),
		Duration:     time.Since(startTime),
		Mode:         "cache_aside",
	}

	// Step 3: Cache Aside 模式 - 落库后重新缓存
	if req.RecacheAfterSync {
		recached, err := recacheUsers(ctx, users, getCacheAsideTTL())
		if err != nil {
			log.Printf("Recache warning: %v", err)
		} else {
			result.RecachedItems = recached
			log.Printf("Synced %d items, recached with TTL %v", len(users), getCacheAsideTTL())
		}
	} else {
		log.Printf("Synced %d items to DB", len(users))
	}

	return result, nil
}

// recacheUsers 重新缓存用户数据
func recacheUsers(ctx context.Context, users []model.User, ttl time.Duration) (int, error) {
	rdb := service.GetRedis()
	pipe := rdb.Pipeline()

	for _, user := range users {
		key := fmt.Sprintf("user:%d", user.ID)
		jsonData, err := json.Marshal(user)
		if err != nil {
			log.Printf("Marshal error for user %d: %v", user.ID, err)
			continue
		}
		pipe.Set(ctx, key, jsonData, ttl)
	}

	_, err := pipe.Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("pipeline exec failed: %w", err)
	}

	return len(users), nil
}

// --- 配置辅助函数（委托到共享 util 包）---

func getCacheAsideTTL() time.Duration {
	return util.GetCacheAsideTTL()
}

func getCacheHitRefresh() bool {
	return util.GetCacheHitRefresh()
}

func getEnvOrDefault(key, defaultValue string) string {
	return util.GetEnvOrDefault(key, defaultValue)
}

// --- 定时同步任务 ---

func startPeriodicSync(ctx context.Context, userSvc *service.ServiceManager[model.User]) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			log.Println("🔄 Auto sync...")

			result, err := syncCacheToDatabase(ctx, userSvc, &CacheToDBRequest{
				KeyPattern:       "user:*",
				ConflictStrategy: "upsert",
				RecacheAfterSync: false, // 落库后不重新缓存，避免无限刷新 TTL
			})

			if err != nil {
				log.Printf("❌ Sync failed: %v", err)
			} else if result.TotalSynced > 0 {
				log.Printf("Synced: %d items, took: %v (no recache)",
					result.TotalSynced, result.Duration)
			}
		case <-ctx.Done():
			return
		}
	}
}

// --- Main ---

func main() {
	initEnv()
	db, redis := initInfra()
	defer db.Close()
	defer redis.Close()

	userSvc := initServices()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go startPeriodicSync(ctx, userSvc)

	router := initRouter(userSvc)
	addr := ":" + getEnvOrDefault("PORT", "8080")

	log.Println("================================")
	log.Println("📌 Cache Aside Mode")
	log.Printf("   Cache TTL: %v", getCacheAsideTTL())
	log.Printf("   Hit Refresh: %v", getCacheHitRefresh())
	log.Println("   Sync Interval: 10s")
	log.Println("================================")
	log.Printf("Server: %s", addr)

	// 创建 http.Server 以支持优雅关闭
	srv := &http.Server{
		Addr:    addr,
		Handler: router,
	}

	// 在独立 goroutine 中启动服务
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server fatal: %v", err)
		}
	}()

	// 等待中断信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("🛑 Shutting down gracefully...")

	// 通知后台任务停止
	cancel()

	// 给 HTTP 服务 5 秒完成当前请求
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
	}

	log.Println("✅ Server stopped")
}
