package telegram

import (
	"context"
	"fmt"
	"strings"
)

// registerSessionCommands registers session lifecycle: start, help, new, clear, status, reset, stop, whoami, commands
func (a *Adapter) registerSessionCommands(registry *CommandRegistry) {
	registry.Register("start", func(ctx context.Context, cmd *Command) (*OutgoingMessage, error) {
		return &OutgoingMessage{
			ChatID:    cmd.ChatID,
			Text:      "👋 欢迎使用 NGOClaw AI 助手！\n\n发送 /new 开始新对话，或直接发送消息。",
			ParseMode: "HTML",
		}, nil
	})

	// /help 命令
	registry.Register("help", func(ctx context.Context, cmd *Command) (*OutgoingMessage, error) {
		helpText := `📚 <b>命令列表</b>

<b>会话</b>
/new — 新对话
/clear — 清除历史
/stop — 停止当前任务
/compact — 压缩上下文
/context — 上下文统计
/reset — 重置会话

<b>模型</b>
/model [名称] — 查看/切换模型
/models — 浏览可用模型
/think [级别] — 思考级别
/verbose [on|off] — 详细模式
/reasoning [模式] — 推理可见性

<b>状态</b>
/status — 当前状态
/whoami — 身份信息
/usage [模式] — 用量统计
/commands — 所有命令

<b>配置</b>
/config — 查看/编辑配置
/security — 安全策略
/trust — 信任工具
/allowlist — 白名单管理
/activation — 群组激活
/sendpolicy — 发送策略

<b>高级</b>
/skills — 技能管理
/cron — 定时任务
/agent — 代理管理
/subagents — 子代理
/tts — 语音合成

💡 直接发送消息即可与 AI 对话`

		return &OutgoingMessage{
			ChatID:    cmd.ChatID,
			Text:      helpText,
			ParseMode: "HTML",
		}, nil
	})

	// /new 命令 - 创建新会话
	registry.Register("new", func(ctx context.Context, cmd *Command) (*OutgoingMessage, error) {
		if registry.sessionManager != nil {
			if err := registry.sessionManager.CreateSession(cmd.ChatID, cmd.UserID); err != nil {
				return &OutgoingMessage{
					ChatID:    cmd.ChatID,
					Text:      fmt.Sprintf("❌ 创建会话失败: %s", err.Error()),
					ParseMode: "HTML",
				}, nil
			}
		}
		// 清除 agent loop 对话历史
		if registry.historyClearer != nil {
			registry.historyClearer.ClearHistory(cmd.ChatID)
		}

		text := "✨ 新对话已开始！"
		// 如果有初始消息，附加说明
		if cmd.RawArgs != "" {
			text = "✨ 新对话已开始！\n\n正在处理您的消息..."
		}

		return &OutgoingMessage{
			ChatID:    cmd.ChatID,
			Text:      text,
			ParseMode: "HTML",
		}, nil
	})

	// /clear 命令
	registry.Register("clear", func(ctx context.Context, cmd *Command) (*OutgoingMessage, error) {
		if registry.sessionManager != nil {
			if err := registry.sessionManager.ClearSession(cmd.ChatID); err != nil {
				return &OutgoingMessage{
					ChatID:    cmd.ChatID,
					Text:      fmt.Sprintf("❌ 清除失败: %s", err.Error()),
					ParseMode: "HTML",
				}, nil
			}
		}
		// 清除 agent loop 对话历史
		if registry.historyClearer != nil {
			registry.historyClearer.ClearHistory(cmd.ChatID)
		}

		return &OutgoingMessage{
			ChatID:    cmd.ChatID,
			Text:      "🗑 对话历史已清除",
			ParseMode: "HTML",
		}, nil
	})

	// /cancel → alias to /stop (registered below)

	// /status 命令 (对标 OpenClaw handleStatusCommand)
	registry.Register("status", func(ctx context.Context, cmd *Command) (*OutgoingMessage, error) {
		currentModel := "未设置"
		if registry.sessionManager != nil {
			if m := registry.sessionManager.GetCurrentModel(cmd.ChatID); m != "" {
				currentModel = m
			}
		}

		runState := "空闲"
		if registry.runController != nil {
			runState = registry.runController.GetRunState(cmd.ChatID)
		}

		statusText := fmt.Sprintf("📊 <b>状态</b>\n\n"+
			"🤖 模型: <code>%s</code>\n"+
			"⚡ 状态: %s\n"+
			"💬 会话: <code>%d</code>\n"+
			"\n使用 /model 切换模型",
			currentModel, runState, cmd.ChatID)

		return &OutgoingMessage{
			ChatID:    cmd.ChatID,
			Text:      statusText,
			ParseMode: "HTML",
		}, nil
	})

	// /reset 命令
	registry.Register("reset", func(ctx context.Context, cmd *Command) (*OutgoingMessage, error) {
		if registry.sessionManager != nil {
			if err := registry.sessionManager.ClearSession(cmd.ChatID); err != nil {
				return &OutgoingMessage{
					ChatID:    cmd.ChatID,
					Text:      fmt.Sprintf("❌ 重置失败: %s", err.Error()),
					ParseMode: "HTML",
				}, nil
			}
		}
		// 清除 agent loop 对话历史
		if registry.historyClearer != nil {
			registry.historyClearer.ClearHistory(cmd.ChatID)
		}

		return &OutgoingMessage{
			ChatID:    cmd.ChatID,
			Text:      "🔄 会话已重置",
			ParseMode: "HTML",
		}, nil
	})

	// /stop 命令 - 停止当前运行 (对标 OpenClaw handleStopCommand)
	registry.Register("stop", func(ctx context.Context, cmd *Command) (*OutgoingMessage, error) {
		if registry.runController != nil {
			aborted := registry.runController.AbortRun(cmd.ChatID)
			if aborted {
				return &OutgoingMessage{
					ChatID:    cmd.ChatID,
					Text:      "⏹ 已停止",
					ParseMode: "HTML",
				}, nil
			}
		}
		return &OutgoingMessage{
			ChatID:    cmd.ChatID,
			Text:      "⏹ 没有正在运行的任务",
			ParseMode: "HTML",
		}, nil
	})

	// /whoami 命令 - 显示发送者 ID
	registry.Register("whoami", func(ctx context.Context, cmd *Command) (*OutgoingMessage, error) {
		return &OutgoingMessage{
			ChatID: cmd.ChatID,
			Text: fmt.Sprintf("🧭 <b>身份信息</b>\n\n渠道: Telegram\n用户 ID: <code>%d</code>\n会话 ID: <code>%d</code>",
				cmd.UserID, cmd.ChatID),
			ParseMode: "HTML",
		}, nil
	})

	// /commands 命令 - 列出所有已注册命令
	registry.Register("commands", func(ctx context.Context, cmd *Command) (*OutgoingMessage, error) {
		registry.mu.RLock()
		names := make([]string, 0, len(registry.handlers))
		for name := range registry.handlers {
			names = append(names, "/"+name)
		}
		registry.mu.RUnlock()
		// sort for stable output
		for i := 0; i < len(names); i++ {
			for j := i + 1; j < len(names); j++ {
				if names[i] > names[j] {
					names[i], names[j] = names[j], names[i]
				}
			}
		}
		text := fmt.Sprintf("📝 已注册命令 (%d):\n%s", len(names), strings.Join(names, "\n"))
		if len(text) > 4000 {
			text = text[:4000] + "\n..."
		}
		return &OutgoingMessage{ChatID: cmd.ChatID, Text: text, ParseMode: "HTML"}, nil
	})

	// /plugin 命令 - 插件命令分发 (对标 OpenClaw handlePluginCommand)

	// Aliases
	registry.Alias("n", "new")
	registry.Alias("h", "help")
	registry.Alias("id", "whoami")
	registry.Alias("abort", "stop")
	registry.Alias("cancel", "stop")
}
