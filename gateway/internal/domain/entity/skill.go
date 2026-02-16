package entity

import "time"

// Skill 技能实体
type Skill struct {
	id          string
	name        string
	description string
	enabled     bool
	config      map[string]interface{}
	createdAt   time.Time
}

// NewSkill 创建新技能
func NewSkill(id, name, description string) (*Skill, error) {
	if id == "" {
		return nil, ErrInvalidSkillID
	}
	if name == "" {
		return nil, ErrInvalidSkillName
	}

	return &Skill{
		id:          id,
		name:        name,
		description: description,
		enabled:     true,
		config:      make(map[string]interface{}),
		createdAt:   time.Now(),
	}, nil
}

// ID 返回技能ID
func (s *Skill) ID() string {
	return s.id
}

// Name 返回技能名称
func (s *Skill) Name() string {
	return s.name
}

// Description 返回技能描述
func (s *Skill) Description() string {
	return s.description
}

// IsEnabled 判断技能是否启用
func (s *Skill) IsEnabled() bool {
	return s.enabled
}

// Enable 启用技能
func (s *Skill) Enable() {
	s.enabled = true
}

// Disable 禁用技能
func (s *Skill) Disable() {
	s.enabled = false
}

// SetConfig 设置配置
func (s *Skill) SetConfig(key string, value interface{}) {
	s.config[key] = value
}

// GetConfig 获取配置
func (s *Skill) GetConfig(key string) (interface{}, bool) {
	val, ok := s.config[key]
	return val, ok
}
