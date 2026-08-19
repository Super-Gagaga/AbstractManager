package race_perf

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Super-Gagaga/AbstractManager/service"
	"github.com/Super-Gagaga/AbstractManager/util/filter_translator"

	"github.com/alicebob/miniredis/v2"
)

// =============================================================================
// TestMain — 全局 miniredis 初始化
// 通过环境变量 + service.InitRedis() 注入 miniredis，避免直接操作 unexported 的 globalRedisManager
// =============================================================================

var (
	testRedisManager *service.RedisManager
	testMiniRedis    *miniredis.Miniredis
)

func TestMain(m *testing.M) {
	// 启动 miniredis
	mr, err := miniredis.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "miniredis.Run failed: %v\n", err)
		os.Exit(1)
	}
	testMiniRedis = mr

	// 解析 miniredis 地址
	host, port, err := net.SplitHostPort(mr.Addr())
	if err != nil {
		fmt.Fprintf(os.Stderr, "SplitHostPort failed: %v\n", err)
		os.Exit(1)
	}

	// 设置环境变量供 service.InitRedis() 使用
	os.Setenv("REDIS_HOST", host)
	os.Setenv("REDIS_PORT", port)
	os.Setenv("REDIS_PASSWORD", "")

	// 调用 service.InitRedis() 完成全局初始化
	rm, err := service.InitRedis()
	if err != nil {
		fmt.Fprintf(os.Stderr, "InitRedis failed: %v\n", err)
		os.Exit(1)
	}
	testRedisManager = rm

	// 运行所有测试
	code := m.Run()

	// 清理
	rm.Close()
	mr.Close()
	os.Exit(code)
}

// =============================================================================
// Shared Test Helpers (used by both race tests and benchmarks)
// =============================================================================

// raceTestUser 测试用模型
type raceTestUser struct {
	ID    uint   `json:"id" gorm:"primaryKey"`
	Name  string `json:"name"`
	Age   int    `json:"age"`
	Email string `json:"email"`
}

// newRaceSM 创建一个用于竞态测试的 ServiceManager
func newRaceSM() *service.ServiceManager[raceTestUser] {
	return service.NewServiceManager(raceTestUser{})
}

// generateUsers 生成指定数量的测试用户数据
func generateUsers(n int) []raceTestUser {
	users := make([]raceTestUser, n)
	for i := 0; i < n; i++ {
		users[i] = raceTestUser{
			ID:    uint(i + 1),
			Name:  fmt.Sprintf("user_%d", i+1),
			Age:   20 + (i % 40),
			Email: fmt.Sprintf("user%d@test.com", i+1),
		}
	}
	return users
}

// flushRedis 在测试之间清空 miniredis，保证测试隔离
func flushRedis(t testing.TB) {
	t.Helper()
	testMiniRedis.FlushAll()
}

// prefillCacheForBench 预填充缓存数据
func prefillCacheForBench(b *testing.B, sm *service.ServiceManager[raceTestUser], numKeys int) {
	b.Helper()
	ctx := context.Background()
	users := generateUsers(numKeys)
	for i := range users {
		key := fmt.Sprintf("bench:user:%d", users[i].ID)
		_ = sm.WritedownSingle(ctx, key, &users[i], &service.WritedownSingleOptions{
			Expiration: 1 * time.Hour,
			Overwrite:  true,
		})
	}
}

// =============================================================================
// RT-001: ConcurrentSubmit — 100 goroutines × 10 async submits
// 检测: asyncStarted 读/写, channel send 数据竞争
// 运行: go test -race -run=TestRace_ConcurrentSubmit -count=3 ./tests/race_perf/
// =============================================================================

func TestRace_ConcurrentSubmit(t *testing.T) {
	flushRedis(t)
	sm := newRaceSM()
	ctx := context.Background()

	var wg sync.WaitGroup
	numGoroutines := 100
	submitsPerRoutine := 10

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for i := 0; i < submitsPerRoutine; i++ {
				user := raceTestUser{
					ID:   uint(gid*submitsPerRoutine + i + 1),
					Name: fmt.Sprintf("g%d_i%d", gid, i),
					Age:  25,
				}
				key := fmt.Sprintf("rt001:user:%d", user.ID)
				sm.WritedownSingleAsync(ctx, key, &user, 1*time.Hour)
			}
		}(g)
	}

	wg.Wait()

	// 等待异步任务处理完成
	time.Sleep(100 * time.Millisecond)
	sm.ShutdownAsyncWorkers()
}

// =============================================================================
// RT-002: StartShutdownRace — 20 并发 start + 20 并发 shutdown
// 检测: asyncMu 锁, close(asyncShutdown) 二次关闭
// 运行: go test -race -run=TestRace_StartShutdownRace -count=5 ./tests/race_perf/
// =============================================================================

func TestRace_StartShutdownRace(t *testing.T) {
	flushRedis(t)
	sm := newRaceSM()
	ctx := context.Background()

	var wg sync.WaitGroup
	numStarters := 20
	numShutdowners := 20

	// 20 goroutines 并发调用 WritedownSingleAsync（内部触发 startAsyncWorkersOnce）
	for i := 0; i < numStarters; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			user := raceTestUser{ID: uint(idx + 1), Name: fmt.Sprintf("starter_%d", idx)}
			sm.WritedownSingleAsync(ctx, fmt.Sprintf("rt002:user:%d", idx), &user, 1*time.Hour)
		}(i)
	}

	// 20 goroutines 并发调用 ShutdownAsyncWorkers
	// 注意: 这里会触发 close of closed channel panic，这是被测代码的已知竞态 bug
	// 使用 recover 捕获 panic，使测试能完成并让 race detector 报告数据竞争
	for i := 0; i < numShutdowners; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					t.Logf("shutdown goroutine %d recovered from panic: %v", idx, r)
				}
			}()
			time.Sleep(time.Duration(idx%5) * time.Millisecond)
			sm.ShutdownAsyncWorkers()
		}(i)
	}

	wg.Wait()
}

// =============================================================================
// RT-003: ShutdownWhileWriting — 4 workers 工作中 → shutdown
// 检测: select 分支在 drain 循环中的竞争
// 运行: go test -race -run=TestRace_ShutdownWhileWriting -count=3 ./tests/race_perf/
// =============================================================================

func TestRace_ShutdownWhileWriting(t *testing.T) {
	flushRedis(t)
	sm := newRaceSM()
	ctx := context.Background()

	// 预填充队列：大量异步写入任务
	numTasks := 200
	for i := 0; i < numTasks; i++ {
		user := raceTestUser{
			ID:    uint(i + 1),
			Name:  fmt.Sprintf("task_%d", i),
			Age:   30,
			Email: fmt.Sprintf("task%d@test.com", i),
		}
		key := fmt.Sprintf("rt003:user:%d", i)
		sm.WritedownSingleAsync(ctx, key, &user, 1*time.Hour)
	}

	// 立即 shutdown，此时 worker 可能正在处理任务
	// 测试 asyncWorker 中 select 的两个分支的竞争：
	// case <-sm.asyncShutdown 和 case task := <-sm.asyncTasks
	time.Sleep(10 * time.Millisecond)
	sm.ShutdownAsyncWorkers()
}

// =============================================================================
// RT-004: HotLoop — 持续读写 + 定期启停
// 检测: 长期运行的 race 累积
// 运行: go test -race -run=TestRace_HotLoop -count=1 ./tests/race_perf/
// =============================================================================

func TestRace_HotLoop(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping hot loop test in short mode")
	}

	flushRedis(t)
	sm := newRaceSM()
	ctx := context.Background()

	done := make(chan struct{})
	duration := 5 * time.Second

	var writerWg sync.WaitGroup

	// Writer goroutines: 持续异步写入
	for w := 0; w < 4; w++ {
		writerWg.Add(1)
		go func(wid int) {
			defer writerWg.Done()
			counter := 0
			for {
				select {
				case <-done:
					return
				default:
				}
				user := raceTestUser{
					ID:    uint(wid*10000 + counter),
					Name:  fmt.Sprintf("hotloop_w%d_c%d", wid, counter),
					Age:   20 + (counter % 40),
				}
				key := fmt.Sprintf("rt004:user:%d", user.ID)
				sm.WritedownSingleAsync(ctx, key, &user, 1*time.Hour)
				counter++
				time.Sleep(time.Millisecond)
			}
		}(w)
	}

	// Reader goroutines: 持续从缓存读取
	for r := 0; r < 2; r++ {
		writerWg.Add(1)
		go func(rid int) {
			defer writerWg.Done()
			for {
				select {
				case <-done:
					return
				default:
				}
				keys := []string{
					fmt.Sprintf("rt004:user:%d", rid*100),
					fmt.Sprintf("rt004:user:%d", rid*100+1),
				}
				_, _ = sm.LookupQuery(ctx, keys, &service.LookupQueryOptions{FallbackToDB: false})
				time.Sleep(2 * time.Millisecond)
			}
		}(r)
	}

	// Start/Stop goroutine: 定期创建和销毁新的 ServiceManager
	writerWg.Add(1)
	go func() {
		defer writerWg.Done()
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				tempSM := service.NewServiceManager(raceTestUser{})
				user := raceTestUser{ID: 99999, Name: "temp"}
				tempSM.WritedownSingleAsync(ctx, "rt004:temp", &user, 1*time.Minute)
				time.Sleep(50 * time.Millisecond)
				tempSM.ShutdownAsyncWorkers()
			}
		}
	}()

	time.Sleep(duration)
	close(done)
	writerWg.Wait()
	sm.ShutdownAsyncWorkers()
}

// =============================================================================
// RT-005: MultiServiceManager — 5 个 sm 实例并发操作
// 检测: globalRedisManager 全局变量读安全
// 运行: go test -race -run=TestRace_MultiServiceManager -count=3 ./tests/race_perf/
// =============================================================================

func TestRace_MultiServiceManager(t *testing.T) {
	flushRedis(t)

	numInstances := 5
	sms := make([]*service.ServiceManager[raceTestUser], numInstances)
	for i := 0; i < numInstances; i++ {
		sms[i] = service.NewServiceManager(raceTestUser{})
	}

	ctx := context.Background()
	var wg sync.WaitGroup

	for i := 0; i < numInstances; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sm := sms[idx]
			user := raceTestUser{
				ID:    uint(idx*100 + 1),
				Name:  fmt.Sprintf("multi_%d", idx),
				Age:   25,
				Email: fmt.Sprintf("multi%d@test.com", idx),
			}

			// 写入缓存
			key := fmt.Sprintf("rt005:user:%d", idx)
			err := sm.WritedownSingle(ctx, key, &user, &service.WritedownSingleOptions{
				Expiration: 1 * time.Hour,
				Overwrite:  true,
			})
			if err != nil {
				t.Logf("instance %d write error: %v", idx, err)
			}

			// 查询缓存
			var result raceTestUser
			rdb := service.GetRedis()
			if err := rdb.Get(ctx, key).Scan(&result); err != nil {
				t.Logf("instance %d read error: %v", idx, err)
			}

			// 使缓存失效
			_ = sm.InvalidateCache(ctx, key)
		}(i)
	}

	wg.Wait()
}

// =============================================================================
// RT-101: WriteReadSameKey — 10 writers + 20 readers on same key
// 检测: Pipeline 并发安全, Set/Get 同一 key 的竞争
// 运行: go test -race -run=TestRace_WriteReadSameKey -count=5 ./tests/race_perf/
// =============================================================================

func TestRace_WriteReadSameKey(t *testing.T) {
	flushRedis(t)
	ctx := context.Background()
	sharedKey := "rt101:shared_user"
	numWriters := 10
	numReaders := 20
	iterations := 50

	var wg sync.WaitGroup

	// Writers: 并发写同一个 key，使用 Pipeline
	for w := 0; w < numWriters; w++ {
		wg.Add(1)
		go func(wid int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				user := raceTestUser{
					ID:    uint(wid*iterations + i + 1),
					Name:  fmt.Sprintf("writer_%d_iter_%d", wid, i),
					Age:   wid + i,
					Email: fmt.Sprintf("w%d_i%d@test.com", wid, i),
				}

				rdb := service.GetRedis()
				pipe := rdb.Pipeline()
				jsonData, _ := json.Marshal(&user)
				pipe.Set(ctx, sharedKey, jsonData, 1*time.Hour)
				_, _ = pipe.Exec(ctx)
			}
		}(w)
	}

	// Readers: 并发读同一个 key
	for r := 0; r < numReaders; r++ {
		wg.Add(1)
		go func(rid int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				var user raceTestUser
				rdb := service.GetRedis()
				data, err := rdb.Get(ctx, sharedKey).Bytes()
				if err == nil {
					_ = json.Unmarshal(data, &user)
				}
			}
		}(r)
	}

	// Mixed: 同时读写
	for m := 0; m < 5; m++ {
		wg.Add(1)
		go func(mid int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				if i%2 == 0 {
					user := raceTestUser{ID: uint(100000 + mid), Name: "mixed"}
					rdb := service.GetRedis()
					jsonData, _ := json.Marshal(&user)
					rdb.Set(ctx, sharedKey, jsonData, 1*time.Hour)
				} else {
					var user raceTestUser
					rdb := service.GetRedis()
					data, err := rdb.Get(ctx, sharedKey).Bytes()
					if err == nil {
						_ = json.Unmarshal(data, &user)
					}
				}
			}
		}(m)
	}

	wg.Wait()
}

// =============================================================================
// RT-102: InvalidateWhileReading — LookupQuery + InvalidateCache 并发
// 检测: 读取过程中删除数据的结果一致性
// 运行: go test -race -run=TestRace_InvalidateWhileReading -count=5 ./tests/race_perf/
// =============================================================================

func TestRace_InvalidateWhileReading(t *testing.T) {
	flushRedis(t)
	sm := newRaceSM()
	ctx := context.Background()

	// 预填充缓存数据
	numKeys := 200
	keys := make([]string, numKeys)
	for i := 0; i < numKeys; i++ {
		user := raceTestUser{
			ID:    uint(i + 1),
			Name:  fmt.Sprintf("cache_user_%d", i),
			Age:   25,
			Email: fmt.Sprintf("cache%d@test.com", i),
		}
		key := fmt.Sprintf("rt102:user:%d", i+1)
		keys[i] = key
		sm.WritedownSingle(ctx, key, &user, &service.WritedownSingleOptions{
			Expiration: 1 * time.Hour,
			Overwrite:  true,
		})
	}

	var wg sync.WaitGroup
	iterations := 30

	// Readers: 持续批量读取
	for r := 0; r < 10; r++ {
		wg.Add(1)
		go func(rid int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				start := (rid * 10) % numKeys
				end := start + 10
				if end > numKeys {
					end = numKeys
				}
				batch := keys[start:end]
				result, err := sm.LookupQuery(ctx, batch, &service.LookupQueryOptions{FallbackToDB: false})
				if err != nil {
					continue // 允许部分 key 被删除后 MGet 返回 nil
				}
				_ = result
			}
		}(r)
	}

	// Invalidators: 持续删除缓存
	for inv := 0; inv < 5; inv++ {
		wg.Add(1)
		go func(iid int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				start := (iid * 5) % numKeys
				deleteKeys := keys[start : start+5]
				_ = sm.InvalidateCache(ctx, deleteKeys...)
				time.Sleep(time.Millisecond)
			}
		}(inv)
	}

	wg.Wait()
}

// =============================================================================
// RT-103: VersionWriteRace — 5 goroutines 并发 WritedownSingleWithVersion
// 检测: Watch/TxPipelined 原子性（乐观锁竞争）
// 运行: go test -race -run=TestRace_VersionWriteRace -count=5 ./tests/race_perf/
// =============================================================================

func TestRace_VersionWriteRace(t *testing.T) {
	flushRedis(t)
	sm := newRaceSM()
	ctx := context.Background()

	sharedKey := "rt103:versioned_user"
	baseVersion := int64(1)

	initUser := raceTestUser{ID: 1, Name: "v1", Age: 20, Email: "v1@test.com"}
	if err := sm.WritedownSingle(ctx, sharedKey, &initUser, &service.WritedownSingleOptions{
		Expiration: 1 * time.Hour,
		Overwrite:  true,
	}); err != nil {
		t.Fatalf("initial write failed: %v", err)
	}

	var wg sync.WaitGroup
	numGoroutines := 5

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			user := raceTestUser{
				ID:    1,
				Name:  fmt.Sprintf("versioned_g%d", gid),
				Age:   20 + gid,
				Email: fmt.Sprintf("vg%d@test.com", gid),
			}
			version := baseVersion + int64(gid)
			err := sm.WritedownSingleWithVersion(ctx, sharedKey, &user, version, 1*time.Hour)
			if err != nil {
				t.Logf("goroutine %d version write conflict (expected): %v", gid, err)
			}
		}(g)
	}

	wg.Wait()
}

// =============================================================================
// RT-104: PatternOpsRace — ScanKeys + InvalidateCacheByPattern 并发
// 检测: SCAN 游标安全，删除操作与扫描的交互
// 运行: go test -race -run=TestRace_PatternOpsRace -count=5 ./tests/race_perf/
// =============================================================================

func TestRace_PatternOpsRace(t *testing.T) {
	flushRedis(t)
	sm := newRaceSM()
	ctx := context.Background()

	// 预填充大量缓存数据
	for i := 0; i < 500; i++ {
		user := raceTestUser{
			ID:    uint(i + 1),
			Name:  fmt.Sprintf("pattern_user_%d", i),
			Age:   25,
		}
		key := fmt.Sprintf("rt104:user:%d", i+1)
		if err := sm.WritedownSingle(ctx, key, &user, &service.WritedownSingleOptions{
			Expiration: 1 * time.Hour,
			Overwrite:  true,
		}); err != nil {
			t.Fatalf("pre-fill failed: %v", err)
		}
	}

	pattern := "rt104:user:*"
	var wg sync.WaitGroup
	iterations := 20

	// Scanners: 持续扫描 key
	for s := 0; s < 5; s++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				keys, err := service.ScanKeys(ctx, service.GetRedis(), pattern, 50)
				if err != nil {
					t.Logf("scan error: %v", err)
					continue
				}
				_ = keys
			}
		}()
	}

	// Invalidators: 持续按模式删除
	for inv := 0; inv < 3; inv++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				_ = sm.InvalidateCacheByPattern(ctx, pattern)
				time.Sleep(5 * time.Millisecond)
			}
		}()
	}

	// Writers: 持续添加新 key
	for w := 0; w < 3; w++ {
		wg.Add(1)
		go func(wid int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				user := raceTestUser{
					ID:   uint(10000 + wid*iterations + i),
					Name: fmt.Sprintf("new_pattern_%d", wid*iterations+i),
				}
				key := fmt.Sprintf("rt104:user:%d", user.ID)
				_ = sm.WritedownSingle(ctx, key, &user, &service.WritedownSingleOptions{
					Expiration: 1 * time.Hour,
					Overwrite:  true,
				})
				time.Sleep(time.Millisecond)
			}
		}(w)
	}

	wg.Wait()
}

// =============================================================================
// RT-201: ConcurrentTranslate — GORM registry.translators map 并发读
// 检测: 无锁 map 的并发读安全性
// 运行: go test -race -run=TestRace_ConcurrentTranslate -count=5 ./tests/race_perf/
// =============================================================================

func TestRace_ConcurrentTranslate(t *testing.T) {
	registry := filter_translator.DefaultGormRegistry

	var wg sync.WaitGroup
	numGoroutines := 50
	operators := []string{"=", "!=", ">", ">=", "<", "<=", "like", "in", "between", "isnull", "isnotnull"}

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			op := operators[gid%len(operators)]
			param := filter_translator.FilterParam{
				Field:    "age",
				Operator: op,
				Value:    25,
			}

			switch op {
			case "in":
				param.Value = []interface{}{1, 2, 3}
			case "between":
				param.Value = []interface{}{10, 100}
			case "like":
				param.Value = "test_pattern"
			case "isnull", "isnotnull":
				param.Value = nil
			}

			_, err := registry.Translate(param)
			if err != nil {
				t.Logf("translate error for op %s: %v", op, err)
			}
		}(g)
	}

	wg.Wait()
}

// =============================================================================
// RT-202: ConcurrentTranslate_Redis — Redis registry.translators map 并发读
// 运行: go test -race -run=TestRace_ConcurrentTranslate_Redis -count=5 ./tests/race_perf/
// =============================================================================

func TestRace_ConcurrentTranslate_Redis(t *testing.T) {
	registry := filter_translator.DefaultRedisRegistry

	var wg sync.WaitGroup
	numGoroutines := 50
	operators := []string{"=", "!=", ">", ">=", "<", "<=", "like", "in", "between", "isnull", "isnotnull"}

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			op := operators[gid%len(operators)]
			param := filter_translator.FilterParam{
				Field:    "name",
				Operator: op,
				Value:    "test_value",
			}
			switch op {
			case "in":
				param.Value = []interface{}{"a", "b", "c"}
			case "between":
				param.Value = []interface{}{1.0, 100.0}
			case "isnull", "isnotnull":
				param.Value = nil
			}

			_, err := registry.Translate(param)
			if err != nil {
				t.Logf("redis translate error for op %s: %v", op, err)
			}
		}(g)
	}

	wg.Wait()
}

// =============================================================================
// RT-301: ConcurrentSetGet — 并发 Set/Get 基础 Redis 操作
// 检测: go-redis 客户端连接池并发安全性
// 运行: go test -race -run=TestRace_ConcurrentSetGet -count=5 ./tests/race_perf/
// =============================================================================

func TestRace_ConcurrentSetGet(t *testing.T) {
	flushRedis(t)
	ctx := context.Background()
	rdb := service.GetRedis()

	var wg sync.WaitGroup
	numGoroutines := 30
	iterations := 50

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				key := fmt.Sprintf("rt301:key:%d:%d", gid, i)
				value := fmt.Sprintf("value_%d_%d", gid, i)

				if err := rdb.Set(ctx, key, value, 1*time.Hour).Err(); err != nil {
					t.Logf("set error: %v", err)
				}

				got, err := rdb.Get(ctx, key).Result()
				if err != nil {
					t.Logf("get error: %v", err)
				}
				_ = got

				_ = rdb.Del(ctx, key).Err()
			}
		}(g)
	}

	wg.Wait()
}
