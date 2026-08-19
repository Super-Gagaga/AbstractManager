package model

import "time"

type User struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Username  string    `gorm:"size:64;uniqueIndex" json:"username"`
	Email     string    `gorm:"size:128;uniqueIndex" json:"email"`
	Age       int       `json:"age"`
	Status    string    `gorm:"size:32;default:active" json:"status"` // active / inactive,自定义过滤器按它筛选
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
