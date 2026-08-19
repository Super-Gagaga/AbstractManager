package unit

import (
	"context"
	"testing"

	"github.com/Super-Gagaga/abstract-manager/service"
	"github.com/Super-Gagaga/abstract-manager/tests/testutil"

	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

// UT-SEC-001: 单条 Increment / Decrement 列名校验(与批量版一致)。
// 校验发生在任何 DB 访问之前,因此无需初始化全局 DB;
// 恶意列名若漏过校验,会因 sm.DB() panic 而使测试失败。
func TestIncrementDecrement_ColumnValidation(t *testing.T) {
	sm := service.NewServiceManager(testutil.TestUser{})
	ctx := context.Background()
	var queryFunc func(*gorm.DB) *gorm.DB // 校验先于 queryFunc 执行,不会触达

	malicious := []string{
		"age; DROP TABLE users;--",
		"age + 1",
		"1;DELETE FROM users",
		"age--",
		"age ",
	}

	for _, col := range malicious {
		err := sm.Increment(ctx, col, 1, queryFunc)
		assert.Error(t, err, "Increment 应拒绝非法列名 %q", col)
		assert.Contains(t, err.Error(), "invalid column name")

		err = sm.Decrement(ctx, col, 1, queryFunc)
		assert.Error(t, err, "Decrement 应拒绝非法列名 %q", col)
		assert.Contains(t, err.Error(), "invalid column name")
	}
}
