// Package integration holds integration tests that verify the full stack
// (miniredis + service layer + EnsureTimeout) wired together.
//
// Run with: go test -v ./tests/integration/
package integration

import (
	"context"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/Super-Gagaga/abstract-manager/service"

	"github.com/alicebob/miniredis/v2"
)

// =============================================================================
// TestMain — 初始化 miniredis + service.InitRedis()
// =============================================================================

var testMiniRedis *miniredis.Miniredis

func TestMain(m *testing.M) {
	mr, err := miniredis.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "miniredis.Run failed: %v\n", err)
		os.Exit(1)
	}
	testMiniRedis = mr

	host, port, err := net.SplitHostPort(mr.Addr())
	if err != nil {
		fmt.Fprintf(os.Stderr, "SplitHostPort failed: %v\n", err)
		os.Exit(1)
	}

	os.Setenv("REDIS_HOST", host)
	os.Setenv("REDIS_PORT", port)
	os.Setenv("REDIS_PASSWORD", "")

	rm, err := service.InitRedis()
	if err != nil {
		fmt.Fprintf(os.Stderr, "InitRedis failed: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()

	rm.Close()
	mr.Close()
	os.Exit(code)
}

// =============================================================================
// Helpers
// =============================================================================

type integTestUser struct {
	ID    uint   `json:"id" gorm:"primaryKey"`
	Name  string `json:"name"`
	Age   int    `json:"age"`
	Email string `json:"email"`
}

func newIntegSM() *service.ServiceManager[integTestUser] {
	return service.NewServiceManager(integTestUser{})
}

func flushRedis(t testing.TB) {
	t.Helper()
	testMiniRedis.FlushAll()
}

// =============================================================================
// IT-001: WritedownSingle + LookupSingle round-trip (Background ctx)
// EnsureTimeout wraps context.Background() with default Redis timeout.
// =============================================================================

func TestIntegration_WritedownAndLookup_BackgroundContext(t *testing.T) {
	flushRedis(t)
	sm := newIntegSM()
	ctx := context.Background() // no deadline → EnsureTimeout adds 10s Redis timeout

	key := "it001:user:1"
	user := integTestUser{ID: 1, Name: "alice", Age: 30, Email: "alice@test.com"}

	// Write
	err := sm.WritedownSingle(ctx, key, &user, &service.WritedownSingleOptions{
		Expiration: 1 * time.Hour,
		Overwrite:  true,
	})
	if err != nil {
		t.Fatalf("WritedownSingle failed: %v", err)
	}

	// Read back
	result, err := sm.LookupSingle(ctx, key, nil)
	if err != nil {
		t.Fatalf("LookupSingle failed: %v", err)
	}
	if result.ID != 1 || result.Name != "alice" {
		t.Errorf("unexpected result: %+v", result)
	}
}

// =============================================================================
// IT-002: WritedownSingle with explicit timeout
// =============================================================================

func TestIntegration_WritedownSingle_ExplicitTimeout(t *testing.T) {
	flushRedis(t)
	sm := newIntegSM()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	key := "it002:user:1"
	user := integTestUser{ID: 2, Name: "bob", Age: 25, Email: "bob@test.com"}

	// EnsureTimeout should keep the 5s timeout, not override with default 10s
	err := sm.WritedownSingle(ctx, key, &user, &service.WritedownSingleOptions{
		Expiration: 1 * time.Hour,
		Overwrite:  true,
	})
	if err != nil {
		t.Fatalf("WritedownSingle with explicit timeout failed: %v", err)
	}

	// Verify it was written
	result, err := sm.LookupSingle(context.Background(), key, nil)
	if err != nil {
		t.Fatalf("LookupSingle failed: %v", err)
	}
	if result.Name != "bob" {
		t.Errorf("unexpected result: %+v", result)
	}
}

// =============================================================================
// IT-003: WritedownSingle with already-expired context
// EnsureTimeout preserves the existing deadline → should fail fast.
// =============================================================================

func TestIntegration_WritedownSingle_ExpiredContext(t *testing.T) {
	flushRedis(t)
	sm := newIntegSM()

	// Create an already-expired context
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-1*time.Second))
	defer cancel()

	key := "it003:user:1"
	user := integTestUser{ID: 3, Name: "charlie", Age: 40, Email: "charlie@test.com"}

	err := sm.WritedownSingle(ctx, key, &user, &service.WritedownSingleOptions{
		Expiration: 1 * time.Hour,
		Overwrite:  true,
	})
	if err == nil {
		t.Error("expected error from expired context, got nil")
	} else {
		t.Logf("correctly got error: %v", err)
	}
}

// =============================================================================
// IT-004: LookupSingle with expired context
// =============================================================================

func TestIntegration_LookupSingle_ExpiredContext(t *testing.T) {
	flushRedis(t)
	sm := newIntegSM()

	// First write with a valid context
	key := "it004:user:1"
	user := integTestUser{ID: 4, Name: "diana", Age: 28, Email: "diana@test.com"}
	err := sm.WritedownSingle(context.Background(), key, &user, &service.WritedownSingleOptions{
		Expiration: 1 * time.Hour,
		Overwrite:  true,
	})
	if err != nil {
		t.Fatalf("setup write failed: %v", err)
	}

	// Now lookup with an expired context
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-1*time.Second))
	defer cancel()

	_, err = sm.LookupSingle(ctx, key, nil)
	if err == nil {
		t.Error("expected error from expired context, got nil")
	} else {
		t.Logf("correctly got error: %v", err)
	}
}

// =============================================================================
// IT-005: InvalidateCache round-trip
// =============================================================================

func TestIntegration_InvalidateCache(t *testing.T) {
	flushRedis(t)
	sm := newIntegSM()
	ctx := context.Background()

	key := "it005:user:1"
	user := integTestUser{ID: 5, Name: "eve", Age: 35, Email: "eve@test.com"}

	// Write
	err := sm.WritedownSingle(ctx, key, &user, &service.WritedownSingleOptions{
		Expiration: 1 * time.Hour,
		Overwrite:  true,
	})
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// Invalidate
	if err := sm.InvalidateCache(ctx, key); err != nil {
		t.Fatalf("InvalidateCache failed: %v", err)
	}

	// Verify miss
	rdb := service.GetRedis()
	exists, err := rdb.Exists(ctx, key).Result()
	if err != nil {
		t.Fatalf("Exists check failed: %v", err)
	}
	if exists != 0 {
		t.Errorf("expected key to be deleted, but it still exists")
	}
}

// =============================================================================
// IT-006: Cache write with NX (only set if not exists)
// =============================================================================

func TestIntegration_WritedownSingle_NX(t *testing.T) {
	flushRedis(t)
	sm := newIntegSM()
	ctx := context.Background()

	key := "it006:user:1"
	user1 := integTestUser{ID: 6, Name: "frank", Age: 22, Email: "frank@test.com"}
	user2 := integTestUser{ID: 6, Name: "frank_v2", Age: 23, Email: "frank2@test.com"}

	// First write succeeds (NX on empty key)
	err := sm.WritedownSingle(ctx, key, &user1, &service.WritedownSingleOptions{
		Expiration: 1 * time.Hour,
		NX:         true,
	})
	if err != nil {
		t.Fatalf("first NX write failed: %v", err)
	}

	// Second write with NX should be silently ignored (key already exists)
	err = sm.WritedownSingle(ctx, key, &user2, &service.WritedownSingleOptions{
		Expiration: 1 * time.Hour,
		NX:         true,
	})
	if err != nil {
		t.Fatalf("second NX write should not error: %v", err)
	}

	// Read back — should still be user1
	result, err := sm.LookupSingle(ctx, key, nil)
	if err != nil {
		t.Fatalf("lookup failed: %v", err)
	}
	if result.Name != "frank" {
		t.Errorf("NX should have kept original value, got: %s", result.Name)
	}
}

// =============================================================================
// IT-007: WritedownSingleWithVersion — optimistic locking
// =============================================================================

func TestIntegration_WritedownSingleWithVersion(t *testing.T) {
	flushRedis(t)
	sm := newIntegSM()
	ctx := context.Background()

	key := "it007:versioned:1"
	user := integTestUser{ID: 7, Name: "grace", Age: 27, Email: "grace@test.com"}

	// Initial write
	if err := sm.WritedownSingle(ctx, key, &user, &service.WritedownSingleOptions{
		Expiration: 1 * time.Hour,
		Overwrite:  true,
	}); err != nil {
		t.Fatalf("initial write failed: %v", err)
	}

	// Write with version 1 (should succeed)
	err := sm.WritedownSingleWithVersion(ctx, key, &user, 1, 1*time.Hour)
	if err != nil {
		t.Fatalf("version 1 write failed: %v", err)
	}

	// Write with version 0 (should fail — version outdated)
	err = sm.WritedownSingleWithVersion(ctx, key, &user, 0, 1*time.Hour)
	if err == nil {
		t.Error("expected version conflict error, got nil")
	} else {
		t.Logf("correctly got version conflict: %v", err)
	}
}

// =============================================================================
// IT-008: ExistsInCache + ExtendCacheTTL
// =============================================================================

func TestIntegration_ExistsInCache_And_ExtendTTL(t *testing.T) {
	flushRedis(t)
	sm := newIntegSM()
	ctx := context.Background()

	key := "it008:user:1"
	user := integTestUser{ID: 8, Name: "hank", Age: 33, Email: "hank@test.com"}

	// Should not exist yet
	exists, err := sm.ExistsInCache(ctx, key)
	if err != nil {
		t.Fatalf("ExistsInCache failed: %v", err)
	}
	if exists {
		t.Error("key should not exist before write")
	}

	// Write
	if err := sm.WritedownSingle(ctx, key, &user, &service.WritedownSingleOptions{
		Expiration: 1 * time.Hour,
		Overwrite:  true,
	}); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// Should exist now
	exists, err = sm.ExistsInCache(ctx, key)
	if err != nil {
		t.Fatalf("ExistsInCache failed: %v", err)
	}
	if !exists {
		t.Error("key should exist after write")
	}

	// Extend TTL
	if err := sm.ExtendCacheTTL(ctx, key, 2*time.Hour); err != nil {
		t.Fatalf("ExtendCacheTTL failed: %v", err)
	}
}

// =============================================================================
// IT-009: Multiple keys — batch LookupQuery
// =============================================================================

func TestIntegration_LookupQuery_Batch(t *testing.T) {
	flushRedis(t)
	sm := newIntegSM()
	ctx := context.Background()

	keys := make([]string, 5)
	for i := 0; i < 5; i++ {
		keys[i] = fmt.Sprintf("it009:user:%d", i+1)
		user := integTestUser{
			ID:    uint(i + 1),
			Name:  fmt.Sprintf("batch_user_%d", i),
			Age:   20 + i,
			Email: fmt.Sprintf("b%d@test.com", i),
		}
		if err := sm.WritedownSingle(ctx, keys[i], &user, &service.WritedownSingleOptions{
			Expiration: 1 * time.Hour,
			Overwrite:  true,
		}); err != nil {
			t.Fatalf("write key %d failed: %v", i, err)
		}
	}

	// Batch lookup
	result, err := sm.LookupQuery(ctx, keys, &service.LookupQueryOptions{FallbackToDB: false})
	if err != nil {
		t.Fatalf("LookupQuery failed: %v", err)
	}
	if len(result) != 5 {
		t.Errorf("expected 5 results, got %d", len(result))
	}
}

// =============================================================================
// IT-010: InvalidateCacheByPattern
// =============================================================================

func TestIntegration_InvalidateCacheByPattern(t *testing.T) {
	flushRedis(t)
	sm := newIntegSM()
	ctx := context.Background()

	// Write several keys under same pattern
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("it010:user:%d", i+1)
		user := integTestUser{
			ID:    uint(i + 1),
			Name:  fmt.Sprintf("pattern_user_%d", i),
			Age:   25,
			Email: fmt.Sprintf("p%d@test.com", i),
		}
		if err := sm.WritedownSingle(ctx, key, &user, &service.WritedownSingleOptions{
			Expiration: 1 * time.Hour,
			Overwrite:  true,
		}); err != nil {
			t.Fatalf("write key %d failed: %v", i, err)
		}
	}

	// Invalidate by pattern
	if err := sm.InvalidateCacheByPattern(ctx, "it010:user:*"); err != nil {
		t.Fatalf("InvalidateCacheByPattern failed: %v", err)
	}

	// Verify all keys are gone
	rdb := service.GetRedis()
	keys, err := service.ScanKeys(ctx, rdb, "it010:user:*", 100)
	if err != nil {
		t.Fatalf("ScanKeys failed: %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("expected 0 keys after pattern invalidation, got %d: %v", len(keys), keys)
	}
}
