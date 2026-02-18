# NGOClaw User Manual

## Table of Contents

- [1. Installation](#1-installation)
- [2. Configuration](#2-configuration)
- [3. CLI Reference](#3-cli-reference)
- [4. Tools Reference](#4-tools-reference)
- [5. Skill System](#5-skill-system)
- [6. Prompt Customization](#6-prompt-customization)
- [7. MCP Integration](#7-mcp-integration)
- [8. Telegram Bot](#8-telegram-bot)
- [9. FAQ & Troubleshooting](#9-faq--troubleshooting)

---

## 1. Installation

### System Requirements

| Requirement | Version |
|-------------|---------|
| Go | 1.24+ |
| Python | 3.10+ (optional, for skills) |
| OS | Linux / macOS |

### Build from Source

```bash
git clone https://github.com/ngoclaw/ngoclaw.git
cd ngoclaw
make build          # Builds to gateway/bin/ngoclaw
make install        # Installs to /usr/local/bin/ngoclaw
```

### Verify Installation

```bash
ngoclaw --version
ngoclaw --help
```

### First Run

On first launch, NGOClaw creates `~/.ngoclaw/` with default configuration:

```bash
ngoclaw          # Starts interactive TUI
```

This auto-generates:
- `~/.ngoclaw/config.yaml` — main configuration
- `~/.ngoclaw/soul.md` — agent personality
- `~/.ngoclaw/prompts/` — prompt components
- `~/.ngoclaw/skills/` — custom skills directory
- `~/.ngoclaw/memory/` — long-term memory storage
- `~/.ngoclaw/mcp.json` — MCP server configuration

---

## 2. Configuration

### Config File Location

| Priority | Location | Description |
|----------|----------|-------------|
| 1 (lowest) | Built-in defaults | Code defaults |
| 2 | `~/.ngoclaw/config.yaml` | Global config |
| 3 | `./config.yaml` | Project-local override |
| 4 (highest) | `NGOCLAW_*` env vars | Environment overrides |

### Full Config Reference

```yaml
# ~/.ngoclaw/config.yaml

agent:
  # Default model (provider/model format)
  default_model: "openai/gpt-4o"

  # LLM Providers (priority-ordered failover)
  providers:
    - name: openai
      base_url: "https://api.openai.com/v1"
      api_key: "sk-..."
      models:
        - "openai/gpt-4o"
        - "openai/gpt-4o-mini"
      priority: 1              # Lower = higher priority

    - name: anthropic
      base_url: "https://api.anthropic.com/v1"
      api_key: "sk-ant-..."
      api_type: "anthropic"    # Required for non-OpenAI protocols
      models:
        - "anthropic/claude-sonnet-4-20250514"
      priority: 2

  # Agent loop configuration
  loop:
    context_max_tokens: 128000   # Context window limit
    context_warn_ratio: 0.7      # Warn when context > 70%
    context_hard_ratio: 0.85     # Force compaction > 85%
    loop_detect_window: 10       # Sliding window for loop detection
    loop_detect_threshold: 5     # Identical calls to trigger reflection
    loop_name_threshold: 8       # Same tool name consecutive calls limit

# Telegram Bot
telegram:
  bot_token: "YOUR_BOT_TOKEN"
  allow_ids: [123456789]         # Allowed Telegram user IDs
  mode: polling                  # polling or webhook

# HTTP Server
server:
  port: 18789
  host: "0.0.0.0"

# Tool settings
tools:
  bash:
    timeout: 60                  # Command timeout in seconds
    sandbox: true                # Enable process isolation
  web_search:
    searxng_url: "http://localhost:8080"  # SearXNG instance URL
```

### Environment Variable Overrides

Any config field can be overridden via environment variables with `NGOCLAW_` prefix:

```bash
export NGOCLAW_AGENT_DEFAULT_MODEL="anthropic/claude-sonnet-4-20250514"
export NGOCLAW_TELEGRAM_BOT_TOKEN="your-token"
export NGOCLAW_SERVER_PORT=8080
```

---

## 3. CLI Reference

### Commands

```bash
ngoclaw                    # Interactive TUI mode
ngoclaw serve              # Start background service (HTTP + Telegram + gRPC)
ngoclaw repl               # Simple REPL mode (no TUI)
ngoclaw version            # Show version
ngoclaw help               # Show help
```

### TUI Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `Enter` | Send message |
| `Shift+Enter` | New line |
| `Ctrl+C` | Exit |
| `/new` | Start new conversation |
| `/model <name>` | Switch model |
| `/status` | Show current status |

---

## 4. Tools Reference

### File Operations

#### `read_file`
Read file contents. Supports text files.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `path` | string | ✅ | Absolute or relative file path |
| `line_start` | int | ❌ | Start line (1-indexed) |
| `line_end` | int | ❌ | End line (1-indexed) |

#### `write_file`
Create or overwrite a file.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `path` | string | ✅ | Target file path |
| `content` | string | ✅ | File content |

#### `edit_file`
Make targeted edits via search-and-replace.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `path` | string | ✅ | File path |
| `old_text` | string | ✅ | Exact text to find |
| `new_text` | string | ✅ | Replacement text |

#### `list_dir`
List directory contents with sizes and types.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `path` | string | ✅ | Directory path |

#### `glob`
Find files matching a glob pattern.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `pattern` | string | ✅ | Glob pattern (e.g., `*.go`, `src/**/*.ts`) |
| `path` | string | ❌ | Root directory (default: workspace) |

#### `apply_patch`
Apply a unified diff patch to one or more files.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `patch` | string | ✅ | Unified diff content |

### Shell & System

#### `bash`
Execute shell commands in a sandboxed environment.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `command` | string | ✅ | Shell command to execute |
| `work_dir` | string | ❌ | Working directory |

> **Constraints**: 60s timeout. Exit code 124 = TIMEOUT. Avoid interactive commands.

#### `grep_search`
Search file contents with regex patterns.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `pattern` | string | ✅ | Search pattern (regex) |
| `path` | string | ❌ | Search root (default: workspace) |
| `include` | string | ❌ | File glob filter (e.g., `*.go`) |

#### `git`
Safe git operations: status, diff, log, commit, show.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `action` | string | ✅ | One of: status, diff, log, commit, show |
| `args` | string | ❌ | Additional git arguments |
| `message` | string | ❌ | Commit message (for commit action) |

### Code Intelligence

#### `lsp`
Language Server Protocol tool for code intelligence.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `action` | string | ✅ | definition, references, hover, diagnostics, symbols, completion |
| `file` | string | ✅ | File path |
| `line` | int | ❌ | Line number (1-indexed) |
| `col` | int | ❌ | Column number |

#### `lint_fix`
Run code quality checks (lint, test, build).

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `action` | string | ✅ | lint, test, or build |
| `path` | string | ❌ | Project path |

#### `repo_map`
Generate a structural map of the codebase.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `path` | string | ✅ | Directory to map |
| `depth` | int | ❌ | Max depth (default: 3) |

### Web & Network

#### `web_search`
Search the web via SearXNG with full-text extraction.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `query` | string | ✅ | Search query |
| `num_results` | int | ❌ | Max results (default: 5) |
| `time_range` | string | ❌ | day, week, month, year |
| `deep` | bool | ❌ | Extract full article content |

#### `web_fetch`
Fetch and extract readable content from a URL.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `url` | string | ✅ | URL to fetch |

### Browser

#### `browser_navigate`
Navigate browser to a URL.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `url` | string | ✅ | Target URL |

#### `browser_screenshot`
Take a screenshot of the current page.

No parameters required.

#### `browser_click`
Click an element by CSS selector.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `selector` | string | ✅ | CSS selector |

#### `browser_type`
Type text into an element.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `selector` | string | ✅ | CSS selector |
| `text` | string | ✅ | Text to type |

### Telegram Media

#### `send_photo`
Send a photo to the current Telegram chat.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `photo` | string | ✅ | Local file path or HTTP(S) URL |
| `caption` | string | ❌ | Photo caption |

#### `send_document`
Send a document/file to the current Telegram chat.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `document` | string | ✅ | Local file path |
| `caption` | string | ❌ | Document caption |

### Agent & Memory

#### `save_memory`
Save a fact to long-term memory.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `fact` | string | ✅ | The fact to remember |
| `category` | string | ❌ | Category (preference, environment, decision, etc.) |

#### `update_plan`
Create or update execution plans.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `action` | string | ✅ | create or update |
| `steps` | array | ❌ | Plan steps (for create) |
| `step_id` | int | ❌ | Step to update |
| `status` | string | ❌ | New status |

#### `spawn_agent`
Delegate a sub-task to an independent agent.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `task` | string | ✅ | Sub-task description |
| `context` | string | ❌ | Additional context |

#### `mcp_manage`
Manage MCP servers.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `action` | string | ✅ | add, remove, list, refresh |
| `name` | string | ❌ | Server name |
| `command` | string | ❌ | Server command (for add) |
| `args` | array | ❌ | Command arguments (for add) |

---

## 5. Skill System

### Creating a Skill

1. Create a directory under `~/.ngoclaw/skills/`:

```bash
mkdir -p ~/.ngoclaw/skills/my-skill
```

2. Create `SKILL.md` with YAML frontmatter:

```markdown
---
name: my-skill
description: Description of what this skill does
tools:
  - name: my_tool
    description: What this tool does
    parameters:
      input:
        type: string
        description: Input parameter
        required: true
      output_format:
        type: string
        description: Output format
        enum: [json, text, markdown]
    command: "python3 ~/.ngoclaw/skills/my-skill/run.py {{input}}"
---

# My Skill

Detailed usage instructions...
```

3. NGOClaw auto-discovers skills on startup.

### Skill Discovery

The agent uses `find_skills` to scan available skills. Skills are matched by name and description against the user's request.

### Built-in Skills

| Skill | Description |
|-------|-------------|
| `web-research` | Deep web research with multi-source extraction |
| `stock-trader-insight` | Stock market analysis with Sina Finance data |
| `image-gen-local` | Image generation via local API |

---

## 6. Prompt Customization

### Three-Layer Architecture

```
~/.ngoclaw/                     # System layer (always loaded)
├── soul.md                     # Agent personality (always first)
├── prompts/
│   ├── rules.md                # Behavioral rules
│   ├── capabilities.md         # Capability descriptions
│   ├── coding.md               # Coding guidelines
│   └── variants/
│       └── claude.md           # Model-specific overrides
├── cli/                        # CLI channel overrides
│   ├── soul.md                 # CLI-specific personality
│   └── prompts/
└── telegram/                   # Telegram channel overrides
    ├── soul.md
    └── prompts/
```

### soul.md

The primary personality definition. Always loaded first for maximum attention weight.

```markdown
You are NGOClaw, an autonomous AI assistant.

## Personality
- Direct and efficient
- Action-oriented: do first, explain briefly after

## Communication
- Use Chinese for user-facing output
- Code comments may be in English
```

### Prompt Components

Files in `prompts/` are loaded based on priority (extracted from YAML frontmatter):

```markdown
---
priority: 10
requires:
  tools: [bash, web_search]
  intent: [coding, research]
---

## Coding Guidelines

When writing code, follow these principles...
```

### Channel Overrides

Place channel-specific files in `~/.ngoclaw/<channel>/` to override shared components. Same-name files in the channel directory replace shared ones.

### Model Variants

Files in `prompts/variants/` are matched by model name prefix:

- `claude.md` → matches all Claude models
- `gpt4.md` → matches GPT-4 models
- `qwen.md` → matches Qwen models

---

## 7. MCP Integration

### What is MCP?

Model Context Protocol allows external tools to be discovered and invoked by the agent.

### Configuration

Edit `~/.ngoclaw/mcp.json`:

```json
{
  "servers": [
    {
      "name": "filesystem",
      "command": "npx",
      "args": ["-y", "@anthropic/mcp-filesystem", "/home/user/projects"]
    },
    {
      "name": "github",
      "command": "npx",
      "args": ["-y", "@anthropic/mcp-github"],
      "env": {
        "GITHUB_TOKEN": "ghp_..."
      }
    }
  ]
}
```

### Managing MCP Servers

Use the `mcp_manage` tool or chat commands:

```
Add a filesystem MCP server for /home/user/projects
List all MCP servers
Refresh tools from the github server
```

### How It Works

1. NGOClaw starts configured MCP servers as child processes
2. Tool schemas are discovered via the MCP protocol
3. MCP tools appear alongside built-in tools in the agent's toolbox
4. Tool calls are proxied to the corresponding MCP server

---

## 8. Telegram Bot

### Setup

1. Create a bot via [@BotFather](https://t.me/BotFather)
2. Get the bot token
3. Get your Telegram user ID (send `/start` to [@userinfobot](https://t.me/userinfobot))
4. Configure `~/.ngoclaw/config.yaml`:

```yaml
telegram:
  bot_token: "8301149736:AAF..."
  allow_ids: [YOUR_USER_ID]
  mode: polling
```

5. Start the service:

```bash
ngoclaw serve
```

### Commands

| Command | Description |
|---------|-------------|
| `/new` | Start new conversation |
| `/model <name>` | Switch model |
| `/status` | Show current status |
| `/help` | Show available commands |

### Media Support

The bot can send photos and documents:
- Agent uses `send_photo` to send images (file path or URL)
- Agent uses `send_document` to send files
- Users can send images for analysis (if image model is configured)

---

## 9. FAQ & Troubleshooting

### Common Issues

**Q: Bot doesn't respond in Telegram**
1. Check `ngoclaw serve` is running: `pgrep -f 'ngoclaw serve'`
2. Verify bot token: `curl https://api.telegram.org/bot<TOKEN>/getMe`
3. Check your user ID is in `allow_ids`
4. Review logs: `tail -f /tmp/ngoclaw.log`

**Q: Tool calls fail with timeout**
- Default bash timeout is 60s. For long commands, consider breaking into smaller steps.
- For network commands, use `timeout 10` wrapper.

**Q: Context window exceeded**
- NGOClaw auto-compresses at 85% usage
- Use `/new` to start fresh
- Increase `context_max_tokens` in config if your model supports it

**Q: Model returns errors**
1. Verify API key and base URL
2. Check model name format: `provider/model-name`
3. Try a different provider (failover is automatic by priority)

**Q: MCP server fails to start**
1. Ensure `npx` or the server binary is in PATH
2. Check `~/.ngoclaw/mcp.json` syntax
3. Run the server command manually to debug

### Logs

```bash
# Service logs
tail -f /tmp/ngoclaw.log

# Filter by level
grep '"level":"error"' /tmp/ngoclaw.log
```

### Getting Help

- [GitHub Issues](https://github.com/ngoclaw/ngoclaw/issues)
- [Architecture Documentation](ARCHITECTURE.md)
