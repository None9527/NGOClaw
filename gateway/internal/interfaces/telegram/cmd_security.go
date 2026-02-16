// Copyright 2026 NGOClaw. All rights reserved.

package telegram

import (
	"context"
	"fmt"
	"strings"

	"github.com/ngoclaw/ngoclaw/gateway/internal/infrastructure/config"
)

// SecurityController exposes security policy operations to TG commands.
type SecurityController interface {
	SetApprovalMode(mode string)
	GetConfig() config.SecurityConfig
	TrustTool(name string)
	UntrustTool(name string)
	TrustCommand(cmd string)
}

// registerSecurityCommands registers /security, /trust, /untrust commands.
func (a *Adapter) registerSecurityCommands(registry *CommandRegistry, ctrl SecurityController) {
	// /security [auto|ask|strict]
	registry.Register("security", func(ctx context.Context, cmd *Command) (*OutgoingMessage, error) {
		if cmd.RawArgs == "" {
			return buildSecurityStatus(cmd.ChatID, ctrl), nil
		}

		mode := strings.TrimSpace(strings.ToLower(cmd.RawArgs))
		switch mode {
		case "auto":
			ctrl.SetApprovalMode("auto")
		case "ask", "ask_dangerous":
			ctrl.SetApprovalMode("ask_dangerous")
		case "strict", "ask_all", "all":
			ctrl.SetApprovalMode("ask_all")
		default:
			return &OutgoingMessage{
				ChatID:    cmd.ChatID,
				Text:      fmt.Sprintf("❌ 未知模式: <code>%s</code>\n可用: <code>auto</code> | <code>ask</code> | <code>strict</code>", mode),
				ParseMode: "HTML",
			}, nil
		}

		return buildSecurityStatus(cmd.ChatID, ctrl), nil
	})

	// /trust <tool_name|cmd:command_name>
	registry.Register("trust", func(ctx context.Context, cmd *Command) (*OutgoingMessage, error) {
		if cmd.RawArgs == "" {
			return &OutgoingMessage{
				ChatID:    cmd.ChatID,
				Text:      "用法: /trust &lt;工具名&gt; 或 /trust cmd:&lt;命令名&gt;",
				ParseMode: "HTML",
			}, nil
		}

		name := strings.TrimSpace(cmd.RawArgs)
		if strings.HasPrefix(name, "cmd:") {
			cmdName := strings.TrimPrefix(name, "cmd:")
			ctrl.TrustCommand(cmdName)
			return &OutgoingMessage{
				ChatID:    cmd.ChatID,
				Text:      fmt.Sprintf("✅ 已信任命令: <code>%s</code>", cmdName),
				ParseMode: "HTML",
			}, nil
		}

		ctrl.TrustTool(name)
		return &OutgoingMessage{
			ChatID:    cmd.ChatID,
			Text:      fmt.Sprintf("✅ 已信任工具: <code>%s</code>", name),
			ParseMode: "HTML",
		}, nil
	})

	// /untrust <tool_name>
	registry.Register("untrust", func(ctx context.Context, cmd *Command) (*OutgoingMessage, error) {
		if cmd.RawArgs == "" {
			return &OutgoingMessage{
				ChatID:    cmd.ChatID,
				Text:      "用法: /untrust &lt;工具名&gt;",
				ParseMode: "HTML",
			}, nil
		}

		name := strings.TrimSpace(cmd.RawArgs)
		ctrl.UntrustTool(name)
		return &OutgoingMessage{
			ChatID:    cmd.ChatID,
			Text:      fmt.Sprintf("🔓 已取消信任: <code>%s</code>", name),
			ParseMode: "HTML",
		}, nil
	})

	// Callback handler for inline keyboard mode switching
	registry.Register("security_mode", func(ctx context.Context, cmd *Command) (*OutgoingMessage, error) {
		mode := strings.TrimSpace(cmd.RawArgs)
		switch mode {
		case "auto":
			ctrl.SetApprovalMode("auto")
		case "ask_dangerous":
			ctrl.SetApprovalMode("ask_dangerous")
		case "ask_all":
			ctrl.SetApprovalMode("ask_all")
		}
		return buildSecurityStatus(cmd.ChatID, ctrl), nil
	})
}

// buildSecurityStatus builds the security status message with toggleable inline keyboard.
func buildSecurityStatus(chatID int64, ctrl SecurityController) *OutgoingMessage {
	cfg := ctrl.GetConfig()

	// Mode label + toggle indicators (checkmark on current mode)
	modeLabel := "❓ 未知"
	var autoIcon, askIcon, strictIcon string
	switch cfg.ApprovalMode {
	case "auto":
		modeLabel = "🟢 全自动"
		autoIcon = "✅ "
	case "ask_dangerous":
		modeLabel = "⚠️ 确认危险操作"
		askIcon = "✅ "
	case "ask_all":
		modeLabel = "🔴 全部确认"
		strictIcon = "✅ "
	}

	trustedStr := "无"
	if len(cfg.TrustedTools) > 0 {
		trustedStr = strings.Join(cfg.TrustedTools, ", ")
	}
	dangerousStr := "无"
	if len(cfg.DangerousTools) > 0 {
		dangerousStr = strings.Join(cfg.DangerousTools, ", ")
	}
	trustedCmdStr := "无"
	if len(cfg.TrustedCommands) > 0 {
		if len(cfg.TrustedCommands) > 8 {
			trustedCmdStr = strings.Join(cfg.TrustedCommands[:8], ", ") + "..."
		} else {
			trustedCmdStr = strings.Join(cfg.TrustedCommands, ", ")
		}
	}

	text := fmt.Sprintf(
		"🔒 <b>安全策略</b>\n━━━━━━━━━━━━━\n"+
			"当前模式: %s\n\n"+
			"📗 <b>信任工具</b>: <code>%s</code>\n"+
			"📕 <b>危险工具</b>: <code>%s</code>\n"+
			"📘 <b>信任命令</b>: <code>%s</code>\n\n"+
			"<i>点击下方按钮切换模式:</i>",
		modeLabel, trustedStr, dangerousStr, trustedCmdStr,
	)

	// Build toggleable inline keyboard
	keyboard := BuildInlineKeyboard([][]InlineButton{
		{
			{Text: autoIcon + "🟢 全自动", CallbackData: "/security_mode auto"},
			{Text: askIcon + "⚠️ 危险确认", CallbackData: "/security_mode ask_dangerous"},
			{Text: strictIcon + "🔴 全部确认", CallbackData: "/security_mode ask_all"},
		},
	})

	return &OutgoingMessage{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: &keyboard,
	}
}
