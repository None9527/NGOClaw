package telegram

import (
	"database/sql"
	"fmt"
	"sync"

	_ "github.com/mattn/go-sqlite3"
)

// PersistentSessionManager SQLite 持久化会话管理器
type PersistentSessionManager struct {
	db           *sql.DB
	cache        map[int64]*ChatSession // 内存缓存
	models       []ModelInfo
	defaultModel string
	mu           sync.RWMutex
}

// NewPersistentSessionManager 创建持久化会话管理器
func NewPersistentSessionManager(dbPath string, defaultModel string) (*PersistentSessionManager, error) {
	if defaultModel == "" {
		defaultModel = "bailian/qwen3-max-2026-01-23"
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	manager := &PersistentSessionManager{
		db:           db,
		cache:        make(map[int64]*ChatSession),
		models:       getDefaultModels(),
		defaultModel: defaultModel,
	}

	if err := manager.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to init schema: %w", err)
	}

	return manager, nil
}

// initSchema 初始化数据库表结构
func (m *PersistentSessionManager) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS sessions (
		chat_id INTEGER PRIMARY KEY,
		user_id INTEGER,
		current_model TEXT DEFAULT 'antigravity/gemini-3-flash',
		think TEXT DEFAULT 'medium',
		verbose INTEGER DEFAULT 0,
		reasoning TEXT DEFAULT 'off',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	
	CREATE TABLE IF NOT EXISTS cron_jobs (
		id TEXT PRIMARY KEY,
		chat_id INTEGER,
		cron_expr TEXT,
		command TEXT,
		enabled INTEGER DEFAULT 1,
		last_run DATETIME,
		next_run DATETIME,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	
	CREATE INDEX IF NOT EXISTS idx_cron_next_run ON cron_jobs(next_run);
	CREATE INDEX IF NOT EXISTS idx_cron_enabled ON cron_jobs(enabled);
	`
	_, err := m.db.Exec(schema)
	return err
}

// getOrCreateSession 获取或创建会话
func (m *PersistentSessionManager) getOrCreateSession(chatID int64) *ChatSession {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 检查缓存
	if session, exists := m.cache[chatID]; exists {
		return session
	}

	// 从数据库加载
	session := &ChatSession{
		ChatID:       chatID,
		CurrentModel: m.defaultModel,
		Think:        "medium",
		Verbose:      false,
		Reasoning:    "off",
	}

	row := m.db.QueryRow(`
		SELECT user_id, current_model, think, verbose, reasoning 
		FROM sessions WHERE chat_id = ?`, chatID)

	var verbose int
	err := row.Scan(&session.UserID, &session.CurrentModel, &session.Think, &verbose, &session.Reasoning)
	if err == nil {
		session.Verbose = verbose != 0
	} else if err != sql.ErrNoRows {
		// 数据库错误，使用默认值
	}

	m.cache[chatID] = session
	return session
}

// saveSession 保存会话到数据库
func (m *PersistentSessionManager) saveSession(session *ChatSession) error {
	verbose := 0
	if session.Verbose {
		verbose = 1
	}

	_, err := m.db.Exec(`
		INSERT INTO sessions (chat_id, user_id, current_model, think, verbose, reasoning, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(chat_id) DO UPDATE SET
			user_id = excluded.user_id,
			current_model = excluded.current_model,
			think = excluded.think,
			verbose = excluded.verbose,
			reasoning = excluded.reasoning,
			updated_at = CURRENT_TIMESTAMP`,
		session.ChatID, session.UserID, session.CurrentModel, session.Think, verbose, session.Reasoning)

	return err
}

// CreateSession 创建新会话
func (m *PersistentSessionManager) CreateSession(chatID int64, userID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	session := &ChatSession{
		ChatID:       chatID,
		UserID:       userID,
		CurrentModel: m.defaultModel,
		Think:        "medium",
		Verbose:      false,
		Reasoning:    "off",
	}

	m.cache[chatID] = session
	return m.saveSession(session)
}

// ClearSession 清除会话历史
func (m *PersistentSessionManager) ClearSession(chatID int64) error {
	session := m.getOrCreateSession(chatID)
	// 保留配置，只清除历史（实际历史存储在其他地方）
	return m.saveSession(session)
}

// GetCurrentModel 获取当前模型
func (m *PersistentSessionManager) GetCurrentModel(chatID int64) string {
	session := m.getOrCreateSession(chatID)
	return session.CurrentModel
}

// SetModel 设置模型
func (m *PersistentSessionManager) SetModel(chatID int64, model string) error {
	resolvedModel := m.resolveModel(model)
	if resolvedModel == "" {
		return fmt.Errorf("未知模型: %s", model)
	}

	session := m.getOrCreateSession(chatID)
	session.CurrentModel = resolvedModel
	return m.saveSession(session)
}

// resolveModel 解析模型名称
func (m *PersistentSessionManager) resolveModel(input string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 完整路径匹配
	for _, model := range m.models {
		if model.ID == input {
			return model.ID
		}
	}

	// 别名匹配
	inputLower := toLowerCase(input)
	for _, model := range m.models {
		if toLowerCase(model.Alias) == inputLower {
			return model.ID
		}
	}

	// 部分匹配
	for _, model := range m.models {
		if contains(model.ID, input) {
			return model.ID
		}
	}

	if contains(input, "/") {
		return input
	}

	return ""
}

// GetAvailableModels 获取可用模型列表
func (m *PersistentSessionManager) GetAvailableModels() []ModelInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]ModelInfo, len(m.models))
	copy(result, m.models)
	return result
}

// SetAvailableModels 设置可用模型列表
func (m *PersistentSessionManager) SetAvailableModels(models []ModelInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.models = models
}

// GetSession 获取会话
func (m *PersistentSessionManager) GetSession(chatID int64) *ChatSession {
	return m.getOrCreateSession(chatID)
}

// SetThink 设置思考级别
func (m *PersistentSessionManager) SetThink(chatID int64, level string) {
	session := m.getOrCreateSession(chatID)
	session.Think = level
	m.saveSession(session)
}

// SetVerbose 设置详细模式
func (m *PersistentSessionManager) SetVerbose(chatID int64, verbose bool) {
	session := m.getOrCreateSession(chatID)
	session.Verbose = verbose
	m.saveSession(session)
}

// SetReasoning 设置推理可见性
func (m *PersistentSessionManager) SetReasoning(chatID int64, mode string) {
	session := m.getOrCreateSession(chatID)
	session.Reasoning = mode
	m.saveSession(session)
}

// Close 关闭数据库连接
func (m *PersistentSessionManager) Close() error {
	return m.db.Close()
}
