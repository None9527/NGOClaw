package service

import (
	"context"

	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/entity"
	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/repository"
	"github.com/ngoclaw/ngoclaw/gateway/pkg/errors"
)

// DefaultAgentSelector 默认代理选择器实现
type DefaultAgentSelector struct {
	agentRepo repository.AgentRepository
}

// NewDefaultAgentSelector 创建默认代理选择器
func NewDefaultAgentSelector(agentRepo repository.AgentRepository) AgentSelector {
	return &DefaultAgentSelector{
		agentRepo: agentRepo,
	}
}

// Select 选择处理消息的代理
func (s *DefaultAgentSelector) Select(ctx context.Context, message *entity.Message) (*entity.Agent, error) {
	// Simple first-match strategy: selects the first agent that can process the message.
	// Future: load-balancing, skill-based routing, priority queues.

	agents, err := s.agentRepo.FindAll(ctx)
	if err != nil {
		return nil, errors.NewInternalError("failed to find agents: " + err.Error())
	}

	if len(agents) == 0 {
		return nil, errors.NewNotFoundError("no agents available")
	}

	// 选择第一个可以处理该消息的代理
	for _, agent := range agents {
		if agent.CanProcessMessage(message) {
			return agent, nil
		}
	}

	return nil, errors.NewNotFoundError("no suitable agent found")
}
