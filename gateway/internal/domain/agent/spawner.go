package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// SpawnConfig Sub-Agent 派生配置
type SpawnConfig struct {
	Name           string            // 子代理名称
	SystemPrompt   string            // 系统提示词
	AllowedTools   []string          // 允许的工具列表
	DeniedTools    []string          // 禁止的工具列表
	InheritContext bool              // 是否继承父代理上下文
	InheritTools   bool              // 是否继承父代理工具权限
	MaxDepth       int               // 最大嵌套深度 (防止无限递归)
	Timeout        time.Duration     // 子代理超时时间
	Metadata       map[string]string // 额外元数据
}

// DefaultSpawnConfig 返回默认派生配置
func DefaultSpawnConfig(name string) *SpawnConfig {
	return &SpawnConfig{
		Name:           name,
		AllowedTools:   []string{},
		DeniedTools:    []string{},
		InheritContext: true,
		InheritTools:   true,
		MaxDepth:       3,
		Timeout:        5 * time.Minute,
		Metadata:       make(map[string]string),
	}
}

// Permission 权限定义
type Permission struct {
	Tools       []string // 可用工具列表
	DeniedTools []string // 禁止工具列表
	CanSpawn    bool     // 是否可以派生子代理
	MaxSpawns   int      // 最大子代理数量
	MaxDepth    int      // 最大派生深度
}

// CanUseTool 检查是否可以使用指定工具
func (p *Permission) CanUseTool(toolName string) bool {
	// 检查禁止列表
	for _, denied := range p.DeniedTools {
		if denied == toolName {
			return false
		}
	}

	// 如果允许列表为空，默认允许所有未禁止的
	if len(p.Tools) == 0 {
		return true
	}

	// 检查允许列表
	for _, allowed := range p.Tools {
		if allowed == toolName {
			return true
		}
	}

	return false
}

// SpawnedAgent 派生的子代理
type SpawnedAgent struct {
	ID           string
	ParentID     string
	Name         string
	SystemPrompt string
	Permission   *Permission
	Depth        int
	CreatedAt    time.Time
	Status       AgentStatus
	mu           sync.RWMutex
}

// AgentStatus 代理状态
type AgentStatus int

const (
	AgentStatusIdle AgentStatus = iota
	AgentStatusRunning
	AgentStatusCompleted
	AgentStatusError
	AgentStatusTerminated
)

// String 返回状态字符串
func (s AgentStatus) String() string {
	switch s {
	case AgentStatusIdle:
		return "idle"
	case AgentStatusRunning:
		return "running"
	case AgentStatusCompleted:
		return "completed"
	case AgentStatusError:
		return "error"
	case AgentStatusTerminated:
		return "terminated"
	default:
		return "unknown"
	}
}

// Spawner Sub-Agent 派生器接口
type Spawner interface {
	// Spawn 创建子代理
	Spawn(ctx context.Context, parentID string, config *SpawnConfig) (*SpawnedAgent, error)
	// Get 获取代理
	Get(agentID string) (*SpawnedAgent, bool)
	// ListChildren 列出子代理
	ListChildren(parentID string) []*SpawnedAgent
	// Terminate 终止代理
	Terminate(agentID string) error
	// TerminateAll 终止所有子代理
	TerminateAll(parentID string) error
	// GetDepth 获取当前嵌套深度
	GetDepth(agentID string) int
}

// InMemorySpawner 内存实现的派生器
type InMemorySpawner struct {
	mu       sync.RWMutex
	agents   map[string]*SpawnedAgent
	children map[string][]string // parentID -> []childID
	logger   *zap.Logger
	maxDepth int
}

// NewInMemorySpawner 创建内存派生器
func NewInMemorySpawner(logger *zap.Logger, maxDepth int) *InMemorySpawner {
	if maxDepth <= 0 {
		maxDepth = 5
	}
	return &InMemorySpawner{
		agents:   make(map[string]*SpawnedAgent),
		children: make(map[string][]string),
		logger:   logger,
		maxDepth: maxDepth,
	}
}

// Spawn 创建子代理
func (s *InMemorySpawner) Spawn(ctx context.Context, parentID string, config *SpawnConfig) (*SpawnedAgent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 检查父代理是否存在（如果不是根代理）
	var parentDepth int
	if parentID != "" {
		parent, exists := s.agents[parentID]
		if !exists {
			return nil, fmt.Errorf("parent agent %s not found", parentID)
		}
		parentDepth = parent.Depth

		// 检查深度限制
		if parentDepth >= s.maxDepth {
			return nil, fmt.Errorf("max spawn depth (%d) exceeded", s.maxDepth)
		}

		// 检查父代理是否有派生权限
		if parent.Permission != nil && !parent.Permission.CanSpawn {
			return nil, fmt.Errorf("parent agent %s cannot spawn sub-agents", parentID)
		}
	}

	// 生成唯一 ID
	agentID := uuid.New().String()

	// 构建权限
	permission := s.buildPermission(parentID, config)

	// 创建子代理
	agent := &SpawnedAgent{
		ID:           agentID,
		ParentID:     parentID,
		Name:         config.Name,
		SystemPrompt: config.SystemPrompt,
		Permission:   permission,
		Depth:        parentDepth + 1,
		CreatedAt:    time.Now(),
		Status:       AgentStatusIdle,
	}

	// 注册
	s.agents[agentID] = agent
	if parentID != "" {
		s.children[parentID] = append(s.children[parentID], agentID)
	}

	if s.logger != nil {
		s.logger.Info("Sub-agent spawned",
			zap.String("agent_id", agentID),
			zap.String("parent_id", parentID),
			zap.String("name", config.Name),
			zap.Int("depth", agent.Depth),
		)
	}

	return agent, nil
}

// buildPermission 构建子代理权限
func (s *InMemorySpawner) buildPermission(parentID string, config *SpawnConfig) *Permission {
	perm := &Permission{
		Tools:       make([]string, 0),
		DeniedTools: make([]string, 0),
		CanSpawn:    config.MaxDepth > 1,
		MaxSpawns:   5,
		MaxDepth:    config.MaxDepth,
	}

	// 如果继承父代理权限
	if config.InheritTools && parentID != "" {
		if parent, exists := s.agents[parentID]; exists && parent.Permission != nil {
			// 继承父代理的工具列表
			perm.Tools = append(perm.Tools, parent.Permission.Tools...)
			perm.DeniedTools = append(perm.DeniedTools, parent.Permission.DeniedTools...)
		}
	}

	// 添加配置中的工具
	perm.Tools = append(perm.Tools, config.AllowedTools...)
	perm.DeniedTools = append(perm.DeniedTools, config.DeniedTools...)

	return perm
}

// Get 获取代理
func (s *InMemorySpawner) Get(agentID string) (*SpawnedAgent, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	agent, exists := s.agents[agentID]
	return agent, exists
}

// ListChildren 列出子代理
func (s *InMemorySpawner) ListChildren(parentID string) []*SpawnedAgent {
	s.mu.RLock()
	defer s.mu.RUnlock()

	childIDs, exists := s.children[parentID]
	if !exists {
		return []*SpawnedAgent{}
	}

	children := make([]*SpawnedAgent, 0, len(childIDs))
	for _, childID := range childIDs {
		if agent, exists := s.agents[childID]; exists {
			children = append(children, agent)
		}
	}

	return children
}

// Terminate 终止代理
func (s *InMemorySpawner) Terminate(agentID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	agent, exists := s.agents[agentID]
	if !exists {
		return fmt.Errorf("agent %s not found", agentID)
	}

	// 先终止所有子代理
	if childIDs, hasChildren := s.children[agentID]; hasChildren {
		for _, childID := range childIDs {
			if child, exists := s.agents[childID]; exists {
				child.mu.Lock()
				child.Status = AgentStatusTerminated
				child.mu.Unlock()
			}
		}
		delete(s.children, agentID)
	}

	// 终止本代理
	agent.mu.Lock()
	agent.Status = AgentStatusTerminated
	agent.mu.Unlock()

	// 从父代理的子列表中移除
	if agent.ParentID != "" {
		if siblings, exists := s.children[agent.ParentID]; exists {
			newSiblings := make([]string, 0, len(siblings)-1)
			for _, siblingID := range siblings {
				if siblingID != agentID {
					newSiblings = append(newSiblings, siblingID)
				}
			}
			s.children[agent.ParentID] = newSiblings
		}
	}

	if s.logger != nil {
		s.logger.Info("Agent terminated",
			zap.String("agent_id", agentID),
		)
	}

	return nil
}

// TerminateAll 终止所有子代理
func (s *InMemorySpawner) TerminateAll(parentID string) error {
	children := s.ListChildren(parentID)
	for _, child := range children {
		if err := s.Terminate(child.ID); err != nil {
			if s.logger != nil {
				s.logger.Warn("Failed to terminate child agent",
					zap.String("child_id", child.ID),
					zap.Error(err),
				)
			}
		}
	}
	return nil
}

// GetDepth 获取当前嵌套深度
func (s *InMemorySpawner) GetDepth(agentID string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if agent, exists := s.agents[agentID]; exists {
		return agent.Depth
	}
	return 0
}

// SetStatus 设置代理状态
func (a *SpawnedAgent) SetStatus(status AgentStatus) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.Status = status
}

// GetStatus 获取代理状态
func (a *SpawnedAgent) GetStatus() AgentStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.Status
}

// IsActive 检查代理是否活跃
func (a *SpawnedAgent) IsActive() bool {
	status := a.GetStatus()
	return status == AgentStatusIdle || status == AgentStatusRunning
}
