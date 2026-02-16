# OpenClaw Telegram 功能完整参考文档

> 基于 OpenClaw 源码 (`/home/none/ngoclaw/openclaw`) 深度分析
> 目标：NGOClaw 完整复现并优化

---

## 目录

1. [架构概览](#1-架构概览)
2. [命令系统](#2-命令系统)
3. [Bot 菜单与原生命令](#3-bot-菜单与原生命令)
4. [模型管理](#4-模型管理)
5. [会话管理](#5-会话管理)
6. [消息处理](#6-消息处理)
7. [流式输出](#7-流式输出)
8. [权限与访问控制](#8-权限与访问控制)
9. [内联键盘](#9-内联键盘)
10. [NGOClaw 实现规划](#10-ngoclaw-实现规划)

---

## 1. 架构概览

### OpenClaw 目录结构

```
src/
├── telegram/                         # Telegram 适配层
│   ├── bot.ts                       (16KB)  - Bot 创建和生命周期
│   ├── bot-native-commands.ts       (700行) - 原生命令注册和处理
│   ├── bot-handlers.ts              (32KB)  - 消息处理器
│   ├── bot-message-context.ts       (25KB)  - 消息上下文构建
│   ├── bot-message-dispatch.ts      (11KB)  - 消息派发
│   ├── send.ts                      (26KB)  - 发送消息/编辑/流式
│   ├── model-buttons.ts             (5.6KB) - 模型选择内联按钮
│   ├── inline-buttons.ts            (2.6KB) - 通用内联按钮
│   ├── draft-stream.ts              (3.4KB) - 流式草稿
│   ├── draft-chunking.ts            (1.5KB) - 消息分块
│   └── ...
├── auto-reply/                        # 自动回复/命令处理
│   ├── commands-registry.ts         (521行) - 命令注册系统
│   ├── commands-registry.data.ts    (615行) - 所有命令定义
│   ├── commands-registry.types.ts            - 类型定义
│   └── reply/
│       ├── commands.ts                       - 命令入口
│       ├── commands-models.ts       (327行) - /models 命令
│       ├── commands-session.ts               - /new /reset 命令
│       ├── commands-core.ts                  - 核心命令处理
│       ├── commands-info.ts                  - /status /help
│       └── ...
├── agents/
│   ├── model-selection.ts                    - 模型选择和别名
│   ├── model-catalog.ts                      - 模型目录
│   └── defaults.ts                           - 默认配置
├── config/
│   ├── types.telegram.ts                     - Telegram 配置类型
│   └── telegram-custom-commands.ts           - 自定义命令
└── routing/
    └── session-key.ts                        - 会话 Key 生成
```

### 技术栈

| 组件 | OpenClaw | NGOClaw 目标 |
|-----|---------|-------------|
| 语言 | TypeScript | Go |
| Telegram SDK | grammy | telegram-bot-api/v5 |
| 运行时 | Node.js | Go Runtime |
| 配置格式 | JSON (openclaw.json) | YAML |

---

## 2. 命令系统

### 2.1 命令定义 (commands-registry.data.ts)

OpenClaw 定义了 **30+ 命令**:

```typescript
// 核心命令定义结构
type ChatCommandDefinition = {
  key: string;           // 内部标识 (如 "model")
  nativeName?: string;   // Telegram 命令名 (如 "model")
  description: string;   // 描述
  textAliases: string[]; // 文本别名 (如 ["/model", "/m"])
  acceptsArgs?: boolean; // 是否接受参数
  args?: CommandArgDefinition[]; // 参数定义
  scope: "text" | "native" | "both";
  category?: string;     // 分类
};
```

### 2.2 完整命令列表

| 命令 | 类型 | 参数 | 功能 |
|-----|:---:|-----|-----|
| `/help` | native | 无 | 显示帮助 |
| `/commands` | native | 无 | 列出所有命令 |
| `/status` | native | 无 | 显示当前状态 |
| `/model` | native | [model] | 查看/切换模型 |
| `/models` | native | [provider] [page] | 列出模型 |
| `/new` | native | [message] | 新建会话 |
| `/reset` | native | 无 | 重置会话 |
| `/stop` | native | 无 | 停止当前运行 |
| `/think` | native | [level] | 设置思考级别 |
| `/verbose` | native | [on/off] | 详细模式 |
| `/reasoning` | native | [on/off/stream] | 推理可见性 |
| `/usage` | native | [mode] | 用量统计 |
| `/tts` | native | [action] | 文字转语音 |
| `/whoami` | native | 无 | 显示发送者 ID |
| `/subagents` | native | [action] [target] | 子代理管理 |
| `/context` | native | [args] | 上下文说明 |
| `/approve` | native | [args] | 审批执行请求 |
| `/config` | native | [action] [path] [value] | 配置管理 |
| `/debug` | native | [action] [path] [value] | 调试设置 |
| `/activation` | native | [mode] | 群组激活模式 |
| `/send` | native | [mode] | 发送策略 |
| `/queue` | native | [options] | 队列设置 |
| `/elevated` | native | [mode] | 提权模式 |
| `/exec` | native | [options] | 执行默认设置 |
| `/skill` | native | [name] [input] | 运行技能 |
| `/restart` | native | 无 | 重启 OpenClaw |
| `/compact` | text | [instructions] | 压缩上下文 |
| `/bash` | text | [command] | 执行 Shell |
| `/allowlist` | text | [args] | 白名单管理 |

### 2.3 命令别名系统

```typescript
// commands-registry.data.ts:580-584
registerAlias(commands, "whoami", "/id");
registerAlias(commands, "think", "/thinking", "/t");
registerAlias(commands, "verbose", "/v");
registerAlias(commands, "reasoning", "/reason");
registerAlias(commands, "elevated", "/elev");
```

### 2.4 命令检测与解析 (commands-registry.ts)

```typescript
// 核心函数
export function resolveTextCommand(raw: string, cfg?: OpenClawConfig) {
  const trimmed = normalizeCommandBody(raw);
  const alias = maybeResolveTextAlias(trimmed, cfg);
  if (!alias) return null;
  
  const spec = getTextAliasMap().get(alias);
  const command = getChatCommands().find((e) => e.key === spec.key);
  
  return { command, args: trimmed.slice(alias.length).trim() };
}

// 参数解析
export function parseCommandArgs(command, raw?: string): CommandArgs {
  if (!command.args || command.argsParsing === "none") {
    return { raw: trimmed };
  }
  return {
    raw: trimmed,
    values: parsePositionalArgs(command.args, trimmed),
  };
}
```

---

## 3. Bot 菜单与原生命令

### 3.1 设置 Bot 菜单 (bot-native-commands.ts:370-375)

```typescript
const allCommands = [
  ...nativeCommands.map((cmd) => ({
    command: cmd.name,
    description: cmd.description,
  })),
  ...pluginCommands,
  ...customCommands,
];

await bot.api.setMyCommands(allCommands);
```

**NGOClaw 实现 (Go):**

```go
func (a *Adapter) SetupBotCommands() error {
    commands := []tgbotapi.BotCommand{
        {Command: "new", Description: "开始新对话"},
        {Command: "model", Description: "查看/切换模型"},
        {Command: "models", Description: "列出可用模型"},
        {Command: "reset", Description: "重置会话"},
        {Command: "stop", Description: "停止当前运行"},
        {Command: "status", Description: "显示状态"},
        {Command: "help", Description: "显示帮助"},
        // ... 更多命令
    }
    config := tgbotapi.NewSetMyCommands(commands...)
    _, err := a.bot.Request(config)
    return err
}
```

### 3.2 命令 Handler 注册 (bot-native-commands.ts:380-613)

```typescript
for (const command of nativeCommands) {
  bot.command(command.name, async (ctx) => {
    const msg = ctx.message;
    if (!msg) return;
    
    // 1. 权限验证
    const auth = await resolveTelegramCommandAuth({
      msg, bot, cfg, telegramCfg,
      allowFrom, groupAllowFrom, useAccessGroups,
      resolveGroupPolicy, resolveTelegramGroupConfig,
      requireAuth: true,
    });
    if (!auth) return;
    
    // 2. 解析参数
    const commandDefinition = findCommandByNativeName(command.name, "telegram");
    const rawText = ctx.match?.trim() ?? "";
    const commandArgs = parseCommandArgs(commandDefinition, rawText);
    
    // 3. 显示参数菜单 (如果需要)
    const menu = resolveCommandArgMenu({ command: commandDefinition, args: commandArgs, cfg });
    if (menu && commandDefinition) {
      // 发送内联键盘
      const rows = menu.choices.map((choice) => ({
        text: choice.label,
        callback_data: buildCommandTextFromArgs(commandDefinition, { values: { [menu.arg.name]: choice.value } }),
      }));
      await bot.api.sendMessage(chatId, title, { reply_markup: buildInlineKeyboard(rows) });
      return;
    }
    
    // 4. 路由到 Agent
    const route = resolveAgentRoute({ cfg, channel: "telegram", accountId, peer, parentPeer });
    
    // 5. 派发响应
    await dispatchReplyWithBufferedBlockDispatcher({
      ctx: ctxPayload,
      cfg,
      dispatcherOptions: {
        deliver: async (payload) => {
          await deliverReplies({ replies: [payload], chatId, token, ... });
        },
      },
    });
  });
}
```

---

## 4. 模型管理

### 4.1 模型别名 (openclaw.json)

```json
{
  "agents": {
    "defaults": {
      "models": {
        "antigravity/gemini-3-flash": { "alias": "Flash" },
        "antigravity/gemini-3-pro-low": { "alias": "ProLow" },
        "antigravity/claude-sonnet-4-5": { "alias": "Sonnet" }
      },
      "model": {
        "primary": "antigravity/gemini-3-pro-low",
        "fallbacks": ["antigravity/gemini-3-flash"]
      }
    }
  }
}
```

### 4.2 模型解析 (model-selection.ts)

```typescript
export function resolveModelRefFromString(params: {
  raw: string;
  defaultProvider: string;
  aliasIndex: Map<string, { provider: string; model: string }>;
}): { ref: { provider: string; model: string }; viaAlias?: string } | null {
  const { raw, defaultProvider, aliasIndex } = params;
  const trimmed = raw.trim();
  
  // 1. 检查别名
  const alias = aliasIndex.get(trimmed.toLowerCase());
  if (alias) {
    return { ref: alias, viaAlias: trimmed };
  }
  
  // 2. 检查 provider/model 格式
  if (trimmed.includes("/")) {
    const [provider, model] = trimmed.split("/", 2);
    return { ref: { provider, model } };
  }
  
  // 3. 仅模型名，使用默认 provider
  return { ref: { provider: defaultProvider, model: trimmed } };
}
```

### 4.3 /models 命令 (commands-models.ts)

```typescript
export async function resolveModelsCommandReply(params) {
  const { byProvider, providers } = await buildModelsProviderData(params.cfg);
  const isTelegram = params.surface === "telegram";
  
  // 无 provider 参数：显示 provider 列表
  if (!provider) {
    if (isTelegram && providers.length > 0) {
      const buttons = buildProviderKeyboard(providerInfos);
      return { text: "Select a provider:", channelData: { telegram: { buttons } } };
    }
    return { text: [...providers.map(p => `- ${p}`)].join("\n") };
  }
  
  // 有 provider：显示该 provider 的模型
  const models = [...byProvider.get(provider)].toSorted();
  if (isTelegram) {
    const buttons = buildModelsKeyboard({ provider, models, currentPage, totalPages });
    return { text: `Models (${provider})`, channelData: { telegram: { buttons } } };
  }
  return { text: models.map(m => `- ${provider}/${m}`).join("\n") };
}
```

### 4.4 模型切换按钮 (model-buttons.ts)

```typescript
export function buildModelsKeyboard(params: {
  provider: string;
  models: string[];
  currentModel?: string;
  currentPage: number;
  totalPages: number;
  pageSize: number;
}): InlineKeyboardButton[][] {
  const { provider, models, currentModel, currentPage, totalPages, pageSize } = params;
  
  const start = (currentPage - 1) * pageSize;
  const pageModels = models.slice(start, start + pageSize);
  
  const rows: InlineKeyboardButton[][] = [];
  
  // 模型按钮 (2列)
  for (let i = 0; i < pageModels.length; i += 2) {
    const row = pageModels.slice(i, i + 2).map((model) => ({
      text: model === currentModel ? `✓ ${model}` : model,
      callback_data: `/model ${provider}/${model}`,
    }));
    rows.push(row);
  }
  
  // 分页按钮
  if (totalPages > 1) {
    const nav = [];
    if (currentPage > 1) {
      nav.push({ text: "◀️", callback_data: `/models ${provider} ${currentPage - 1}` });
    }
    nav.push({ text: `${currentPage}/${totalPages}`, callback_data: "noop" });
    if (currentPage < totalPages) {
      nav.push({ text: "▶️", callback_data: `/models ${provider} ${currentPage + 1}` });
    }
    rows.push(nav);
  }
  
  // 返回按钮
  rows.push([{ text: "← Back", callback_data: "/models" }]);
  
  return rows;
}
```

---

## 5. 会话管理

### 5.1 会话 Key 生成 (session-key.ts)

```typescript
export function resolveSessionKey(params: {
  channel: string;
  accountId?: string;
  peer: { kind: "dm" | "group"; id: string };
  agentId: string;
}): string {
  const { channel, accountId, peer, agentId } = params;
  
  // 格式: channel:account:peer:agent
  const parts = [channel];
  if (accountId) parts.push(accountId);
  parts.push(peer.kind === "group" ? `group:${peer.id}` : peer.id);
  parts.push(agentId);
  
  return parts.join(":");
}
```

### 5.2 /new 命令实现

```typescript
// commands-session.ts
export async function handleNewCommand(params) {
  const { sessionKey, cfg, ctx } = params;
  
  // 1. 清除现有会话
  await clearSession(sessionKey);
  
  // 2. 如果有初始消息，作为第一条消息处理
  if (ctx.CommandArgs?.raw) {
    await processMessage(ctx.CommandArgs.raw, sessionKey);
  }
  
  return { text: "✨ New session started." };
}
```

### 5.3 /reset 命令

```typescript
export async function handleResetCommand(params) {
  const { sessionKey, cfg, ctx } = params;
  
  // 重置会话 (保留配置，清除历史)
  await resetSession(sessionKey);
  
  return { text: "🔄 Session reset." };
}
```

---

## 6. 消息处理

### 6.1 消息上下文构建 (bot-message-context.ts)

```typescript
export function buildMessageContext(params: {
  msg: TelegramMessage;
  cfg: OpenClawConfig;
  telegramCfg: TelegramAccountConfig;
  route: AgentRoute;
}): InboundContext {
  return {
    Body: msg.text || "",
    RawBody: msg.text || "",
    From: `telegram:${msg.chat.id}`,
    To: `telegram:${botId}`,
    ChatType: isGroup ? "group" : "direct",
    ConversationLabel: isGroup ? msg.chat.title : senderName,
    GroupSubject: isGroup ? msg.chat.title : undefined,
    SenderName: buildSenderName(msg),
    SenderId: String(msg.from.id),
    SenderUsername: msg.from.username,
    Surface: "telegram",
    MessageSid: String(msg.message_id),
    Timestamp: msg.date * 1000,
    WasMentioned: wasMentioned,
    SessionKey: route.sessionKey,
    AccountId: route.accountId,
  };
}
```

### 6.2 消息派发 (bot-message-dispatch.ts)

```typescript
export async function dispatchTelegramMessage(params: {
  ctx: TelegramContext;
  cfg: OpenClawConfig;
  media: MediaFile[];
}) {
  const { ctx, cfg, media } = params;
  
  // 1. 检查是否是命令
  if (isCommandMessage(ctx.Body)) {
    const resolved = resolveTextCommand(ctx.Body, cfg);
    if (resolved) {
      return handleTextCommand(resolved.command, resolved.args, ctx, cfg);
    }
  }
  
  // 2. 普通消息：发送给 Agent
  await dispatchReplyWithBufferedBlockDispatcher({
    ctx,
    cfg,
    dispatcherOptions: {
      deliver: async (payload) => {
        await sendTelegramMessage(ctx.From, payload);
      },
    },
  });
}
```

---

## 7. 流式输出

### 7.1 流式模式配置

```json
{
  "channels": {
    "telegram": {
      "streamMode": "partial"  // "off" | "partial" | "full"
    }
  }
}
```

### 7.2 流式草稿 (draft-stream.ts)

```typescript
export class DraftStream {
  private chatId: string;
  private messageId?: number;
  private lastText: string = "";
  private throttleMs: number = 500;
  private lastUpdate: number = 0;
  
  async update(text: string) {
    const now = Date.now();
    if (now - this.lastUpdate < this.throttleMs) {
      return; // 节流
    }
    
    if (!this.messageId) {
      // 首次发送
      const msg = await bot.api.sendMessage(this.chatId, text);
      this.messageId = msg.message_id;
    } else {
      // 编辑消息
      await bot.api.editMessageText(this.chatId, this.messageId, text);
    }
    
    this.lastText = text;
    this.lastUpdate = now;
  }
  
  async finalize(finalText: string) {
    if (this.messageId && finalText !== this.lastText) {
      await bot.api.editMessageText(this.chatId, this.messageId, finalText);
    }
  }
}
```

### 7.3 消息分块 (draft-chunking.ts)

```typescript
const TELEGRAM_MESSAGE_LIMIT = 4096;

export function chunkMessage(text: string): string[] {
  if (text.length <= TELEGRAM_MESSAGE_LIMIT) {
    return [text];
  }
  
  const chunks: string[] = [];
  let remaining = text;
  
  while (remaining.length > 0) {
    if (remaining.length <= TELEGRAM_MESSAGE_LIMIT) {
      chunks.push(remaining);
      break;
    }
    
    // 在段落/句子边界分割
    let splitIndex = remaining.lastIndexOf("\n\n", TELEGRAM_MESSAGE_LIMIT);
    if (splitIndex < TELEGRAM_MESSAGE_LIMIT * 0.5) {
      splitIndex = remaining.lastIndexOf("\n", TELEGRAM_MESSAGE_LIMIT);
    }
    if (splitIndex < TELEGRAM_MESSAGE_LIMIT * 0.5) {
      splitIndex = remaining.lastIndexOf(". ", TELEGRAM_MESSAGE_LIMIT);
    }
    if (splitIndex < 0) {
      splitIndex = TELEGRAM_MESSAGE_LIMIT;
    }
    
    chunks.push(remaining.slice(0, splitIndex));
    remaining = remaining.slice(splitIndex).trimStart();
  }
  
  return chunks;
}
```

---

## 8. 权限与访问控制

### 8.1 配置 (openclaw.json)

```json
{
  "channels": {
    "telegram": {
      "dmPolicy": "allowlist",        // "open" | "allowlist" | "disabled"
      "groupPolicy": "allowlist",     // "open" | "allowlist" | "disabled"
      "allowFrom": ["None", "iLab2077", "6153003667"],
      "groupAllowFrom": []
    }
  }
}
```

### 8.2 权限检查 (bot-access.ts)

```typescript
export function isSenderAllowed(params: {
  allow: { entries: Set<string>; hasEntries: boolean };
  senderId: string;
  senderUsername?: string;
}): boolean {
  const { allow, senderId, senderUsername } = params;
  
  if (!allow.hasEntries) {
    return true; // 无白名单 = 开放
  }
  
  // 检查 ID
  if (allow.entries.has(senderId)) {
    return true;
  }
  
  // 检查用户名 (不区分大小写)
  if (senderUsername) {
    const lower = senderUsername.toLowerCase();
    if (allow.entries.has(lower) || allow.entries.has(`@${lower}`)) {
      return true;
    }
  }
  
  return false;
}
```

### 8.3 群组策略

```typescript
export function resolveGroupPolicy(chatId: string | number): ChannelGroupPolicy {
  const cfg = getConfig();
  const groupConfig = cfg.channels?.telegram?.groups?.[String(chatId)];
  
  return {
    allowlistEnabled: cfg.channels?.telegram?.groupPolicy === "allowlist",
    allowed: groupConfig?.enabled !== false,
    policy: groupConfig?.policy ?? cfg.channels?.telegram?.groupPolicy ?? "open",
  };
}
```

---

## 9. 内联键盘

### 9.1 构建内联键盘 (inline-buttons.ts)

```typescript
export function buildInlineKeyboard(
  rows: Array<Array<{ text: string; callback_data: string }>>
): InlineKeyboardMarkup {
  return {
    inline_keyboard: rows.map((row) =>
      row.map((btn) => ({
        text: btn.text,
        callback_data: btn.callback_data.slice(0, 64), // Telegram 限制 64 字节
      }))
    ),
  };
}
```

### 9.2 回调处理

```typescript
bot.callbackQuery(/.*/, async (ctx) => {
  const data = ctx.callbackQuery.data;
  
  // 解析回调数据为命令
  if (data.startsWith("/")) {
    const resolved = resolveTextCommand(data, cfg);
    if (resolved) {
      await handleTextCommand(resolved.command, resolved.args, ctx, cfg);
    }
  }
  
  // 应答回调 (移除加载动画)
  await ctx.answerCallbackQuery();
});
```

---

## 10. NGOClaw 实现规划

### Phase 1: 核心功能 (P0)

| 任务 | 文件 | 参考 |
|-----|-----|-----|
| 集成 CommandRegistry | adapter.go | commands-registry.ts |
| setMyCommands | adapter.go | bot-native-commands.ts:370 |
| 命令路由分发 | adapter.go | bot-native-commands.ts:380-613 |
| /model 实现 | commands.go | commands-models.ts |
| /new 实现 | commands.go | 会话创建逻辑 |

### Phase 2: 用户体验 (P1)

| 任务 | 说明 |
|-----|-----|
| 模型别名 | 从配置读取 `alias` 映射 |
| 内联键盘 | /models 显示按钮选择 |
| 回调处理 | 按钮点击事件 |
| 权限增强 | 群组策略支持 |

### Phase 3: 高级功能 (P2)

| 任务 | 说明 |
|-----|-----|
| 流式输出 | 编辑消息实现 |
| 消息分块 | 超长消息处理 |
| 会话持久化 | ChatID -> Session 关联 |
| 定时任务 | /cron 支持 |

### Phase 4: 完整复现 (P3)

| 任务 | 说明 |
|-----|-----|
| 多 Agent | /agent 切换 |
| 工具执行 | 沙箱支持 |
| 技能系统 | /skill 命令 |
| 子代理 | /subagents 管理 |

---

## 附录：关键文件参考

| 功能 | OpenClaw 文件 | 行数 |
|-----|-------------|:---:|
| 命令定义 | src/auto-reply/commands-registry.data.ts | 615 |
| 命令注册 | src/auto-reply/commands-registry.ts | 521 |
| Bot 命令 | src/telegram/bot-native-commands.ts | 700 |
| 模型命令 | src/auto-reply/reply/commands-models.ts | 327 |
| 模型按钮 | src/telegram/model-buttons.ts | 200 |
| 消息发送 | src/telegram/send.ts | 900+ |
| 流式处理 | src/telegram/draft-stream.ts | 100 |
| **总计** | | **~3500** |
