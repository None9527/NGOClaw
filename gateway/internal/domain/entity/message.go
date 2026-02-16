package entity

import (
	"time"

	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/valueobject"
)

// Message 消息实体
type Message struct {
	id           string
	conversationID string
	content      valueobject.MessageContent
	sender       valueobject.User
	timestamp    time.Time
	metadata     map[string]interface{}
}

// NewMessage 创建新消息（工厂方法）
func NewMessage(
	id string,
	conversationID string,
	content valueobject.MessageContent,
	sender valueobject.User,
) (*Message, error) {
	if id == "" {
		return nil, ErrInvalidMessageID
	}
	if conversationID == "" {
		return nil, ErrInvalidConversationID
	}

	return &Message{
		id:             id,
		conversationID: conversationID,
		content:        content,
		sender:         sender,
		timestamp:      time.Now(),
		metadata:       make(map[string]interface{}),
	}, nil
}

// ReconstructMessage 重建消息（用于从持久化层恢复）
func ReconstructMessage(
	id string,
	conversationID string,
	content valueobject.MessageContent,
	sender valueobject.User,
	timestamp time.Time,
	metadata map[string]interface{},
) *Message {
	return &Message{
		id:             id,
		conversationID: conversationID,
		content:        content,
		sender:         sender,
		timestamp:      timestamp,
		metadata:       metadata,
	}
}

// ID 返回消息ID
func (m *Message) ID() string {
	return m.id
}

// ConversationID 返回会话ID
func (m *Message) ConversationID() string {
	return m.conversationID
}

// Content 返回消息内容
func (m *Message) Content() valueobject.MessageContent {
	return m.content
}

// Sender 返回发送者
func (m *Message) Sender() valueobject.User {
	return m.sender
}

// Timestamp 返回时间戳
func (m *Message) Timestamp() time.Time {
	return m.timestamp
}

// SetMetadata 设置元数据
func (m *Message) SetMetadata(key string, value interface{}) {
	m.metadata[key] = value
}

// GetMetadata 获取元数据
func (m *Message) GetMetadata(key string) (interface{}, bool) {
	val, ok := m.metadata[key]
	return val, ok
}

// GetAllMetadata 获取所有元数据
func (m *Message) GetAllMetadata() map[string]interface{} {
	// 返回副本
	result := make(map[string]interface{}, len(m.metadata))
	for k, v := range m.metadata {
		result[k] = v
	}
	return result
}

// Metadata 获取所有元数据（别名）
func (m *Message) Metadata() map[string]interface{} {
	return m.GetAllMetadata()
}

// IsFromUser 判断是否来自用户（业务规则）
func (m *Message) IsFromUser() bool {
	return m.sender.Type() == "user"
}

// IsFromBot 判断是否来自机器人（业务规则）
func (m *Message) IsFromBot() bool {
	return m.sender.Type() == "bot"
}
