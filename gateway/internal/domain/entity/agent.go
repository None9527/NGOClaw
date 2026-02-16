package entity

import (
	"time"

	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/valueobject"
)

// Agent 代理聚合根
// 代理是一个可以处理消息并生成响应的智能实体
type Agent struct {
	id          string
	name        string
	modelConfig valueobject.ModelConfig
	skills      []Skill
	workspace   string
	createdAt   time.Time
	updatedAt   time.Time
}

// NewAgent 创建新的代理（工厂方法）
func NewAgent(id, name string, modelConfig valueobject.ModelConfig) (*Agent, error) {
	// 验证
	if id == "" {
		return nil, ErrInvalidAgentID
	}
	if name == "" {
		return nil, ErrInvalidAgentName
	}

	now := time.Now()
	return &Agent{
		id:          id,
		name:        name,
		modelConfig: modelConfig,
		skills:      make([]Skill, 0),
		createdAt:   now,
		updatedAt:   now,
	}, nil
}

// ReconstructAgent 重建代理（用于从持久化层恢复）
func ReconstructAgent(
	id, name string,
	modelConfig valueobject.ModelConfig,
	skills []Skill,
	workspace string,
	createdAt, updatedAt time.Time,
) *Agent {
	return &Agent{
		id:          id,
		name:        name,
		modelConfig: modelConfig,
		skills:      skills,
		workspace:   workspace,
		createdAt:   createdAt,
		updatedAt:   updatedAt,
	}
}

// ID 返回代理ID（聚合根标识）
func (a *Agent) ID() string {
	return a.id
}

// Name 返回代理名称
func (a *Agent) Name() string {
	return a.name
}

// ModelConfig 返回模型配置
func (a *Agent) ModelConfig() valueobject.ModelConfig {
	return a.modelConfig
}

// Skills 返回技能列表
func (a *Agent) Skills() []Skill {
	// 返回副本以保护不变性
	skills := make([]Skill, len(a.skills))
	copy(skills, a.skills)
	return skills
}

// AddSkill 添加技能（领域行为）
func (a *Agent) AddSkill(skill Skill) error {
	// 检查技能是否已存在
	for _, s := range a.skills {
		if s.ID() == skill.ID() {
			return ErrSkillAlreadyExists
		}
	}

	a.skills = append(a.skills, skill)
	a.updatedAt = time.Now()
	return nil
}

// RemoveSkill 移除技能（领域行为）
func (a *Agent) RemoveSkill(skillID string) error {
	for i, skill := range a.skills {
		if skill.ID() == skillID {
			a.skills = append(a.skills[:i], a.skills[i+1:]...)
			a.updatedAt = time.Now()
			return nil
		}
	}
	return ErrSkillNotFound
}

// UpdateModelConfig 更新模型配置（领域行为）
func (a *Agent) UpdateModelConfig(config valueobject.ModelConfig) {
	a.modelConfig = config
	a.updatedAt = time.Now()
}

// CanProcessMessage 判断代理是否可以处理消息（领域规则）
func (a *Agent) CanProcessMessage(msg *Message) bool {
	// 代理需要有效的模型配置才能处理消息
	if a.modelConfig.Model() == "" {
		return false
	}
	return true
}
