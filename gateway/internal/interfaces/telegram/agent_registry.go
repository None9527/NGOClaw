package telegram

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// AgentInfo Agent 信息
type AgentInfo struct {
	ID          string
	Name        string
	Description string
	Workspace   string
	Model       string
	Status      string // running, idle, stopped
	CreatedAt   time.Time
}

// AgentRegistry Agent 注册表
type AgentRegistry struct {
	agents      map[string]*AgentInfo
	activeAgent string // 当前激活的 Agent ID
	mu          sync.RWMutex
}

// NewAgentRegistry 创建 Agent 注册表
func NewAgentRegistry() *AgentRegistry {
	r := &AgentRegistry{
		agents: make(map[string]*AgentInfo),
	}

	// 注册默认 Agent
	r.Register(&AgentInfo{
		ID:          "default",
		Name:        "默认助手",
		Description: "通用 AI 助手",
		Workspace:   "/home/none/clawd",
		Model:       "antigravity/gemini-3-flash",
		Status:      "running",
		CreatedAt:   time.Now(),
	})
	r.activeAgent = "default"

	return r
}

// Register 注册 Agent
func (r *AgentRegistry) Register(agent *AgentInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agents[agent.ID] = agent
}

// Unregister 注销 Agent
func (r *AgentRegistry) Unregister(agentID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if agentID == "default" {
		return fmt.Errorf("不能注销默认 Agent")
	}

	if _, exists := r.agents[agentID]; !exists {
		return fmt.Errorf("Agent 不存在: %s", agentID)
	}

	delete(r.agents, agentID)

	// 如果注销的是当前激活的 Agent，切换到默认
	if r.activeAgent == agentID {
		r.activeAgent = "default"
	}

	return nil
}

// Get 获取 Agent
func (r *AgentRegistry) Get(agentID string) *AgentInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.agents[agentID]
}

// GetActive 获取当前激活的 Agent
func (r *AgentRegistry) GetActive() *AgentInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.agents[r.activeAgent]
}

// SetActive 设置激活的 Agent
func (r *AgentRegistry) SetActive(agentID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.agents[agentID]; !exists {
		return fmt.Errorf("Agent 不存在: %s", agentID)
	}

	r.activeAgent = agentID
	return nil
}

// List 列出所有 Agent
func (r *AgentRegistry) List() []*AgentInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*AgentInfo, 0, len(r.agents))
	for _, agent := range r.agents {
		result = append(result, agent)
	}
	return result
}

// Spawn 创建新 Agent
func (r *AgentRegistry) Spawn(ctx context.Context, name, workspace, model string) (*AgentInfo, error) {
	agent := &AgentInfo{
		ID:          fmt.Sprintf("agent_%d", time.Now().UnixNano()),
		Name:        name,
		Description: "",
		Workspace:   workspace,
		Model:       model,
		Status:      "running",
		CreatedAt:   time.Now(),
	}

	r.Register(agent)
	return agent, nil
}

// Terminate 终止 Agent
func (r *AgentRegistry) Terminate(agentID string) error {
	r.mu.Lock()
	agent, exists := r.agents[agentID]
	if exists {
		agent.Status = "stopped"
	}
	r.mu.Unlock()

	if !exists {
		return fmt.Errorf("Agent 不存在: %s", agentID)
	}

	return r.Unregister(agentID)
}

// GetActiveID 获取当前激活的 Agent ID
func (r *AgentRegistry) GetActiveID() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.activeAgent
}
