package repository

import (
	"context"

	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/entity"
)

// MessageRepository 消息仓储接口
type MessageRepository interface {
	// Save 保存消息
	Save(ctx context.Context, message *entity.Message) error

	// FindByID 根据ID查找消息
	FindByID(ctx context.Context, id string) (*entity.Message, error)

	// FindByConversationID 根据会话ID查找消息列表
	FindByConversationID(ctx context.Context, conversationID string, limit, offset int) ([]*entity.Message, error)

	// Delete 删除消息
	Delete(ctx context.Context, id string) error

	// Count 统计会话中的消息数量
	Count(ctx context.Context, conversationID string) (int64, error)
}
