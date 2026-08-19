package model

import (
	"time"

	"gorm.io/gorm"
)

// Product 数据库读写示例使用的商品模型。
//
// 相比 cache_example 的 User,这里额外演示两个字段:
//   - Status:配合 QueryRouterGroup 内置的 active_list 查询方法(status = 'active')
//   - DeletedAt:gorm.DeletedAt 软删除,DELETE 接口带 "soft": true 时写入,
//     之后所有查询自动过滤已软删的行
type Product struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	Name      string         `gorm:"size:128;uniqueIndex" json:"name"`
	Category  string         `gorm:"size:64;index" json:"category"`
	Price     float64        `json:"price"`
	Stock     int            `json:"stock"`
	Status    string         `gorm:"size:32;default:active" json:"status"` // active / inactive
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}
