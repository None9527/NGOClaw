package models

import (
	"time"

	"gorm.io/gorm"
)

// AgentModel 数据库代理模型
type AgentModel struct {
	ID             string `gorm:"primaryKey;size:64"`
	Name           string `gorm:"uniqueIndex;size:64;not null"`
	ModelProvider  string `gorm:"size:32"`
	ModelName      string `gorm:"size:128"`
	MaxTokens      int
	Temperature    float64
	TopP           float64
	SystemPrompt   string         `gorm:"type:text"`
	Workspace      string         `gorm:"size:255"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeletedAt      gorm.DeletedAt `gorm:"index"`
	Skills         string         `gorm:"type:text"` // JSON encoded list of skill IDs
}

// TableName 指定表名
func (AgentModel) TableName() string {
	return "agents"
}
