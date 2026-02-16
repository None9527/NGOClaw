package persistence

import (
	"context"
	"sync"

	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/entity"
	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/repository"
	"github.com/ngoclaw/ngoclaw/gateway/pkg/errors"
)

// MemoryMessageRepository 内存实现的消息仓储（用于开发/测试）
type MemoryMessageRepository struct {
	mu       sync.RWMutex
	messages map[string]*entity.Message
	// 会话ID到消息ID列表的映射
	convMessages map[string][]string
}

// NewMemoryMessageRepository 创建内存消息仓储
func NewMemoryMessageRepository() repository.MessageRepository {
	return &MemoryMessageRepository{
		messages:     make(map[string]*entity.Message),
		convMessages: make(map[string][]string),
	}
}

// Save 保存消息
func (r *MemoryMessageRepository) Save(ctx context.Context, message *entity.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.messages[message.ID()] = message

	// 维护会话消息索引
	convID := message.ConversationID()
	if _, ok := r.convMessages[convID]; !ok {
		r.convMessages[convID] = make([]string, 0)
	}
	r.convMessages[convID] = append(r.convMessages[convID], message.ID())

	return nil
}

// FindByID 根据ID查找消息
func (r *MemoryMessageRepository) FindByID(ctx context.Context, id string) (*entity.Message, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	message, ok := r.messages[id]
	if !ok {
		return nil, errors.NewNotFoundError("message not found")
	}
	return message, nil
}

// FindByConversationID 根据会话ID查找消息列表
func (r *MemoryMessageRepository) FindByConversationID(ctx context.Context, conversationID string, limit, offset int) ([]*entity.Message, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	messageIDs, ok := r.convMessages[conversationID]
	if !ok {
		return []*entity.Message{}, nil
	}

	// 应用分页
	total := len(messageIDs)
	if offset >= total {
		return []*entity.Message{}, nil
	}

	end := offset + limit
	if end > total {
		end = total
	}

	messages := make([]*entity.Message, 0, end-offset)
	for i := offset; i < end; i++ {
		if msg, ok := r.messages[messageIDs[i]]; ok {
			messages = append(messages, msg)
		}
	}

	return messages, nil
}

// Delete 删除消息
func (r *MemoryMessageRepository) Delete(ctx context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	message, ok := r.messages[id]
	if !ok {
		return errors.NewNotFoundError("message not found")
	}

	// 从会话消息索引中移除
	convID := message.ConversationID()
	if messageIDs, ok := r.convMessages[convID]; ok {
		newIDs := make([]string, 0)
		for _, msgID := range messageIDs {
			if msgID != id {
				newIDs = append(newIDs, msgID)
			}
		}
		r.convMessages[convID] = newIDs
	}

	delete(r.messages, id)
	return nil
}

// Count 统计会话中的消息数量
func (r *MemoryMessageRepository) Count(ctx context.Context, conversationID string) (int64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	messageIDs, ok := r.convMessages[conversationID]
	if !ok {
		return 0, nil
	}
	return int64(len(messageIDs)), nil
}
