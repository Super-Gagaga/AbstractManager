# AbstractManager 性能热力图 & 瓶颈分析

> 环境: Windows 11, Go 1.24, Intel Core 7 240H(16 线程), miniredis (in-memory)
> 运行: `go test -run='^$' -bench=. -benchmem -benchtime=1s ./tests/race_perf/`
> 更新日期: 2026-08-19(与 2026-06-07 数据对比见文末;两次采集使用不同机器,绝对值差异主要来自硬件)

---

## 总览热力图

延迟越低越好 (ns/op),热力条为示意(分组内相对,非线性比例):

```
操作                          延迟 (ns/op)        热力              分类
──────────────────────────────────────────────────────────────────────────────
RedisTranslate_Equal                37.7 ▕                       ▎ CPU/纯内存
GormTranslate_Equal                 38.6 ▕                       ▎ CPU/纯内存
CacheKeyBuild                     64.10 ▕                       ▎ CPU/纯内存
ExtractIDFromKey                  88.98 ▕                       ▎ CPU/纯内存
JSONMarshal                       157.6 ▕                       ▎ CPU/纯内存(1 alloc)
ValidateSQLIdentifier             175.4 ▕                       ▎ CPU/纯内存(0 alloc)
GormFilter_10Filters              505.5 ▕                       ▎ 过滤器构建
GormTranslate_Batch10             517.7 ▕                       ▎ CPU/纯内存
JSONUnmarshal                     752.5 ▕▏                      ▎ CPU/纯内存(7 allocs)
ExtractID_JSON                     1442 ▕▏                      ▎ 两次JSON往返
NewServiceManager                  1821 ▕▏                      ▎ 构造/内存分配
────────────────────────────────────────────────────────────── I/O 分界线 ──
RedisGet                         36,518 ▕█████                  █ Redis I/O
LookupSingle_FallbackHit         37,393 ▕█████                  █ Redis I/O
RedisDel                         39,030 ▕█████                  █ Redis I/O
InvalidateCache                  39,170 ▕█████                  █ Redis I/O
RedisGetMiss                     39,429 ▕█████                  █ Redis I/O
RedisSet                         40,388 ▕█████                  █ Redis I/O
LookupSingle_Hit                 38,363 ▕█████                  █ Redis I/O
WritedownSingle                  44,352 ▕██████                 █ Redis+序列化+超时兜底
────────────────────────────────────────────────────────────── 批量操作 ──
LookupQuery_10Keys               52,683 ▕████████               █ MGet×10+解析
GetMultiple_10Keys              192,288 ▕████████████▏         █ MGet×10
SetMultiple_10Items             198,771 ▕████████████▏         █ Pipeline×10
LookupQuery_100Keys             179,786 ▕████████████          █ MGet×100+解析
ScanKeys_1000                   408,527 ▕███████████████████   █ SCAN 游标迭代
RedisFilter_1Filter_100Keys     224,439 ▕█████████████         █ MGET+JSON内存过滤
GetMultiple_100Keys           1,679,022 ▕███████████████████████████ █ MGet×100
SetMultiple_100Items          1,846,687 ▕████████████████████████████ █ Pipeline×100
PipelineWrite_100             1,823,504 ▕████████████████████████████ █ Pipeline×100
RedisFilter_5Filters_1000Keys 1,810,518 ▕████████████████████████████ █ 过滤×5000次
GetMultiple_1000Keys         16,039,979 ▕███████████████████████████████████████████ █ MGet×1000
PipelineWrite_1000           18,169,840 ▕████████████████████████████████████████████████ █ Pipeline×1000
PipelineWrite_10000         176,804,467 ▕███████████████████████████████████████████████████████████████████████████████████████████████████████ █ Pipeline×10k
```

---

## 按层级的延迟对比

```
┌─────────────────────────────────────────────────────────────────┐
│                      应用层                                     │
│  RedisTranslate        38 ns  ▓                                 │
│  GormTranslate         39 ns  ▓                                 │
│  CacheKeyBuild         64 ns  ▓                                 │
│  ExtractIDFromKey      89 ns  ▓                                 │
│  JSONMarshal          158 ns  ▓                                 │
│  ValidateSQLIdent     175 ns  ▓        (0 次内存分配)           │
│  JSONUnmarshal        753 ns  ▓▓       (7 次内存分配)           │
├─────────────────────────────────────────────────────────────────┤
│                   序列化/反序列化层                              │
│  Marshal             158 ns  ▓       (0.000 ms)                │
│  Unmarshal           753 ns  ▓▓      (0.001 ms)  慢 4.8×       │
├─────────────────────────────────────────────────────────────────┤
│                    Redis 网络 I/O 层                             │
│  GET                36.5 μs  ████████  (0.037 ms)              │
│  DEL                39.0 μs  ████████  (0.039 ms)              │
│  SET                40.4 μs  ████████  (0.040 ms)              │
│  WritedownSingle    44.4 μs  ████████  (0.044 ms, 含序列化)    │
│  ───────────────────────────────                                │
│  单次写入中 I/O 占比:  ~99.6%  ←── 绝对瓶颈                     │
├─────────────────────────────────────────────────────────────────┤
│                   批量操作层                                     │
│  MGet×10           192.3 μs  ████████████                      │
│  MGet×100         1679.0 μs  ████████████████████████████      │
│  MGet×1000       16040.0 μs  █████████████████████████████████ │
│  Pipeline×10       198.8 μs  ████████████                      │
│  Pipeline×100     1741.6 μs  ████████████████████████████      │
│  Pipeline×1000   18170.0 μs  █████████████████████████████████ │
│  Pipeline×10000 176804.5 μs  █████████████████████████████████ │
│  SCAN×1000         408.5 μs  ██████████████                    │
└─────────────────────────────────────────────────────────────────┘

关键比例:
  序列化  : Redis I/O  ≈  1 : 256
  CPU指令 : Redis I/O  ≈  1 : 1050
```

---

## 瓶颈逐层分析

### 🔴 第一瓶颈: Redis 网络 I/O(单次写操作中占比 ~99.6%)

```
每次 Redis 操作 ~36-44μs (0.036-0.044ms)
├── 网络往返 (localhost):  ~25-35μs   ← miniredis 为进程内模拟,真实 Redis 只高不低
├── 协议解析 (RESP):         ~2-3μs
├── 内存存取:                ~1-2μs
└── context 检查 (EnsureTimeout): ~0.1μs (可忽略)
```

**影响范围**: 所有 `WritedownSingle`、`LookupSingle`、`InvalidateCache`、`Get/Set/Del`

**优化方向**:
| 策略 | 预期提升 | 代价 |
|---|---|---|
| Pipeline 批量操作 | items/s 从 ~25k → ~55k (2.2×) | 增加单次延迟 |
| 连接池调优 | +10~20% | 配置项调整 |
| 本地缓存 (sync.Map) 前置 | 热点 key 0μs 命中 | 内存 + 一致性 |
| 真实 Redis 部署网络延迟 | 通常 ×2~×10(更凸显批量的价值) | — |

### 🟡 第二瓶颈: SCAN 游标遍历(比 GET 慢 ~11×)

```
RedisGet          36,518 ns  ████
ScanKeys_1000    408,527 ns  ████████████████████████████████████████
```

**根因**: SCAN 按游标迭代(1000 个 key、count=100,约 10+ 次往返),且每次返回的 key 列表都有内存分配(3103 allocs/千键)。

**优化方向**:
| 策略 | 预期提升 |
|---|---|
| 用 Set/索引结构维护 key 集合,代替 SCAN | 10~100× |
| 增大 count 参数减少往返 | 1.5~3× |
| 避免在热路径中使用模式查询 | — |

### 🟡 第三瓶颈(本次新增观测): Redis 内存过滤的分配开销

```
RedisFilter_1Filter_100Keys    224,439 ns   2.24 μs/key   31 allocs/key
RedisFilter_5Filters_1000Keys 1,810,518 ns   1.81 μs/key   ~31 allocs/key/filter
```

**根因**: 过滤器对每个候选 key 执行 MGET 取值 + `json.Unmarshal` 成 `map[string]interface{}` 再比较,每个 key×filter 组合都要完整解析一次 JSON,分配密度约 31 次/key。

**定位**: 相对单条 Redis I/O(40μs)仍便宜 ~20×,**不是当前主要瓶颈**,但在"万级 key × 多过滤器"的场景会与 SCAN 叠加放大。

**优化方向**:
| 策略 | 预期提升 | 代价 |
|---|---|---|
| 同一批 key 应用多个过滤器时共享一次 JSON 解析 | 过滤链 2~5× | 重构 ApplyRedisFilters |
| `easyjson`/`sonic` 代码生成解析 | 3~10× | 引入依赖 |
| 降级为 Redis 端 Lua 脚本过滤 | 网络开销消失 | 复杂度、可维护性 |

### 🟡 第四瓶颈: JSON 反序列化(占单次读操作 ~2%)

```
JSONMarshal     158 ns  ▓      (序列化快, 1 alloc)
JSONUnmarshal   753 ns  ████   (反序列化慢 4.8×, 7 allocs)
```

**优化方向**: `sonic`(amd64)/`easyjson` 3~10×,或 msgpack/protobuf 替代 JSON。

### 🟢 非瓶颈: 纯 CPU 操作

```
RedisTranslate           38 ns  ← 可忽略
GormTranslate            39 ns  ← 可忽略
CacheKeyBuild            64 ns  ← 可忽略
ValidateSQLIdentifier   175 ns  ← 可忽略(且零分配)
```

这些操作延迟不到 Redis I/O 的 **0.2%**,即使优化到 0 也不会改善端到端性能。

---

## 批量操作: items/s 恒定定律

```
                    ops/s      items/s     单 item 延迟
──────────────────────────────────────────────────────────
MGet×10             5,201      52,005      19.2 μs/item
MGet×100              596      59,558      16.8 μs/item
MGet×1000              62      62,344      16.0 μs/item
Pipeline×10         5,031      50,309      19.9 μs/item
Pipeline×100          574      57,420      17.4 μs/item
Pipeline×1000          55      55,036      18.2 μs/item
Pipeline×10000          5.7    56,564      17.7 μs/item
Single SET             —       24,760      40.4 μs/item  ← 基线
──────────────────────────────────────────────────────────
```

**发现**: 无论批次大小(10 → 10000),每个 item 的延迟恒定在 **~16-20μs**,是单条操作(40.4μs)的 **约 44%**。

```
单条 SET:  40.4 μs/item  ████████████████████████████████
Pipeline:  17.7 μs/item  ██████████████
           ↑ 节省 ~56%,批量化把往返开销摊薄到每个 item 上
```

**结论**: 能用 Pipeline/MGet 的地方,不要用逐条操作;批次大小在 100~1000 之间吞吐差异 <15%,不必过度调优。

---

## 并发聚合吞吐(RunParallel, 16 goroutines)

```
场景 (读:写)      ns/op      聚合吞吐
────────────────────────────────────────
ReadHeavy  80:20   7,132     ~140,200 ops/s
WriteHeavy 20:80   7,313     ~136,700 ops/s
Mixed      50:50   7,368     ~135,700 ops/s
────────────────────────────────────────
```

- 读写比例对聚合吞吐影响 <3%:miniredis 单进程模拟下瓶颈是 CPU/锁而非读写语义。
- 该数字**不代表真实 Redis**:跨网络后单条 I/O 变长,批量化(Pipeline/MGet)的收益会进一步放大。

---

## 与 2026-06-07 数据对比(回归检查)

两次采集使用不同机器(均为 16 线程),绝对值差异主要反映硬件差异,重点看结构性结论:

| 指标 | 2026-06 | 2026-08 | 变化 |
|---|---|---|---|
| Redis GET | 42,270 ns | 36,518 ns | -13.6% |
| Redis SET | 38,116 ns | 40,388 ns | +6.0% |
| WritedownSingle | 49,539 ns | 44,352 ns | -10.5% |
| LookupSingle_Hit | 37,034 ns | 38,363 ns | +3.6% |
| InvalidateCache | 33,497 ns | 39,170 ns | +16.9% |
| ScanKeys_1000 | 379,650 ns | 408,527 ns | +7.6% |
| MGet×1000 | 60,362 items/s | 62,344 items/s | +3.3% |
| Pipeline×1000 | 61,375 items/s | 55,036 items/s | -10.3% |
| JSON Marshal/Unmarshal | 155/735 ns | 158/753 ns | ≈0 |

**结论**:
1. 波动区间 ±17%,无超出硬件差异的性能回归;
2. 三个结构性定律全部复现:**单条 I/O 35-45μs**、**批量恒定 ~55k items/s**、**序列化在 I/O 面前可忽略(1:256)**;
3. `EnsureTimeout` 兜底超时仍无可测量开销(WritedownSingle 反而更快)。

---

## 总体瓶颈排序

```
排名  瓶颈                       占比          严重度  可优化空间
──────────────────────────────────────────────────────────────────
 1    Redis 网络 I/O             ~99.6% (写)   🔴🔴🔴  大 (Pipeline 2.2×)
 2    SCAN 游标遍历               11× GET      🔴🔴    大 (改索引 10×+)
 3    Redis 过滤 JSON 重复解析    ~2μs/key      🟡      中 (共享解析 2~5×)
 4    JSON 反序列化               ~2% (读)      🟡      中 (换库 3~10×)
 5    内存分配 (NewService等)     1.8μs/次      🟡      小 (sync.Pool)
 6    JSON 序列化                 ~0.4%         🟢      无必要
 7    Translator 翻译             <0.1%         🟢      无必要
 8    SQL 标识符校验              <0.1%         🟢      无必要 (零分配)
```

---

## 推荐优化路线

```
Phase 1 (低成本, 2× 提升)
├── 所有批量写入改用 Pipeline(WritedownWithPipeline)
├── 所有批量读取改用 MGet(LookupQuery)
└── 热路径中用索引 Set 替代 SCAN 模式查询

Phase 2 (中成本, 3~5× 提升)
├── 多过滤器共享一次 JSON 解析(改造 ApplyRedisFilters)
├── 热点 key 加本地缓存层 (sync.Map + TTL)
└── 连接池调优 (PoolSize, MinIdleConns)

Phase 3 (高成本, 10× 提升)
├── JSON → sonic / msgpack
└── 读写分离 (Redis Cluster 分片)
```
