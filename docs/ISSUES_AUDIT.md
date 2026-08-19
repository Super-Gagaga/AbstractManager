# AbstractManager 代码审计问题清单

> 审计日期：2026-06-06（2026-08-19 复审计）
> 审计范围：全项目（`service/` `http_router/` `util/` `example/` `tests/`）
> 问题总数：20
> 已解决：20 / 部分解决：0 / 未解决：0（#14 需在 GitHub 侧同步仓库改名方可生效）

---

## 严重度说明

| 级别 | 含义 |
|------|------|
| 🔴 **严重** | 直接导致生产事故/安全事故/数据丢失 |
| 🟠 **高危** | 在特定条件下触发严重问题 |
| 🟡 **中等** | 影响代码质量、可维护性、性能 |
| 🔵 **建议** | 设计层面的改进建议 |

---

## 🔴 严重

### 1. `KEYS` 命令阻塞 Redis —— 生产事故

| 属性 | 内容 |
|------|------|
| **文件** | [http_router/cache_get_router_group.go#L144](http_router/cache_get_router_group.go#L144) 、 [service/lookup_query.go#L282](service/lookup_query.go#L282) |
| **状态** | ✅ 已解决 |

**问题：**

```go
func (lrg *LookupRouterGroup[T]) executeLookup(...) (...) {
    redisClient := service.GetRedis()
    allKeys, err := redisClient.Keys(ctx, keyPattern).Result() // ← KEYS 是 O(N) 阻塞命令
```

`KEYS` 在 Redis 中是**单线程阻塞遍历整个 keyspace** 的命令。生产环境百万级 key 时会让 Redis 完全卡死不响应其他请求，造成服务雪崩。

相同项目里 `ServiceManager.LookupQueryByPattern()` 已经正确使用了 `SCAN` 游标遍历（[service/lookup_query.go#L95](service/lookup_query.go#L95)），但 `executeLookup` 却没用。

**建议修复：**

```go
// 使用 SCAN 代替 KEYS
func scanAllKeys(ctx context.Context, client *redis.Client, pattern string) ([]string, error) {
    var allKeys []string
    var cursor uint64
    for {
        keys, nextCursor, err := client.Scan(ctx, cursor, pattern, 100).Result()
        if err != nil {
            return nil, fmt.Errorf("scan failed: %w", err)
        }
        allKeys = append(allKeys, keys...)
        cursor = nextCursor
        if cursor == 0 {
            break
        }
    }
    return allKeys, nil
}
```

---

### 2. SQL 注入 —— 安全漏洞

| 属性 | 内容 |
|------|------|
| **文件** | [service/set_query.go](service/set_query.go) 、 [service/get_query.go](service/get_query.go) 、 [util/filter_translator/grom_filter.go](util/filter_translator/grom_filter.go) 、 [service/set_single.go](service/set_single.go) |
| **状态** | ✅ 已解决（2026-08-19，单条 Increment/Decrement 校验补齐） |

**问题：**

```go
// BatchIncrement / BatchDecrement
result := tx.Model(&sm.Resource).UpdateColumn(column,
    gorm.Expr(fmt.Sprintf("%s + ?", column), value)) // column 直接拼入 SQL

// applyQueryOptions
db = db.Group(opts.Group)                           // 直接拼入
db = db.Order(fmt.Sprintf("%s %s", opts.OrderBy, order)) // 直接拼入
db = db.Having(key, value)                          // key 直接拼入
```

`column`、`OrderBy`、`Group`、`Having` 的 key 都来自 HTTP 请求参数，可以注入任意 SQL 片段（例如 `column = "1; DROP TABLE users;--"`）。

**建议修复：**

```go
// 对 column 做白名单校验
var allowedColumns = map[string]bool{"id": true, "age": true, "status": true, "score": true}

func (sm *ServiceManager[T]) BatchIncrement(ctx context.Context, column string, value interface{}, ...) (int64, error) {
    if !allowedColumns[column] {
        return 0, fmt.Errorf("invalid column: %s", column)
    }
    // ... 然后再拼接
}
```

或者直接用 GORM 的安全方法 `db.UpdateColumn(clause.Column{Name: column}, ...)` 。

**⚠️ 遗留（2026-08-19 复审计，同日修复）：**

单条记录的 `Increment` / `Decrement` 曾缺少列名校验，且经由 HTTP 路由暴露：

- [service/set_single.go](service/set_single.go)：`gorm.Expr(fmt.Sprintf("%s + ?", column))` 直接拼接 `column`
- [http_router/set_router_group.go](http_router/set_router_group.go)：`POST /increment` 路由把请求体里的 `req.Column` 透传进来，仅做了非空校验

批量版 `BatchIncrement` / `BatchDecrement`（[service/set_query.go](service/set_query.go)）已有 `ValidateSQLIdentifier`，单条版已按相同方式补齐：

```go
func (sm *ServiceManager[T]) Increment(ctx context.Context, column string, value interface{}, ...) error {
    if err := filter_translator.ValidateSQLIdentifier(column); err != nil {
        return fmt.Errorf("invalid column name %q: %w", column, err)
    }
    // ...
}
```

**修复验证：** [tests/unit/increment_validation_test.go](../tests/unit/increment_validation_test.go) 覆盖 5 种恶意列名在 Increment / Decrement 入口被拒（校验先于任何 DB 访问）；[tests/router/write_router_test.go](../tests/router/write_router_test.go) 验证 `POST /increment` 携带恶意列名时不触达数据库即返回错误。

---

### 3. Key 硬编码 `user:` —— 泛型框架假象

| 属性 | 内容 |
|------|------|
| **文件** | [http_router/cache_get_router_group.go#L260](http_router/cache_get_router_group.go#L260) |
| **状态** | ✅ 已解决 |

**问题：**

```go
// loadFromDBAndCache 中：
key := fmt.Sprintf("user:%d", uint(id))  // ← 硬编码了 "user:"
```

`LookupRouterGroup[T]` 是泛型的，`T` 可以是 `Product`、`Order`、任何模型。但 `loadFromDBAndCache` 里 key 前缀写死了 `user:`。

当你用这个框架管理 `Product` 时：
- 缓存 key 被写成 `user:123` 而不是 `product:123`
- `LookupRouterGroup[T]` 里的 `extractIDFromKey` 倒是泛型适用，但 `loadFromDBAndCache` 不是

**建议修复：**

```go
// 根据 T 的类型名动态生成前缀
func (lrg *LookupRouterGroup[T]) loadFromDBAndCache(...) (...) {
    typeName := reflect.TypeOf(lrg.Service.Resource).Name()
    prefix := strings.ToLower(typeName)
    // ...
    key := fmt.Sprintf("%s:%d", prefix, uint(id))
}
```

或者直接让 `LookupRouterGroup` 的配置支持 `keyPrefix` 字段。

---

### 4. 零测试覆盖

| 属性 | 内容 |
|------|------|
| **文件** | 全项目 |
| **状态** | ✅ 已解决 (2026-06-07) |

**修复：**

现已添加完整测试套件（约 230 个测试函数 + 52 个基准测试），使用 `miniredis` 做 Redis mock、`sqlmock` 做 DB mock：

| 测试目录/文件 | 覆盖范围 |
|----------|---------|
| [tests/unit/](../../tests/unit/) | cache_key_builder、env、filter_translator、service、logger 单元测试 |
| [tests/router/](../../tests/router/) | HTTP 路由：query / cache / write / middleware |
| [tests/race_perf/](../../tests/race_perf/) | 竞态检测（12 个 `TestRace_*`）+ 性能基准（52 个 `Benchmark*`）|
| [tests/integration/](../../tests/integration/) | 集成测试 |
| [tests/testutil/](../../tests/testutil/) | 测试替身 / 录制 fixture |
| [service/extract_test.go](../../service/extract_test.go) | extractID / buildCacheKey |
| [service/query_options_test.go](../../service/query_options_test.go) | QueryOptions 安全校验 |
| [util/context_test.go](../../util/context_test.go) | EnsureTimeout 超时兜底 |
| [util/filter_translator/tofloat64_test.go](../../util/filter_translator/tofloat64_test.go) | toFloat64 转换 |

---

### 5. 全局单例 —— 不可测试、不可扩展

| 属性 | 内容 |
|------|------|
| **文件** | [service/sql_pool.go](service/sql_pool.go) 、 [service/cache_pool.go](service/cache_pool.go) 、 [service/service_model.go](service/service_model.go) |
| **状态** | ✅ 已解决（2026-08-19，非破坏性注入方案） |

**问题：**

```go
var globalDBManager *DBManager       // sql_pool.go
var globalRedisManager *RedisManager // cache_pool.go

func GetDB() *gorm.DB { return globalDBManager.DB }      // 到处都用
func GetRedis() *redis.Client { return globalRedisManager.Client } // 到处都用
```

后果：
- 一个进程只能连一个 DB 和一个 Redis
- 单元测试完全无法 mock 数据库/缓存依赖
- 并发场景下 `InitDB()` 被多次调用没有保护
- 所有 `ServiceManager` 方法都隐形依赖全局状态，调用者根本不知道

**修复（2026-08-19）：**

未采用"修改 `NewServiceManager` 构造函数签名"的破坏性方案，改为**实例级可选注入 + 全局回退**，对现有调用方零破坏：

- [service/service_model.go](service/service_model.go) — `ServiceManager` 新增私有 `db` / `redisClient` 字段与链式注入方法 `WithDB(db)` / `WithRedis(client)`；`DB()` / `Redis()` 访问器优先返回实例注入，未注入时回退全局单例
- `service` 全部内部调用点（约 50 处 `GetDB()` / `GetRedis()`）已切换为 `sm.DB()` / `sm.Redis()`；`GetRedisManager()` 同样感知实例注入
- [http_router/cache_get_router_group.go](http_router/cache_get_router_group.go) 的 3 处 `service.GetRedis()` 改走 `lrg.Service.Redis()`，路由组随 ServiceManager 一起支持多实例
- 全局侧补齐对称注入点：`SetGlobalRedis(client)` 与 `SetGlobalDB(db)` 对称，且传 nil 可恢复未初始化状态，便于测试隔离

**效果：** 可测试性——`tests/unit/di_test.go` 演示了在全局 DB/Redis 均未初始化的进程里，通过 `WithDB`(sqlmock) / `WithRedis`(miniredis) 独立驱动实例；可扩展性——一个进程可以按模型分别注入不同的 DB/Redis。全局单例保留为默认 convenience 路径（`InitDB` / `InitRedis` + 回退），审计原建议"构造函数强制注入"被判定为不必要的破坏性变更。

---

## 🟠 高危

### 6. 启动错误被静默吞掉

| 属性 | 内容 |
|------|------|
| **文件** | [example/dataConsistency_db_cache_example/ddce_main.go#L22](example/dataConsistency_db_cache_example/ddce_main.go#L22) 、 [http_router/cache_get_router_group.go#L314](http_router/cache_get_router_group.go#L314) 、 [service/writedown_query.go#L77-L79](service/writedown_query.go#L77-L79) |
| **状态** | ✅ 已解决 |

**问题：**

```go
_ = godotenv.Load()               // .env 加载失败 → 静默继续，DB_USER 全是空
redisClient.Expire(ctx, key, ...) // Expire 返回值丢弃，TTL 设置失败也不知道
_ = router.Run(addr)              // 服务启动失败不处理
```

**建议修复：**

```go
if err := godotenv.Load(); err != nil {
    log.Printf("WARNING: .env file not loaded: %v", err)
}
// router.Run 返回 err 必须处理
if err := router.Run(addr); err != nil {
    log.Fatalf("Server fatal: %v", err)
}
```

---

### 7. 无 Graceful Shutdown

| 属性 | 内容 |
|------|------|
| **文件** | [example/dataConsistency_db_cache_example/ddce_main.go#L220-L245](example/dataConsistency_db_cache_example/ddce_main.go#L220-L245) |
| **状态** | ✅ 已解决 |

**问题：**

```go
func main() {
    // ...
    go startPeriodicSync(ctx, userSvc)  // 后台 goroutine
    _ = router.Run(addr)                // 阻塞，kill 时直接死掉
}
```

不存在 `signal.Notify`、`http.Server.Shutdown`。被杀掉时：
- 正在进行的缓存 Pipeline 操作丢失
- 数据库事务可能没有回滚/提交
- 后台定时任务 goroutine 残留

**建议修复：**

```go
func main() {
    // ...
    srv := &http.Server{Addr: addr, Handler: router}
    go func() {
        if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            log.Fatalf("listen: %s", err)
        }
    }()

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit

    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    srv.Shutdown(ctx)
    cancel()  // 通知后台任务停掉
    db.Close()
    redis.Close()
}
```

---

### 8. `GetQuery` 对普通 SELECT 开了事务

| 属性 | 内容 |
|------|------|
| **文件** | [service/get_query.go#L34-L93](service/get_query.go#L34-L93) |
| **状态** | ✅ 已解决 |

**问题：**

```go
func (sm *ServiceManager[T]) GetQuery(ctx context.Context, queryFunc func(*gorm.DB) *gorm.DB, opts *QueryOptions) (*QueryResult[T], error) {
    db := GetDB().WithContext(ctx)
    db = db.Begin()          // 纯只读查询开事务
    defer func() {
        if r := recover(); r != nil {
            db.Rollback()
        }
    }()
    // ... Count + Find ...
    db.Commit()              // 再提交一个没有写操作的事务
```

只读 SELECT 不需要事务，GORM 自带连接池管理。多余的事务：
- 增加数据库开销（事务日志、锁资源）
- Count 到 Find 之间可能读到不一致数据（取决于隔离级别）
- 整体查询变慢

**建议修复：**

直接用 `GetQueryWithoutTransaction` 的逻辑。如果真的需要一致性快照，应该用 `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ` 的只读事务，而不是默认事务。

分隔开 Transaction API 和只读 API，不要混用。

---

### 9. 异步写入的 goroutine 没有生命周期管理

| 属性 | 内容 |
|------|------|
| **文件** | [service/writedown_single.go#L149-L162](service/writedown_single.go#L149-L162) |
| **状态** | ✅ 已解决 |

**问题：**

```go
func (sm *ServiceManager[T]) WritedownSingleAsync(ctx context.Context, key string, data *T, expiration time.Duration) {
    go func() {
        asyncCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()
        if err := sm.WritedownSingle(asyncCtx, key, data, ...); err != nil {
            fmt.Printf("[AsyncCache] Failed for key %s: %v\n", key, err)
        }
    }()
}
```

- Shutdown 时 goroutine 可能还在跑，写入半路连接断开
- `fmt.Printf` 在 HTTP 服务里不是结构化日志，排查困难
- goroutine 数量无限增长（高并发时可能瞬间启动数千个）

**建议修复：**

```go
// 使用 worker pool 或者 buffered channel
type AsyncCacheWriter struct {
    tasks chan cacheTask
    done  chan struct{}
}
// 在 ServiceManager 初始化时创建固定数量的 worker
// Shutdown 时 close(ch) 通知 worker 退出
```

---

### 10. `toFloat64` 静默吞掉转换错误

| 属性 | 内容 |
|------|------|
| **文件** | [util/filter_translator/redis_filter.go#L178-L191](util/filter_translator/redis_filter.go#L178-L191) |
| **状态** | ✅ 已解决 |

**问题：**

```go
func toFloat64(v interface{}) (float64, error) { ... }

// 调用方：
target, _ := toFloat64(f.Value)  // ← error 被丢弃
```

当 `f.Value` 是 `"abc"` 时返回 `(0, error)`，error 被忽略后 `target = 0`。结果是 `age < "abc"` 变成了 `age < 0`，过滤结果全错。

**建议修复：**

```go
func (f *RedisGreaterThanFilter) ApplyRedis(ctx context.Context, client *redis.Client, keys []string) ([]string, error) {
    target, err := toFloat64(f.Value)
    if err != nil {
        return nil, fmt.Errorf("invalid value for > filter on field %s: %w", f.Field, err)
    }
    // ...
}
```

---

### 11. `createIndex` 的 `Unique` 参数完全无效

| 属性 | 内容 |
|------|------|
| **文件** | [service/create.go#L79-L89](service/create.go#L79-L89) |
| **状态** | ✅ 已解决 |

**问题：**

```go
func (sm *ServiceManager[T]) createIndex(db *gorm.DB, idx Index) error {
    // ...
    if idx.Unique {
        return db.Table(tableName).Migrator().CreateIndex(&sm.Resource, idx.Name)
    }
    return db.Table(tableName).Migrator().CreateIndex(&sm.Resource, idx.Name)
    // ↑ 两个分支代码完全相同！
}
```

`Unique: true` 进去跟 `Unique: false` 一样的结果。唯一索引根本没建。

**建议修复：**

```go
if idx.Unique {
    return db.Table(tableName).Migrator().CreateUniqueIndex(&sm.Resource, idx.Columns...)
} else {
    return db.Table(tableName).Migrator().CreateIndex(&sm.Resource, idx.Name)
}
```

---

## 🟡 中等

### 12. 重复函数定义四处粘贴

| 属性 | 内容 |
|------|------|
| **文件** | [http_router/cache_get_router_group.go#L556-L563](http_router/cache_get_router_group.go#L556-L563) 、 [http_router/cache_set_router_group.go#L112-L121](http_router/cache_set_router_group.go#L112-L121) 、 [example/.../ddce_main.go#L169-L176](example/dataConsistency_db_cache_example/ddce_main.go#L169-L176) |
| **状态** | ✅ 已解决 |

**问题：**

`getCacheAsideTTL` 和 `getCacheHitRefresh` 在三个包里各实现了一遍，参数名还不统一（`CACHE_ASIDE_TTL` vs `CACHE_TTL_SECONDS`）。

**建议修复：**

提取到一个公共包 `util/env.go` 中：

```go
package config

func GetCacheAsideTTL() time.Duration { ... }
func GetCacheHitRefresh() bool { ... }
```

---

### 13. `QueryOptions.Having` 类型游离于泛型体系之外

| 属性 | 内容 |
|------|------|
| **文件** | [service/get_query.go#L20](service/get_query.go#L20) |
| **状态** | ✅ 已解决 |

```go
Having map[string]interface{} // 完全丢失类型安全
```

整个框架围绕泛型构建，到 Having 突然变成了 `interface{}`。应该支持与 FilterTranslator 相同的过滤体系。

**修复：** 新增 `HavingCondition` 结构体（含 Field/Operator/Value），`QueryOptions` 新增 `HavingConditions []HavingCondition` 字段，支持 `=`、`>`、`>=`、`<`、`<=`、`!=` 运算符，带 SQL 注入校验。

---

### 14. Go Module 命名不规范

| 属性 | 内容 |
|------|------|
| **文件** | [go.mod#L1](go.mod#L1) |
| **状态** | ✅ 已解决（2026-08-19，待仓库同步改名） |

```go
module github.com/Super-Gagaga/abstract-manager  // 仓库段已小写化
```

**修复（2026-08-19）：**

模块已从 `github.com/Super-Gagaga/AbstractManager` 重命名为 `github.com/Super-Gagaga/abstract-manager`（仓库段小写 + 连字符，符合 Go 惯例；用户名段保持 GitHub 账号规范大小写，与 `github.com/BurntSushi/toml` 同理）。全仓库约 42 处引用（go.mod、import、README、docs）已同步更新，全部测试通过。

**⚠️ 需要配套的仓库操作（代码之外的步骤）：**

1. 在 GitHub 上把仓库重命名为 `abstract-manager`（Settings → Rename）——`go get` 通过 `?go-get=1` 元信息做路径精确匹配（大小写敏感），仓库不改名则新模块路径无法解析；
2. 重命名后打 tag 发布（如 `v0.2.0`）。旧路径 `.../AbstractManager` 的引用会因元信息不匹配而失败，属预期行为——v0.1.0 此前已被 retract，实际上不存在可用的外部消费者，改名窗口成本为零。

---

### 15. 目录名有 typo 且存在两个版本

| 属性 | 内容 |
|------|------|
| **文件** | `example/dataconsistency_db_cache_example/` |
| **状态** | ✅ 已解决 |

```
example/dataConsistency_db_cache_example/  ← 正确的
example/dataconsistency_db_cache_example/  ← 少了个字母 s
```

两个目录同时存在。删掉 typo 版本 `dataconsistency_db_cache_example`。

**修复：** typo 目录实际上不存在于磁盘（仅在 `ddce_main.go` 的 import 中存在拼写错误 `dataconsistency`→`dataConsistency`）。已修正 import 路径。

---

### 16. 批量 Set 后逐个 Expire 的低效实现

| 属性 | 内容 |
|------|------|
| **文件** | [service/writedown_query.go#L77-L79](service/writedown_query.go#L77-L79) |
| **状态** | ✅ 已解决 |

**问题：**

```go
// WritedownQuery 用 MSet（不支持设置过期）后逐个 Expire
redis.MSet(ctx, cacheItems)  // 一次网络往返
for key := range cacheItems {
    redis.Expire(ctx, key, opts.Expiration)  // N 次网络往返
}
```

你已经写了 `WritedownWithPipeline` 的正确实现（用 `pipe.Set(ctx, key, valueBytes, opts.Expiration)`），为什么不统一用它？`WritedownQuery` 比 `WritedownWithPipeline` 慢了 N 倍。

**建议修复：**

直接用 Pipeline + Set（带 TTL），删掉 MSet 版本。

---

### 17. `lookupFromDB` 的 key 构建有问题

| 属性 | 内容 |
|------|------|
| **文件** | [service/lookup_query.go#L171-L218](service/lookup_query.go#L171-L218) |
| **状态** | ✅ 已解决 |

```go
key := fmt.Sprintf("%s:%v", sm.CacheKeyName, item) // ← 把整个 struct 格式化了
```

`item` 是一个 `T` 结构体，`%v` 打印出来是 `{1 alice alice@example.com 25 ...}`，结果 key 变成了 `User_key:{1 alice alice@example.com 25 ...}`，完全不正确。

---

### 18. `InvalidateCacheByPattern` 用了 `KEYS`

| 属性 | 内容 |
|------|------|
| **文件** | [service/lookup_query.go#L282](service/lookup_query.go#L282) |
| **状态** | ✅ 已解决 |

```go
keys, err := redis.Keys(ctx, pattern).Result()
```

同问题 #1。失效缓存时也应该用 `SCAN`。

---

## 🔵 建议

### 19. Context 的超时只出现在初始化阶段，核心逻辑全无保护

> **状态:** ✅ 已解决 (2026-06-07)

`InitDB()` 和 `InitRedis()` 连接时有 5 秒超时，但 `LookupQuery`、`SetQuery`、`WritedownQuery` 等核心方法完全透传用户 ctx，没有在框架层加任何超时。一个慢 SQL 能永远跑下去。

**修复方案：**

1. 新建 [util/context.go](../../util/context.go)，提供 `EnsureTimeout` 辅助函数：
   - 若 ctx 已有 deadline，直接返回（不覆盖调用方设置）
   - 否则添加默认超时作为兜底
   - 超时时间通过环境变量配置：`DB_TIMEOUT_SECONDS`（默认 30s）、`REDIS_TIMEOUT_SECONDS`（默认 10s）、`DDL_TIMEOUT_SECONDS`（默认 60s）

2. 在以下文件的所有核心 I/O 方法入口处调用 `EnsureTimeout`：
   - **DB 操作:** [get_query.go](../../service/get_query.go)、[get_single.go](../../service/get_single.go)、[set_query.go](../../service/set_query.go)、[set_single.go](../../service/set_single.go)、[create.go](../../service/create.go)
   - **Redis 操作:** [writedown_single.go](../../service/writedown_single.go)、[writedown_query.go](../../service/writedown_query.go)、[lookup_query.go](../../service/lookup_query.go)、[lookup_single.go](../../service/lookup_single.go)
   - **HTTP 路由层:** [cache_get_router_group.go](../../http_router/cache_get_router_group.go)

```go
// util/context.go
func EnsureTimeout(ctx context.Context, defaultTimeout time.Duration) (context.Context, context.CancelFunc) {
    if _, ok := ctx.Deadline(); ok {
        return ctx, func() {}  // 已有 deadline，不覆盖
    }
    return context.WithTimeout(ctx, defaultTimeout)
}

// 使用示例 (service/get_query.go)
func (sm *ServiceManager[T]) GetQueryWithoutTransaction(ctx context.Context, ...) {
    ctx, cancel := util.EnsureTimeout(ctx, util.GetDefaultDBTimeout())
    defer cancel()
    // ...
}
```

---

### 20. 日志策略零规划

> **状态:** ✅ 已解决 (2026-08-19)

原问题：全部使用 `log.Printf` / `fmt.Printf` 混打，没有结构化日志、日志级别、trace ID。

**修复方案：** 引入 Go 标准库 `slog`，统一日志体系：

- [util/logger/logger.go](../../util/logger/logger.go) — 统一日志入口，支持 `LOG_LEVEL`（debug/info/warn/error）、`LOG_FORMAT`（text/json），测试进程自动降级静默；`WithRequestID` / `FromContext` 自动携带 request_id
- [http_router/middleware.go](../../http_router/middleware.go) — `RequestLogger` 中间件透传/生成 `X-Request-ID` 并输出结构化访问日志；`Recovery` 替代 `gin.Recovery` 输出带堆栈的 panic 日志
- [service/gorm_logger.go](../../service/gorm_logger.go) — 将 GORM SQL 日志桥接到 slog（`db.error` / `db.slow_query` / `db.sql`），并做 DSN 密码脱敏

核心 service 层错误路径已改用 `logger.FromContext(ctx)` 输出结构化日志（如 [service/writedown_single.go](service/writedown_single.go)、[service/set_query.go](service/set_query.go)）。示例 [ddce_main.go](example/dataConsistency_db_cache_example/ddce_main.go) 中的 `log.Printf` 属应用层示例代码，框架层已统一。

---

## 修复优先级

| 优先级 | 问题编号 | 理由 |
|--------|----------|------|
| P0 立即修 | ~~#1 KEYS~~、~~#3 硬编码key~~、~~#2 SQL注入（批量路径 + 单条 Increment/Decrement）~~ → 全部完成 | 安全问题 + 生产可用性 |
| P1 本周修 | ~~#6 错误静默~~、~~#7 Graceful Shutdown~~、~~#9 goroutine生命周期~~、~~#10 toFloat64~~、~~#11 createIndex~~ → 全部完成 | 线上稳定性基础 |
| P2 本月修 | ~~#4 测试~~、~~#5 全局单例（实例级注入方案）~~ → 已完成 | 可靠性保障 |
| P3 下个迭代 | ~~#14 module命名~~ → 已完成（待 GitHub 仓库同步改名）、~~#20 日志策略~~ → 已完成 | 工程质量 |
| ~~P3 下个迭代~~ | ~~#12-#13~~、~~#15-#18~~、~~#19 context超时~~ → 全部完成 | 工程质量 |

---

## 总结

| 维度 | 评分 |
|------|------|
| 设计思路 | ⭐⭐⭐⭐ |
| 代码质量 | ⭐⭐⭐ |
| 安全性 | ⭐⭐⭐⭐ |
| 可测试性 | ⭐⭐⭐⭐ |
| 生产就绪度 | ⭐⭐⭐⭐ |
| 文档完整度 | ⭐⭐⭐⭐ |

核心矛盾：设计方向正确，早期的硬编码 key、SQL 注入、全局状态等问题已全部修复；全局单例以「实例级可选注入 + 全局回退」的非破坏性方案解决，模块路径已小写化。

**P0（已修复）**：KEYS→SCAN、SQL 注入校验（批量路径 + 过滤/排序/分组）、key 硬编码修复。
**P1（已修复）**：错误静默吞掉、Graceful Shutdown、SELECT 冗余事务、goroutine 生命周期（worker pool）、toFloat64 错误、createIndex Unique 修复。
**P2（已修复）**：测试覆盖（约 230 个测试函数 + 52 个基准测试，分布 `tests/unit`、`tests/router`、`tests/race_perf`、`tests/integration`）；#5 全局单例（`WithDB`/`WithRedis` 实例级注入 + 全局回退，`tests/unit/di_test.go` 覆盖）。
**P3-P4 中等/建议（已修复）**：重复函数提取到 util/env.go、Having 结构化条件、typo import 修正、WritedownQuery 改用 Pipeline、lookupFromDB key 修复、InvalidateCacheByPattern SCAN 化、#20 日志 slog 化、#14 模块重命名为 `github.com/Super-Gagaga/abstract-manager`。
**🔵 建议（已修复）**：#19 context 超时全线覆盖（DB 30s / Redis 10s / DDL 60s 兜底）。

**剩余工作：**

- #14 配套操作（代码外）：GitHub 仓库重命名为 `abstract-manager` 并打 tag 发布，新模块路径方可被 `go get` 解析

审计清单 20 项已全部关闭，无代码侧遗留。
