package telegram

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// Command Telegram 命令
type Command struct {
	Name    string   // 命令名 (不含 /)
	Args    []string // 参数列表
	RawArgs string   // 原始参数字符串
	ChatID  int64
	UserID  int64
}

// CommandHandler 命令处理器
type CommandHandler func(ctx context.Context, cmd *Command) (*OutgoingMessage, error)

// SessionManager 会话管理接口
type SessionManager interface {
	CreateSession(chatID int64, userID int64) error
	ClearSession(chatID int64) error
	GetCurrentModel(chatID int64) string
	SetModel(chatID int64, model string) error
	GetAvailableModels() []ModelInfo
}

// ContextController 上下文控制器接口 - 用于 /compact 和 /context 命令
type ContextController interface {
	// CompactContext 压缩指定 chat 的上下文，返回 (tokensBefore, tokensAfter, error)
	CompactContext(ctx context.Context, chatID int64, instructions string) (int, int, error)
	// GetContextStats 获取上下文统计信息
	GetContextStats(chatID int64) *ContextStats
}

// SessionSettings 会话设置接口 - 用于持久化用户偏好 (对标 OpenClaw sessionEntry)
type SessionSettings interface {
	GetUsageMode(chatID int64) string // "off"|"tokens"|"full"
	SetUsageMode(chatID int64, mode string)
	GetThinkLevel(chatID int64) string // "off"|"low"|"medium"|"high"
	SetThinkLevel(chatID int64, level string)
	GetVerbose(chatID int64) bool
	SetVerbose(chatID int64, on bool)
	GetReasoning(chatID int64) string // "on"|"off"|"stream"
	SetReasoning(chatID int64, mode string)
	GetActivation(chatID int64) string // "always"|"mention"
	SetActivation(chatID int64, mode string)
	GetSendPolicy(chatID int64) string // "allow"|"deny"|"inherit"
	SetSendPolicy(chatID int64, policy string)
}

// ContextStats 上下文统计
type ContextStats struct {
	MessageCount int
	TokenCount   int
	MaxTokens    int
}

// ConfigManager 配置管理接口 (对标 OpenClaw commands-config.ts)
type ConfigManager interface {
	GetConfigValue(path string) (interface{}, error)
	SetConfigValue(path string, value string) error
	UnsetConfigValue(path string) error
	GetDebugOverrides() map[string]interface{}
	SetDebugOverride(path string, value string) error
	UnsetDebugOverride(path string) error
	ResetDebugOverrides()
	IsFeatureEnabled(feature string) bool // "config", "debug", "bash", "restart"
	GetConfigJSON() string
}

// BashExecutor 命令执行接口 (对标 OpenClaw commands-bash.ts)
type BashExecutor interface {
	Execute(ctx context.Context, chatID int64, command string) (string, error)
}

// ApprovalManager 审批管理接口 (对标 OpenClaw commands-approve.ts)
type ApprovalManager interface {
	ResolveApproval(ctx context.Context, approvalID string, decision string) error
}

// HistoryClearer 对话历史清除接口 — 允许命令层清除 agent loop 的对话记忆
type HistoryClearer interface {
	ClearHistory(chatID int64)
}

// AllowlistManager 白名单管理接口 (对标 OpenClaw commands-allowlist.ts)
type AllowlistManager interface {
	ListAllowlist(chatID int64, scope string) (entries []string, policy string, err error)
	AddAllowlist(chatID int64, scope string, entry string) error
	RemoveAllowlist(chatID int64, scope string, entry string) error
}

// SubagentInfo 子代理信息
type SubagentInfo struct {
	Index      int
	RunID      string
	SessionKey string
	Label      string
	Status     string // "running"|"done"|"error"
	Runtime    string
	Task       string
}

// SubagentManager 子代理管理接口 (对标 OpenClaw commands-subagents.ts)
type SubagentManager interface {
	ListSubagents(chatID int64) []SubagentInfo
	StopSubagent(ctx context.Context, chatID int64, target string) (string, error)
	StopAllSubagents(ctx context.Context, chatID int64) (int, error)
	SubagentInfo(chatID int64, target string) (string, error)
	SubagentLog(chatID int64, target string, limit int) (string, error)
	SendToSubagent(ctx context.Context, chatID int64, target string, message string) (string, error)
}

// PluginManager 插件命令接口 (对标 OpenClaw commands-plugin.ts)
type PluginManager interface {
	MatchCommand(normalized string) (cmd string, args string, matched bool)
	ExecuteCommand(ctx context.Context, cmd string, args string, chatID int64) (string, error)
}

// TtsStatus TTS 状态信息
type TtsStatus struct {
	Enabled       bool
	Provider      string
	ProviderReady bool
	TextLimit     int
	AutoSummary   bool
}

// TtsController TTS 控制接口 (对标 OpenClaw commands-tts.ts)
type TtsController interface {
	IsEnabled(chatID int64) bool
	SetEnabled(chatID int64, on bool)
	GetProvider(chatID int64) string
	SetProvider(chatID int64, provider string) error
	GetLimit(chatID int64) int
	SetLimit(chatID int64, limit int) error
	IsSummaryEnabled(chatID int64) bool
	SetSummaryEnabled(chatID int64, on bool)
	GenerateAudio(ctx context.Context, chatID int64, text string) (string, error)
	GetStatus(chatID int64) *TtsStatus
}

// ModelInfo 模型信息
type ModelInfo struct {
	ID          string // 模型 ID (如 "antigravity/gemini-3-flash")
	Alias       string // 别名 (如 "Flash")
	Provider    string // 提供商
	Description string // 描述
}

// CommandRegistry 命令注册表
type CommandRegistry struct {
	handlers          map[string]CommandHandler
	aliases           map[string]string
	sessionManager    SessionManager
	runController     RunController
	contextController ContextController
	sessionSettings   SessionSettings
	configManager     ConfigManager
	bashExecutor      BashExecutor
	approvalManager   ApprovalManager
	allowlistManager  AllowlistManager
	subagentManager   SubagentManager
	pluginManager     PluginManager
	ttsController     TtsController
	skillManager      *SkillManager
	cronService       *CronService
	historyClearer    HistoryClearer
	mu                sync.RWMutex
}

// NewCommandRegistry 创建命令注册表
func NewCommandRegistry() *CommandRegistry {
	return &CommandRegistry{
		handlers: make(map[string]CommandHandler),
		aliases:  make(map[string]string),
	}
}

// SetSessionManager 设置会话管理器
func (r *CommandRegistry) SetSessionManager(sm SessionManager) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessionManager = sm
}

// SetRunController 设置运行控制器
func (r *CommandRegistry) SetRunController(ctrl RunController) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runController = ctrl
}

// SetContextController 设置上下文控制器
func (r *CommandRegistry) SetContextController(ctrl ContextController) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.contextController = ctrl
}

// SetSessionSettings 设置会话设置
func (r *CommandRegistry) SetSessionSettings(ss SessionSettings) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessionSettings = ss
}

// SetConfigManager 设置配置管理器
func (r *CommandRegistry) SetConfigManager(cm ConfigManager) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.configManager = cm
}

// SetBashExecutor 设置命令执行器
func (r *CommandRegistry) SetBashExecutor(be BashExecutor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bashExecutor = be
}

// SetApprovalManager 设置审批管理器
func (r *CommandRegistry) SetApprovalManager(am ApprovalManager) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.approvalManager = am
}

// SetAllowlistManager 设置白名单管理器
func (r *CommandRegistry) SetAllowlistManager(alm AllowlistManager) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.allowlistManager = alm
}

// SetSubagentManager 设置子代理管理器
func (r *CommandRegistry) SetSubagentManager(sm SubagentManager) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.subagentManager = sm
}

// SetPluginManager 设置插件管理器
func (r *CommandRegistry) SetPluginManager(pm PluginManager) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pluginManager = pm
}

// SetTtsController 设置 TTS 控制器
func (r *CommandRegistry) SetTtsController(tc TtsController) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ttsController = tc
}

// SetSkillManager sets the skill manager.
func (r *CommandRegistry) SetSkillManager(sm *SkillManager) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.skillManager = sm
}

// SetCronService sets the cron service.
func (r *CommandRegistry) SetCronService(cs *CronService) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cronService = cs
}

// SetHistoryClearer 设置对话历史清除器
func (r *CommandRegistry) SetHistoryClearer(hc HistoryClearer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.historyClearer = hc
}

// Register 注册命令
func (r *CommandRegistry) Register(name string, handler CommandHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[strings.ToLower(name)] = handler
}

// Alias 注册命令别名
func (r *CommandRegistry) Alias(alias, target string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.aliases[strings.ToLower(alias)] = strings.ToLower(target)
}

// Handle 处理命令
func (r *CommandRegistry) Handle(ctx context.Context, cmd *Command) (*OutgoingMessage, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	name := strings.ToLower(cmd.Name)

	// 检查别名
	if target, ok := r.aliases[name]; ok {
		name = target
	}

	handler, exists := r.handlers[name]
	if !exists {
		return nil, false, nil
	}

	response, err := handler(ctx, cmd)
	return response, true, err
}

// ParseCommand 解析命令
func ParseCommand(text string) *Command {
	if !strings.HasPrefix(text, "/") {
		return nil
	}

	// 移除 @ 后缀 (群组中的 /cmd@botname)
	parts := strings.SplitN(text[1:], " ", 2)
	cmdPart := parts[0]
	if idx := strings.Index(cmdPart, "@"); idx != -1 {
		cmdPart = cmdPart[:idx]
	}

	cmd := &Command{
		Name: cmdPart,
	}

	if len(parts) > 1 {
		cmd.RawArgs = parts[1]
		cmd.Args = strings.Fields(parts[1])
	}

	return cmd
}

// RegisterBuiltinCommands 注册内置命令 (delegated to cmd_*.go files)
func (a *Adapter) RegisterBuiltinCommands(registry *CommandRegistry, secCtrl ...SecurityController) {
	a.registerSessionCommands(registry)
	a.registerModelCommands(registry)
	a.registerSettingsCommands(registry)
	a.registerContextCommands(registry)
	a.registerAgentCommands(registry)
	a.registerAdminCommands(registry)
	if len(secCtrl) > 0 && secCtrl[0] != nil {
		a.registerSecurityCommands(registry, secCtrl[0])
	}
}




// SetCommandRegistry 设置命令注册表
func (a *Adapter) SetCommandRegistry(registry *CommandRegistry) {
	a.commandRegistry = registry
}

// parsePageNumber 解析页码 (返回 -1 表示无效)
func parsePageNumber(s string) int {
	if len(s) == 0 {
		return -1
	}
	n := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return -1
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// formatTokenCount 格式化 token 数量 (对标 OpenClaw formatTokenCount)
func formatTokenCount(tokens int) string {
	if tokens >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(tokens)/1_000_000)
	}
	if tokens >= 1_000 {
		return fmt.Sprintf("%.1fk", float64(tokens)/1_000)
	}
	return fmt.Sprintf("%d", tokens)
}
