# set(为了方便下面称为write) 模块使用文档

## 一、路由字段说明表

### 1.1 单个操作路由

| 路由字段 | 对应 Service 方法 | 入参（示例） | 请求方式 | 出参（示例） |
|---------|------------------|------------|---------|------------|
| `POST /api/v1/{resource}/set` | `SetSingle` | `{"data":{...},"on_conflict_update":true}` | POST | `{"code":0,"message":"success"}` |
| `POST /api/v1/{resource}/insert` | `Insert` | `{"data":{...}}` | POST | `{"code":0,"message":"success"}` |
| `PUT /api/v1/{resource}/update` | `Update` / `UpdateByID` | `{"id":1,"updates":{"name":"John"}}` | PUT | `{"code":0,"message":"success"}` |
| `DELETE /api/v1/{resource}/delete` | `Delete` / `DeleteByID` | `{"id":1,"soft":false}` | DELETE | `{"code":0,"message":"success"}` |
| `POST /api/v1/{resource}/upsert` | `Upsert` | `{"data":{...},"conflict_columns":["id"]}` | POST | `{"code":0,"message":"success"}` |
| `POST /api/v1/{resource}/increment` | `Increment` / `Decrement` | `{"id":1,"column":"count","value":1}` | POST | `{"code":0,"message":"success"}` |

### 1.2 批量操作路由

| 路由字段 | 对应 Service 方法 | 入参（示例） | 请求方式 | 出参（示例） |
|---------|------------------|------------|---------|------------|
| `POST /api/v1/{resource}/batch/set` | `SetQuery` | `{"data":[{...}],"batch_size":100}` | POST | `{"code":0,"message":"success","rows_affected":100}` |
| `POST /api/v1/{resource}/batch/insert` | `BatchInsert` | `{"data":[{...}],"batch_size":100}` | POST | `{"code":0,"message":"success","rows_affected":100}` |
| `PUT /api/v1/{resource}/batch/update` | `BatchUpdate` | `{"updates":{"status":"active"}}` | PUT | `{"code":0,"message":"success","rows_affected":50}` |
| `DELETE /api/v1/{resource}/batch/delete` | `BatchDelete` / `BatchSoftDelete` | `{"ids":[1,2,3],"soft":false}` | DELETE | `{"code":0,"message":"success","rows_affected":3}` |
| `POST /api/v1/{resource}/batch/upsert` | `BatchUpsert` | `{"data":[{...}],"conflict_columns":["id"]}` | POST | `{"code":0,"message":"success","rows_affected":100}` |
| `POST /api/v1/{resource}/batch/increment` | `BatchIncrement` / `BatchDecrement` | `{"ids":[1,2,3],"column":"count","value":1}` | POST | `{"code":0,"message":"success","rows_affected":3}` |

## 二、完整使用示例

### 2.1 基础配置代码

```go
package main

import (
    "github.com/gin-gonic/gin"
    
    "github.com/Super-Gagaga/AbstractManager/http_router"
    "github.com/Super-Gagaga/AbstractManager/service"
)

// 数据模型
type User struct {
    ID     uint   `json:"id" gorm:"primaryKey;autoIncrement"`
    Name   string `json:"name" gorm:"size:100"`
    Age    int    `json:"age"`
    Status string `json:"status" gorm:"size:20"`
    Email  string `json:"email" gorm:"size:100;uniqueIndex"`
    Count  int    `json:"count" gorm:"default:0"`
}

func main() {
    r := gin.Default()
    v1 := r.Group("/api/v1")
    
    // 1. 创建 ServiceManager
    userService := service.NewServiceManager(User{})
    
    // 2. 创建 Write 路由组
    userWriter := http_router.NewWriteRouterGroup(v1, userService)
    
    // 3. 注册路由
    userWriter.RegisterRoutes("/users")
    
    r.Run(":8080")
}
```

### 2.2 单个操作 API 调用示例

#### 示例 1: Set - 设置单个用户(新增或更新)

**请求:**
```bash
POST /api/v1/users/set
Content-Type: application/json

{
  "data": {
    "id": 1,
    "name": "John Doe",
    "age": 25,
    "status": "active",
    "email": "john@example.com"
  },
  "on_conflict_update": true,
  "invalidate_cache": true
}
```

**响应:**
```json
{
  "code": 0,
  "message": "success"
}
```

**说明:**
- `on_conflict_update`: 当主键或唯一索引冲突时是否更新(默认 true)
- `invalidate_cache`: 是否使缓存失效(默认 true)

#### 示例 2: Insert - 插入用户(不处理冲突)

**请求:**
```bash
POST /api/v1/users/insert
Content-Type: application/json

{
  "data": {
    "name": "Jane Smith",
    "age": 30,
    "status": "active",
    "email": "jane@example.com"
  }
}
```

**响应:**
```json
{
  "code": 0,
  "message": "success"
}
```

**错误响应(唯一索引冲突):**
```json
{
  "code": 500,
  "message": "insert failed: UNIQUE constraint failed"
}
```

#### 示例 3: Update - 更新用户信息

**按ID更新:**
```bash
PUT /api/v1/users/update
Content-Type: application/json

{
  "id": 1,
  "updates": {
    "name": "John Smith",
    "age": 26,
    "status": "inactive"
  }
}
```

**全局更新(不推荐,会更新所有记录):**
```bash
PUT /api/v1/users/update
Content-Type: application/json

{
  "updates": {
    "status": "pending"
  }
}
```

**响应:**
```json
{
  "code": 0,
  "message": "success"
}
```

#### 示例 4: Delete - 删除用户

**硬删除:**
```bash
DELETE /api/v1/users/delete
Content-Type: application/json

{
  "id": 1,
  "soft": false
}
```

**软删除(设置 deleted_at):**
```bash
DELETE /api/v1/users/delete
Content-Type: application/json

{
  "id": 1,
  "soft": true
}
```

**响应:**
```json
{
  "code": 0,
  "message": "success"
}
```

#### 示例 5: Upsert - 插入或更新

**请求:**
```bash
POST /api/v1/users/upsert
Content-Type: application/json

{
  "data": {
    "id": 1,
    "name": "John Doe",
    "age": 25,
    "status": "active",
    "email": "john@example.com"
  },
  "conflict_columns": ["id"],
  "update_columns": ["name", "age"]
}
```

**响应:**
```json
{
  "code": 0,
  "message": "success"
}
```

**说明:**
- `conflict_columns`: 冲突检测字段(如 ["id"] 或 ["email"])
- `update_columns`: 冲突时要更新的字段(为空则更新所有字段)

#### 示例 6: Increment - 字段增量操作

**增加计数:**
```bash
POST /api/v1/users/increment
Content-Type: application/json

{
  "id": 1,
  "column": "count",
  "value": 5,
  "is_decr": false
}
```

**减少计数:**
```bash
POST /api/v1/users/increment
Content-Type: application/json

{
  "id": 1,
  "column": "count",
  "value": 3,
  "is_decr": true
}
```

**响应:**
```json
{
  "code": 0,
  "message": "success"
}
```

### 2.3 批量操作 API 调用示例

#### 示例 7: 批量设置用户

**请求:**
```bash
POST /api/v1/users/batch/set
Content-Type: application/json

{
  "data": [
    {
      "id": 1,
      "name": "John Doe",
      "age": 25,
      "status": "active",
      "email": "john@example.com"
    },
    {
      "id": 2,
      "name": "Jane Smith",
      "age": 30,
      "status": "active",
      "email": "jane@example.com"
    },
    {
      "id": 3,
      "name": "Bob Johnson",
      "age": 35,
      "status": "inactive",
      "email": "bob@example.com"
    }
  ],
  "batch_size": 100,
  "on_conflict_update": true,
  "invalidate_cache": true
}
```

**响应:**
```json
{
  "code": 0,
  "message": "success",
  "rows_affected": 3
}
```

#### 示例 8: 批量插入用户

**请求:**
```bash
POST /api/v1/users/batch/insert
Content-Type: application/json

{
  "data": [
    {
      "name": "User 1",
      "age": 20,
      "status": "active",
      "email": "user1@example.com"
    },
    {
      "name": "User 2",
      "age": 22,
      "status": "active",
      "email": "user2@example.com"
    }
  ],
  "batch_size": 50
}
```

**响应:**
```json
{
  "code": 0,
  "message": "success",
  "rows_affected": 2
}
```

#### 示例 9: 批量更新用户

**按条件批量更新(预留功能):**
```bash
PUT /api/v1/users/batch/update
Content-Type: application/json

{
  "updates": {
    "status": "verified",
    "count": 0
  }
}
```

**响应:**
```json
{
  "code": 0,
  "message": "success",
  "rows_affected": 15
}
```

**说明:** 当前版本会更新所有记录,后续可以添加 filters 支持条件更新

#### 示例 10: 批量删除用户

**硬删除:**
```bash
DELETE /api/v1/users/batch/delete
Content-Type: application/json

{
  "ids": [1, 2, 3, 4, 5],
  "soft": false
}
```

**软删除:**
```bash
DELETE /api/v1/users/batch/delete
Content-Type: application/json

{
  "ids": [1, 2, 3],
  "soft": true
}
```

**响应:**
```json
{
  "code": 0,
  "message": "success",
  "rows_affected": 3
}
```

#### 示例 11: 批量 Upsert

**请求:**
```bash
POST /api/v1/users/batch/upsert
Content-Type: application/json

{
  "data": [
    {
      "email": "john@example.com",
      "name": "John Doe",
      "age": 25,
      "status": "active"
    },
    {
      "email": "jane@example.com",
      "name": "Jane Smith",
      "age": 30,
      "status": "active"
    }
  ],
  "conflict_columns": ["email"],
  "update_columns": ["name", "age", "status"],
  "batch_size": 100
}
```

**响应:**
```json
{
  "code": 0,
  "message": "success",
  "rows_affected": 2
}
```

#### 示例 12: 批量增量操作

**批量增加计数:**
```bash
POST /api/v1/users/batch/increment
Content-Type: application/json

{
  "ids": [1, 2, 3, 4, 5],
  "column": "count",
  "value": 10,
  "is_decr": false
}
```

**批量减少计数:**
```bash
POST /api/v1/users/batch/increment
Content-Type: application/json

{
  "ids": [1, 2, 3],
  "column": "count",
  "value": 5,
  "is_decr": true
}
```

**响应:**
```json
{
  "code": 0,
  "message": "success",
  "rows_affected": 5
}
```

## 三、代码讲解

### 3.1 核心组件说明

#### WriteRouterGroup - 写操作路由组
```go
type WriteRouterGroup[T any] struct {
    RouterGroup *gin.RouterGroup           // Gin路由组
    Service     *service.ServiceManager[T] // 服务管理器
}
```

**设计特点:**
- 无需过滤器翻译器(写操作不涉及复杂查询)
- 直接调用 ServiceManager 的写操作方法
- 统一的请求/响应格式
- 完整的错误处理

### 3.2 操作分类

#### 单个操作
- **Set**: 智能新增或更新(根据冲突策略)
- **Insert**: 纯插入(冲突时报错)
- **Update**: 更新指定字段
- **Delete**: 硬删除或软删除
- **Upsert**: 精确控制冲突策略
- **Increment/Decrement**: 原子性增减操作

#### 批量操作
- **Batch Set**: 批量智能写入
- **Batch Insert**: 批量纯插入
- **Batch Update**: 批量更新
- **Batch Delete**: 批量删除
- **Batch Upsert**: 批量 Upsert
- **Batch Increment**: 批量增减

### 3.3 请求选项说明

#### SetSingle/SetQuery 选项
```go
{
  "on_conflict_update": true,  // 冲突时是否更新(默认true)
  "invalidate_cache": true,    // 是否使缓存失效(默认true)
  "batch_size": 100            // 批次大小(仅批量操作,默认100)
}
```

#### Upsert 选项
```go
{
  "conflict_columns": ["id"],              // 冲突检测字段(必填)
  "update_columns": ["name", "age"],       // 冲突时更新的字段(空=全部)
  "batch_size": 100                        // 批量操作的批次大小
}
```

### 3.4 事务处理

所有写操作都自动包裹在事务中:
- 隔离级别: `REPEATABLE READ`
- 自动提交: 操作成功时自动提交
- 自动回滚: 操作失败时自动回滚
- 批量操作: 按 batch_size 分批,每批独立事务

## 四、最佳实践

### 4.1 选择合适的操作类型

```go
// 场景1: 不确定数据是否存在,使用 Set
POST /users/set
{
  "data": {...},
  "on_conflict_update": true
}

// 场景2: 确定是新数据,使用 Insert
POST /users/insert
{
  "data": {...}
}

// 场景3: 需要精确控制更新字段,使用 Upsert
POST /users/upsert
{
  "data": {...},
  "conflict_columns": ["email"],
  "update_columns": ["name", "age"]
}
```

### 4.2 批量操作性能优化

```go
// 大数据量导入: 调整 batch_size
{
  "data": [...10000条数据...],
  "batch_size": 500,  // 每批500条
  "on_conflict_update": false  // 纯插入更快
}

// 中等数据量: 使用默认值
{
  "data": [...1000条数据...],
  "batch_size": 100  // 默认100
}
```

### 4.3 软删除 vs 硬删除

```go
// 推荐: 软删除(可恢复)
{
  "id": 1,
  "soft": true
}

// 谨慎: 硬删除(不可恢复)
{
  "id": 1,
  "soft": false
}
```

### 4.4 原子性计数操作

```go
// ✅ 正确: 使用 Increment(原子性)
POST /users/increment
{
  "id": 1,
  "column": "count",
  "value": 1
}

// ❌ 错误: 先查询再更新(非原子性,可能丢失更新)
// 1. GET /users/1  → count = 10
// 2. PUT /users/update {"id":1, "updates":{"count":11}}
```

### 4.5 前端封装示例

```javascript
class UserWriteAPI {
  // 单个操作
  static async set(data, options = {}) {
    return fetch('/api/v1/users/set', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        data,
        on_conflict_update: options.onConflictUpdate ?? true,
        invalidate_cache: options.invalidateCache ?? true
      })
    }).then(r => r.json());
  }
  
  static async insert(data) {
    return fetch('/api/v1/users/insert', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ data })
    }).then(r => r.json());
  }
  
  static async update(id, updates) {
    return fetch('/api/v1/users/update', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ id, updates })
    }).then(r => r.json());
  }
  
  static async delete(id, soft = true) {
    return fetch('/api/v1/users/delete', {
      method: 'DELETE',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ id, soft })
    }).then(r => r.json());
  }
  
  static async increment(id, column, value, isDecr = false) {
    return fetch('/api/v1/users/increment', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ id, column, value, is_decr: isDecr })
    }).then(r => r.json());
  }
  
  // 批量操作
  static async batchSet(data, options = {}) {
    return fetch('/api/v1/users/batch/set', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        data,
        batch_size: options.batchSize ?? 100,
        on_conflict_update: options.onConflictUpdate ?? true,
        invalidate_cache: options.invalidateCache ?? true
      })
    }).then(r => r.json());
  }
  
  static async batchDelete(ids, soft = true) {
    return fetch('/api/v1/users/batch/delete', {
      method: 'DELETE',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ ids, soft })
    }).then(r => r.json());
  }
}

// 使用示例
await UserWriteAPI.set({ id: 1, name: 'John', age: 25 });
await UserWriteAPI.batchSet([...], { batchSize: 200 });
await UserWriteAPI.increment(1, 'view_count', 1);
```

### 4.6 完整应用示例

```go
func SetupUserRoutes(rg *gin.RouterGroup) {
    userService := service.NewServiceManager(User{})
    
    // 查询路由
    userQuery := http_router.NewQueryRouterGroup(rg, userService)
    userQuery.RegisterCommonMethods(20)
    userQuery.RegisterRoutes("/users")
    
    // 写入路由
    userWrite := http_router.NewWriteRouterGroup(rg, userService)
    userWrite.RegisterRoutes("/users")
    
    // Lookup路由(可选)
    userLookup := http_router.NewLookupRouterGroup(rg, userService)
    userLookup.RegisterListMethod("cache:user:*", 1*time.Hour)
    userLookup.RegisterRoutes("/users")
}
```

## 五、错误处理

| 错误码 | 说明 | 常见原因 |
|-------|------|---------|
| 400 | 请求参数错误 | data为空、字段缺失、类型错误 |
| 500 | 服务器错误 | 数据库连接失败、约束冲突、事务失败 |

**常见错误示例:**

```json
// 唯一索引冲突
{
  "code": 500,
  "message": "insert failed: UNIQUE constraint failed: users.email"
}

// 外键约束失败
{
  "code": 500,
  "message": "delete failed: FOREIGN KEY constraint failed"
}

// 数据为空
{
  "code": 400,
  "message": "data cannot be nil"
}
```

## 六、与其他模块的配合

### 6.1 Query + Write 完整 CRUD

```go
// 查询
GET  /api/v1/users/:id          → 获取单个
POST /api/v1/users/query        → 分页查询
POST /api/v1/users/count        → 计数

// 写入
POST   /api/v1/users/set        → 新增或更新
PUT    /api/v1/users/update     → 更新
DELETE /api/v1/users/delete     → 删除
```

### 6.2 Lookup + Write 缓存同步

```go
// 1. 写入数据并自动失效缓存
POST /api/v1/users/set
{
  "data": {...},
  "invalidate_cache": true  // 自动清除缓存
}

// 2. 从缓存查询最新数据
POST /api/v1/users/lookup
{
  "method": "list",
  "filters": [...]
}
```

### 6.3 三模块协同工作流

```
用户请求
  ↓
Write 模块(写入数据 + 失效缓存)
  ↓
Query 模块(查询数据库)
  ↓
Lookup 模块(更新缓存)
  ↓
返回结果
```
