package persistence

import (
	"context"
	"sync"

	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/entity"
	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/repository"
	"github.com/ngoclaw/ngoclaw/gateway/pkg/errors"
)

// MemoryAgentRepository 内存实现的代理仓储（用于开发/测试）
type MemoryAgentRepository struct {
	mu     sync.RWMutex
	agents map[string]*entity.Agent
}

// NewMemoryAgentRepository 创建内存代理仓储
func NewMemoryAgentRepository() repository.AgentRepository {
	return &MemoryAgentRepository{
		agents: make(map[string]*entity.Agent),
	}
}

// FindByID 根据ID查找代理
func (r *MemoryAgentRepository) FindByID(ctx context.Context, id string) (*entity.Agent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	agent, ok := r.agents[id]
	if !ok {
		return nil, errors.NewNotFoundError("agent not found")
	}
	return agent, nil
}

// FindAll 查找所有代理
func (r *MemoryAgentRepository) FindAll(ctx context.Context) ([]*entity.Agent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	agents := make([]*entity.Agent, 0, len(r.agents))
	for _, agent := range r.agents {
		agents = append(agents, agent)
	}
	return agents, nil
}

// FindByName 根据名称查找代理
func (r *MemoryAgentRepository) FindByName(ctx context.Context, name string) (*entity.Agent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, agent := range r.agents {
		if agent.Name() == name {
			return agent, nil
		}
	}
	return nil, errors.NewNotFoundError("agent not found")
}

// Save 保存代理（创建或更新）
func (r *MemoryAgentRepository) Save(ctx context.Context, agent *entity.Agent) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.agents[agent.ID()] = agent
	return nil
}

// Delete 删除代理
func (r *MemoryAgentRepository) Delete(ctx context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.agents[id]; !ok {
		return errors.NewNotFoundError("agent not found")
	}
	delete(r.agents, id)
	return nil
}

// Exists 判断代理是否存在
func (r *MemoryAgentRepository) Exists(ctx context.Context, id string) (bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, ok := r.agents[id]
	return ok, nil
}
