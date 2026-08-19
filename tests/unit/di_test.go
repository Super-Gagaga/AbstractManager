package unit

import (
	"context"
	"testing"
	"time"

	"github.com/Super-Gagaga/abstract-manager/service"
	"github.com/Super-Gagaga/abstract-manager/tests/testutil"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// UT-DI-001: WithRedis 注入后,实例操作完全不经过全局单例。
// 本测试包没有调用 InitRedis(全局未初始化):
// 若注入未生效,sm.Redis() 会 panic("redis not initialized")。
func TestServiceManager_WithRedis_OverridesGlobal(t *testing.T) {
	client, mr := testutil.SetupMiniRedis(t)
	defer mr.Close()

	sm := service.NewServiceManager(testutil.TestUser{}).WithRedis(client)
	ctx := context.Background()
	user := testutil.TestUser{ID: 1, Username: "di"}

	require.NoError(t, sm.WritedownSingle(ctx, "di:user:1", &user,
		&service.WritedownSingleOptions{Expiration: time.Minute}))

	require.NoError(t, client.Get(ctx, "di:user:1").Err(), "data should land in the injected client")

	got, err := sm.LookupSingle(ctx, "di:user:1", nil)
	require.NoError(t, err)
	assert.Equal(t, "di", got.Username)

	// GetRedisManager 同样感知实例注入
	assert.Same(t, client, sm.GetRedisManager().Client)
}

// UT-DI-002: WithDB 注入后,查询走注入的 sqlmock 替身而非全局 DB。
// 本测试包没有调用 InitDB(全局未初始化),注入失败会 panic。
func TestServiceManager_WithDB_OverridesGlobal(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer sqlDB.Close()

	gormDB, err := gorm.Open(mysql.New(mysql.Config{
		Conn:                      sqlDB,
		SkipInitializeWithVersion: true,
	}), &gorm.Config{})
	require.NoError(t, err)

	sm := service.NewServiceManager(testutil.TestUser{}).WithDB(gormDB)

	mock.ExpectQuery("SELECT").WillReturnRows(userRowsDI(testutil.TestUser{ID: 7, Username: "mocked"}))

	got, err := sm.GetSingleByID(context.Background(), 7, nil)
	require.NoError(t, err)
	assert.Equal(t, "mocked", got.Username)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// UT-DI-003: 未注入时回退全局单例(通过 SetGlobalDB 设置后应能取到)
func TestServiceManager_FallbackToGlobal(t *testing.T) {
	sqlDB, _, err := sqlmock.New()
	require.NoError(t, err)
	defer sqlDB.Close()

	gormDB, err := gorm.Open(mysql.New(mysql.Config{
		Conn:                      sqlDB,
		SkipInitializeWithVersion: true,
	}), &gorm.Config{})
	require.NoError(t, err)

	service.SetGlobalDB(gormDB)
	t.Cleanup(func() { service.SetGlobalDB(nil) })

	sm := service.NewServiceManager(testutil.TestUser{})
	assert.Same(t, gormDB, sm.DB(), "without WithDB, DB() should return the global")
}

func userRowsDI(users ...testutil.TestUser) *sqlmock.Rows {
	rows := sqlmock.NewRows([]string{"id", "username", "email", "age", "status"})
	for _, u := range users {
		rows.AddRow(u.ID, u.Username, u.Email, u.Age, u.Status)
	}
	return rows
}
