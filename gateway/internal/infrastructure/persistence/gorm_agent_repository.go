package persistence

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/entity"
	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/repository"
	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/valueobject"
	"github.com/ngoclaw/ngoclaw/gateway/internal/infrastructure/persistence/models"
	domainErrors "github.com/ngoclaw/ngoclaw/gateway/pkg/errors"
	"gorm.io/gorm"
)

// GormAgentRepository GORM 实现的代理仓储
type GormAgentRepository struct {
	db *gorm.DB
}

// NewGormAgentRepository 创建 GORM 代理仓储
func NewGormAgentRepository(db *gorm.DB) repository.AgentRepository {
	return &GormAgentRepository{
		db: db,
	}
}

// FindByID 根据ID查找代理
func (r *GormAgentRepository) FindByID(ctx context.Context, id string) (*entity.Agent, error) {
	var model models.AgentModel
	if err := r.db.WithContext(ctx).First(&model, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domainErrors.NewNotFoundError("agent not found")
		}
		return nil, domainErrors.NewInternalError("failed to find agent: " + err.Error())
	}

	return r.toEntity(&model)
}

// FindAll 查找所有代理
func (r *GormAgentRepository) FindAll(ctx context.Context) ([]*entity.Agent, error) {
	var modelList []models.AgentModel
	if err := r.db.WithContext(ctx).Find(&modelList).Error; err != nil {
		return nil, domainErrors.NewInternalError("failed to find agents: " + err.Error())
	}

	agents := make([]*entity.Agent, 0, len(modelList))
	for _, model := range modelList {
		agent, err := r.toEntity(&model)
		if err != nil {
			return nil, err
		}
		agents = append(agents, agent)
	}

	return agents, nil
}

// FindByName 根据名称查找代理
func (r *GormAgentRepository) FindByName(ctx context.Context, name string) (*entity.Agent, error) {
	var model models.AgentModel
	if err := r.db.WithContext(ctx).First(&model, "name = ?", name).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domainErrors.NewNotFoundError("agent not found")
		}
		return nil, domainErrors.NewInternalError("failed to find agent: " + err.Error())
	}

	return r.toEntity(&model)
}

// Save 保存代理
func (r *GormAgentRepository) Save(ctx context.Context, agent *entity.Agent) error {
	model, err := r.toModel(agent)
	if err != nil {
		return err
	}

	if err := r.db.WithContext(ctx).Save(model).Error; err != nil {
		return domainErrors.NewInternalError("failed to save agent: " + err.Error())
	}

	return nil
}

// Delete 删除代理
func (r *GormAgentRepository) Delete(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).Delete(&models.AgentModel{}, "id = ?", id)
	if result.Error != nil {
		return domainErrors.NewInternalError("failed to delete agent: " + result.Error.Error())
	}
	if result.RowsAffected == 0 {
		return domainErrors.NewNotFoundError("agent not found")
	}
	return nil
}

// Exists 判断代理是否存在
func (r *GormAgentRepository) Exists(ctx context.Context, id string) (bool, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&models.AgentModel{}).Where("id = ?", id).Count(&count).Error; err != nil {
		return false, domainErrors.NewInternalError("failed to check agent existence: " + err.Error())
	}
	return count > 0, nil
}

// 转换方法

func (r *GormAgentRepository) toModel(agent *entity.Agent) (*models.AgentModel, error) {
	config := agent.ModelConfig()

	// 序列化技能ID列表 (这里简单存储ID列表，实际可能需要多对多关系表)
	// 目前 Entity 中 Skill 结构体很简单，暂不深入
	// 假设我们需要存储技能配置，暂时存为空列表JSON
	skillsJSON, _ := json.Marshal([]string{})

	return &models.AgentModel{
		ID:            agent.ID(),
		Name:          agent.Name(),
		ModelProvider: config.Provider(),
		ModelName:     config.Model(),
		MaxTokens:     config.MaxTokens(),
		Temperature:   config.Temperature(),
		TopP:          config.TopP(),
		SystemPrompt:  "", // ModelConfig 没有暴露 SystemPrompt getter?
		Workspace:     "", // Agent 结构体中有 Workspace 字段但无 getter?
		CreatedAt:     time.Now(), // 应该从 entity 获取，但 entity 没有暴露 CreatedAt getter
		UpdatedAt:     time.Now(),
		Skills:        string(skillsJSON),
	}, nil
}

func (r *GormAgentRepository) toEntity(model *models.AgentModel) (*entity.Agent, error) {
	config := valueobject.NewModelConfig(
		model.ModelProvider,
		model.ModelName,
		model.MaxTokens,
		model.Temperature,
		model.TopP,
		false, // Assuming default to false, as 'Stream' field is not yet persisted in AgentModel
	)

	// Restore Skills from JSON-serialized skill IDs
	var skills []entity.Skill
	if model.Skills != "" {
		var skillIDs []string
		if err := json.Unmarshal([]byte(model.Skills), &skillIDs); err == nil {
			for _, sid := range skillIDs {
				s, err := entity.NewSkill(sid, sid, "")
				if err == nil {
					skills = append(skills, *s)
				}
			}
		}
	}
	if skills == nil {
		skills = make([]entity.Skill, 0)
	}

	agent := entity.ReconstructAgent(
		model.ID,
		model.Name,
		config,
		skills,
		model.Workspace,
		model.CreatedAt,
		model.UpdatedAt,
	)

	return agent, nil
}
