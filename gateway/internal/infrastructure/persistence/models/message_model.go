package models

import (
	"time"

	"gorm.io/gorm"
)

// MessageModel 数据库消息模型
type MessageModel struct {
	ID             string `gorm:"primaryKey;size:64"`
	ConversationID string `gorm:"index;size:64;not null"`
	Content        string `gorm:"type:text;not null"`
	ContentType    string `gorm:"size:32;not null"`
	SenderID       string `gorm:"size:64;not null"`
	SenderName     string `gorm:"size:64"`
	SenderType     string `gorm:"size:32;not null"` // user, bot, system
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeletedAt      gorm.DeletedAt `gorm:"index"`
	Metadata       string         `gorm:"type:text"` // JSON encoded metadata
}

// TableName 指定表名
func (MessageModel) TableName() string {
	return "messages"
}
