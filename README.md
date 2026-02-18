<h1 align="center">🐾 NGOClaw</h1>

<p align="center">
  <strong>Autonomous AI Agent Framework</strong> — Pure Go, batteries included
</p>

<p align="center">
  <a href="#-quick-start">Quick Start</a> •
  <a href="#-features">Features</a> •
  <a href="#-architecture">Architecture</a> •
  <a href="#-configuration">Configuration</a> •
  <a href="docs/USER_MANUAL.md">User Manual</a>
</p>

<p align="center">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="MIT License"></a>
  <img src="https://img.shields.io/badge/go-1.24+-00ADD8?logo=go" alt="Go 1.24+">
  <img src="https://img.shields.io/badge/DDD-Clean_Architecture-green" alt="DDD">
</p>

---

> **NGOClaw** is a self-hosted, autonomous AI agent that runs a full **ReAct loop** (Reason → Act → Observe) with 25 built-in tools, MCP protocol support, a hot-pluggable skill/prompt system, and multi-channel interfaces (CLI/TUI, Telegram, HTTP API, gRPC, WebSocket, REPL). Written entirely in Go with DDD architecture.

---

## ✨ Features

| Feature | Description |
|---------|-------------|
| 🤖 **ReAct Agent Loop** | Reason → Act → Observe cycle with automatic multi-step planning |
| 🔧 **25 Built-in Tools** | File I/O, shell, web search, code intelligence, browser, media send, git, LSP... |
| 🧩 **MCP Protocol** | One-click integration with Model Context Protocol external services |
| 📦 **Hot-Pluggable Skills** | Drop a `SKILL.md` into `~/.ngoclaw/skills/` — auto-discovered |
| 💬 **Multi-Interface** | CLI (TUI) · Telegram Bot · HTTP API · gRPC · WebSocket · REPL |
| 🔄 **Multi-Provider** | OpenAI / Anthropic / Gemini / Bailian / MiniMax — protocol-compatible, priority routing |
| 🧠 **Context Compression** | XML-structured summarization + automatic memory extraction + Daily Log |
| 🛡️ **Tool Sandbox** | Process-level isolation with configurable tool policies |
| ⚡ **Hot Configuration** | `config.yaml` + MCP JSON + Prompt files all support hot-reload |
| 📊 **Observability** | EventBus + Monitoring + structured logging (Zap) |

## 🚀 Quick Start

### Prerequisites

- **Go 1.24+**
- **Python 3.10+** + Conda/venv (only for Stock/Web skills — core has no Python dependency)
- At least one LLM Provider API key

### Option 1: Build from Source

```bash
git clone https://github.com/ngoclaw/ngoclaw.git
cd ngoclaw
make build          # → gateway/bin/ngoclaw
make install        # → /usr/local/bin/ngoclaw (optional)
```

### Option 2: Direct Build

```bash
cd gateway
go build -o bin/ngoclaw ./cmd/cli
./bin/ngoclaw
```

### First Run

NGOClaw auto-creates default config and prompts at `~/.ngoclaw/`:

```
~/.ngoclaw/
├── config.yaml          # Main config (API keys, providers, models)
├── soul.md              # Agent personality definition
├── prompts/             # Hot-pluggable prompt components
│   ├── rules.md
│   ├── capabilities.md
│   ├── coding.md
│   └── variants/        # Model-specific variants
├── skills/              # Custom skills directory
├── memory/              # Long-term memory storage
└── mcp.json             # MCP server configuration
```

Edit `~/.ngoclaw/config.yaml` with your LLM provider info:

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

Then launch:

```bash
ngoclaw          # Interactive CLI (TUI)
ngoclaw serve    # Background service (HTTP + Telegram + gRPC)
```

## 🏗️ Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    Interfaces Layer                          │
│  CLI/TUI │ Telegram Bot │ HTTP API │ gRPC │ WebSocket │ REPL│
├─────────────────────────────────────────────────────────────┤
│                    Application Layer                        │
│              ProcessMessageUseCase · AgentLoop               │
├─────────────────────────────────────────────────────────────┤
│                      Domain Layer                           │
│  Entity │ ValueObject │ Service │ Tool │ Agent │ Memory      │
├─────────────────────────────────────────────────────────────┤
│                  Infrastructure Layer                       │
│  LLM Router│Tool Registry│Prompt Engine│Sandbox│EventBus    │
│  Persistence│Config│Monitoring│Plugin│VectorStore│Embedding  │
└─────────────────────────────────────────────────────────────┘
```

**DDD Layered** — Domain logic has zero external dependencies; infrastructure is swappable; interface layer is freely extensible.

### Core Flow

```
User Input → Interface Routing → AgentLoop (ReAct)
                                      ↓
                           LLM Router (multi-provider failover)
                                      ↓
                           Reason → Tool Call → Observe → Loop
                                      ↓
                           Context Compression (automatic)
                                      ↓
                           Response Output
```

## ⚙️ Configuration

Config loads in priority order (low → high):

1. **Built-in defaults** — code defaults
2. **`~/.ngoclaw/config.yaml`** — global config (auto-created on first run)
3. **`./config.yaml`** — project-local override (optional)
4. **Environment variables** — `NGOCLAW_` prefixed overrides

### LLM Providers

NGOClaw supports any OpenAI-protocol compatible API:

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

## 🔧 Built-in Tools

| Tool | Kind | Description |
|------|------|-------------|
| `bash` | execute | Execute shell commands in sandboxed environment |
| `read_file` | read | Read file contents |
| `write_file` | execute | Create or overwrite files |
| `edit_file` | execute | Precise file edits via search-and-replace |
| `list_dir` | read | List directory contents |
| `grep_search` | read | Search file contents with regex |
| `glob` | read | Find files by glob pattern |
| `apply_patch` | execute | Apply unified diff patches |
| `web_search` | fetch | Web search via SearXNG with full-text extraction |
| `web_fetch` | fetch | Fetch and extract content from a URL |
| `git` | execute | Safe git operations (status/diff/log/commit/show) |
| `lint_fix` | execute | Run code quality checks (lint/test/build) |
| `lsp` | read | Language Server Protocol (definition/references/hover/diagnostics) |
| `repo_map` | read | Generate structural codebase map |
| `save_memory` | think | Save facts to long-term memory |
| `update_plan` | think | Create/update execution plans |
| `spawn_agent` | execute | Delegate sub-tasks to independent agents |
| `send_photo` | execute | Send photo via Telegram (file path or URL) |
| `send_document` | execute | Send document/file via Telegram |
| `mcp_manage` | execute | Manage MCP servers (add/remove/list/refresh) |
| `stock_analysis` | fetch | Stock market data and technical analysis |
| `browser_navigate` | fetch | Navigate browser to URL |
| `browser_screenshot` | read | Take page screenshot |
| `browser_click` | execute | Click page element by CSS selector |
| `browser_type` | execute | Type text into page element |

## 📦 Skill System

Create a directory in `~/.ngoclaw/skills/` with a `SKILL.md`:

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

This skill is used for...
```

NGOClaw auto-discovers and registers skills on startup.

## 📁 Project Structure

```
ngoclaw/
├── gateway/                    # Go main program
│   ├── cmd/cli/               # Entry point
│   ├── internal/
│   │   ├── domain/            # Domain layer (entities, value objects, services, tool interfaces)
│   │   ├── application/       # Application layer (use cases)
│   │   ├── infrastructure/    # Infrastructure (LLM, tools, config, persistence, sandbox)
│   │   └── interfaces/        # Interface layer (CLI, Telegram, HTTP, gRPC, WS, REPL)
│   └── go.mod
├── sdk/                        # Client SDKs (Go, Python)
├── shared/                     # Shared protobuf definitions
├── docs/                       # Documentation
├── Makefile
└── LICENSE
```

## 🛠️ Tech Stack

| Component | Technology |
|-----------|-----------|
| Language | Go 1.24 |
| HTTP | Gin |
| Telegram | telegram-bot-api/v5 |
| TUI | Bubble Tea + Lip Gloss + Glamour |
| Database | SQLite / PostgreSQL (GORM) |
| Logging | Zap |
| Configuration | Viper |
| Vector Store | LanceDB |
| gRPC | google.golang.org/grpc |

## 📄 License

[MIT License](LICENSE)

## 🤝 Contributing

Contributions are welcome! Please follow these guidelines:

- **Architecture**: Respect DDD layering — domain logic must have zero external dependencies
- **Code Style**: Follow SOLID principles, keep components < 500 lines
- **Testing**: Add tests for new tools and services
- **Commits**: Use conventional commit messages
- **Issues**: Bug reports and feature requests via GitHub Issues
- **PRs**: Fork → branch → implement → test → PR

See the [User Manual](docs/USER_MANUAL.md) for detailed documentation.
