# Lookup 模块使用文档

## 一、路由字段说明表

| 路由字段 | 对应 Service 方法 | 入参（示例） | 请求方式 | 出参（示例） |
|---------|------------------|------------|---------|------------|
| `POST /api/v1/{resource}/lookup` | `LookupQuery` | `{"method":"list","filters":[{"field":"age","operator":"gt","value":18}]}` | POST | `{"code":0,"message":"success","data":{"cache:user:1":{...}},"keys":["cache:user:1"],"count":1}` |
| `GET /api/v1/{resource}/:key` | `LookupSingle` | URL参数: `cache:user:1` | GET | `{"code":0,"message":"success","data":{"id":1,"name":"John",...}}` |
| `POST /api/v1/{resource}/count` | `LookupQuery` (仅计数) | `{"method":"list","filters":[{"field":"status","operator":"eq","value":"active"}]}` | POST | `{"code":0,"message":"success","count":42}` |
| `POST /api/v1/{resource}/invalidate` | `InvalidateCache` / `InvalidateCacheByPattern` | `{"keys":["cache:user:1","cache:user:2"]}` 或 `{"pattern":"cache:user:*"}` | POST | `{"code":0,"message":"success","count":2}` |

## 二、支持的过滤器操作符

| 操作符 | 说明 | 示例 |
|-------|------|------|
| `eq` | 等于 | `{"field":"status","operator":"eq","value":"active"}` |
| `ne` | 不等于 | `{"field":"status","operator":"ne","value":"inactive"}` |
| `gt` | 大于 | `{"field":"age","operator":"gt","value":18}` |
| `gte` | 大于等于 | `{"field":"age","operator":"gte","value":18}` |
| `lt` | 小于 | `{"field":"price","operator":"lt","value":100}` |
| `lte` | 小于等于 | `{"field":"price","operator":"lte","value":100}` |
| `like` | 模糊匹配 | `{"field":"name","operator":"like","value":"John"}` |
| `in` | IN查询 | `{"field":"status","operator":"in","value":["active","pending"]}` |
| `between` | 范围查询 | `{"field":"age","operator":"between","value":[18,30]}` |
| `isnull` | 字段不存在 | `{"field":"deleted_at","operator":"isnull"}` |
| `isnotnull` | 字段存在 | `{"field":"email","operator":"isnotnull"}` |

## 三、完整使用示例

### 3.1 基础配置代码

```go
package main

import (
    "context"
    "time"
    
    "github.com/Super-Gagaga/abstract-manager/http_router"
    "github.com/Super-Gagaga/abstract-manager/service"
    
    "github.com/gin-gonic/gin"
)

// 数据模型
type User struct {
    ID     uint   `json:"id"`
    Name   string `json:"name"`
    Age    int    `json:"age"`
    Status string `json:"status"`
    Email  string `json:"email"`
}

func main() {
    r := gin.Default()
    v1 := r.Group("/api/v1")
    
    // 1. 创建 ServiceManager
    userService := service.NewServiceManager(User{})
    userService.CacheKeyType = "cache"
    userService.CacheKeyName = "user"
    
    // 2. 创建 Lookup 路由组
    userLookup := http_router.NewLookupRouterGroup(v1, userService)
    
    // 3. 注册查询方法
    // 基础列表查询
    userLookup.RegisterListMethod("cache:user:*", 1*time.Hour)
    
    // 带数据库回源的查询
    userLookup.RegisterFallbackMethod("cache:user:*", 30*time.Minute)
    
    // 自定义查询方法（活跃用户）
    userLookup.RegisterActiveListMethod(
        "cache:user:*",
        1*time.Hour,
        func(ctx context.Context, client *redis.Client, keys []string) ([]string, error) {
            var activeKeys []string
            for _, key := range keys {
                status, err := client.HGet(ctx, key, "status").Result()
                if err == nil && status == "active" {
                    activeKeys = append(activeKeys, key)
                }
            }
            return activeKeys, nil
        },
    )
    
    // 4. 注册路由
    userLookup.RegisterRoutes("/users")
    
    r.Run(":8080")
}
```

### 3.2 API 调用示例

#### 示例 1: 基础查询 - 获取所有活跃用户

**请求:**
```bash
POST /api/v1/users/lookup
Content-Type: application/json

{
  "method": "list",
  "filters": [
    {
      "field": "status",
      "operator": "eq",
      "value": "active"
    }
  ]
}
```

**响应:**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "cache:user:1": {
      "id": 1,
      "name": "John Doe",
      "age": 25,
      "status": "active",
      "email": "john@example.com"
    },
    "cache:user:2": {
      "id": 2,
      "name": "Jane Smith",
      "age": 30,
      "status": "active",
      "email": "jane@example.com"
    }
  },
  "keys": ["cache:user:1", "cache:user:2"],
  "count": 2
}
```

#### 示例 2: 组合过滤 - 年龄大于18且名字包含"John"的活跃用户

**请求:**
```bash
POST /api/v1/users/lookup
Content-Type: application/json

{
  "method": "list",
  "filters": [
    {
      "field": "age",
      "operator": "gt",
      "value": 18
    },
    {
      "field": "name",
      "operator": "like",
      "value": "John"
    },
    {
      "field": "status",
      "operator": "eq",
      "value": "active"
    }
  ]
}
```

**响应:**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "cache:user:1": {
      "id": 1,
      "name": "John Doe",
      "age": 25,
      "status": "active",
      "email": "john@example.com"
    },
    "cache:user:5": {
      "id": 5,
      "name": "Johnny Test",
      "age": 35,
      "status": "active",
      "email": "johnny@example.com"
    }
  },
  "keys": ["cache:user:1", "cache:user:5"],
  "count": 2
}
```

#### 示例 3: 单个查询 - 根据键获取用户

**请求:**
```bash
GET /api/v1/users/cache:user:1
```

**响应:**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": 1,
    "name": "John Doe",
    "age": 25,
    "status": "active",
    "email": "john@example.com"
  }
}
```

#### 示例 4: 计数查询 - 统计活跃用户数量

**请求:**
```bash
POST /api/v1/users/count
Content-Type: application/json

{
  "method": "list",
  "filters": [
    {
      "field": "status",
      "operator": "eq",
      "value": "active"
    }
  ]
}
```

**响应:**
```json
{
  "code": 0,
  "message": "success",
  "count": 42
}
```

#### 示例 5: 缓存失效 - 按键删除

**请求:**
```bash
POST /api/v1/users/invalidate
Content-Type: application/json

{
  "keys": ["cache:user:1", "cache:user:2", "cache:user:3"]
}
```

**响应:**
```json
{
  "code": 0,
  "message": "success",
  "count": 3
}
```

#### 示例 6: 缓存失效 - 按模式删除

**请求:**
```bash
POST /api/v1/users/invalidate
Content-Type: application/json

{
  "pattern": "cache:user:*"
}
```

**响应:**
```json
{
  "code": 0,
  "message": "success",
  "count": -1
}
```
*注: count 为 -1 表示按模式删除,无法精确统计数量*

#### 示例 7: 范围查询 - 年龄在18-30之间的用户

**请求:**
```bash
POST /api/v1/users/lookup
Content-Type: application/json

{
  "method": "list",
  "filters": [
    {
      "field": "age",
      "operator": "between",
      "value": [18, 30]
    }
  ]
}
```

#### 示例 8: IN 查询 - 多状态查询

**请求:**
```bash
POST /api/v1/users/lookup
Content-Type: application/json

{
  "method": "list",
  "filters": [
    {
      "field": "status",
      "operator": "in",
      "value": ["active", "pending", "verified"]
    }
  ]
}
```

## 四、代码讲解

### 4.1 核心组件说明

#### LookupQueryMethod - 查询方法定义
```go
type LookupQueryMethod[T any] struct {
    Name             string                    // 方法名称(如"list","active_list")
    Service          *service.ServiceManager[T] // 服务管理器
    KeyPattern       string                     // Redis键模式
    CacheExpire      time.Duration              // 缓存过期时间
    FallbackToDB     bool                       // 是否回源数据库
    CustomFilterFunc func(...)                  // 自定义过滤函数
}
```

**执行流程:**
1. 获取所有匹配 KeyPattern 的 Redis 键
2. 应用自定义过滤函数(如果有)
3. 应用前端传入的过滤器
4. 从缓存查询数据(必要时回源数据库)

#### LookupRouterGroup - 路由组管理器
```go
type LookupRouterGroup[T any] struct {
    RouterGroup        *gin.RouterGroup              // Gin路由组
    Service            *service.ServiceManager[T]    // 服务管理器
    MethodRegistry     *LookupMethodRegistry[T]      // 方法注册表
    TranslatorRegistry *filter_translator.RedisTranslatorRegistry // 过滤器翻译器
}
```

**主要功能:**
- 注册查询方法
- 注册 HTTP 路由
- 处理请求并返回响应

### 4.2 使用方式详解

#### 方式 1: 使用预定义方法

```go
// 最简单的方式 - 注册通用方法
userLookup.RegisterCommonMethods("cache:user:*", 1*time.Hour)
// 等价于:
// - RegisterListMethod: 基础列表查询
// - RegisterFallbackMethod: 带数据库回源查询
```

#### 方式 2: 单独注册方法

```go
// 基础列表
userLookup.RegisterListMethod("cache:user:*", 1*time.Hour)

// 活跃列表(带自定义过滤)
userLookup.RegisterActiveListMethod("cache:user:*", 1*time.Hour, customFunc)

// 数据库回源
userLookup.RegisterFallbackMethod("cache:user:*", 30*time.Minute)
```

#### 方式 3: 完全自定义方法

```go
userLookup.RegisterMethod(
    "vip_users",              // 方法名
    "cache:user:*",           // 键模式
    2*time.Hour,              // 缓存时间
    true,                     // 是否回源
    func(ctx context.Context, client *redis.Client, keys []string) ([]string, error) {
        // 自定义过滤逻辑
        var vipKeys []string
        for _, key := range keys {
            // 检查用户等级等...
            vipKeys = append(vipKeys, key)
        }
        return vipKeys, nil
    },
)
```

### 4.3 过滤器工作原理

过滤器采用**两阶段过滤**:

1. **自定义过滤阶段** (可选)
   - 在方法注册时通过 `CustomFilterFunc` 定义
   - 适用于复杂的业务逻辑过滤

2. **标准过滤阶段**
   - 前端传入的 filters 参数
   - 通过 `RedisTranslatorRegistry` 翻译为 `RedisFilter`
   - 支持 11 种标准操作符

**过滤执行顺序:**
```
所有键 → 自定义过滤 → 标准过滤器1 → 标准过滤器2 → ... → 最终结果
```

## 五、最佳实践

### 5.1 合理设置缓存时间

```go
// 频繁变动的数据
userLookup.RegisterListMethod("cache:session:*", 5*time.Minute)

// 相对稳定的数据
userLookup.RegisterListMethod("cache:user:*", 1*time.Hour)

// 很少变动的数据
configLookup.RegisterListMethod("cache:config:*", 24*time.Hour)
```

### 5.2 组合使用多种查询方法

```go
// 基础查询(不回源)
userLookup.RegisterListMethod("cache:user:*", 1*time.Hour)

// 带回源的查询(数据可能不完整时使用)
userLookup.RegisterFallbackMethod("cache:user:*", 30*time.Minute)

// 特定业务场景的查询
userLookup.RegisterMethod("premium_users", "cache:user:*", 2*time.Hour, false, premiumFilterFunc)
```

### 5.3 前端调用建议

```javascript
// 封装 API 调用函数
async function lookupUsers(filters) {
  const response = await fetch('/api/v1/users/lookup', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      method: 'list',
      filters: filters
    })
  });
  return response.json();
}

// 使用示例
const activeAdults = await lookupUsers([
  { field: 'age', operator: 'gte', value: 18 },
  { field: 'status', operator: 'eq', value: 'active' }
]);
```

## 六、错误处理

常见错误及处理:

| 错误码 | 说明 | 处理方式 |
|-------|------|---------|
| 400 | 请求参数错误 | 检查 method 和 filters 格式 |
| 404 | 键不存在 | 确认键名正确或使用带回源的方法 |
| 500 | 服务器错误 | 检查 Redis 连接和数据格式 |

---

以上就是 Lookup 模块的完整使用文档,包含了路由说明、代码示例和最佳实践。
