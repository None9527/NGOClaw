package service

import (
	"context"

	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/entity"
)

// MessageRouter 消息路由领域服务
// 负责将消息路由到合适的代理
type MessageRouter interface {
	// Route 路由消息到合适的代理
	Route(ctx context.Context, message *entity.Message) (*entity.Agent, error)
}

// DefaultMessageRouter 默认消息路由实现
type DefaultMessageRouter struct {
	agentSelector AgentSelector
}

// AgentSelector 代理选择器接口
type AgentSelector interface {
	// Select 选择处理消息的代理
	Select(ctx context.Context, message *entity.Message) (*entity.Agent, error)
}

// NewDefaultMessageRouter 创建默认消息路由器
func NewDefaultMessageRouter(selector AgentSelector) *DefaultMessageRouter {
	return &DefaultMessageRouter{
		agentSelector: selector,
	}
}

// Route 实现消息路由逻辑
func (r *DefaultMessageRouter) Route(ctx context.Context, message *entity.Message) (*entity.Agent, error) {
	// 使用代理选择器选择合适的代理
	return r.agentSelector.Select(ctx, message)
}
