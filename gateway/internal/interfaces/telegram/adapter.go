package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.uber.org/zap"
)

// Config Telegram 适配器配置
type Config struct {
	BotToken       string
	AllowedUserIDs []int64
	WebhookURL     string // 可选，留空则使用 polling
	Debug          bool
	// 策略配置
	DMPolicy       string   // open / allowlist / disabled
	GroupPolicy    string   // open / allowlist / disabled
	GroupAllowFrom []string // 允许的群组 ID 列表
}


// Adapter Telegram 适配器
type Adapter struct {
	bot             *tgbotapi.BotAPI
	config          *Config
	logger          *zap.Logger
	messageHandler  MessageHandler
	approvalHandler ApprovalHandler
	commandRegistry *CommandRegistry
	runController   RunController
	inboundBuffer   *InboundBuffer
	reactionHandler ReactionHandler
	inlineHandler   *InlineHandler
	mu              sync.RWMutex
	pendingApproval map[string]*ApprovalRequest
	cancel          context.CancelFunc
}

// MessageHandler 消息处理器接口
type MessageHandler interface {
	HandleMessage(ctx context.Context, msg *IncomingMessage) (*OutgoingMessage, error)
}

// ApprovalHandler 审批处理器接口
type ApprovalHandler interface {
	HandleApproval(ctx context.Context, requestID string, approved bool) error
}

// RunController 运行控制器接口 - 用于命令处理器中止/查询运行状态
type RunController interface {
	// AbortRun 中止指定 chat 的当前运行
	AbortRun(chatID int64) bool
	// IsRunActive 检查指定 chat 是否有活跃运行
	IsRunActive(chatID int64) bool
	// GetRunState 获取指定 chat 的运行状态
	GetRunState(chatID int64) string
}

// ReactionHandler 表情反应处理器接口
type ReactionHandler interface {
	// HandleReaction 处理用户对消息的表情反应
	// action: "save_memory" | "retry" | "regenerate" | "pin"
	HandleReaction(ctx context.Context, chatID int64, messageID int, action string) error
}

// IncomingMessage 入站消息
type IncomingMessage struct {
	MessageID      int
	ChatID         int64
	UserID         int64
	Username       string
	Text           string
	ReplyToMessage *IncomingMessage
	Timestamp      time.Time
	// Media 附件信息 (图片/语音/音频/视频/文档)
	Media     *MediaInfo
	MediaData []byte
	// MediaGroup 相册模式下的所有媒体附件
	MediaGroup []MediaInfo
}

// OutgoingMessage 出站消息
type OutgoingMessage struct {
	ChatID      int64
	Text        string
	ParseMode   string // "Markdown", "HTML", ""
	ReplyMarkup interface{}
	ReplyToID   int
}

// ApprovalRequest 审批请求
type ApprovalRequest struct {
	ID           string
	ChatID       int64
	MessageID    int
	ToolName     string
	ToolArgs     string
	CreatedAt    time.Time
	ResponseChan chan bool
}

// NewAdapter 创建 Telegram 适配器
func NewAdapter(config *Config, logger *zap.Logger) (*Adapter, error) {
	bot, err := tgbotapi.NewBotAPI(config.BotToken)
	if err != nil {
		return nil, fmt.Errorf("failed to create bot: %w", err)
	}

	bot.Debug = config.Debug

	logger.Info("Telegram bot authorized",
		zap.String("username", bot.Self.UserName),
	)

	adapter := &Adapter{
		bot:             bot,
		config:          config,
		logger:          logger,
		pendingApproval: make(map[string]*ApprovalRequest),
	}

	// Initialize inbound buffer — handler will be set when messageHandler is wired
	adapter.inboundBuffer = NewInboundBuffer(func(ctx context.Context, msg *IncomingMessage) {
		adapter.processBufferedMessage(ctx, msg)
	}, logger)

	return adapter, nil
}

// SetMessageHandler 设置消息处理器
func (a *Adapter) SetMessageHandler(handler MessageHandler) {
	a.messageHandler = handler
}

// SetApprovalHandler 设置审批处理器
func (a *Adapter) SetApprovalHandler(handler ApprovalHandler) {
	a.approvalHandler = handler
}

// SetRunController 设置运行控制器
func (a *Adapter) SetRunController(ctrl RunController) {
	a.runController = ctrl
}

// Start 启动适配器 (轮询模式)
func (a *Adapter) Start(ctx context.Context) error {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	// 创建可取消的 context
	innerCtx, cancel := context.WithCancel(ctx)
	a.cancel = cancel

	// 设置 Bot 命令菜单
	if err := a.SetupBotCommands(); err != nil {
		a.logger.Warn("Failed to setup bot commands", zap.Error(err))
	}

	updates := a.bot.GetUpdatesChan(u)

	a.logger.Info("Starting Telegram polling")

	go func() {
		for {
			select {
			case <-innerCtx.Done():
				a.bot.StopReceivingUpdates()
				a.logger.Info("Telegram adapter stopped")
				return
			case update := <-updates:
				go a.handleUpdate(innerCtx, update)
			}
		}
	}()

	return nil
}

// SetupBotCommands 设置 Bot 命令菜单
func (a *Adapter) SetupBotCommands() error {
	commands := []tgbotapi.BotCommand{
		{Command: "new", Description: "✨ 新对话"},
		{Command: "stop", Description: "⏹ 停止运行"},
		{Command: "models", Description: "🤖 切换模型"},
		{Command: "status", Description: "📊 当前状态"},
		{Command: "think", Description: "🧠 思考级别"},
		{Command: "compact", Description: "⚙️ 压缩上下文"},
		{Command: "security", Description: "🔒 安全策略"},
		{Command: "skills", Description: "🎯 技能管理"},
		{Command: "plan", Description: "📝 查看计划"},
		{Command: "help", Description: "❓ 帮助"},
	}

	config := tgbotapi.NewSetMyCommands(commands...)
	_, err := a.bot.Request(config)
	if err != nil {
		return fmt.Errorf("failed to set bot commands: %w", err)
	}

	a.logger.Info("Bot commands menu configured", zap.Int("count", len(commands)))
	return nil
}


// CreateDraftStream creates a new streaming message updater for the given chat.
// Deprecated: Use CreateStagedReply for TG card interactions.
func (a *Adapter) CreateDraftStream(chatID int64) *DraftStream {
	return NewDraftStream(a.bot, chatID)
}

// CreateStagedReply creates an Antigravity-style staged reply handler.
// Phase 1: status message updates (thinking → tool exec → step progress)
// Phase 2: delete status → deliver final complete reply
func (a *Adapter) CreateStagedReply(chatID int64) *StagedReply {
	return NewStagedReply(a.bot, chatID)
}


// Stop 停止适配器
func (a *Adapter) Stop() {
	if a.cancel != nil {
		a.cancel()
	}
}

// handleUpdate 处理更新
func (a *Adapter) handleUpdate(ctx context.Context, update tgbotapi.Update) {
	// 处理回调查询 (审批按钮 / 命令回调)
	if update.CallbackQuery != nil {
		a.handleCallback(ctx, update.CallbackQuery)
		return
	}

	// 处理 Inline 查询 (@bot 即问即答)
	if update.InlineQuery != nil {
		if a.inlineHandler != nil {
			a.inlineHandler.HandleInlineQuery(ctx, a.bot, update.InlineQuery)
		}
		return
	}

	// 处理编辑消息
	if update.EditedMessage != nil {
		a.handleEditedMessage(ctx, update.EditedMessage)
		return
	}

	// 处理消息
	if update.Message == nil {
		return
	}

	msg := update.Message

	// 检查权限 (私聊 + 群组)
	isGroup := msg.Chat.IsGroup() || msg.Chat.IsSuperGroup()
	if !a.isAllowedChat(msg.Chat.ID, msg.From.ID, isGroup) {
		a.logger.Warn("Unauthorized access",
			zap.Int64("chat_id", msg.Chat.ID),
			zap.Int64("user_id", msg.From.ID),
			zap.String("username", msg.From.UserName),
			zap.Bool("is_group", isGroup),
		)
		return
	}


	// 先检查是否是命令
	if cmd := ParseCommand(msg.Text); cmd != nil {
		cmd.ChatID = msg.Chat.ID
		cmd.UserID = msg.From.ID

		// 使用命令注册表处理
		if a.commandRegistry != nil {
			response, handled, err := a.commandRegistry.Handle(ctx, cmd)
			if err != nil {
				a.logger.Error("Failed to handle command",
					zap.String("command", cmd.Name),
					zap.Error(err),
				)
				a.sendError(msg.Chat.ID, err)
				return
			}
			if handled {
				if response != nil {
					a.SendMessage(response)
				}
				return
			}
		}

		a.logger.Debug("Unknown command, treating as message",
			zap.String("command", cmd.Name),
		)
	}

	// 转换消息
	incoming := &IncomingMessage{
		MessageID: msg.MessageID,
		ChatID:    msg.Chat.ID,
		UserID:    msg.From.ID,
		Username:  msg.From.UserName,
		Text:      msg.Text,
		Timestamp: time.Unix(int64(msg.Date), 0),
	}

	if msg.ReplyToMessage != nil {
		incoming.ReplyToMessage = &IncomingMessage{
			MessageID: msg.ReplyToMessage.MessageID,
			Text:      msg.ReplyToMessage.Text,
		}
	}

	// 提取媒体附件 (图片/语音/音频/视频/文档)
	if mediaInfo := ExtractMedia(msg); mediaInfo != nil {
		incoming.Media = mediaInfo
		// 如果有 caption 且没有 text，使用 caption 作为文本
		if incoming.Text == "" && mediaInfo.Caption != "" {
			incoming.Text = mediaInfo.Caption
		}

		// 下载媒体文件
		data, err := DownloadFile(a.bot, mediaInfo.FileID, a.logger)
		if err != nil {
			a.logger.Error("Failed to download media file",
				zap.String("file_id", mediaInfo.FileID),
				zap.String("type", string(mediaInfo.Type)),
				zap.Error(err),
			)
		} else {
			incoming.MediaData = data
			a.logger.Info("Media attachment extracted",
				zap.String("type", string(mediaInfo.Type)),
				zap.String("mime", mediaInfo.MimeType),
				zap.Int("size_bytes", len(data)),
			)
		}
	}

	// Submit to inbound buffer (handles debounce, text fragments, media groups)
	a.inboundBuffer.Submit(ctx, incoming, msg.MediaGroupID)
}

// handleCallback 处理回调查询 (内联按钮点击)
func (a *Adapter) handleCallback(ctx context.Context, callback *tgbotapi.CallbackQuery) {
	data := callback.Data

	// 处理 noop 回调 (分页指示器等)
	if data == "noop" {
		a.bot.Send(tgbotapi.NewCallback(callback.ID, ""))
		return
	}

	// 处理命令回调 (以 / 开头)
	if strings.HasPrefix(data, "/") {
		a.handleCommandCallback(ctx, callback)
		return
	}

	// 格式: approve:<request_id> 或 deny:<request_id>
	parts := strings.SplitN(data, ":", 2)
	if len(parts) != 2 {
		a.bot.Send(tgbotapi.NewCallback(callback.ID, "无效回调"))
		return
	}

	action := parts[0]
	requestID := parts[1]

	a.mu.Lock()
	request, exists := a.pendingApproval[requestID]
	if exists {
		delete(a.pendingApproval, requestID)
	}
	a.mu.Unlock()

	if !exists {
		// 请求已过期或已处理
		a.bot.Send(tgbotapi.NewCallback(callback.ID, "请求已过期"))
		return
	}

	approved := action == "approve"

	// 回复回调
	var callbackText string
	if approved {
		callbackText = "✅ 已批准"
	} else {
		callbackText = "❌ 已拒绝"
	}
	a.bot.Send(tgbotapi.NewCallback(callback.ID, callbackText))

	// 更新原消息
	editMsg := tgbotapi.NewEditMessageText(
		request.ChatID,
		request.MessageID,
		fmt.Sprintf("工具调用: `%s`\n状态: %s", request.ToolName, callbackText),
	)
	editMsg.ParseMode = "Markdown"
	a.bot.Send(editMsg)

	// 通知等待的协程
	if request.ResponseChan != nil {
		request.ResponseChan <- approved
		close(request.ResponseChan)
	}

	// 调用审批处理器
	if a.approvalHandler != nil {
		a.approvalHandler.HandleApproval(ctx, requestID, approved)
	}
}

// handleCommandCallback 处理命令回调（内联按钮触发命令）
func (a *Adapter) handleCommandCallback(ctx context.Context, callback *tgbotapi.CallbackQuery) {
	data := callback.Data

	// 解析命令
	cmd := ParseCommand(data)
	if cmd == nil {
		a.bot.Send(tgbotapi.NewCallback(callback.ID, "无效命令"))
		return
	}

	// 设置 chat 和 user ID
	if callback.Message != nil {
		cmd.ChatID = callback.Message.Chat.ID
	}
	if callback.From != nil {
		cmd.UserID = callback.From.ID
	}

	// 应答回调 (移除加载动画)
	a.bot.Send(tgbotapi.NewCallback(callback.ID, ""))

	// 使用命令注册表处理
	if a.commandRegistry != nil {
		response, handled, err := a.commandRegistry.Handle(ctx, cmd)
		if err != nil {
			a.logger.Error("Failed to handle callback command",
				zap.String("command", cmd.Name),
				zap.Error(err),
			)
			return
		}
		if handled && response != nil {
			// 如果有原消息，编辑它；否则发送新消息
			if callback.Message != nil {
				a.editMessageWithKeyboard(callback.Message.Chat.ID, callback.Message.MessageID, response)
			} else {
				a.SendMessage(response)
			}
		}
	}
}

// editMessageWithKeyboard 编辑消息（支持键盘）
func (a *Adapter) editMessageWithKeyboard(chatID int64, messageID int, msg *OutgoingMessage) {
	editMsg := tgbotapi.NewEditMessageText(chatID, messageID, msg.Text)
	if msg.ParseMode != "" {
		editMsg.ParseMode = msg.ParseMode
	}
	if msg.ReplyMarkup != nil {
		// 类型断言获取 InlineKeyboardMarkup
		if keyboard, ok := msg.ReplyMarkup.(*tgbotapi.InlineKeyboardMarkup); ok {
			editMsg.ReplyMarkup = keyboard
		}
	}
	a.bot.Send(editMsg)
}



// RequestApproval 请求用户审批 (Ask Mode)
func (a *Adapter) RequestApproval(ctx context.Context, chatID int64, toolName string, toolArgs string) (bool, error) {
	requestID := fmt.Sprintf("req_%d_%d", chatID, time.Now().UnixNano())

	// 创建审批请求
	request := &ApprovalRequest{
		ID:           requestID,
		ChatID:       chatID,
		ToolName:     toolName,
		ToolArgs:     toolArgs,
		CreatedAt:    time.Now(),
		ResponseChan: make(chan bool, 1),
	}

	// 构建内联键盘
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ 批准", "approve:"+requestID),
			tgbotapi.NewInlineKeyboardButtonData("❌ 拒绝", "deny:"+requestID),
		),
	)

	// 发送审批消息 — 人类可读格式, 不是原始 JSON
	text := formatApprovalMessage(toolName, toolArgs)

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard

	sentMsg, err := a.bot.Send(msg)
	if err != nil {
		return false, fmt.Errorf("failed to send approval request: %w", err)
	}

	request.MessageID = sentMsg.MessageID

	// 注册待审批请求
	a.mu.Lock()
	a.pendingApproval[requestID] = request
	a.mu.Unlock()

	// 等待响应或超时
	select {
	case approved := <-request.ResponseChan:
		return approved, nil
	case <-time.After(5 * time.Minute):
		// 超时，自动拒绝
		a.mu.Lock()
		delete(a.pendingApproval, requestID)
		a.mu.Unlock()

		// 更新消息
		editMsg := tgbotapi.NewEditMessageText(chatID, request.MessageID,
			fmt.Sprintf("工具调用: `%s`\n状态: ⏰ 已超时 (自动拒绝)", toolName))
		editMsg.ParseMode = "Markdown"
		a.bot.Send(editMsg)

		return false, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

// SendMessage 发送消息
func (a *Adapter) SendMessage(out *OutgoingMessage) error {
	msg := tgbotapi.NewMessage(out.ChatID, out.Text)

	if out.ParseMode != "" {
		msg.ParseMode = out.ParseMode
	}

	if out.ReplyToID > 0 {
		msg.ReplyToMessageID = out.ReplyToID
	}

	if out.ReplyMarkup != nil {
		msg.ReplyMarkup = out.ReplyMarkup
	}

	_, err := a.bot.Send(msg)

	// Fallback: if HTML parsing fails, retry as plain text.
	// Safety net for edge cases where goldmark produces invalid TG HTML.
	if err != nil && msg.ParseMode != "" && strings.Contains(err.Error(), "can't parse entities") {
		a.logger.Warn("Markdown parse failed, retrying as plain text",
			zap.Int64("chat_id", out.ChatID),
			zap.Error(err),
		)
		msg.ParseMode = ""
		_, err = a.bot.Send(msg)
	}

	return err
}

// SendTyping 发送打字状态
func (a *Adapter) SendTyping(chatID int64) {
	action := tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping)
	a.bot.Send(action)
}

// sendError 发送错误消息 — 分类错误并提供操作建议
func (a *Adapter) sendError(chatID int64, err error) {
	errStr := strings.ToLower(err.Error())

	var text string
	switch {
	case strings.Contains(errStr, "unauthorized") || strings.Contains(errStr, "invalid api key"):
		text = "🔑 API 密钥无效，请联系管理员检查配置"
	case strings.Contains(errStr, "model not found") || strings.Contains(errStr, "not found"):
		text = "🤖 模型暂不可用，尝试 /model 切换其他模型"
	case strings.Contains(errStr, "timeout") || strings.Contains(errStr, "deadline exceeded"):
		text = "⏰ 响应超时，请稍后重试或尝试简化问题"
	case strings.Contains(errStr, "rate limit") || strings.Contains(errStr, "too many requests") || strings.Contains(errStr, "429"):
		text = "🚦 请求过于频繁，请稍等片刻后重试"
	case strings.Contains(errStr, "context canceled"):
		text = "⏹ 操作已取消"
	case strings.Contains(errStr, "overloaded") || strings.Contains(errStr, "503") || strings.Contains(errStr, "529"):
		text = "🔄 服务暂时过载，请稍后重试"
	default:
		// Generic: show simplified error
		short := err.Error()
		if len(short) > 200 {
			short = short[:200] + "..."
		}
		text = fmt.Sprintf("❌ 出错了: %s", short)
	}

	msg := tgbotapi.NewMessage(chatID, text)
	a.bot.Send(msg)
}

// isAllowedUser 检查用户是否被允许 (私聊)
func (a *Adapter) isAllowedUser(userID int64) bool {
	// 检查 DM 策略
	switch a.config.DMPolicy {
	case "disabled":
		return false
	case "allowlist":
		return a.isInUserAllowlist(userID)
	default: // "open" 或空
		// 如果配置了 AllowedUserIDs，则使用白名单
		if len(a.config.AllowedUserIDs) > 0 {
			return a.isInUserAllowlist(userID)
		}
		return true
	}
}

// isAllowedGroup 检查群组是否被允许
func (a *Adapter) isAllowedGroup(chatID int64) bool {
	// 检查群组策略
	switch a.config.GroupPolicy {
	case "disabled":
		return false
	case "allowlist":
		return a.isInGroupAllowlist(chatID)
	default: // "open" 或空
		return true
	}
}

// isAllowedChat 综合检查聊天是否被允许
func (a *Adapter) isAllowedChat(chatID int64, userID int64, isGroup bool) bool {
	if isGroup {
		// 群组：检查群组策略 + 用户权限
		if !a.isAllowedGroup(chatID) {
			return false
		}
		// 群组中也可选检查用户
		return true
	}
	// 私聊：检查用户权限
	return a.isAllowedUser(userID)
}

// isInUserAllowlist 检查用户是否在白名单
func (a *Adapter) isInUserAllowlist(userID int64) bool {
	if len(a.config.AllowedUserIDs) == 0 {
		return true // 空白名单 = 允许所有
	}
	for _, id := range a.config.AllowedUserIDs {
		if id == userID {
			return true
		}
	}
	return false
}

// isInGroupAllowlist 检查群组是否在白名单
func (a *Adapter) isInGroupAllowlist(chatID int64) bool {
	if len(a.config.GroupAllowFrom) == 0 {
		return true // 空白名单 = 允许所有
	}
	chatIDStr := fmt.Sprintf("%d", chatID)
	for _, id := range a.config.GroupAllowFrom {
		if id == chatIDStr {
			return true
		}
	}
	return false
}


// truncate 截断字符串
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// processBufferedMessage handles a message after it exits the inbound buffer
func (a *Adapter) processBufferedMessage(ctx context.Context, msg *IncomingMessage) {
	if a.messageHandler == nil {
		a.logger.Warn("No message handler set")
		return
	}

	response, err := a.messageHandler.HandleMessage(ctx, msg)
	if err != nil {
		a.logger.Error("Failed to handle message",
			zap.Error(err),
		)
		a.sendError(msg.ChatID, err)
		return
	}

	if response != nil {
		a.SendMessage(response)
	}
}

// SetReactionHandler 设置表情反应处理器
func (a *Adapter) SetReactionHandler(handler ReactionHandler) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.reactionHandler = handler
}

// SetInlineHandler 设置 Inline 查询处理器
func (a *Adapter) SetInlineHandler(handler *InlineHandler) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.inlineHandler = handler
}

// handleEditedMessage 处理编辑消息 — 用户修正已发送文本后重新触发 AI
func (a *Adapter) handleEditedMessage(ctx context.Context, msg *tgbotapi.Message) {
	if msg.From == nil {
		return
	}

	isGroup := msg.Chat.IsGroup() || msg.Chat.IsSuperGroup()
	if !a.isAllowedChat(msg.Chat.ID, msg.From.ID, isGroup) {
		return
	}

	a.logger.Info("Edited message received",
		zap.Int64("chat_id", msg.Chat.ID),
		zap.Int("message_id", msg.MessageID),
		zap.String("new_text", truncate(msg.Text, 100)),
	)

	// 构造新的 IncomingMessage, 标记为编辑
	incoming := &IncomingMessage{
		MessageID: msg.MessageID,
		ChatID:    msg.Chat.ID,
		UserID:    msg.From.ID,
		Username:  msg.From.UserName,
		Text:      msg.Text,
		Timestamp: time.Unix(int64(msg.Date), 0),
	}

	// 处理媒体附件
	if mediaInfo := ExtractMedia(msg); mediaInfo != nil {
		incoming.Media = mediaInfo
		if incoming.Text == "" && mediaInfo.Caption != "" {
			incoming.Text = mediaInfo.Caption
		}
		data, err := DownloadFile(a.bot, mediaInfo.FileID, a.logger)
		if err == nil {
			incoming.MediaData = data
		}
	}

	// 加前缀 hint 告知 AI 这是修正
	if incoming.Text != "" {
		incoming.Text = "[用户编辑了上一条消息] " + incoming.Text
	}

	// 直接走消息处理 (不经过 debounce, 编辑消息需要即时响应)
	a.processBufferedMessage(ctx, incoming)
}

// handleReaction 处理消息表情反应 — 映射 emoji 到语义操作
func (a *Adapter) handleReaction(ctx context.Context, chatID int64, messageID int, emoji string) {
	// Emoji → Action 映射
	actionMap := map[string]string{
		"👍": "save_memory",  // 存入记忆 (标记为高质量回答)
		"👎": "retry",        // 重新生成 (标记为不良回答)
		"🔄": "regenerate",   // 重新生成 (不标记)
		"📌": "pin",          // Pin 到上下文 (compaction 不压缩)
		"❤":  "save_memory",  // 同 👍
		"🔥": "save_memory",  // 同 👍
		"🤔": "retry",        // 同 👎
	}

	action, exists := actionMap[emoji]
	if !exists {
		a.logger.Debug("Ignoring unrecognized reaction",
			zap.String("emoji", emoji),
			zap.Int64("chat_id", chatID),
		)
		return
	}

	a.logger.Info("Reaction action triggered",
		zap.String("emoji", emoji),
		zap.String("action", action),
		zap.Int64("chat_id", chatID),
		zap.Int("message_id", messageID),
	)

	if a.reactionHandler != nil {
		if err := a.reactionHandler.HandleReaction(ctx, chatID, messageID, action); err != nil {
			a.logger.Error("Failed to handle reaction",
				zap.String("action", action),
				zap.Error(err),
			)
		}
	}

	// 根据 action 发送确认反馈
	var feedback string
	switch action {
	case "save_memory":
		feedback = "💾 已保存到记忆"
	case "retry":
		feedback = "🔄 正在重新生成..."
	case "regenerate":
		feedback = "🔄 正在重新生成..."
	case "pin":
		feedback = "📌 已固定到上下文"
	}

	if feedback != "" {
		a.SendMessage(&OutgoingMessage{
			ChatID:    chatID,
			Text:      feedback,
			ReplyToID: messageID,
		})
	}
}

// formatApprovalMessage creates a human-readable tool approval card.
// Instead of dumping raw JSON, it extracts key information and presents it cleanly.
func formatApprovalMessage(toolName string, toolArgs string) string {
	// Parse the JSON args
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(toolArgs), &args); err != nil {
		// Fallback to raw display if not valid JSON
		return fmt.Sprintf("🔧 *请求执行工具*\n\n工具: `%s`\n参数: %s\n\n请确认是否执行：",
			toolName, truncate(toolArgs, 300))
	}

	var lines []string
	lines = append(lines, "🔧 *请求执行工具*\n")

	switch toolName {
	case "bash", "bash_exec", "shell":
		cmd := argStr(args, "command")
		if cmd == "" {
			cmd = argStr(args, "cmd")
		}
		lines = append(lines, fmt.Sprintf("执行命令:\n```\n%s\n```", truncate(cmd, 500)))

	case "write_file":
		path := argStr(args, "path")
		content := argStr(args, "content")
		baseName := path
		if idx := strings.LastIndex(path, "/"); idx >= 0 {
			baseName = path[idx+1:]
		}
		contentLen := len([]rune(content))
		lines = append(lines, fmt.Sprintf("写入文件: `%s` (%d 字符)", baseName, contentLen))
		if contentLen > 0 {
			preview := truncate(content, 200)
			lines = append(lines, fmt.Sprintf("\n内容预览:\n```\n%s\n```", preview))
		}

	case "read_file":
		path := argStr(args, "path")
		lines = append(lines, fmt.Sprintf("读取文件: `%s`", path))

	case "web_search", "search":
		query := argStr(args, "query")
		lines = append(lines, fmt.Sprintf("搜索: `%s`", query))

	case "web_fetch":
		url := argStr(args, "url")
		lines = append(lines, fmt.Sprintf("抓取网页: %s", truncate(url, 100)))

	default:
		// Generic: show key=value pairs, truncate long values
		lines = append(lines, fmt.Sprintf("工具: `%s`", toolName))
		for k, v := range args {
			valStr := fmt.Sprintf("%v", v)
			if len(valStr) > 100 {
				valStr = truncate(valStr, 100)
			}
			lines = append(lines, fmt.Sprintf("• %s: %s", k, valStr))
		}
	}

	lines = append(lines, "\n请确认是否执行：")
	return strings.Join(lines, "\n")
}
