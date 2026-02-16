package telegram

import (
	"fmt"
	"sync"
)

// DefaultSessionManager 默认会话管理器实现
type DefaultSessionManager struct {
	mu           sync.RWMutex
	sessions     map[int64]*ChatSession // chatID -> session
	models       []ModelInfo            // 可用模型列表
	defaultModel string                 // 新会话默认模型
}

// ChatSession 聊天会话
type ChatSession struct {
	ChatID       int64
	UserID       int64
	CurrentModel string
	Think        string // off/low/medium/high
	Verbose      bool
	Reasoning    string // off/on/stream
}

// NewDefaultSessionManager 创建默认会话管理器
func NewDefaultSessionManager(defaultModel string) *DefaultSessionManager {
	if defaultModel == "" {
		defaultModel = "bailian/qwen3-max-2026-01-23"
	}
	return &DefaultSessionManager{
		sessions:     make(map[int64]*ChatSession),
		models:       getDefaultModels(),
		defaultModel: defaultModel,
	}
}

// getDefaultModels 获取默认模型列表 (与 OpenClaw 对齐)
func getDefaultModels() []ModelInfo {
	return []ModelInfo{
		// Bailian (主力)
		{ID: "bailian/qwen3-max-2026-01-23", Alias: "qwen3-max-thinking", Provider: "Bailian", Description: "Qwen3 Max Thinking"},
		{ID: "bailian/qwen3-coder-plus", Alias: "coder", Provider: "Bailian", Description: "Qwen3 Coder Plus"},

		// MiniMax
		{ID: "minimax/MiniMax-M2.1", Alias: "Minimax", Provider: "MiniMax", Description: "MiniMax M2.1"},
		{ID: "minimax/MiniMax-M2.1-lightning", Alias: "minimax-light", Provider: "MiniMax", Description: "MiniMax M2.1 Lightning"},
	}
}

// getOrCreateSession 获取或创建会话
func (m *DefaultSessionManager) getOrCreateSession(chatID int64) *ChatSession {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, exists := m.sessions[chatID]
	if !exists {
		session = &ChatSession{
			ChatID:       chatID,
			CurrentModel: m.defaultModel,
			Think:        "medium",
			Verbose:      false,
			Reasoning:    "off",
		}
		m.sessions[chatID] = session
	}
	return session
}

// CreateSession 创建新会话
func (m *DefaultSessionManager) CreateSession(chatID int64, userID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 创建新会话，重置所有状态
	m.sessions[chatID] = &ChatSession{
		ChatID:       chatID,
		UserID:       userID,
		CurrentModel: m.defaultModel,
		Think:        "medium",
		Verbose:      false,
		Reasoning:    "off",
	}

	return nil
}

// ClearSession 清除会话历史
func (m *DefaultSessionManager) ClearSession(chatID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 保留配置，只清除历史（重置 session）
	if session, exists := m.sessions[chatID]; exists {
		// 保留当前模型设置
		currentModel := session.CurrentModel
		think := session.Think
		verbose := session.Verbose
		reasoning := session.Reasoning

		m.sessions[chatID] = &ChatSession{
			ChatID:       chatID,
			UserID:       session.UserID,
			CurrentModel: currentModel,
			Think:        think,
			Verbose:      verbose,
			Reasoning:    reasoning,
		}
	}

	return nil
}

// GetCurrentModel 获取当前模型
func (m *DefaultSessionManager) GetCurrentModel(chatID int64) string {
	session := m.getOrCreateSession(chatID)
	return session.CurrentModel
}

// SetModel 设置模型
func (m *DefaultSessionManager) SetModel(chatID int64, model string) error {
	// 解析模型输入 (支持别名和完整路径)
	resolvedModel := m.resolveModel(model)
	if resolvedModel == "" {
		return fmt.Errorf("未知模型: %s", model)
	}

	session := m.getOrCreateSession(chatID)
	session.CurrentModel = resolvedModel

	return nil
}

// resolveModel 解析模型名称 (别名或完整路径)
func (m *DefaultSessionManager) resolveModel(input string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 首先检查是否是完整路径
	for _, model := range m.models {
		if model.ID == input {
			return model.ID
		}
	}

	// 检查别名 (不区分大小写)
	inputLower := toLowerCase(input)
	for _, model := range m.models {
		if toLowerCase(model.Alias) == inputLower {
			return model.ID
		}
	}

	// 检查部分匹配 (模型名末尾)
	for _, model := range m.models {
		if contains(model.ID, input) {
			return model.ID
		}
	}

	// 假设是有效模型名，直接返回
	if contains(input, "/") {
		return input
	}

	return ""
}

// GetAvailableModels 获取可用模型列表
func (m *DefaultSessionManager) GetAvailableModels() []ModelInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]ModelInfo, len(m.models))
	copy(result, m.models)
	return result
}

// SetAvailableModels 设置可用模型列表
func (m *DefaultSessionManager) SetAvailableModels(models []ModelInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.models = models
}

// GetSession 获取会话
func (m *DefaultSessionManager) GetSession(chatID int64) *ChatSession {
	return m.getOrCreateSession(chatID)
}

// SetThink 设置思考级别
func (m *DefaultSessionManager) SetThink(chatID int64, level string) {
	session := m.getOrCreateSession(chatID)
	session.Think = level
}

// SetVerbose 设置详细模式
func (m *DefaultSessionManager) SetVerbose(chatID int64, verbose bool) {
	session := m.getOrCreateSession(chatID)
	session.Verbose = verbose
}

// SetReasoning 设置推理可见性
func (m *DefaultSessionManager) SetReasoning(chatID int64, mode string) {
	session := m.getOrCreateSession(chatID)
	session.Reasoning = mode
}

// 辅助函数
func toLowerCase(s string) string {
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		result[i] = c
	}
	return string(result)
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr) >= 0))
}

func findSubstring(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
