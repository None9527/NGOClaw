<h1 align="center">🐾 NGOClaw</h1>

<p align="center">
  <strong>Autonomous AI Agent Framework</strong> — Pure Go, batteries included<br>
  <strong>自主 AI Agent 框架</strong> — 纯 Go 实现，开箱即用
</p>

<p align="center">
  <a href="#-quick-start--快速开始">Quick Start</a> •
  <a href="#-features--功能亮点">Features</a> •
  <a href="#-architecture--架构">Architecture</a> •
  <a href="#-configuration--配置">Configuration</a> •
  <a href="docs/USER_MANUAL.md">User Manual</a> •
  <a href="sdk/">SDK</a>
</p>

<p align="center">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="MIT License"></a>
  <img src="https://img.shields.io/badge/go-1.24+-00ADD8?logo=go" alt="Go 1.24+">
  <img src="https://img.shields.io/badge/DDD-Clean_Architecture-green" alt="DDD">
</p>

---

> **NGOClaw** is a self-hosted, autonomous AI agent running a full **ReAct loop** (Reason → Act → Observe) with 25 built-in tools, MCP protocol support, hot-pluggable skills/prompts, and multi-channel interfaces.
>
> **NGOClaw** 是一个自托管的自主 AI Agent，运行完整的 **ReAct 循环**（推理→行动→观察），内置 25 个工具，支持 MCP 协议、热插拔技能/提示词系统和多通道接口。

---

## ✨ Features / 功能亮点

| Feature / 功能 | Description / 说明 |
|----------------|-------------------|
| 🤖 **ReAct Agent Loop** | Reason → Act → Observe with automatic multi-step planning / 推理→行动→观察，自动规划多步任务 |
| 🔧 **25 Built-in Tools** | File I/O, shell, web search, code intelligence, browser, media, git, LSP / 文件读写、Shell、搜索、代码智能、浏览器、媒体发送 |
| 🧩 **MCP Protocol** | One-click integration with Model Context Protocol / 一键接入 MCP 外部服务 |
| 📦 **Hot-Pluggable Skills** | Drop `SKILL.md` into `~/.ngoclaw/skills/` — auto-discovered / 放入即可自动发现 |
| 💬 **Multi-Interface** | CLI (TUI) · Telegram Bot · HTTP API · gRPC · WebSocket · REPL |
| 🔄 **Multi-Provider** | OpenAI / Anthropic / Gemini / Bailian / MiniMax — priority routing / 优先级路由、自动容灾 |
| 🧠 **Context Compression** | XML summarization + memory extraction + Daily Log / XML 摘要 + 记忆提取 |
| 🛡️ **Tool Sandbox** | Process-level isolation with configurable policies / 进程级隔离，可配工具策略 |
| ⚡ **Hot Config** | `config.yaml` + MCP JSON + Prompts all support hot-reload / 均支持热重载 |
| 📊 **Observability** | EventBus + Monitoring + structured logging (Zap) / 结构化日志 |

## 🚀 Quick Start / 快速开始

### Prerequisites / 前置依赖

- **Go 1.24+**
- **Python 3.10+** + Conda/venv（only for Stock/Web skills / 仅技能需要，核心无 Python 依赖）
- At least one LLM Provider API key / 至少一个 LLM Provider API Key

### Build from Source / 源码编译

```bash
git clone https://github.com/ngoclaw/ngoclaw.git
cd ngoclaw
make build          # → gateway/bin/ngoclaw
make install        # → /usr/local/bin/ngoclaw (optional / 可选)
```

### First Run / 首次运行

NGOClaw auto-creates config at `~/.ngoclaw/`:
NGOClaw 会自动在 `~/.ngoclaw/` 创建默认配置：

```
~/.ngoclaw/
├── config.yaml          # Main config / 主配置
├── soul.md              # Agent personality / Agent 人格
├── prompts/             # Hot-pluggable prompt components / 提示词组件
│   ├── rules.md
│   ├── capabilities.md
│   ├── coding.md
│   └── variants/        # Model-specific variants / 模型变体
├── skills/              # Custom skills / 自定义技能
├── memory/              # Long-term memory / 长期记忆
└── mcp.json             # MCP server config / MCP 配置
```

Edit config with your LLM provider / 编辑配置填入 LLM Provider 信息：

```yaml
agent:
  default_model: "your-provider/your-model"
  providers:
    - name: your-provider
      base_url: "https://api.example.com/v1"
      api_key: "your-api-key"
      models:
        - "your-provider/model-name"
      priority: 1
```

Launch / 启动：

```bash
ngoclaw          # Interactive CLI (TUI) / 交互式终端
ngoclaw serve    # Background service / 后台服务 (HTTP + Telegram + gRPC)
```

## 🏗️ Architecture / 架构

```
┌─────────────────────────────────────────────────────────────┐
│                    Interfaces Layer / 接口层                  │
│  CLI/TUI │ Telegram Bot │ HTTP API │ gRPC │ WebSocket │ REPL│
├─────────────────────────────────────────────────────────────┤
│                   Application Layer / 应用层                 │
│              ProcessMessageUseCase · AgentLoop               │
├─────────────────────────────────────────────────────────────┤
│                     Domain Layer / 领域层                     │
│  Entity │ ValueObject │ Service │ Tool │ Agent │ Memory      │
├─────────────────────────────────────────────────────────────┤
│                 Infrastructure Layer / 基础设施层             │
│  LLM Router│Tool Registry│Prompt Engine│Sandbox│EventBus    │
│  Persistence│Config│Monitoring│Plugin│VectorStore│Embedding  │
└─────────────────────────────────────────────────────────────┘
```

**DDD Layered** — Domain logic has zero external dependencies; infrastructure is swappable.
**DDD 分层** — 领域逻辑零外部依赖；基础设施可替换；接口层随意扩展。

### Core Flow / 核心流程

```
User Input → Interface Routing → AgentLoop (ReAct)
用户输入       接口层路由              ↓
                           LLM Router (multi-provider failover / 多 Provider 容灾)
                                      ↓
                           Reason → Tool Call → Observe → Loop
                           推理       工具调用     观察     循环
                                      ↓
                           Context Compression (automatic / 自动)
                                      ↓
                           Response Output / 响应输出
```

## ⚙️ Configuration / 配置

Config priority (low → high) / 配置优先级（低→高）：

1. **Built-in defaults / 内置默认值**
2. **`~/.ngoclaw/config.yaml`** — Global / 全局配置
3. **`./config.yaml`** — Project-local override / 项目本地覆盖
4. **`NGOCLAW_*` env vars / 环境变量**

### LLM Providers

```yaml
agent:
  providers:
    - name: openai
      base_url: "https://api.openai.com/v1"
      api_key: "sk-..."
      models: ["openai/gpt-4o"]
      priority: 1

    - name: anthropic
      base_url: "https://api.anthropic.com/v1"
      api_key: "sk-ant-..."
      models: ["anthropic/claude-sonnet-4-20250514"]
      priority: 2
```

### Telegram Bot

```yaml
telegram:
  bot_token: "YOUR_BOT_TOKEN"
  allow_ids: [YOUR_TELEGRAM_USER_ID]
  mode: polling
```

### MCP Servers

Edit `~/.ngoclaw/mcp.json`:

```json
{
  "servers": [
    {
      "name": "filesystem",
      "command": "npx",
      "args": ["-y", "@anthropic/mcp-filesystem", "/path/to/dir"]
    }
  ]
}
```

## 🔧 Built-in Tools / 内置工具

| Tool | Kind | Description / 说明 |
|------|------|--------------------|
| `bash` | execute | Execute shell commands / Shell 命令执行 |
| `read_file` | read | Read file contents / 读取文件 |
| `write_file` | execute | Create or overwrite files / 写入文件 |
| `edit_file` | execute | Precise edits via search-and-replace / 精准编辑 |
| `list_dir` | read | List directory contents / 列目录 |
| `grep_search` | read | Regex search in files / 正则搜索 |
| `glob` | read | Find files by glob pattern / 按模式查找文件 |
| `apply_patch` | execute | Apply unified diff patches / 应用补丁 |
| `web_search` | fetch | Web search via SearXNG / 互联网搜索 |
| `web_fetch` | fetch | Fetch URL content / 抓取网页 |
| `git` | execute | Safe git ops (status/diff/log/commit/show) / 安全 Git 操作 |
| `lint_fix` | execute | Code quality checks (lint/test/build) / 代码质量检查 |
| `lsp` | read | LSP (definition/references/hover/diagnostics) / 语言服务 |
| `repo_map` | read | Generate codebase structure map / 代码地图 |
| `save_memory` | think | Save facts to long-term memory / 长期记忆 |
| `update_plan` | think | Create/update execution plans / 任务计划 |
| `spawn_agent` | execute | Delegate to independent sub-agent / 子 Agent 委派 |
| `send_photo` | execute | Send photo via Telegram / 发送图片 |
| `send_document` | execute | Send document via Telegram / 发送文件 |
| `mcp_manage` | execute | Manage MCP servers / 管理 MCP 服务器 |
| `stock_analysis` | fetch | Stock data & technical analysis / 股票分析 |
| `browser_navigate` | fetch | Navigate browser to URL / 浏览器导航 |
| `browser_screenshot` | read | Take page screenshot / 网页截图 |
| `browser_click` | execute | Click element by CSS selector / 点击元素 |
| `browser_type` | execute | Type text into element / 输入文本 |

## 📦 Skill System / 技能系统

Create a directory in `~/.ngoclaw/skills/` with a `SKILL.md`:
在 `~/.ngoclaw/skills/` 中创建目录并放入 `SKILL.md`：

```markdown
---
name: my-skill
description: My custom skill
tools:
  - name: my_tool
    description: Does something
    parameters:
      input:
        type: string
        description: Input content
    command: "python3 ~/.ngoclaw/skills/my-skill/run.py {{input}}"
---

# My Skill
Usage instructions here...
```

Auto-discovered on startup. / 启动时自动发现并注册。

## 📁 Project Structure / 项目结构

```
ngoclaw/
├── gateway/                    # Go main program / Go 主程序
│   ├── cmd/cli/               # Entry point / 入口
│   ├── internal/
│   │   ├── domain/            # Domain layer / 领域层
│   │   ├── application/       # Application layer / 应用层
│   │   ├── infrastructure/    # Infrastructure / 基础设施层
│   │   └── interfaces/        # Interface layer / 接口层
│   └── go.mod
├── sdk/                        # Client SDKs (Go, Python)
├── shared/                     # Shared protobuf definitions
├── docs/                       # Documentation / 文档
├── Makefile
└── LICENSE
```

## 🛠️ Tech Stack / 技术栈

| Component / 组件 | Technology / 技术 |
|------------------|-------------------|
| Language / 语言 | Go 1.24 |
| HTTP | Gin |
| Telegram | telegram-bot-api/v5 |
| TUI | Bubble Tea + Lip Gloss + Glamour |
| Database / 数据库 | SQLite / PostgreSQL (GORM) |
| Logging / 日志 | Zap |
| Config / 配置 | Viper |
| Vector Store / 向量存储 | LanceDB |
| gRPC | google.golang.org/grpc |

## 📄 License / 许可证

[MIT License](LICENSE)

## 🤝 Contributing / 贡献

Contributions welcome! / 欢迎贡献！

- **Architecture / 架构**: Respect DDD layering / 遵循 DDD 分层
- **Code Style / 代码风格**: SOLID principles, components < 500 lines / SOLID 原则
- **Testing / 测试**: Add tests for new tools and services / 新功能请附测试
- **Commits / 提交**: Use conventional commit messages
- **Issues**: Bug reports and feature requests via GitHub Issues
- **PRs**: Fork → branch → implement → test → PR
