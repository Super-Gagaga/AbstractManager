# Query 模块使用文档

## 一、路由字段说明表

| 路由字段 | 对应 Service 方法 | 入参（示例） | 请求方式 | 出参（示例） |
|---------|------------------|------------|---------|------------|
| `POST /api/v1/{resource}/query` | `GetQuery` | `{"method":"list","page":1,"filters":[{"field":"age","operator":"gt","value":18}]}` | POST | `{"code":0,"message":"success","data":[{...}],"total":100,"page":1,"page_size":20,"total_pages":5}` |
| `GET /api/v1/{resource}/:id` | `GetSingleByID` | URL参数: `123` | GET | `{"code":0,"message":"success","data":{"id":123,"name":"John",...}}` |
| `POST /api/v1/{resource}/count` | `CountQuery` | `{"filters":[{"field":"status","operator":"eq","value":"active"}]}` | POST | `{"code":0,"message":"success","count":42}` |

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
| `isnull` | 字段为空 | `{"field":"deleted_at","operator":"isnull"}` |
| `isnotnull` | 字段不为空 | `{"field":"email","operator":"isnotnull"}` |

## 三、完整使用示例

### 3.1 基础配置代码

```go
package main

import (
    "github.com/gin-gonic/gin"
    "gorm.io/gorm"
    
    "github.com/Super-Gagaga/abstract-manager/http_router"
    "github.com/Super-Gagaga/abstract-manager/service"
)

// 数据模型
type User struct {
    ID        uint      `json:"id" gorm:"primaryKey;autoIncrement"`
    Name      string    `json:"name" gorm:"size:100"`
    Age       int       `json:"age"`
    Status    string    `json:"status" gorm:"size:20"`
    Email     string    `json:"email" gorm:"size:100"`
    CreatedAt time.Time `json:"created_at"`
    DeletedAt *time.Time `json:"deleted_at" gorm:"index"`
}

func main() {
    r := gin.Default()
    v1 := r.Group("/api/v1")
    
    // 1. 创建 ServiceManager
    userService := service.NewServiceManager(User{})
    
    // 2. 创建 Query 路由组
    userRouter := http_router.NewQueryRouterGroup(v1, userService)
    
    // 3. 注册查询方法
    // 方式1: 使用便捷方法注册常用查询(list, active_list, search)
    userRouter.RegisterCommonMethods(20) // 每页20条
    
    // 方式2: 单独注册方法
    // 基础列表查询
    userRouter.RegisterListMethod(20)
    
    // 活跃用户列表
    userRouter.RegisterActiveListMethod(20)
    
    // 自定义查询方法
    userRouter.RegisterMethod(
        "vip_list",              // 方法名
        10,                      // 每页条数
        func(db *gorm.DB) *gorm.DB {
            return db.Where("age >= ?", 18)
        },
        "age",                   // 排序字段
        "DESC",                  // 排序方式
    )
    
    // 4. 注册路由
    userRouter.RegisterRoutes("/users")
    
    r.Run(":8080")
}
```

### 3.2 API 调用示例

#### 示例 1: 基础分页查询 - 获取所有用户

**请求:**
```bash
POST /api/v1/users/query
Content-Type: application/json

{
  "method": "list",
  "page": 1,
  "filters": []
}
```

**响应:**
```json
{
  "code": 0,
  "message": "success",
  "data": [
    {
      "id": 1,
      "name": "John Doe",
      "age": 25,
      "status": "active",
      "email": "john@example.com",
      "created_at": "2024-01-15T10:30:00Z"
    },
    {
      "id": 2,
      "name": "Jane Smith",
      "age": 30,
      "status": "active",
      "email": "jane@example.com",
      "created_at": "2024-01-16T11:20:00Z"
    }
  ],
  "total": 100,
  "page": 1,
  "page_size": 20,
  "total_pages": 5
}
```

#### 示例 2: 单条件过滤 - 获取活跃用户

**请求:**
```bash
POST /api/v1/users/query
Content-Type: application/json

{
  "method": "list",
  "page": 1,
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
  "data": [
    {
      "id": 1,
      "name": "John Doe",
      "age": 25,
      "status": "active",
      "email": "john@example.com",
      "created_at": "2024-01-15T10:30:00Z"
    }
  ],
  "total": 42,
  "page": 1,
  "page_size": 20,
  "total_pages": 3
}
```

#### 示例 3: 组合过滤 - 年龄大于18且名字包含"John"的活跃用户

**请求:**
```bash
POST /api/v1/users/query
Content-Type: application/json

{
  "method": "list",
  "page": 1,
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
  "data": [
    {
      "id": 1,
      "name": "John Doe",
      "age": 25,
      "status": "active",
      "email": "john@example.com",
      "created_at": "2024-01-15T10:30:00Z"
    },
    {
      "id": 5,
      "name": "Johnny Test",
      "age": 35,
      "status": "active",
      "email": "johnny@example.com",
      "created_at": "2024-01-18T09:15:00Z"
    }
  ],
  "total": 2,
  "page": 1,
  "page_size": 20,
  "total_pages": 1
}
```

#### 示例 4: 单个查询 - 根据ID获取用户

**请求:**
```bash
GET /api/v1/users/123
```

**响应:**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": 123,
    "name": "John Doe",
    "age": 25,
    "status": "active",
    "email": "john@example.com",
    "created_at": "2024-01-15T10:30:00Z"
  }
}
```

**错误响应 (用户不存在):**
```json
{
  "code": 404,
  "message": "record not found"
}
```

#### 示例 5: 计数查询 - 统计活跃用户数量

**请求:**
```bash
POST /api/v1/users/count
Content-Type: application/json

{
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

#### 示例 6: 范围查询 - 年龄在18-30之间的用户

**请求:**
```bash
POST /api/v1/users/query
Content-Type: application/json

{
  "method": "list",
  "page": 1,
  "filters": [
    {
      "field": "age",
      "operator": "between",
      "value": [18, 30]
    }
  ]
}
```

#### 示例 7: IN 查询 - 多状态查询

**请求:**
```bash
POST /api/v1/users/query
Content-Type: application/json

{
  "method": "list",
  "page": 1,
  "filters": [
    {
      "field": "status",
      "operator": "in",
      "value": ["active", "pending", "verified"]
    }
  ]
}
```

#### 示例 8: 使用自定义方法 - VIP用户列表

**请求:**
```bash
POST /api/v1/users/query
Content-Type: application/json

{
  "method": "vip_list",
  "page": 1,
  "filters": []
}
```

**响应:**
```json
{
  "code": 0,
  "message": "success",
  "data": [
    {
      "id": 1,
      "name": "John Doe",
      "age": 25,
      "status": "active",
      "email": "john@example.com"
    }
  ],
  "total": 15,
  "page": 1,
  "page_size": 10,
  "total_pages": 2
}
```

#### 示例 9: NULL 值过滤 - 查询未删除的用户

**请求:**
```bash
POST /api/v1/users/query
Content-Type: application/json

{
  "method": "list",
  "page": 1,
  "filters": [
    {
      "field": "deleted_at",
      "operator": "isnull"
    }
  ]
}
```

#### 示例 10: 分页查询第2页

**请求:**
```bash
POST /api/v1/users/query
Content-Type: application/json

{
  "method": "list",
  "page": 2,
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
  "data": [...],
  "total": 42,
  "page": 2,
  "page_size": 20,
  "total_pages": 3
}
```

## 四、代码讲解

### 4.1 核心组件说明

#### PaginatedQueryMethod - 分页查询方法定义
```go
type PaginatedQueryMethod[T any] struct {
    Name       string                    // 方法名称(如"list","vip_list")
    PageSize   int                       // 每页条数
    Service    *service.ServiceManager[T] // 服务管理器
    FilterFunc func(*gorm.DB) *gorm.DB   // 预定义过滤函数
    OrderBy    string                    // 排序字段
    Order      string                    // 排序方式(ASC/DESC)
}
```

**执行流程:**
1. 应用预定义的 FilterFunc (如果有)
2. 应用前端传入的 GORM 过滤器
3. 调用 ServiceManager 的 GetQuery 方法进行分页查询
4. 返回包含数据、总数、分页信息的结果

#### QueryRouterGroup - 路由组管理器
```go
type QueryRouterGroup[T any] struct {
    RouterGroup        *gin.RouterGroup              // Gin路由组
    Service            *service.ServiceManager[T]    // 服务管理器
    MethodRegistry     *QueryMethodRegistry[T]       // 方法注册表
    TranslatorRegistry *filter_translator.GormTranslatorRegistry // GORM过滤器翻译器
}
```

**主要功能:**
- 注册查询方法
- 注册 HTTP 路由
- 翻译前端过滤器为 GORM 过滤器
- 处理请求并返回响应

### 4.2 使用方式详解

#### 方式 1: 使用便捷方法 (推荐)

```go
// 一次性注册 list, active_list, search 三个方法
userRouter.RegisterCommonMethods(20) // 每页20条
```

#### 方式 2: 单独注册预定义方法

```go
// 基础列表 (所有数据)
userRouter.RegisterListMethod(20)

// 活跃列表 (status='active' 且未删除)
userRouter.RegisterActiveListMethod(20)

// 搜索方法 (预留接口)
userRouter.RegisterSearchMethod(20, []string{"name", "email"})
```

#### 方式 3: 完全自定义方法

```go
// 注册 VIP 用户查询
userRouter.RegisterMethod(
    "vip_list",              // 方法名
    10,                      // 每页10条
    func(db *gorm.DB) *gorm.DB {
        // 自定义过滤逻辑
        return db.Where("age >= ?", 18).
                  Where("vip_level > ?", 0)
    },
    "vip_level",             // 按VIP等级排序
    "DESC",                  // 降序
)

// 最近注册用户
userRouter.RegisterMethod(
    "recent_users",
    50,
    func(db *gorm.DB) *gorm.DB {
        return db.Where("created_at > ?", time.Now().AddDate(0, 0, -7))
    },
    "created_at",
    "DESC",
)

// 待审核用户
userRouter.RegisterMethod(
    "pending_users",
    15,
    func(db *gorm.DB) *gorm.DB {
        return db.Where("status = ?", "pending")
    },
    "created_at",
    "ASC",
)
```

### 4.3 过滤器工作原理

过滤器采用**两阶段过滤**:

1. **预定义过滤阶段** (可选)
   - 在方法注册时通过 `FilterFunc` 定义
   - 在 Execute 方法中首先应用
   - 适用于固定的业务逻辑过滤

2. **动态过滤阶段**
   - 前端传入的 filters 参数
   - 通过 `GormTranslatorRegistry` 翻译为 `GormFilter`
   - 支持 11 种标准操作符

**过滤执行顺序:**
```
GORM查询 → 预定义FilterFunc → 动态过滤器1 → 动态过滤器2 → ... → 分页 → 排序 → 结果
```

### 4.4 GORM 过滤器翻译流程

```go
// 1. 前端发送 FilterParam
{
  "field": "age",
  "operator": "gt",
  "value": 18
}

// 2. GormTranslatorRegistry 翻译为 GormFilter
filter := &GormGreaterThanFilter{
    GenericFilter: &GenericFilter{
        Field: "age",
        Operator: "gt",
        Value: 18,
    },
}

// 3. 应用到 GORM 查询
db = filter.ApplyGorm(db)
// 等价于: db.Where("age > ?", 18)
```

## 五、最佳实践

### 5.1 合理设置分页大小

```go
// 大数据量列表
userRouter.RegisterListMethod(50)

// 详细信息列表
orderRouter.RegisterListMethod(20)

// 精简选择列表
categoryRouter.RegisterListMethod(100)
```

### 5.2 组合使用多种查询方法

```go
// 全量查询
userRouter.RegisterListMethod(20)

// 业务场景1: 活跃用户
userRouter.RegisterActiveListMethod(20)

// 业务场景2: VIP用户
userRouter.RegisterMethod("vip_users", 10, vipFilterFunc, "vip_level", "DESC")

// 业务场景3: 新用户
userRouter.RegisterMethod("new_users", 30, newUserFilterFunc, "created_at", "DESC")
```

### 5.3 合理使用排序

```go
// 按创建时间倒序 (最新的在前)
userRouter.RegisterMethod("list", 20, nil, "created_at", "DESC")

// 按年龄升序
userRouter.RegisterMethod("age_list", 20, nil, "age", "ASC")

// 按价格降序 (热门商品)
productRouter.RegisterMethod("hot_products", 20, nil, "price", "DESC")
```

### 5.4 前端调用封装

```javascript
// 封装 API 调用函数
class UserAPI {
  static async query(method, page, filters) {
    const response = await fetch('/api/v1/users/query', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ method, page, filters })
    });
    return response.json();
  }
  
  static async getByID(id) {
    const response = await fetch(`/api/v1/users/${id}`);
    return response.json();
  }
  
  static async count(filters) {
    const response = await fetch('/api/v1/users/count', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ filters })
    });
    return response.json();
  }
}

// 使用示例
const result = await UserAPI.query('list', 1, [
  { field: 'age', operator: 'gte', value: 18 },
  { field: 'status', operator: 'eq', value: 'active' }
]);

const user = await UserAPI.getByID(123);

const count = await UserAPI.count([
  { field: 'status', operator: 'eq', value: 'active' }
]);
```

### 5.5 完整的多资源应用示例

```go
func SetupRoutes() *gin.Engine {
    r := gin.Default()
    v1 := r.Group("/api/v1")
    
    // 用户模块
    setupUserRoutes(v1)
    
    // 订单模块
    setupOrderRoutes(v1)
    
    // 产品模块
    setupProductRoutes(v1)
    
    return r
}

func setupUserRoutes(rg *gin.RouterGroup) {
    userService := service.NewServiceManager(User{})
    userRouter := http_router.NewQueryRouterGroup(rg, userService)
    
    // 注册常用方法
    userRouter.RegisterCommonMethods(20)
    
    // 注册自定义方法
    userRouter.RegisterMethod("vip_users", 10, func(db *gorm.DB) *gorm.DB {
        return db.Where("vip_level > ?", 0)
    }, "vip_level", "DESC")
    
    userRouter.RegisterRoutes("/users")
}

func setupOrderRoutes(rg *gin.RouterGroup) {
    orderService := service.NewServiceManager(Order{})
    orderRouter := http_router.NewQueryRouterGroup(rg, orderService)
    
    orderRouter.RegisterListMethod(30)
    
    // 待支付订单
    orderRouter.RegisterMethod("pending_payment", 20, func(db *gorm.DB) *gorm.DB {
        return db.Where("status = ?", "pending_payment")
    }, "created_at", "DESC")
    
    // 已完成订单
    orderRouter.RegisterMethod("completed", 20, func(db *gorm.DB) *gorm.DB {
        return db.Where("status = ?", "completed")
    }, "completed_at", "DESC")
    
    orderRouter.RegisterRoutes("/orders")
}

func setupProductRoutes(rg *gin.RouterGroup) {
    productService := service.NewServiceManager(Product{})
    productRouter := http_router.NewQueryRouterGroup(rg, productService)
    
    productRouter.RegisterListMethod(20)
    
    // 热销产品
    productRouter.RegisterMethod("hot_products", 20, func(db *gorm.DB) *gorm.DB {
        return db.Where("sales_count > ?", 100)
    }, "sales_count", "DESC")
    
    productRouter.RegisterRoutes("/products")
}
```

## 六、错误处理

常见错误及处理:

| 错误码 | 说明 | 处理方式 |
|-------|------|---------|
| 400 | 请求参数错误 | 检查 method, page, filters 格式 |
| 404 | 记录不存在 | 确认 ID 是否正确 |
| 500 | 服务器错误 | 检查数据库连接和查询语句 |

## 七、Query 与 Lookup 的区别

| 特性 | Query 模块 | Lookup 模块 |
|-----|-----------|------------|
| **数据源** | 数据库 (GORM) | Redis 缓存 |
| **分页** | 支持 (page, page_size) | 不支持 (一次性返回) |
| **过滤器** | GormFilter (SQL查询) | RedisFilter (内存过滤) |
| **性能** | 取决于数据库索引 | 快速 (内存操作) |
| **数据量** | 适合大数据量 | 适合中小数据量 |
| **使用场景** | 持久化数据查询 | 缓存数据快速查询 |
| **回源** | 直接查数据库 | 可选回源到数据库 |

---

以上就是 Query 模块的完整使用文档。该模块与 Lookup 模块配合使用,可以构建高性能的数据查询系统。
