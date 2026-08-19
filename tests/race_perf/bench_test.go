package race_perf

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Super-Gagaga/AbstractManager/service"
	"github.com/Super-Gagaga/AbstractManager/util/filter_translator"
)

// =============================================================================
// 性能基准测试 (Benchmarks)
// 运行: go test -bench=. -benchmem ./tests/race_perf/
// =============================================================================

// =============================================================================
// BM-001 ~ BM-007: 缓存写入性能
// =============================================================================

func BenchmarkWritedownQuery_Pipeline_100(b *testing.B) {
	flushRedis(b)
	sm := newRaceSM()
	users := generateUsers(100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = sm.WritedownWithPipeline(
			context.Background(),
			users,
			func(u *raceTestUser) string {
				return fmt.Sprintf("b001:user:%d:%d", i, u.ID)
			},
			&service.WritedownQueryOptions{
				Expiration: 1 * time.Hour,
				BatchSize:  100,
				Overwrite:  true,
			},
		)
	}
}

func BenchmarkWritedownQuery_Pipeline_1000(b *testing.B) {
	flushRedis(b)
	sm := newRaceSM()
	users := generateUsers(1000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = sm.WritedownWithPipeline(
			context.Background(),
			users,
			func(u *raceTestUser) string {
				return fmt.Sprintf("b002:user:%d:%d", i, u.ID)
			},
			&service.WritedownQueryOptions{
				Expiration: 1 * time.Hour,
				BatchSize:  100,
				Overwrite:  true,
			},
		)
	}
}

func BenchmarkWritedownQuery_Pipeline_10000(b *testing.B) {
	flushRedis(b)
	sm := newRaceSM()
	users := generateUsers(10000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = sm.WritedownWithPipeline(
			context.Background(),
			users,
			func(u *raceTestUser) string {
				return fmt.Sprintf("b003:user:%d:%d", i, u.ID)
			},
			&service.WritedownQueryOptions{
				Expiration: 1 * time.Hour,
				BatchSize:  100,
				Overwrite:  true,
			},
		)
	}
}

func BenchmarkWritedownQuery_BatchSize_50(b *testing.B) {
	flushRedis(b)
	sm := newRaceSM()
	users := generateUsers(1000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = sm.WritedownQuery(
			context.Background(),
			users,
			func(u *raceTestUser) string {
				return fmt.Sprintf("b004:user:%d:%d", i, u.ID)
			},
			&service.WritedownQueryOptions{
				Expiration: 1 * time.Hour,
				BatchSize:  50,
				Overwrite:  true,
			},
		)
	}
}

func BenchmarkWritedownQuery_BatchSize_100(b *testing.B) {
	flushRedis(b)
	sm := newRaceSM()
	users := generateUsers(1000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = sm.WritedownQuery(
			context.Background(),
			users,
			func(u *raceTestUser) string {
				return fmt.Sprintf("b005:user:%d:%d", i, u.ID)
			},
			&service.WritedownQueryOptions{
				Expiration: 1 * time.Hour,
				BatchSize:  100,
				Overwrite:  true,
			},
		)
	}
}

func BenchmarkWritedownQuery_BatchSize_500(b *testing.B) {
	flushRedis(b)
	sm := newRaceSM()
	users := generateUsers(1000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = sm.WritedownQuery(
			context.Background(),
			users,
			func(u *raceTestUser) string {
				return fmt.Sprintf("b006:user:%d:%d", i, u.ID)
			},
			&service.WritedownQueryOptions{
				Expiration: 1 * time.Hour,
				BatchSize:  500,
				Overwrite:  true,
			},
		)
	}
}

func BenchmarkWritedownSingle_Set(b *testing.B) {
	flushRedis(b)
	sm := newRaceSM()
	user := raceTestUser{ID: 1, Name: "bench_single", Age: 25, Email: "bench@test.com"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("b007:user:%d", i)
		_ = sm.WritedownSingle(
			context.Background(),
			key,
			&user,
			&service.WritedownSingleOptions{
				Expiration: 1 * time.Hour,
				Overwrite:  true,
			},
		)
	}
}

// =============================================================================
// BM-101 ~ BM-104: 缓存读取性能
// =============================================================================

func BenchmarkLookupQuery_10Keys(b *testing.B) {
	flushRedis(b)
	sm := newRaceSM()
	ctx := context.Background()

	keys := make([]string, 10)
	for i := 0; i < 10; i++ {
		user := raceTestUser{
			ID:    uint(i + 1),
			Name:  fmt.Sprintf("bench_lookup_%d", i),
			Age:   25,
			Email: fmt.Sprintf("bl%d@test.com", i),
		}
		key := fmt.Sprintf("b101:user:%d", i+1)
		keys[i] = key
		_ = sm.WritedownSingle(ctx, key, &user, &service.WritedownSingleOptions{
			Expiration: 1 * time.Hour,
			Overwrite:  true,
		})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := sm.LookupQuery(ctx, keys, &service.LookupQueryOptions{FallbackToDB: false})
		if err != nil {
			b.Fatalf("lookup failed: %v", err)
		}
	}
}

func BenchmarkLookupQuery_100Keys(b *testing.B) {
	flushRedis(b)
	sm := newRaceSM()
	ctx := context.Background()

	keys := make([]string, 100)
	for i := 0; i < 100; i++ {
		user := raceTestUser{
			ID:    uint(i + 1),
			Name:  fmt.Sprintf("bench_lookup_%d", i),
			Age:   25,
			Email: fmt.Sprintf("bl%d@test.com", i),
		}
		key := fmt.Sprintf("b102:user:%d", i+1)
		keys[i] = key
		_ = sm.WritedownSingle(ctx, key, &user, &service.WritedownSingleOptions{
			Expiration: 1 * time.Hour,
			Overwrite:  true,
		})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := sm.LookupQuery(ctx, keys, &service.LookupQueryOptions{FallbackToDB: false})
		if err != nil {
			b.Fatalf("lookup failed: %v", err)
		}
	}
}

func BenchmarkLookupSingleWithFallback_Hit(b *testing.B) {
	flushRedis(b)
	sm := newRaceSM()
	ctx := context.Background()
	key := "b103:user:hit"
	user := raceTestUser{ID: 1, Name: "cache_hit", Age: 25, Email: "hit@test.com"}

	_ = sm.WritedownSingle(ctx, key, &user, &service.WritedownSingleOptions{
		Expiration: 1 * time.Hour,
		Overwrite:  true,
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var result raceTestUser
		rdb := service.GetRedis()
		_ = rdb.Get(ctx, key).Scan(&result)
	}
}

func BenchmarkLookupSingleWithFallback_Miss(b *testing.B) {
	flushRedis(b)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("b104:user:miss:%d", i)
		var result raceTestUser
		rdb := service.GetRedis()
		err := rdb.Get(ctx, key).Scan(&result)
		_ = err // redis.Nil 预期
	}
}

// =============================================================================
// BM-201 ~ BM-203: 序列化性能
// =============================================================================

func BenchmarkMarshalForRedis(b *testing.B) {
	user := raceTestUser{
		ID:    1,
		Name:  "bench_marshal_user",
		Age:   28,
		Email: "marshal@benchmark.com",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := json.Marshal(&user)
		if err != nil {
			b.Fatalf("marshal failed: %v", err)
		}
	}
}

func BenchmarkUnmarshalForRedis(b *testing.B) {
	user := raceTestUser{
		ID:    1,
		Name:  "bench_unmarshal_user",
		Age:   28,
		Email: "unmarshal@benchmark.com",
	}
	data, _ := json.Marshal(&user)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var result raceTestUser
		err := json.Unmarshal(data, &result)
		if err != nil {
			b.Fatalf("unmarshal failed: %v", err)
		}
	}
}

// BenchmarkExtractID_JSON — JSON 往返提取 ID（通过公开 API 模拟）
// 注意: extractID 是 unexported 函数，这里通过 json.Marshal → map 模拟等价操作
func BenchmarkExtractID_JSON(b *testing.B) {
	user := raceTestUser{
		ID:    12345,
		Name:  "extract_id_user",
		Age:   30,
		Email: "extract@benchmark.com",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		jsonData, _ := json.Marshal(&user)
		var tempMap map[string]interface{}
		_ = json.Unmarshal(jsonData, &tempMap)
		id, ok := tempMap["id"].(float64)
		if !ok {
			b.Fatal("extractID failed")
		}
		_ = uint(id)
	}
}

// =============================================================================
// BM-301 ~ BM-303: 过滤性能（Redis + GORM）
// =============================================================================

func BenchmarkRedisFilter_1Filter_100Keys(b *testing.B) {
	flushRedis(b)
	ctx := context.Background()
	rdb := service.GetRedis()

	keys := make([]string, 100)
	for i := 0; i < 100; i++ {
		user := raceTestUser{
			ID:    uint(i + 1),
			Name:  fmt.Sprintf("filter_user_%d", i),
			Age:   20 + (i % 40),
			Email: fmt.Sprintf("f%d@test.com", i),
		}
		key := fmt.Sprintf("b301:user:%d", i+1)
		keys[i] = key
		jsonData, _ := json.Marshal(&user)
		_ = rdb.Set(ctx, key, jsonData, 1*time.Hour).Err()
	}

	registry := filter_translator.DefaultRedisRegistry
	filterParam := filter_translator.FilterParam{
		Field:    "Age",
		Operator: "=",
		Value:    25,
	}
	redisFilter, err := registry.Translate(filterParam)
	if err != nil {
		b.Fatalf("translate failed: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		keysCopy := make([]string, len(keys))
		copy(keysCopy, keys)
		filtered, err := filter_translator.ApplyRedisFilters(ctx, rdb, keysCopy, []filter_translator.RedisFilter{redisFilter})
		if err != nil {
			b.Fatalf("filter failed: %v", err)
		}
		_ = filtered
	}
}

func BenchmarkRedisFilter_5Filters_1000Keys(b *testing.B) {
	flushRedis(b)
	ctx := context.Background()
	rdb := service.GetRedis()

	keys := make([]string, 1000)
	for i := 0; i < 1000; i++ {
		user := raceTestUser{
			ID:    uint(i + 1),
			Name:  fmt.Sprintf("filter_user_%d", i),
			Age:   20 + (i % 50),
			Email: fmt.Sprintf("f%d@test.com", i),
		}
		key := fmt.Sprintf("b302:user:%d", i+1)
		keys[i] = key
		jsonData, _ := json.Marshal(&user)
		_ = rdb.Set(ctx, key, jsonData, 1*time.Hour).Err()
	}

	registry := filter_translator.DefaultRedisRegistry
	filterParams := []filter_translator.FilterParam{
		{Field: "Age", Operator: ">=", Value: 25},
		{Field: "Age", Operator: "<=", Value: 50},
		{Field: "Name", Operator: "like", Value: "filter"},
		{Field: "Email", Operator: "like", Value: "test"},
		{Field: "ID", Operator: ">=", Value: float64(1)},
	}

	filters := make([]filter_translator.RedisFilter, len(filterParams))
	for i, p := range filterParams {
		f, err := registry.Translate(p)
		if err != nil {
			b.Fatalf("translate filter %d failed: %v", i, err)
		}
		filters[i] = f
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		keysCopy := make([]string, len(keys))
		copy(keysCopy, keys)
		filtered, err := filter_translator.ApplyRedisFilters(ctx, rdb, keysCopy, filters)
		if err != nil {
			b.Fatalf("filter chain failed: %v", err)
		}
		_ = filtered
	}
}

func BenchmarkGormFilter_10Filters(b *testing.B) {
	registry := filter_translator.DefaultGormRegistry
	filterParams := []filter_translator.FilterParam{
		{Field: "id", Operator: ">=", Value: 1},
		{Field: "id", Operator: "<=", Value: 100},
		{Field: "name", Operator: "like", Value: "test"},
		{Field: "age", Operator: ">=", Value: 18},
		{Field: "age", Operator: "<=", Value: 65},
		{Field: "email", Operator: "like", Value: "gmail"},
		{Field: "status", Operator: "=", Value: "active"},
		{Field: "role", Operator: "!=", Value: "admin"},
		{Field: "score", Operator: ">", Value: 50.0},
		{Field: "deleted_at", Operator: "isnull", Value: nil},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		filters, err := registry.TranslateBatch(filterParams)
		if err != nil {
			b.Fatalf("translate batch failed: %v", err)
		}
		_ = filters
	}
}

// =============================================================================
// BM-401 ~ BM-403: 并发吞吐量
// =============================================================================

func benchmarkThroughput(b *testing.B, readRatio float64, writeRatio float64) {
	flushRedis(b)
	ctx := context.Background()
	rdb := service.GetRedis()

	// 预填充一些初始 key
	for i := 0; i < 200; i++ {
		key := fmt.Sprintf("bt:user:%d", i)
		user := raceTestUser{ID: uint(i + 1), Name: fmt.Sprintf("tp_user_%d", i), Age: 25}
		jsonData, _ := json.Marshal(&user)
		_ = rdb.Set(ctx, key, jsonData, 1*time.Hour).Err()
	}

	var readOps, writeOps atomic.Int64
	totalRatio := readRatio + writeRatio

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		localCounter := 0
		for pb.Next() {
			r := float64(localCounter%100) / 100.0 * totalRatio
			localCounter++

			if r < readRatio {
				key := fmt.Sprintf("bt:user:%d", localCounter%200)
				var user raceTestUser
				_ = rdb.Get(ctx, key).Scan(&user)
				readOps.Add(1)
			} else {
				key := fmt.Sprintf("bt:user:w:%d", localCounter)
				user := raceTestUser{
					ID:    uint(localCounter),
					Name:  fmt.Sprintf("tp_write_%d", localCounter),
					Age:   25 + (localCounter % 40),
				}
				jsonData, _ := json.Marshal(&user)
				_ = rdb.Set(ctx, key, jsonData, 10*time.Second).Err()
				writeOps.Add(1)
			}
		}
	})

	b.ReportMetric(float64(readOps.Load()), "reads")
	b.ReportMetric(float64(writeOps.Load()), "writes")
}

func BenchmarkThroughput_ReadHeavy(b *testing.B) {
	benchmarkThroughput(b, 0.8, 0.2)
}

func BenchmarkThroughput_WriteHeavy(b *testing.B) {
	benchmarkThroughput(b, 0.2, 0.8)
}

func BenchmarkThroughput_Mixed(b *testing.B) {
	benchmarkThroughput(b, 0.5, 0.5)
}

// =============================================================================
// 额外: ScanKeys 与 Pipeline 批量操作性能
// =============================================================================

func BenchmarkScanKeys_1000Keys(b *testing.B) {
	flushRedis(b)
	ctx := context.Background()
	rdb := service.GetRedis()

	for i := 0; i < 1000; i++ {
		_ = rdb.Set(ctx, fmt.Sprintf("scan_bench:%d", i), "value", 1*time.Hour).Err()
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		keys, err := service.ScanKeys(ctx, rdb, "scan_bench:*", 100)
		if err != nil {
			b.Fatalf("scan failed: %v", err)
		}
		_ = keys
	}
}

func BenchmarkSetMultiple_100Items(b *testing.B) {
	flushRedis(b)
	ctx := context.Background()
	// 通过 ServiceManager.GetRedisManager() 获取 *RedisManager
	sm := newRaceSM()
	rm := sm.GetRedisManager()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		items := make(map[string]interface{}, 100)
		for j := 0; j < 100; j++ {
			key := fmt.Sprintf("bsm:%d:%d", i, j)
			user := raceTestUser{ID: uint(j + 1), Name: "batch_set", Age: 25}
			items[key] = &user
		}
		_ = rm.SetMultiple(ctx, items, 1*time.Hour)
	}
}

func BenchmarkGetMultiple_100Items(b *testing.B) {
	flushRedis(b)
	ctx := context.Background()
	sm := newRaceSM()
	rm := sm.GetRedisManager()

	keys := make([]string, 100)
	for i := 0; i < 100; i++ {
		keys[i] = fmt.Sprintf("bgm:%d", i)
		user := raceTestUser{ID: uint(i + 1), Name: "batch_get", Age: 25}
		_ = rm.Set(ctx, keys[i], &user, 1*time.Hour)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := rm.GetMultiple(ctx, keys)
		if err != nil {
			b.Fatalf("get multiple failed: %v", err)
		}
	}
}

// =============================================================================
// 辅助基准测试
// =============================================================================

func BenchmarkBuildCacheKey_Uint(b *testing.B) {
	// buildCacheKey 是 unexported 方法，这里内联其逻辑进行基准测试
	cacheKeyName := "race_test_user"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = fmt.Sprintf("%s:%v", cacheKeyName, uint(i))
	}
}

func BenchmarkBuildCacheKey_String(b *testing.B) {
	cacheKeyName := "race_test_user"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = fmt.Sprintf("%s:%v", cacheKeyName, strconv.Itoa(i))
	}
}

func BenchmarkExtractIDFromKey(b *testing.B) {
	// extractIDFromKey 是 unexported 函数，这里内联其逻辑进行基准测试
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("prefix:%d", i)
		parts := strings.SplitN(key, ":", 2) // 等价于 extractIDFromKey 的逻辑
		if len(parts) >= 2 {
			_, _ = strconv.ParseUint(parts[len(parts)-1], 10, 64)
		}
	}
}

func BenchmarkNewServiceManager(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = service.NewServiceManager(raceTestUser{})
	}
}

// =============================================================================
// 综合: 带过期时间设置的 Pipeline 批量写入
// =============================================================================

func BenchmarkWritedownQuery_Baseline(b *testing.B) {
	flushRedis(b)
	sm := newRaceSM()
	users := generateUsers(1000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = sm.WritedownQuery(
			context.Background(),
			users,
			func(u *raceTestUser) string {
				return fmt.Sprintf("bbase:user:%d:%d", i, u.ID)
			},
			&service.WritedownQueryOptions{
				Expiration: 1 * time.Hour,
				BatchSize:  100,
				Overwrite:  true,
			},
		)
	}
}
