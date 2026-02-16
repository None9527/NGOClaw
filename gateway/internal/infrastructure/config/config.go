package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/viper"
)

// Config 应用配置
type Config struct {
	Gateway   GatewayConfig   `mapstructure:"gateway"`
	AIService AIServiceConfig `mapstructure:"ai_service"`
	Telegram  TelegramConfig  `mapstructure:"telegram"`
	Database  DatabaseConfig  `mapstructure:"database"`
	Log       LogConfig       `mapstructure:"log"`
	Agent     AgentConfig     `mapstructure:"agent"`
	Heartbeat HeartbeatConfig `mapstructure:"heartbeat"`
	Memory    MemoryConfig    `mapstructure:"memory"`
	PythonEnv string          `mapstructure:"python_env"` // 全局 Python 环境路径 (conda/venv 根目录)
}

// GatewayConfig 网关配置
type GatewayConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
	Mode string `mapstructure:"mode"` // local, production
}

// AIServiceConfig AI 服务配置
type AIServiceConfig struct {
	Host    string `mapstructure:"host"`
	Port    int    `mapstructure:"port"`
	Timeout int    `mapstructure:"timeout"` // seconds
}

// TelegramConfig Telegram 配置
type TelegramConfig struct {
	BotToken       string   `mapstructure:"bot_token"`
	AllowIDs       []int64  `mapstructure:"allow_ids"`
	Mode           string   `mapstructure:"mode"` // polling, webhook
	// 群组策略
	DMPolicy       string   `mapstructure:"dm_policy"`        // open, allowlist, disabled
	GroupPolicy    string   `mapstructure:"group_policy"`     // open, allowlist, disabled
	GroupAllowFrom []string `mapstructure:"group_allow_from"` // 允许的群组 ID 列表
}

// DatabaseConfig 数据库配置
type DatabaseConfig struct {
	Type string `mapstructure:"type"` // sqlite, postgres
	DSN  string `mapstructure:"dsn"`
}

// LogConfig 日志配置
type LogConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

// AgentConfig Agent 配置
type AgentConfig struct {
	DefaultModel    string        `mapstructure:"default_model"`
	DefaultProvider string        `mapstructure:"default_provider"`
	Workspace       string        `mapstructure:"workspace"`
	MaxIterations   int           `mapstructure:"max_iterations"`
	AskMode         bool          `mapstructure:"ask_mode"`
	Models          []ModelConfig `mapstructure:"models"`          // 可用模型列表
	FallbackModels  []string      `mapstructure:"fallback_models"` // 容灾备选模型链
	Providers       []LLMProviderConfig `mapstructure:"providers"` // LLM provider configs for Go builtin

	// Per-model policy overrides (model family key → overrides).
	// Keys are matched by substring against model ID, e.g. "qwen3", "minimax", "claude".
	// Nil values / omitted keys use auto-detected defaults from resolveModelPolicy.
	ModelPolicies map[string]ModelPolicyConfig `mapstructure:"model_policies"`

	// 运行时、防护栏、工具、安全、压缩、MCP 配置
	Runtime    RuntimeConfig    `mapstructure:"runtime"`
	Guardrails GuardrailsConfig `mapstructure:"guardrails"`
	Tools      ToolsConfig      `mapstructure:"tools"`
	Security   SecurityConfig   `mapstructure:"security"`
	Compaction CompactionConfig `mapstructure:"compaction"`
	MCP        MCPConfig        `mapstructure:"mcp"`
	GRPCPort   int              `mapstructure:"grpc_port"` // gRPC agent server port (default 50051)
}

// ModelPolicyConfig holds YAML-configurable per-model policy overrides.
// All fields are pointers so nil = "don't override, use auto-detected value".
type ModelPolicyConfig struct {
	RepairToolPairing   *bool   `mapstructure:"repair_tool_pairing"`
	EnforceTurnOrdering *bool   `mapstructure:"enforce_turn_ordering"`
	ReasoningFormat     *string `mapstructure:"reasoning_format"`
	ProgressInterval    *int    `mapstructure:"progress_interval"`
	ProgressEscalation  *bool   `mapstructure:"progress_escalation"`
	PromptStyle         *string `mapstructure:"prompt_style"`
	SystemRoleSupport   *bool   `mapstructure:"system_role_support"`
	ThinkingTagHint     *bool   `mapstructure:"thinking_tag_hint"`
}

// LLMProviderConfig configures a Go-native LLM provider (used by llm.Router)
type LLMProviderConfig struct {
	Name     string   `mapstructure:"name"`
	BaseURL  string   `mapstructure:"base_url"`
	APIKey   string   `mapstructure:"api_key"`
	Models   []string `mapstructure:"models"`
	Priority int      `mapstructure:"priority"`
}

// ModelConfig 模型配置
type ModelConfig struct {
	ID          string `mapstructure:"id"`          // 如 "antigravity/gemini-3-flash"
	Alias       string `mapstructure:"alias"`       // 如 "Flash"
	Provider    string `mapstructure:"provider"`    // 如 "Antigravity"
	Description string `mapstructure:"description"` // 描述
}

// RuntimeConfig Agent 运行时参数 (全部可通过 config.yaml 调整)
type RuntimeConfig struct {
	ToolTimeout       time.Duration `mapstructure:"tool_timeout"`        // 单个工具执行超时
	RunTimeout        time.Duration `mapstructure:"run_timeout"`         // 单次 Run 最大时长
	SubAgentTimeout   time.Duration `mapstructure:"sub_agent_timeout"`   // 子 Agent 超时
	SubAgentMaxSteps  int           `mapstructure:"sub_agent_max_steps"` // 子 Agent 最大步数
	MaxTokenBudget    int64         `mapstructure:"max_token_budget"`    // Token 预算上限
	ConcurrentTools   bool          `mapstructure:"concurrent_tools"`    // 是否并发执行工具
	MaxRetries        int           `mapstructure:"max_retries"`         // LLM 调用最大重试次数 (default: 3)
	RetryBaseWait     time.Duration `mapstructure:"retry_base_wait"`     // 重试基础等待时间 (default: 2s, 指数退避)
}

// GuardrailsConfig 防护栏配置
type GuardrailsConfig struct {
	ContextMaxTokens    int     `mapstructure:"context_max_tokens"`    // 上下文窗口大小
	ContextWarnRatio    float64 `mapstructure:"context_warn_ratio"`    // 警告阈值 (0.7 = 70%)
	ContextHardRatio    float64 `mapstructure:"context_hard_ratio"`    // 强制压缩阈值
	LoopDetectWindow    int     `mapstructure:"loop_detect_window"`    // 循环检测滑动窗口
	LoopDetectThreshold int     `mapstructure:"loop_detect_threshold"` // 同一工具连续 N 次视为循环
	CostGuardEnabled    bool    `mapstructure:"cost_guard_enabled"`    // 启用成本保护
}

// SecurityConfig 工具安全策略配置
type SecurityConfig struct {
	// ApprovalMode: "auto" | "ask_dangerous" | "ask_all"
	//   auto          — 全自动执行
	//   ask_dangerous — 仅对危险工具类别询问用户确认
	//   ask_all       — 所有工具调用都需要用户确认
	ApprovalMode    string        `mapstructure:"approval_mode"`
	DangerousTools  []string      `mapstructure:"dangerous_tools"`  // 需要确认的工具名列表
	TrustedTools    []string      `mapstructure:"trusted_tools"`    // 始终免确认的工具名列表
	TrustedCommands []string      `mapstructure:"trusted_commands"` // 免确认的命令前缀
	ApprovalTimeout time.Duration `mapstructure:"approval_timeout"` // 确认超时（默认 5m）
}

// ToolsConfig 工具注册表配置
type ToolsConfig struct {
	Registry []ToolRegConfig `mapstructure:"registry"`
}

// ToolRegConfig 单个工具注册配置
type ToolRegConfig struct {
	Name       string              `mapstructure:"name"`        // 规范工具名
	Backend    string              `mapstructure:"backend"`     // go | python | command | grpc
	Command    string              `mapstructure:"command"`     // backend=command 时的命令
	ArgsFormat string              `mapstructure:"args_format"` // 参数格式模板
	Handler    string              `mapstructure:"handler"`     // backend=go 时内置处理器名
	GRPCMethod string              `mapstructure:"grpc_method"` // backend=python/grpc 时
	GRPCEndpoint string            `mapstructure:"grpc_endpoint"` // backend=grpc 时的地址
	Enabled    bool                `mapstructure:"enabled"`     // 是否启用
	Timeout    time.Duration       `mapstructure:"timeout"`     // 可选，覆盖全局 tool_timeout
	Aliases    map[string][]string `mapstructure:"aliases"`     // provider → 别名列表
}

// CompactionConfig 压缩参数配置
type CompactionConfig struct {
	MessageThreshold int  `mapstructure:"message_threshold"`  // 消息数触发阈值
	TokenThreshold   int  `mapstructure:"token_threshold"`    // Token 数触发阈值
	KeepRecent       int  `mapstructure:"keep_recent"`        // 保留最近 N 条
	SummaryMaxTokens int  `mapstructure:"summary_max_tokens"` // 摘要最大 token
	PreFlushToMemory bool `mapstructure:"pre_flush_to_memory"` // 压缩前写关键事实到向量库
}

// MCPConfig MCP 服务器配置
type MCPConfig struct {
	Servers []MCPServerConfig `mapstructure:"servers"`
}

// MCPServerConfig 单个 MCP 服务器
type MCPServerConfig struct {
	Name     string `mapstructure:"name"`     // 服务名称
	Endpoint string `mapstructure:"endpoint"` // JSON-RPC endpoint
	Enabled  bool   `mapstructure:"enabled"`  // 是否启用
}

// HeartbeatConfig 心跳配置
type HeartbeatConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	FilePath string `mapstructure:"file_path"` // HEARTBEAT.md 路径
	Interval int    `mapstructure:"interval"`  // 检查间隔(分钟)
	ChatID   int64  `mapstructure:"chat_id"`   // 目标 Telegram ChatID
}

// MemoryConfig 向量记忆配置
type MemoryConfig struct {
	Enabled    bool   `mapstructure:"enabled"`
	OllamaURL  string `mapstructure:"ollama_url"`   // Ollama 服务地址 (http://host:port)
	EmbedModel string `mapstructure:"embed_model"`  // 嵌入模型名, 如 qwen3-embedding
	StorePath  string `mapstructure:"store_path"`   // LanceDB 持久化目录
	StoreType  string `mapstructure:"store_type"`   // lancedb | memory
}

// Load 加载配置
func Load() (*Config, error) {
	v := viper.New()

	// 设置默认值
	setDefaults(v)

	// ─── 分层配置加载 (与 Claude Code / Gemini CLI 一致) ───
	// 优先级 (低 → 高): 默认值 → 全局 ~/.ngoclaw/ → 项目本地 → 环境变量
	v.SetConfigName("config")
	v.SetConfigType("yaml")

	// Layer 1: 全局配置 ~/.ngoclaw/config.yaml (基础层 — API keys, providers, telegram)
	globalDir := filepath.Join(os.Getenv("HOME"), ".ngoclaw")
	v.AddConfigPath(globalDir)
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("failed to read global config: %w", err)
		}
	}

	// Layer 2: 项目本地配置 (覆盖层 — workspace, models, runtime 等)
	// 检查 ./config/config.yaml 和 ./config.yaml, 用 MergeInConfig 叠加
	for _, localDir := range []string{"./config", "."} {
		localPath := filepath.Join(localDir, "config.yaml")
		if _, err := os.Stat(localPath); err == nil {
			v2 := viper.New()
			v2.SetConfigFile(localPath)
			if err := v2.ReadInConfig(); err == nil {
				_ = v.MergeConfigMap(v2.AllSettings())
			}
			break // 只取第一个找到的本地配置
		}
	}

	// 叠加兼容的 openclaw.json (仅补充 providers/model/telegram)
	_ = loadOpenClawConfig(v)

	// 环境变量覆盖
	v.SetEnvPrefix("NGOCLAW")
	v.AutomaticEnv()

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return &cfg, nil
}

// setDefaults 设置默认配置
func setDefaults(v *viper.Viper) {
	// Gateway 默认值
	v.SetDefault("gateway.host", "0.0.0.0")
	v.SetDefault("gateway.port", 18789)
	v.SetDefault("gateway.mode", "local")

	// AI Service 默认值
	v.SetDefault("ai_service.host", "localhost")
	v.SetDefault("ai_service.port", 50051)
	v.SetDefault("ai_service.timeout", 120)

	// Database 默认值
	v.SetDefault("database.type", "sqlite")
	v.SetDefault("database.dsn", "ngoclaw.db")

	// Log 默认值
	v.SetDefault("log.level", "info")
	v.SetDefault("log.format", "json")

	// Agent Runtime 默认值
	v.SetDefault("agent.runtime.tool_timeout", "30s")
	v.SetDefault("agent.runtime.run_timeout", "5m")
	v.SetDefault("agent.runtime.sub_agent_timeout", "2m")
	v.SetDefault("agent.runtime.max_token_budget", 100000)
	v.SetDefault("agent.runtime.concurrent_tools", true)
	v.SetDefault("agent.runtime.max_retries", 3)
	v.SetDefault("agent.runtime.retry_base_wait", "2s")

	// Guardrails 默认值
	v.SetDefault("agent.guardrails.context_max_tokens", 128000)
	v.SetDefault("agent.guardrails.context_warn_ratio", 0.7)
	v.SetDefault("agent.guardrails.context_hard_ratio", 0.85)
	v.SetDefault("agent.guardrails.loop_detect_window", 10)
	v.SetDefault("agent.guardrails.loop_detect_threshold", 5)
	v.SetDefault("agent.guardrails.cost_guard_enabled", true)

	// Compaction 默认值
	v.SetDefault("agent.compaction.message_threshold", 30)
	v.SetDefault("agent.compaction.token_threshold", 30000)
	v.SetDefault("agent.compaction.keep_recent", 10)
	v.SetDefault("agent.compaction.summary_max_tokens", 1000)
	v.SetDefault("agent.compaction.pre_flush_to_memory", true)

	// Security 默认值
	v.SetDefault("agent.security.approval_mode", "ask_dangerous")
	v.SetDefault("agent.security.dangerous_tools", []string{"shell_exec", "write_file", "delete_file", "python_exec"})
	v.SetDefault("agent.security.trusted_tools", []string{"read_file", "list_files", "web_search", "think"})
	v.SetDefault("agent.security.trusted_commands", []string{"ls", "cat", "head", "tail", "grep", "find", "wc", "echo", "pwd", "which", "file", "stat"})
	v.SetDefault("agent.security.approval_timeout", "5m")
}

// loadOpenClawConfig 加载兼容的 openclaw.json 配置
func loadOpenClawConfig(v *viper.Viper) error {
	// 搜索 openclaw.json
	paths := []string{
		filepath.Join(os.Getenv("HOME"), ".openclaw", "openclaw.json"),
		"openclaw.json",
	}

	var configPath string
	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			configPath = path
			break
		}
	}

	if configPath == "" {
		return fmt.Errorf("openclaw.json not found")
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("read openclaw.json: %w", err)
	}

	// Parse the JSON
	var oc map[string]interface{}
	if err := json.Unmarshal(data, &oc); err != nil {
		return fmt.Errorf("parse openclaw.json: %w", err)
	}

	// Map providers
	if providers, ok := oc["providers"].([]interface{}); ok {
		for _, p := range providers {
			prov, ok := p.(map[string]interface{})
			if !ok {
				continue
			}
			name, _ := prov["name"].(string)
			apiKey, _ := prov["apiKey"].(string)
			baseURL, _ := prov["baseURL"].(string)

			if name != "" && apiKey != "" {
				v.Set(fmt.Sprintf("providers.%s.api_key", name), apiKey)
			}
			if name != "" && baseURL != "" {
				v.Set(fmt.Sprintf("providers.%s.base_url", name), baseURL)
			}
		}
	}

	// Map default model
	if model, ok := oc["model"].(string); ok && model != "" {
		v.Set("agent.runtime.model", model)
	}

	// Map telegram bot token
	if tg, ok := oc["telegram"].(map[string]interface{}); ok {
		if token, ok := tg["botToken"].(string); ok && token != "" {
			v.Set("telegram.token", token)
		}
	}

	return nil
}
