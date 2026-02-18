package config

import (
	"fmt"
	"os"
	"path/filepath"

	"go.uber.org/zap"
)

// AppName is the canonical application name
const AppName = "ngoclaw"

// WorkspaceDirName is the directory name used for workspace-level config.
// Place .ngoclaw/ in a project root for project-specific overrides.
const WorkspaceDirName = "." + AppName

// HomeDir returns the user's NGO-Claw configuration home: ~/.ngoclaw
func HomeDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "."+AppName)
}

// Bootstrap ensures the ~/.ngoclaw directory exists with all default content.
// Called once at startup. Safe to call multiple times — only creates missing items.
func Bootstrap(logger *zap.Logger) error {
	root := HomeDir()

	// Directory tree
	dirs := []string{
		root,
		filepath.Join(root, "prompts"),
		filepath.Join(root, "prompts", "variants"),
		filepath.Join(root, "skills"),
		filepath.Join(root, "modules"),
		filepath.Join(root, "memory"),
		filepath.Join(root, "logs"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create dir %s: %w", dir, err)
		}
	}

	// Default files — only written if they don't already exist (never overwrite user edits)
	defaults := map[string]string{
		filepath.Join(root, "config.yaml"):                     defaultConfig,
		filepath.Join(root, "soul.md"):                         defaultSoul,
		filepath.Join(root, "prompts", "rules.md"):             defaultRules,
		filepath.Join(root, "prompts", "capabilities.md"):      defaultCapabilities,
		filepath.Join(root, "prompts", "coding.md"):            defaultCoding,
		filepath.Join(root, "prompts", "finance.md"):           defaultFinance,
		filepath.Join(root, "prompts", "variants", "qwen.md"):  defaultVariantQwen,
		filepath.Join(root, "prompts", "variants", "default.md"): defaultVariantDefault,
	}

	created := 0
	for path, content := range defaults {
		if _, err := os.Stat(path); err == nil {
			continue // Already exists, skip
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			logger.Warn("Failed to write default file", zap.String("path", path), zap.Error(err))
			continue
		}
		created++
	}

	if created > 0 {
		logger.Info("NGO-Claw bootstrap complete",
			zap.String("home", root),
			zap.Int("files_created", created),
		)
	} else {
		logger.Debug("NGO-Claw home directory OK", zap.String("home", root))
	}

	return nil
}

// ──────────────────────────────────────────────────────────────
// Embedded default file contents
// ──────────────────────────────────────────────────────────────

const defaultConfig = `# ═══════════════════════════════════════════════════════════════
# NGOClaw Configuration / NGOClaw 配置文件
# Auto-generated on first launch — feel free to edit
# 首次启动自动生成 — 可自由编辑
# Docs: https://github.com/None9527/NGOClaw/blob/main/docs/USER_MANUAL.md
# ═══════════════════════════════════════════════════════════════

# ─── Gateway Server / 网关服务 ────────────────────────────────
# HTTP API server settings.
# HTTP API 服务监听地址。
gateway:
  host: 0.0.0.0
  port: 18790
  mode: local                  # local | production

# ─── Telegram Bot / Telegram 机器人 ──────────────────────────
# Leave bot_token empty to disable Telegram interface.
# bot_token 为空则不启用 Telegram 接口。
telegram:
  bot_token: ""                # Get from @BotFather / 从 @BotFather 获取
  allow_ids: []                # Allowed user IDs / 允许的用户 ID 列表
  mode: polling                # polling | webhook
  dm_policy: allowlist         # allowlist | open
  group_policy: allowlist      # allowlist | open

# ─── Database / 数据库 ───────────────────────────────────────
# Conversation history storage.
# 会话历史存储。
database:
  type: sqlite                 # sqlite | postgres
  dsn: ngoclaw.db              # File path (sqlite) or connection string (postgres)

# ─── Logging / 日志 ──────────────────────────────────────────
log:
  level: info                  # debug | info | warn | error
  format: console              # console | json

# ─── Agent Core / Agent 核心 ─────────────────────────────────
# Main agent behavior settings.
# Agent 主要行为配置。
agent:
  default_model: ""            # e.g. "openai/gpt-4o" / 格式: "provider/model"
  workspace: ""                # Default workspace dir / 默认工作目录 (空=当前目录)
  max_iterations: 50           # Max ReAct loop steps / 最大循环步数

  # ─── LLM Providers / LLM 服务商 ──────────────────────────
  # Add one or more providers. Lower priority = preferred.
  # 添加一个或多个 Provider。priority 越小越优先。
  # Supports: OpenAI, Anthropic, Google, Bailian, MiniMax, etc.
  providers: []
  # Example / 示例:
  # providers:
  #   - name: openai
  #     base_url: "https://api.openai.com/v1"
  #     api_key: "sk-..."
  #     models:
  #       - "openai/gpt-4o"
  #       - "openai/gpt-4o-mini"
  #     priority: 1
  #
  #   - name: anthropic
  #     base_url: "https://api.anthropic.com/v1"
  #     api_key: "sk-ant-..."
  #     api_type: "anthropic"
  #     models:
  #       - "anthropic/claude-sonnet-4-20250514"
  #     priority: 2

  # ─── Runtime Limits / 运行时限制 ──────────────────────────
  # Timeout and resource constraints for tool execution.
  # 工具执行的超时和资源约束。
  runtime:
    tool_timeout: 60s          # Single tool timeout / 单次工具超时
    run_timeout: 10m           # Total agent run timeout / 总运行超时
    sub_agent_timeout: 3m      # Sub-agent timeout / 子 Agent 超时
    sub_agent_max_steps: 25    # Sub-agent max steps / 子 Agent 最大步数
    max_token_budget: 180000   # Token budget per run / 单次 Token 预算
    concurrent_tools: true     # Allow parallel tool calls / 允许并行工具调用
    max_retries: 3             # Auto-retry on failure / 失败自动重试次数
    retry_base_wait: 2s        # Retry backoff base / 重试等待基数

  # ─── Guardrails / 安全护栏 ────────────────────────────────
  # Context window management and loop detection.
  # 上下文窗口管理和循环检测。
  guardrails:
    context_max_tokens: 180000 # Max context window / 最大上下文窗口
    context_warn_ratio: 0.7    # Warn at 70% usage / 70% 时警告
    context_hard_ratio: 0.85   # Force compaction at 85% / 85% 时强制压缩
    loop_detect_threshold: 5   # Identical calls threshold / 相同调用阈值

  # ─── Context Compaction / 上下文压缩 ──────────────────────
  # Automatic conversation summarization when context grows large.
  # 上下文过大时自动摘要压缩。
  compaction:
    message_threshold: 30      # Trigger after N messages / N 条消息后触发
    keep_recent: 10            # Keep last N messages / 保留最近 N 条
    summary_max_tokens: 1000   # Summary budget / 摘要 Token 上限

# ─── Long-term Memory / 长期记忆 ─────────────────────────────
# Vector-based memory for cross-conversation recall.
# 基于向量的跨会话记忆（需要 Ollama 提供嵌入服务）。
memory:
  enabled: false               # Enable memory system / 启用记忆系统
  ollama_url: ""               # Ollama API URL / Ollama 服务地址
  embed_model: ""              # Embedding model name / 嵌入模型名
  store_path: "~/.ngoclaw/memory/lancedb"
  store_type: "lancedb"        # lancedb (default)
`

const defaultSoul = `You are NGO-Claw, an autonomous AI agent with deep expertise across software engineering, data analysis, research, and general problem-solving.

## Core Identity

- You are direct, precise, and action-oriented
- You execute tasks autonomously — act first, explain briefly after
- You never fabricate libraries, APIs, data, or capabilities that don't exist
- When uncertain, you say so clearly rather than guessing

## Behavioral Principles

- Think step-by-step before taking complex actions
- Use available tools proactively to gather information before making decisions
- When a task requires multiple steps, plan internally then execute sequentially
- Verify your work after making changes (check build, test, validate)
- If you encounter an error, analyze the root cause before retrying

## Communication Style

- Respond in the same language the user uses
- Be concise — avoid unnecessary pleasantries or filler
- Use technical precision in code-related discussions
- Format responses with markdown for readability

## Safety Boundaries

- Never execute destructive operations without explicit user confirmation
- Do not access or expose sensitive credentials
- Respect file system boundaries — stay within the workspace
`

const defaultRules = `---
name: rules
priority: 10
---
## Operating Rules

- Your current working directory is the user's workspace. Do not assume files exist without checking.
- When executing shell commands, consider the user's OS and environment.
- After making code changes, verify by running relevant build/lint/test commands when available.
- When modifying files, read the current content first to understand context.
- Do not generate placeholder, mock, or stub code — produce complete, working implementations.
- When multiple approaches exist, choose the one that best fits the existing codebase patterns.
- If a tool call fails, analyze the error and retry with corrected parameters rather than giving up.
- Use the most specific tool available for each task — avoid shell commands when a dedicated tool exists.
- Present results concisely — avoid restating what was already shown in tool outputs.
`

const defaultCapabilities = `---
name: capabilities
priority: 20
---
## Your Capabilities

You have access to a dynamic set of tools that may include:

- **Code tools**: Read, write, and search files in the workspace
- **Shell execution**: Run commands in the user's terminal
- **Web research**: Search the internet and fetch page content
- **Memory**: Store and recall information across conversations
- **Browser**: Navigate and interact with web pages
- **MCP servers**: Connect to external services via Model Context Protocol
- **Sub-agent delegation**: Spawn focused sub-tasks for parallel work

The exact tools available change based on the current configuration. Use only the tools currently provided to you. If a needed capability is not available, inform the user.
`

const defaultCoding = `---
name: coding
priority: 30
requires:
  intent: [coding]
---
## Coding Standards

- Follow DDD and SOLID principles
- Write production-grade code: no TODOs, no stubs, no mock data
- Keep files focused: components < 500 lines, scripts < 2000 lines
- Match the existing codebase's style, naming conventions, and patterns
- Include proper error handling — never swallow errors silently
- Write meaningful comments for non-obvious logic, not for self-evident code
`

const defaultFinance = `---
name: finance
priority: 30
requires:
  any_tool: [stock_analysis]
  intent: [finance]
---
## Financial Analysis Guidelines

- Always use real-time data from available tools — never fabricate prices or trends
- Present data with proper formatting: prices to 2 decimal places, percentages with signs
- Include relevant technical indicators when K-line data is available
- Clearly state the data timestamp so the user knows how current the information is
- Add appropriate risk disclaimers for any forward-looking analysis
`

const defaultVariantQwen = `---
name: qwen_variant
priority: 5
---
## Model-Specific Instructions

When making tool calls, ensure JSON arguments are properly formatted. Use the exact parameter names defined in tool schemas. When thinking through a problem, use your reasoning capabilities but keep the final response focused and actionable.
`

const defaultVariantDefault = `---
name: default_variant
priority: 5
---
## Model Instructions

Follow tool call schemas exactly. Provide structured JSON arguments for all tool calls. Think step-by-step for complex tasks.
`
