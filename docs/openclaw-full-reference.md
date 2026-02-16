# OpenClaw 完整功能参考文档

> 源码深度分析 | NGOClaw 复现指南
> 版本: 2026.2.4 | 代码库: `/home/none/ngoclaw/openclaw`

---

## 目录

1. [项目概览](#1-项目概览)
2. [核心模块](#2-核心模块)
3. [Agent 系统](#3-agent-系统)
4. [网关服务](#4-网关服务)
5. [配置系统](#5-配置系统)
6. [消息频道](#6-消息频道)
7. [工具系统](#7-工具系统)
8. [浏览器自动化](#8-浏览器自动化)
9. [插件系统](#9-插件系统)
10. [其他模块](#10-其他模块)
11. [NGOClaw 实现路线图](#11-ngoclaw-实现路线图)

---

## 1. 项目概览

### 1.1 技术栈

| 组件 | 技术 |
|-----|-----|
| 语言 | TypeScript (ESM) |
| 运行时 | Node.js >= 22.12.0 |
| 包管理 | pnpm 10.23.0 |
| 构建 | tsdown + rolldown |
| 测试 | Vitest |
| Lint | oxlint |

### 1.2 项目规模

```
src/
├── 50 个顶级模块目录
├── ~2550 个文件
└── 估计 ~200,000+ 行代码
```

### 1.3 核心模块统计

| 模块 | 文件数 | 功能 |
|-----|:---:|-----|
| agents | 454 | AI Agent 核心 |
| auto-reply | 209 | 自动回复/命令 |
| gateway | 193 | 网关服务 |
| cli | 171 | 命令行工具 |
| config | 133 | 配置管理 |
| channels | 101 | 频道抽象 |
| telegram | 88 | Telegram 集成 |
| browser | 81 | 浏览器自动化 |
| discord | 67 | Discord 集成 |
| slack | 65 | Slack 集成 |
| tui | 39 | 终端 UI |
| plugins | 37 | 插件系统 |
| cron | 35 | 定时任务 |
| hooks | 39 | 钩子/事件 |

### 1.4 主要依赖

```json
{
  "grammy": "^1.39.3",           // Telegram Bot
  "@slack/bolt": "^4.6.0",       // Slack
  "discord-api-types": "^0.38",  // Discord
  "@line/bot-sdk": "^10.6.0",    // Line
  "playwright-core": "1.58.1",   // 浏览器自动化
  "@mariozechner/pi-ai": "0.52.6", // Pi Agent SDK
  "hono": "4.11.7",              // HTTP 框架
  "ws": "^8.19.0",               // WebSocket
  "sqlite-vec": "0.1.7-alpha.2", // 向量搜索
  "sharp": "^0.34.5",            // 图像处理
}
```

---

## 2. 核心模块

### 2.1 入口点 (index.ts)

```typescript
// 主要导出
export {
  loadConfig,              // 配置加载
  getReplyFromConfig,      // 自动回复
  resolveSessionKey,       // 会话 Key
  loadSessionStore,        // 会话存储
  saveSessionStore,
  monitorWebChannel,       // Web 频道监控
  runCommandWithTimeout,   // 命令执行
  ensurePortAvailable,     // 端口检查
};

// 启动流程
loadDotEnv();
normalizeEnv();
ensureOpenClawCliOnPath();
enableConsoleCapture();
assertSupportedRuntime();
const program = buildProgram();
program.parseAsync(process.argv);
```

### 2.2 运行时环境 (runtime.ts)

```typescript
export type RuntimeEnv = {
  log?: (message: string) => void;
  warn?: (message: string) => void;
  error?: (message: string) => void;
  debug?: (message: string) => void;
  emit?: (event: string, payload: unknown) => void;
};
```

---

## 3. Agent 系统

### 3.1 目录结构

```
agents/
├── pi-embedded-runner/     # 嵌入式 Agent 运行器
│   ├── run.ts             # runEmbeddedPiAgent 主函数
│   ├── compact.ts         # 上下文压缩
│   ├── history.ts         # 历史管理
│   ├── lanes.ts           # 执行通道
│   └── types.ts           # 类型定义
├── pi-tools.ts            # 工具定义和注册
├── pi-embedded-helpers/   # 辅助函数
├── skills/                # 技能系统
├── sandbox/               # 沙箱执行
├── bash-tools.exec.ts     # Bash 执行 (54KB!)
├── system-prompt.ts       # 系统提示词 (26KB)
├── model-selection.ts     # 模型选择
├── model-fallback.ts      # 模型降级
├── subagent-*.ts          # 子代理系统
└── ...
```

### 3.2 核心功能

#### 3.2.1 模型选择 (model-selection.ts)

```typescript
// 模型引用解析
export function resolveModelRefFromString(params: {
  raw: string;
  defaultProvider: string;
  aliasIndex: Map<string, ModelRef>;
}): ModelRefResolution | null;

// 构建别名索引
export function buildModelAliasIndex(params: {
  cfg: OpenClawConfig;
  defaultProvider: string;
}): Map<string, ModelRef>;

// 构建允许的模型集合
export function buildAllowedModelSet(params: {
  cfg: OpenClawConfig;
  catalog: ModelCatalog;
  defaultProvider: string;
  defaultModel: string;
}): AllowedModelSet;
```

#### 3.2.2 模型降级 (model-fallback.ts)

```typescript
export type FallbackResult = {
  model: string;
  provider: string;
  viaFallback: boolean;
  fallbackReason?: string;
};

export function resolveFallbackModel(params: {
  cfg: OpenClawConfig;
  provider: string;
  model: string;
  error: Error;
}): FallbackResult | null;
```

#### 3.2.3 系统提示词 (system-prompt.ts)

```typescript
// 26KB 的系统提示词构建逻辑
export function buildSystemPrompt(params: {
  cfg: OpenClawConfig;
  agentId: string;
  workspace: string;
  skills: SkillEntry[];
  tools: ToolDefinition[];
  identity: AgentIdentity;
  bootstrap: BootstrapFiles;
  memory: MemoryContext;
}): string;
```

#### 3.2.4 Bash 执行 (bash-tools.exec.ts)

```typescript
// 54KB - 最大的单文件！
export async function executeBashCommand(params: {
  command: string;
  cwd: string;
  timeout: number;
  approval?: ApprovalContext;
  sandbox?: SandboxConfig;
  pty?: boolean;
}): Promise<BashResult>;

// 功能:
// - PTY 伪终端支持
// - 后台执行
// - 审批流程
// - 沙箱隔离
// - 进程注册
```

### 3.3 技能系统 (skills/)

```typescript
// skills.ts
export async function buildWorkspaceSkillsPrompt(params: {
  cfg: OpenClawConfig;
  agentId: string;
  workspace: string;
}): Promise<SkillsPrompt>;

// skills-install.ts (15KB)
export async function installSkill(params: {
  source: string;      // npm, git, local
  target: string;      // 安装目录
  options: InstallOptions;
}): Promise<SkillInstallResult>;

// skills-status.ts
export function buildWorkspaceSkillStatus(params: {
  cfg: OpenClawConfig;
  workspace: string;
}): SkillStatus[];
```

### 3.4 沙箱系统 (sandbox/)

```typescript
// sandbox.ts
export function resolveSandboxContext(params: {
  cfg: OpenClawConfig;
  agentId: string;
}): SandboxContext;

// 支持:
// - Docker 容器隔离
// - 文件系统挂载
// - 网络限制
// - 资源限制
```

### 3.5 子代理系统

```typescript
// subagent-registry.ts
export class SubagentRegistry {
  spawn(agentId: string, options: SpawnOptions): Promise<SubagentHandle>;
  terminate(handle: SubagentHandle): Promise<void>;
  list(): SubagentHandle[];
}

// subagent-announce.ts
export function announceToSubagent(params: {
  target: SubagentHandle;
  message: string;
  channel: string;
}): Promise<void>;
```

---

## 4. 网关服务

### 4.1 目录结构

```
gateway/
├── server.impl.ts         # 服务器实现 (22KB)
├── server-http.ts         # HTTP 路由 (14KB)
├── server-chat.ts         # 聊天处理 (12KB)
├── server-channels.ts     # 频道管理 (10KB)
├── server-methods/        # RPC 方法 (36个文件)
├── control-ui.ts          # 控制面板 UI
├── openai-http.ts         # OpenAI 兼容 API
├── openresponses-http.ts  # OpenResponses API (28KB)
├── protocol/              # 协议定义 (21个文件)
├── session-utils.ts       # 会话工具 (22KB)
└── ...
```

### 4.2 服务器启动

```typescript
// server.impl.ts
export async function startGateway(params: {
  cfg: OpenClawConfig;
  port: number;
  host: string;
  channels: ChannelConfig[];
  runtime: RuntimeEnv;
}): Promise<GatewayServer>;

// 启动流程:
// 1. 加载配置
// 2. 初始化频道
// 3. 启动 HTTP/WS 服务
// 4. 注册 RPC 方法
// 5. 启动定时任务
// 6. 广播就绪事件
```

### 4.3 RPC 方法 (server-methods/)

```
server-methods/
├── agent-*.ts          # Agent 相关
├── auth-*.ts           # 认证相关
├── browser-*.ts        # 浏览器相关
├── channel-*.ts        # 频道相关
├── chat-*.ts           # 聊天相关
├── config-*.ts         # 配置相关
├── cron-*.ts           # 定时任务
├── hooks-*.ts          # 钩子相关
├── model-*.ts          # 模型相关
├── node-*.ts           # 节点相关
├── plugin-*.ts         # 插件相关
├── session-*.ts        # 会话相关
└── skills-*.ts         # 技能相关
```

### 4.4 OpenAI 兼容 API

```typescript
// openai-http.ts
// POST /v1/chat/completions
export async function handleChatCompletions(
  req: Request,
  ctx: GatewayContext
): Promise<Response>;

// 支持:
// - 流式响应 (SSE)
// - 工具调用
// - 多模态输入
// - 模型路由
```

### 4.5 控制面板 UI

```typescript
// control-ui.ts
export function serveControlUi(params: {
  basePath: string;
  root: string;
  allowedOrigins: string[];
}): Hono;

// 功能:
// - WebSocket 实时通信
// - 会话管理
// - 配置编辑
// - 日志查看
```

---

## 5. 配置系统

### 5.1 配置结构 (1104行 Schema)

```typescript
// config/schema.ts
export const CONFIG_GROUPS = {
  wizard: "Wizard",
  update: "Update",
  diagnostics: "Diagnostics",
  logging: "Logging",
  gateway: "Gateway",
  nodeHost: "Node Host",
  agents: "Agents",
  tools: "Tools",
  bindings: "Bindings",
  audio: "Audio",
  models: "Models",
  messages: "Messages",
  commands: "Commands",
  session: "Session",
  cron: "Cron",
  hooks: "Hooks",
  ui: "UI",
  browser: "Browser",
  talk: "Talk",
  channels: "Messaging Channels",
  skills: "Skills",
  plugins: "Plugins",
  discovery: "Discovery",
  presence: "Presence",
  voicewake: "Voice Wake",
};
```

### 5.2 核心配置类型

```typescript
// types.ts
export type OpenClawConfig = {
  meta?: ConfigMeta;
  update?: UpdateConfig;
  diagnostics?: DiagnosticsConfig;
  gateway?: GatewayConfig;
  nodeHost?: NodeHostConfig;
  agents?: AgentsConfig;
  tools?: ToolsConfig;
  memory?: MemoryConfig;
  auth?: AuthConfig;
  commands?: CommandsConfig;
  session?: SessionConfig;
  messages?: MessagesConfig;
  cron?: CronConfig;
  hooks?: HooksConfig;
  ui?: UiConfig;
  browser?: BrowserConfig;
  talk?: TalkConfig;
  channels?: ChannelsConfig;
  skills?: SkillsConfig;
  plugins?: PluginsConfig;
  discovery?: DiscoveryConfig;
  presence?: PresenceConfig;
  voicewake?: VoiceWakeConfig;
};
```

### 5.3 频道配置

```typescript
// types.telegram.ts
export type TelegramAccountConfig = {
  botToken?: string;
  dmPolicy?: "open" | "allowlist" | "pairing" | "disabled";
  groupPolicy?: "open" | "allowlist" | "disabled";
  allowFrom?: (string | number)[];
  groupAllowFrom?: (string | number)[];
  streamMode?: "off" | "partial" | "block";
  blockStreaming?: boolean;
  textLimit?: number;
  textChunkLimit?: number;
  replyToMode?: ReplyToMode;
  linkPreview?: "default" | "disabled" | "above" | "below";
  capabilities?: TelegramCapabilities;
  customCommands?: TelegramCustomCommand[];
  groups?: Record<string, TelegramGroupConfig>;
  commands?: ChannelCommandsConfig;
  // ...更多选项
};
```

### 5.4 Agent 默认配置

```typescript
// types.agent-defaults.ts
export type AgentDefaults = {
  workspace?: string;
  repoRoot?: string;
  bootstrapMaxChars?: number;
  envelopeTimezone?: EnvelopeTimezone;
  envelopeTimestamp?: "on" | "off";
  envelopeElapsed?: "on" | "off";
  memorySearch?: MemorySearchConfig;
  model?: ModelConfig;
  imageModel?: ModelConfig;
  models?: Record<string, ModelEntry>;
  cliBackends?: CliBackendConfig[];
  humanDelay?: HumanDelayConfig;
  identity?: AgentIdentity;
  thinking?: ThinkingConfig;
  verbose?: VerboseConfig;
  reasoning?: ReasoningConfig;
  compaction?: CompactionConfig;
  // ...更多选项
};
```

---

## 6. 消息频道

### 6.1 支持的频道

| 频道 | 目录 | 文件数 | 状态 |
|-----|-----|:---:|:---:|
| Telegram | telegram/ | 88 | ✅ 完整 |
| Discord | discord/ | 67 | ✅ 完整 |
| Slack | slack/ | 65 | ✅ 完整 |
| WhatsApp | web/ | 78 | ✅ 完整 |
| Signal | signal/ | 24 | ✅ 完整 |
| iMessage | imessage/ | 17 | ✅ macOS |
| Line | line/ | 34 | ✅ 完整 |
| MS Teams | - | - | ⚠️ 基础 |

### 6.2 频道抽象 (channels/)

```typescript
// registry.ts
export const CHANNEL_IDS = [
  "whatsapp",
  "telegram",
  "discord",
  "slack",
  "signal",
  "imessage",
  "bluebubbles",
  "msteams",
  "mattermost",
  "line",
  "googlechat",
  "lark",
] as const;

// dock.ts (15KB)
export class ChannelDock {
  register(channel: Channel): void;
  send(target: string, message: OutboundMessage): Promise<void>;
  broadcast(message: OutboundMessage): Promise<void>;
}
```

### 6.3 Telegram 详细功能

参见 [openclaw-telegram-reference.md](./openclaw-telegram-reference.md)

---

## 7. 工具系统

### 7.1 工具定义 (pi-tools.ts)

```typescript
// 17KB 的工具定义
export function createOpenClawCodingTools(params: {
  cfg: OpenClawConfig;
  workspace: string;
  sandbox: SandboxContext;
}): ToolDefinition[];

// 核心工具:
// - bash: 执行 Shell 命令
// - write_file: 写入文件
// - read_file: 读取文件
// - search_files: 搜索文件
// - list_dir: 列目录
// - web_search: 网页搜索
// - web_fetch: 网页抓取
// - browser_*: 浏览器操作
// - message_send: 发送消息
// - session_*: 会话管理
// - agent_*: Agent 管理
```

### 7.2 工具策略 (tool-policy.ts)

```typescript
export type ToolPolicy = {
  profile?: "full" | "limited" | "readonly";
  alsoAllow?: string[];
  byProvider?: Record<string, ProviderToolPolicy>;
};

export function resolveToolPolicy(params: {
  cfg: OpenClawConfig;
  agentId: string;
  provider: string;
  model: string;
}): ResolvedToolPolicy;
```

### 7.3 执行审批 (approvals)

```typescript
// gateway/exec-approval-manager.ts
export class ExecApprovalManager {
  request(params: ApprovalRequest): Promise<Approval>;
  approve(id: string): void;
  reject(id: string): void;
  list(): ApprovalRequest[];
}
```

---

## 8. 浏览器自动化

### 8.1 目录结构

```
browser/
├── chrome.ts              # Chrome 启动/管理
├── chrome.executables.ts  # Chrome 路径检测 (17KB)
├── cdp.ts                 # CDP 协议 (14KB)
├── pw-session.ts          # Playwright 会话 (18KB)
├── pw-tools-core*.ts      # 浏览器工具
├── server-context.ts      # 服务端上下文 (22KB)
├── extension-relay.ts     # 扩展中继 (23KB)
├── profiles-service.ts    # 浏览器配置文件
└── routes/                # HTTP 路由 (14个文件)
```

### 8.2 核心功能

```typescript
// pw-session.ts
export class PlaywrightSession {
  async open(url: string): Promise<Page>;
  async screenshot(): Promise<Buffer>;
  async evaluate(script: string): Promise<unknown>;
  async close(): Promise<void>;
}

// pw-tools-core.interactions.ts
export async function browserClick(params: {
  session: PlaywrightSession;
  selector: string;
  options: ClickOptions;
}): Promise<void>;

export async function browserType(params: {
  session: PlaywrightSession;
  selector: string;
  text: string;
}): Promise<void>;

// pw-role-snapshot.ts
export async function takeRoleSnapshot(params: {
  page: Page;
  options: SnapshotOptions;
}): Promise<RoleSnapshot>;
```

---

## 9. 插件系统

### 9.1 目录结构

```
plugins/
├── types.ts               # 类型定义 (15KB)
├── registry.ts            # 插件注册 (14KB)
├── loader.ts              # 插件加载 (14KB)
├── install.ts             # 插件安装 (17KB)
├── discovery.ts           # 插件发现 (10KB)
├── hooks.ts               # 钩子系统 (14KB)
├── commands.ts            # 命令扩展 (8KB)
├── tools.ts               # 工具扩展
└── runtime/               # 运行时
```

### 9.2 插件类型

```typescript
// types.ts
export type PluginManifest = {
  id: string;
  name?: string;
  version?: string;
  description?: string;
  exports?: {
    commands?: CommandSpec[];
    tools?: ToolSpec[];
    hooks?: HookSpec[];
    providers?: ProviderSpec[];
    slots?: SlotSpec[];
  };
  config?: {
    schema?: JSONSchema;
    defaults?: Record<string, unknown>;
  };
};

export type PluginContext = {
  id: string;
  config: Record<string, unknown>;
  runtime: RuntimeEnv;
  gateway: GatewayContext;
};
```

### 9.3 插件 SDK

```typescript
// plugin-sdk/index.ts
export function definePlugin(manifest: PluginManifest): Plugin;
export function defineCommand(spec: CommandSpec): CommandHandler;
export function defineTool(spec: ToolSpec): ToolHandler;
export function defineHook(spec: HookSpec): HookHandler;
```

---

## 10. 其他模块

### 10.1 定时任务 (cron/)

```typescript
// service.ts
export class CronService {
  schedule(job: CronJob): string;
  cancel(jobId: string): void;
  list(): CronJob[];
  run(jobId: string): Promise<void>;
}

// 支持:
// - Cron 表达式
// - 一次性任务
// - 重复任务
// - 任务持久化
```

### 10.2 钩子系统 (hooks/)

```typescript
// hooks.ts
export type HookEvent = 
  | "session:start"
  | "session:end"
  | "message:inbound"
  | "message:outbound"
  | "tool:before"
  | "tool:after"
  | "agent:spawn"
  | "agent:terminate";

export function registerHook(event: HookEvent, handler: HookHandler): void;

// Gmail 集成
// gmail.ts, gmail-watcher.ts
export class GmailWatcher {
  watch(options: WatchOptions): void;
  stop(): void;
}
```

### 10.3 媒体理解 (media-understanding/)

```typescript
// runner.ts (38KB!)
export async function understandMedia(params: {
  type: "image" | "audio" | "video";
  data: Buffer | string;
  options: UnderstandingOptions;
}): Promise<UnderstandingResult>;

// 支持:
// - 图像理解 (多模态 LLM)
// - 音频转录 (Whisper, Deepgram)
// - 视频分析
// - 格式转换
```

### 10.4 内存/向量搜索

```typescript
// agents/memory-search.ts (10KB)
export class MemorySearch {
  async index(files: string[]): Promise<void>;
  async search(query: string): Promise<MemoryResult[]>;
  async clear(): Promise<void>;
}

// 支持:
// - OpenAI Embeddings
// - Gemini Embeddings
// - 本地 GGUF 模型 (node-llama-cpp)
// - SQLite + sqlite-vec 存储
// - BM25 + 向量混合搜索
```

### 10.5 TUI (终端 UI)

```typescript
// tui/
// 39 个文件的终端界面
// 基于 ink (React for CLI)
export function startTui(params: {
  cfg: OpenClawConfig;
  gateway: GatewayClient;
}): void;
```

---

## 11. NGOClaw 实现路线图

### Phase 1: 核心基础 (2-3 周)

| 模块 | 优先级 | 工作量 | OpenClaw 参考 |
|-----|:---:|:---:|-----|
| 配置系统 | P0 | 3天 | config/, 133文件 |
| Telegram 完整 | P0 | 2天 | telegram/, 88文件 |
| 命令注册 | P0 | 1天 | auto-reply/, 209文件 |
| 会话管理 | P0 | 2天 | config/sessions.ts |
| 模型路由 | P0 | 1天 | agents/model-*.ts |

### Phase 2: 高级功能 (2-3 周)

| 模块 | 优先级 | 工作量 | OpenClaw 参考 |
|-----|:---:|:---:|-----|
| 流式输出 | P1 | 2天 | telegram/draft-*.ts |
| 工具系统 | P1 | 3天 | agents/pi-tools.ts |
| 浏览器自动化 | P1 | 3天 | browser/, 81文件 |
| 插件系统 | P1 | 2天 | plugins/, 37文件 |
| 定时任务 | P2 | 1天 | cron/, 35文件 |

### Phase 3: 完整复现 (3-4 周)

| 模块 | 优先级 | 工作量 | OpenClaw 参考 |
|-----|:---:|:---:|-----|
| Discord 集成 | P2 | 2天 | discord/, 67文件 |
| Slack 集成 | P2 | 2天 | slack/, 65文件 |
| 媒体理解 | P2 | 2天 | media-understanding/ |
| 内存搜索 | P2 | 2天 | agents/memory-*.ts |
| 子代理 | P3 | 2天 | agents/subagent-*.ts |
| 技能系统 | P3 | 2天 | agents/skills/ |

### Phase 4: 优化增强

- 控制面板 UI
- 性能优化
- 测试覆盖
- 文档完善

---

## 附录

### A. 关键文件索引

| 功能 | 文件 | 大小 |
|-----|-----|:---:|
| 配置 Schema | config/schema.ts | 55KB |
| Bash 执行 | agents/bash-tools.exec.ts | 54KB |
| 媒体理解 | media-understanding/runner.ts | 38KB |
| 系统提示词 | agents/system-prompt.ts | 27KB |
| OpenResponses | gateway/openresponses-http.ts | 28KB |
| 扩展中继 | browser/extension-relay.ts | 23KB |
| 服务器上下文 | browser/server-context.ts | 22KB |
| 会话工具 | gateway/session-utils.ts | 22KB |
| 服务器实现 | gateway/server.impl.ts | 22KB |
| Agent 订阅 | agents/pi-embedded-subscribe.ts | 20KB |

### B. 测试统计

- 测试框架: Vitest
- 覆盖率阈值: 70%
- 测试文件: ~500+
- E2E 测试: 独立配置

### C. 相关文档

- [Telegram 功能详解](./openclaw-telegram-reference.md)
- [Gap 分析报告](../gap_analysis.md)
- [实现计划](../implementation_plan.md)
