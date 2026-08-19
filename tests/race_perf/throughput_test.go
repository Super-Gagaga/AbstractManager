package race_perf

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Super-Gagaga/abstract-manager/service"
	"github.com/Super-Gagaga/abstract-manager/util/filter_translator"
)

// =============================================================================
// Throughput Benchmark Suite — 吞吐量 & 延迟基准测试
//
// 每个 benchmark 输出两个指标:
//   - ns/op    (Go 自动输出) — 单次操作耗时，纳秒
//   - ops/s    (ReportMetric) — 每秒操作数 = 1e9 / ns_per_op
//
// 运行:
//   go test -bench=Throughput -benchmem -benchtime=3s ./tests/race_perf/
//
// 只看吞吐量汇总表格:
//   go test -bench=Throughput -benchmem ./tests/race_perf/ | grep -E "(Benchmark|ns/op)"
// =============================================================================

// throughputReport 在 benchmark 结束后计算并输出 ops/s。
// 用法: b.StopTimer; defer throughputReport(b, "op_label")
func throughputReport(b *testing.B, label string) {
	elapsed := b.Elapsed().Seconds()
	ops := float64(b.N)
	if elapsed > 0 {
		opsPerSec := ops / elapsed
		nsPerOp := elapsed / ops * 1e9
		b.ReportMetric(opsPerSec, "ops/s")
		b.ReportMetric(nsPerOp, "ns/op")
	}
}

// =============================================================================
// TP-001 ~ TP-004: 基础 Redis 读写
// =============================================================================

// BenchmarkThroughput_RedisSet 单条 SET 吞吐量
func BenchmarkThroughput_RedisSet(b *testing.B) {
	flushRedis(b)
	ctx := context.Background()
	rdb := service.GetRedis()

	payload := `{"id":1,"name":"tp","age":25,"email":"tp@test.com"}`
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("tp:set:%d", i)
		_ = rdb.Set(ctx, key, payload, 1*time.Hour).Err()
	}

	b.StopTimer()
	elapsed := b.Elapsed().Seconds()
	b.ReportMetric(float64(b.N)/elapsed, "ops/s")
}

// BenchmarkThroughput_RedisGet 单条 GET 吞吐量（命中）
func BenchmarkThroughput_RedisGet(b *testing.B) {
	flushRedis(b)
	ctx := context.Background()
	rdb := service.GetRedis()

	// 预填充
	for i := 0; i < 1000; i++ {
		_ = rdb.Set(ctx, fmt.Sprintf("tp:get:%d", i), "value", 1*time.Hour).Err()
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = rdb.Get(ctx, fmt.Sprintf("tp:get:%d", i%1000)).Result()
	}

	b.StopTimer()
	elapsed := b.Elapsed().Seconds()
	b.ReportMetric(float64(b.N)/elapsed, "ops/s")
}

// BenchmarkThroughput_RedisGetMiss 单条 GET 吞吐量（未命中）
func BenchmarkThroughput_RedisGetMiss(b *testing.B) {
	flushRedis(b)
	ctx := context.Background()
	rdb := service.GetRedis()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = rdb.Get(ctx, fmt.Sprintf("tp:miss:%d", i)).Result()
	}

	b.StopTimer()
	elapsed := b.Elapsed().Seconds()
	b.ReportMetric(float64(b.N)/elapsed, "ops/s")
}

// BenchmarkThroughput_RedisDel 单条 DEL 吞吐量
func BenchmarkThroughput_RedisDel(b *testing.B) {
	flushRedis(b)
	ctx := context.Background()
	rdb := service.GetRedis()

	// 预填充（每个 op 先 set 再 del，所以需要数据存在）
	for i := 0; i < b.N; i++ {
		_ = rdb.Set(ctx, fmt.Sprintf("tp:del:%d", i), "v", 1*time.Hour).Err()
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = rdb.Del(ctx, fmt.Sprintf("tp:del:%d", i)).Err()
	}

	b.StopTimer()
	elapsed := b.Elapsed().Seconds()
	b.ReportMetric(float64(b.N)/elapsed, "ops/s")
}

// =============================================================================
// TP-005 ~ TP-009: ServiceManager 操作
// =============================================================================

// BenchmarkThroughput_WritedownSingle 单条 WritedownSingle 吞吐量
func BenchmarkThroughput_WritedownSingle(b *testing.B) {
	flushRedis(b)
	sm := newRaceSM()
	user := raceTestUser{ID: 1, Name: "tp_single", Age: 25, Email: "tp@test.com"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("tp:ws:%d", i)
		_ = sm.WritedownSingle(
			context.Background(), key, &user,
			&service.WritedownSingleOptions{Expiration: 1 * time.Hour, Overwrite: true},
		)
	}

	b.StopTimer()
	elapsed := b.Elapsed().Seconds()
	b.ReportMetric(float64(b.N)/elapsed, "ops/s")
}

// BenchmarkThroughput_LookupSingle_Hit 单条缓存查询吞吐量（命中）
func BenchmarkThroughput_LookupSingle_Hit(b *testing.B) {
	flushRedis(b)
	sm := newRaceSM()
	ctx := context.Background()

	// 预填充一个固定的 key
	user := raceTestUser{ID: 1, Name: "hit", Age: 25, Email: "hit@test.com"}
	key := "tp:lookup:hit"
	_ = sm.WritedownSingle(ctx, key, &user,
		&service.WritedownSingleOptions{Expiration: 1 * time.Hour, Overwrite: true},
	)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var result raceTestUser
		_ = sm.GetRedisManager().Get(ctx, key, &result)
	}

	b.StopTimer()
	elapsed := b.Elapsed().Seconds()
	b.ReportMetric(float64(b.N)/elapsed, "ops/s")
}

// BenchmarkThroughput_NewServiceManager ServiceManager 构造吞吐量
func BenchmarkThroughput_NewServiceManager(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = service.NewServiceManager(raceTestUser{})
	}

	b.StopTimer()
	elapsed := b.Elapsed().Seconds()
	b.ReportMetric(float64(b.N)/elapsed, "ops/s")
}

// BenchmarkThroughput_InvalidateCache 单 key 失效吞吐量
func BenchmarkThroughput_InvalidateCache(b *testing.B) {
	flushRedis(b)
	sm := newRaceSM()
	ctx := context.Background()

	// 预填充
	for i := 0; i < min(b.N, 10000); i++ {
		user := raceTestUser{ID: uint(i + 1), Name: fmt.Sprintf("inv_%d", i)}
		_ = sm.WritedownSingle(ctx, fmt.Sprintf("tp:inv:%d", i), &user,
			&service.WritedownSingleOptions{Expiration: 1 * time.Hour, Overwrite: true},
		)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = sm.InvalidateCache(ctx, fmt.Sprintf("tp:inv:%d", i%10000))
	}

	b.StopTimer()
	elapsed := b.Elapsed().Seconds()
	b.ReportMetric(float64(b.N)/elapsed, "ops/s")
}

// =============================================================================
// TP-010 ~ TP-013: 批量操作
// =============================================================================

// benchmarkThroughputGetMultiple 通用批量 GET 吞吐量
func benchmarkThroughputGetMultiple(b *testing.B, numKeys int) {
	flushRedis(b)
	ctx := context.Background()
	sm := newRaceSM()
	rm := sm.GetRedisManager()

	// 预填充
	keys := make([]string, numKeys)
	for i := 0; i < numKeys; i++ {
		keys[i] = fmt.Sprintf("tp:gm:%d", i)
		user := raceTestUser{ID: uint(i + 1), Name: fmt.Sprintf("gm_%d", i)}
		_ = rm.Set(ctx, keys[i], &user, 1*time.Hour)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = rm.GetMultiple(ctx, keys)
	}

	b.StopTimer()
	elapsed := b.Elapsed().Seconds()
	opsPerSec := float64(b.N) / elapsed
	itemThroughput := opsPerSec * float64(numKeys)
	b.ReportMetric(opsPerSec, "ops/s")
	b.ReportMetric(itemThroughput, "items/s")
}

func BenchmarkThroughput_GetMultiple_10Keys(b *testing.B)   { benchmarkThroughputGetMultiple(b, 10) }
func BenchmarkThroughput_GetMultiple_100Keys(b *testing.B)  { benchmarkThroughputGetMultiple(b, 100) }
func BenchmarkThroughput_GetMultiple_1000Keys(b *testing.B) { benchmarkThroughputGetMultiple(b, 1000) }

// benchmarkThroughputSetMultiple 通用批量 SET 吞吐量
func benchmarkThroughputSetMultiple(b *testing.B, numItems int) {
	flushRedis(b)
	ctx := context.Background()
	sm := newRaceSM()
	rm := sm.GetRedisManager()

	// 预构建 items map
	items := make(map[string]interface{}, numItems)
	for j := 0; j < numItems; j++ {
		items[fmt.Sprintf("tp:sm:%d", j)] = &raceTestUser{ID: uint(j + 1), Name: "batch", Age: 25}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = rm.SetMultiple(ctx, items, 1*time.Hour)
	}

	b.StopTimer()
	elapsed := b.Elapsed().Seconds()
	opsPerSec := float64(b.N) / elapsed
	b.ReportMetric(opsPerSec, "ops/s")
	b.ReportMetric(opsPerSec*float64(numItems), "items/s")
}

func BenchmarkThroughput_SetMultiple_10Items(b *testing.B)  { benchmarkThroughputSetMultiple(b, 10) }
func BenchmarkThroughput_SetMultiple_100Items(b *testing.B) { benchmarkThroughputSetMultiple(b, 100) }

// =============================================================================
// TP-014 ~ TP-016: 序列化
// =============================================================================

func BenchmarkThroughput_JSONMarshal(b *testing.B) {
	user := raceTestUser{
		ID: 1, Name: "marshal_throughput", Age: 28, Email: "mt@benchmark.com",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = json.Marshal(&user)
	}

	b.StopTimer()
	elapsed := b.Elapsed().Seconds()
	b.ReportMetric(float64(b.N)/elapsed, "ops/s")
}

func BenchmarkThroughput_JSONUnmarshal(b *testing.B) {
	user := raceTestUser{ID: 1, Name: "unmarshal_tp", Age: 28, Email: "ut@benchmark.com"}
	data, _ := json.Marshal(&user)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var result raceTestUser
		_ = json.Unmarshal(data, &result)
	}

	b.StopTimer()
	elapsed := b.Elapsed().Seconds()
	b.ReportMetric(float64(b.N)/elapsed, "ops/s")
}

// BenchmarkThroughput_CacheKeyBuild 缓存 key 构建吞吐量
func BenchmarkThroughput_CacheKeyBuild(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = fmt.Sprintf("cache:race_test_user:%d", uint(i))
	}

	b.StopTimer()
	elapsed := b.Elapsed().Seconds()
	b.ReportMetric(float64(b.N)/elapsed, "ops/s")
}

// =============================================================================
// TP-017 ~ TP-019: Filter Translator
// =============================================================================

func BenchmarkThroughput_GormTranslate_Equal(b *testing.B) {
	registry := filter_translator.DefaultGormRegistry
	param := filter_translator.FilterParam{Field: "age", Operator: "=", Value: 25}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = registry.Translate(param)
	}

	b.StopTimer()
	elapsed := b.Elapsed().Seconds()
	b.ReportMetric(float64(b.N)/elapsed, "ops/s")
}

func BenchmarkThroughput_GormTranslate_Batch10(b *testing.B) {
	registry := filter_translator.DefaultGormRegistry
	params := []filter_translator.FilterParam{
		{Field: "id", Operator: ">=", Value: 1},
		{Field: "name", Operator: "like", Value: "test"},
		{Field: "age", Operator: ">=", Value: 18},
		{Field: "age", Operator: "<=", Value: 65},
		{Field: "email", Operator: "like", Value: "gmail"},
		{Field: "status", Operator: "=", Value: "active"},
		{Field: "role", Operator: "!=", Value: "admin"},
		{Field: "score", Operator: ">", Value: 50.0},
		{Field: "id", Operator: "in", Value: []interface{}{1, 2, 3}},
		{Field: "deleted_at", Operator: "isnull", Value: nil},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = registry.TranslateBatch(params)
	}

	b.StopTimer()
	elapsed := b.Elapsed().Seconds()
	b.ReportMetric(float64(b.N)/elapsed, "ops/s")
}

func BenchmarkThroughput_RedisTranslate_Equal(b *testing.B) {
	registry := filter_translator.DefaultRedisRegistry
	param := filter_translator.FilterParam{Field: "name", Operator: "=", Value: "test"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = registry.Translate(param)
	}

	b.StopTimer()
	elapsed := b.Elapsed().Seconds()
	b.ReportMetric(float64(b.N)/elapsed, "ops/s")
}

// =============================================================================
// TP-020 ~ TP-021: ScanKeys & 杂项
// =============================================================================

func BenchmarkThroughput_ScanKeys_1000(b *testing.B) {
	flushRedis(b)
	ctx := context.Background()
	rdb := service.GetRedis()

	for i := 0; i < 1000; i++ {
		_ = rdb.Set(ctx, fmt.Sprintf("tp:scan:%d", i), "v", 1*time.Hour).Err()
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = service.ScanKeys(ctx, rdb, "tp:scan:*", 100)
	}

	b.StopTimer()
	elapsed := b.Elapsed().Seconds()
	b.ReportMetric(float64(b.N)/elapsed, "ops/s")
}

func BenchmarkThroughput_ExtractIDFromKey(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("prefix:%d", i)
		parts := strings.SplitN(key, ":", 2)
		if len(parts) >= 2 {
			_, _ = parseUintFast(parts[len(parts)-1])
		}
	}

	b.StopTimer()
	elapsed := b.Elapsed().Seconds()
	b.ReportMetric(float64(b.N)/elapsed, "ops/s")
}

// parseUintFast 内联的快速字符串→uint 转换，避免 strconv 开销影响测量
func parseUintFast(s string) (uint, error) {
	var n uint
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("invalid char: %c", c)
		}
		n = n*10 + uint(c-'0')
	}
	return n, nil
}

// =============================================================================
// TP-022 ~ TP-024: Pipeline 批量写（按单条 item 计吞吐）
// =============================================================================

// benchmarkThroughputPipelineWrite items/s = ops/s × batch size
func benchmarkThroughputPipelineWrite(b *testing.B, batchSize int) {
	flushRedis(b)
	sm := newRaceSM()
	users := generateUsers(batchSize)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = sm.WritedownWithPipeline(
			context.Background(), users,
			func(u *raceTestUser) string {
				return fmt.Sprintf("tp:pipe:%d:%d", i, u.ID)
			},
			&service.WritedownQueryOptions{
				Expiration: 1 * time.Hour, BatchSize: batchSize, Overwrite: true,
			},
		)
	}

	b.StopTimer()
	elapsed := b.Elapsed().Seconds()
	opsPerSec := float64(b.N) / elapsed
	b.ReportMetric(opsPerSec, "ops/s")
	b.ReportMetric(opsPerSec*float64(batchSize), "items/s")
}

func BenchmarkThroughput_PipelineWrite_100(b *testing.B)  { benchmarkThroughputPipelineWrite(b, 100) }
func BenchmarkThroughput_PipelineWrite_1000(b *testing.B) { benchmarkThroughputPipelineWrite(b, 1000) }

// =============================================================================
// TP-025: SQLIdentifier 验证
// =============================================================================

func BenchmarkThroughput_ValidateSQLIdentifier(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = filter_translator.ValidateSQLIdentifier("user_email_123")
	}

	b.StopTimer()
	elapsed := b.Elapsed().Seconds()
	b.ReportMetric(float64(b.N)/elapsed, "ops/s")
}
