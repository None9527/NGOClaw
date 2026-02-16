package repository

import (
	"context"

	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/entity"
)

// AgentRepository 代理仓储接口（遵循依赖倒置原则）
// 定义在领域层，实现在基础设施层
type AgentRepository interface {
	// FindByID 根据ID查找代理
	FindByID(ctx context.Context, id string) (*entity.Agent, error)

	// FindAll 查找所有代理
	FindAll(ctx context.Context) ([]*entity.Agent, error)

	// FindByName 根据名称查找代理
	FindByName(ctx context.Context, name string) (*entity.Agent, error)

	// Save 保存代理（创建或更新）
	Save(ctx context.Context, agent *entity.Agent) error

	// Delete 删除代理
	Delete(ctx context.Context, id string) error

	// Exists 判断代理是否存在
	Exists(ctx context.Context, id string) (bool, error)
}
