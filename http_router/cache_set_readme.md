# cache set (写入) 模块使用文档

## 一、路由字段说明表

| 路由字段 | 对应 Handler | 入参（示例） | 请求方式 | 出参（示例） |
|---------|--------------|------------|---------|------------|
| `POST /api/v1/{resource}/cache/write` | `HandleWritedownSingle` | `{"key":"cache:user:1","data":{...},"expiration":3600}` 或 `{"key":"cache:user:1","id":1}` | POST | `{"code":0,"message":"success","items_written":1}` |
| `POST /api/v1/{resource}/cache/write-lock` | `HandleWritedownWithLock` | `{"key":"cache:user:1","id":1,"expiration":3600}` | POST | `{"code":0,"message":"success","items_written":1,"data":{...}}` |
| `POST /api/v1/{resource}/cache/write-version` | `HandleWritedownWithVersion` | `{"key":"cache:user:1","data":{...},"version":2,"expiration":3600}` | POST | `{"code":0,"message":"success","items_written":1}` |
| `POST /api/v1/{resource}/cache/refresh` | `HandleRefreshCache` | `{"key":"cache:user:1","id":1,"expiration":3600}` | POST | `{"code":0,"message":"success","items_written":1}` |
| `POST /api/v1/{resource}/cache/batch-write` | `HandleWritedownQuery` | `{"key_template":"cache:user:{id}","data":[...],"expiration":3600}` 或 `{"key_template":"cache:user:{id}","ids":[1,2,3]}` | POST | `{"code":0,"message":"success","items_written":N}` |
| `POST /api/v1/{resource}/cache/warmup` | `HandleWarmupCache` | `{"key_template":"cache:user:{id}","limit":1000,"order_by":"access_count"}` | POST | `{"code":0,"message":"success"}` |

## 二、请求结构说明（简要）

- 单个写入 `WritedownSingleRequest`:
  - `key`: 必填，缓存键
  - `data`: 可选，直接提供要写入的数据
  - `id`: 可选，通过数据库加载数据（与 `data` 二选一）
  - `expiration`: 过期时间（秒），默认 3600
  - `overwrite`/`nx`/`xx`: 控制是否覆盖或仅在存在/不存在时写入
  - `async`: 是否异步写入

- 批量写入 `WritedownQueryRequest`:
  - `data` / `ids` / `load_all`：三选一（直接提供数据、指定 ID 列表或加载全量）
  - `key_template`: 必填，键模板，如 `cache:user:{id}`
  - `expiration`、`batch_size`、`use_pipeline`、`incremental` 等控制写入行为

- 带锁写入 `WritedownWithLockRequest`：用于并发场景，提供 `key` 和 `id`，可设置 `lock_timeout`
- 带版本写入 `WritedownWithVersionRequest`：提供 `data` 与 `version`，用于乐观并发控制
- 预热 `WarmupCacheRequest`：按 `key_template` + 排序/limit 预热缓存

## 三、快速示例代码（注册路由）

```go
package main

import (
    "github.com/gin-gonic/gin"
    "time"

    "github.com/Super-Gagaga/AbstractManager/http_router"
    "github.com/Super-Gagaga/AbstractManager/service"
)

// 假设已有模型 User
func main() {
    r := gin.Default()
    v1 := r.Group("/api/v1")

    userService := service.NewServiceManager(User{})
    // 创建缓存写入路由组
    userW := http_router.NewWritedownRouterGroup(v1, userService)
    // 可选：自定义 KeyBuilder
    // userW.SetKeyBuilder(customBuilder)

    // 注册路由（资源路径）
    userW.RegisterRoutes("/users")

    r.Run(":8080")
}
```

## 四、API 调用示例

### 4.1 单个写入（同步）

请求:
```bash
POST /api/v1/users/cache/write
Content-Type: application/json

{
  "key": "cache:user:1",
  "data": {
    "id": 1,
    "name": "John",
    "age": 30
  },
  "expiration": 3600,
  "overwrite": true
}
```

响应:
```json
{
  "code": 0,
  "message": "success",
  "items_written": 1
}
```

### 4.2 单个写入（通过 ID 从 DB 加载）

请求:
```bash
POST /api/v1/users/cache/write
Content-Type: application/json

{
  "key": "cache:user:1",
  "id": 1,
  "expiration": 3600
}
```

响应同上。

### 4.3 带锁写入（确保串行化）

请求:
```bash
POST /api/v1/users/cache/write-lock
Content-Type: application/json

{
  "key": "cache:user:1",
  "id": 1,
  "expiration": 3600,
  "lock_timeout": 5
}
```

响应:
```json
{ "code": 0, "message": "success", "items_written": 1, "data": { /* 返回写入的数据 */ } }
```

### 4.4 带版本写入（乐观并发）

请求:
```bash
POST /api/v1/users/cache/write-version
Content-Type: application/json

{
  "key": "cache:user:1",
  "data": { "id":1, "name":"John", "age":31 },
  "version": 2,
  "expiration": 3600
}
```

响应:
```json
{ "code": 0, "message": "success", "items_written": 1 }
```

### 4.5 刷新单个缓存（从 DB 重新加载）

请求:
```bash
POST /api/v1/users/cache/refresh
Content-Type: application/json

{
  "key": "cache:user:1",
  "id": 1,
  "expiration": 3600
}
```

响应:
```json
{ "code": 0, "message": "success", "items_written": 1 }
```

### 4.6 批量写入 / 预热

请求（按数据列表写入）:
```bash
POST /api/v1/users/cache/batch-write
Content-Type: application/json

{
  "key_template": "cache:user:{id}",
  "data": [ {"id":1,...}, {"id":2,...} ],
  "expiration": 3600,
  "batch_size": 100
}
```

请求（按 ID 列表写入）:
```bash
POST /api/v1/users/cache/batch-write
Content-Type: application/json

{
  "key_template": "cache:user:{id}",
  "ids": [1,2,3]
}
```

请求（预热）:
```bash
POST /api/v1/users/cache/warmup
Content-Type: application/json

{
  "key_template": "cache:user:{id}",
  "limit": 1000,
  "order_by": "access_count",
  "expiration": 3600
}
```

响应示例:
```json
{ "code": 0, "message": "success", "items_written": N }
```

## 五、注意事项

- `key_template` 必须与 `KeyBuilder`/模型字段匹配，例如 `cache:user:{id}`。
- 当使用 `nx`/`xx` 或 `overwrite` 时，请注意并发场景下的语义区别。
- 批量写入支持 `use_pipeline` 来提高大规模写入性能，但会占用更多 Redis 连接。
- 对于需要强一致性的场景，可使用带锁写入或带版本写入。


---

如需我把示例中的模型或请求体改成更贴合你项目的字段，告诉我模型结构即可，我会更新示例。