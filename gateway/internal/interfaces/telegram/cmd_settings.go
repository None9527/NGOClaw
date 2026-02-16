package telegram

import (
	"context"
	"fmt"
	"strings"
)

// registerSettingsCommands registers session settings: think, verbose, reasoning, activation, sendpolicy
func (a *Adapter) registerSettingsCommands(registry *CommandRegistry) {
	// _think_set — internal handler for inline keyboard callbacks
	registry.Register("_think_set", func(ctx context.Context, cmd *Command) (*OutgoingMessage, error) {
		if len(cmd.Args) == 0 {
			return nil, nil
		}
		level := cmd.Args[0]
		if registry.sessionSettings != nil {
			registry.sessionSettings.SetThinkLevel(cmd.ChatID, level)
		}
		return buildThinkStatus(cmd.ChatID, level), nil
	})

	registry.Register("think", func(ctx context.Context, cmd *Command) (*OutgoingMessage, error) {
		current := "medium"
		if registry.sessionSettings != nil {
			if v := registry.sessionSettings.GetThinkLevel(cmd.ChatID); v != "" {
				current = v
			}
		}
		if len(cmd.Args) == 0 {
			return buildThinkStatus(cmd.ChatID, current), nil
		}
		level := strings.ToLower(cmd.Args[0])
		valid := map[string]bool{"off": true, "low": true, "medium": true, "high": true}
		if !valid[level] {
			return &OutgoingMessage{
				ChatID:    cmd.ChatID,
				Text:      "⚙️ 用法: /think off|low|medium|high",
				ParseMode: "HTML",
			}, nil
		}
		if registry.sessionSettings != nil {
			registry.sessionSettings.SetThinkLevel(cmd.ChatID, level)
		}
		return buildThinkStatus(cmd.ChatID, level), nil
	})

	// /verbose 命令 - 详细模式 (对标 OpenClaw verbose toggle)
	registry.Register("verbose", func(ctx context.Context, cmd *Command) (*OutgoingMessage, error) {
		current := false
		if registry.sessionSettings != nil {
			current = registry.sessionSettings.GetVerbose(cmd.ChatID)
		}
		if len(cmd.Args) == 0 {
			// toggle
			next := !current
			if registry.sessionSettings != nil {
				registry.sessionSettings.SetVerbose(cmd.ChatID, next)
			}
			label := "off"
			if next {
				label = "on"
			}
			return &OutgoingMessage{
				ChatID:    cmd.ChatID,
				Text:      fmt.Sprintf("📝 详细模式: %s", label),
				ParseMode: "HTML",
			}, nil
		}
		mode := strings.ToLower(cmd.Args[0])
		on := mode == "on" || mode == "true" || mode == "1"
		if registry.sessionSettings != nil {
			registry.sessionSettings.SetVerbose(cmd.ChatID, on)
		}
		label := "off"
		if on {
			label = "on"
		}
		return &OutgoingMessage{
			ChatID:    cmd.ChatID,
			Text:      fmt.Sprintf("📝 详细模式: %s", label),
			ParseMode: "HTML",
		}, nil
	})

	// /reasoning 命令 - 推理可见性 (对标 OpenClaw reasoning levels)
	registry.Register("reasoning", func(ctx context.Context, cmd *Command) (*OutgoingMessage, error) {
		current := "off"
		if registry.sessionSettings != nil {
			if v := registry.sessionSettings.GetReasoning(cmd.ChatID); v != "" {
				current = v
			}
		}
		if len(cmd.Args) == 0 {
			return &OutgoingMessage{
				ChatID: cmd.ChatID,
				Text: fmt.Sprintf("💭 <b>推理可见性</b>\n\n当前: %s\n\n用法: /reasoning on|off|stream", current),
				ParseMode: "HTML",
			}, nil
		}
		mode := strings.ToLower(cmd.Args[0])
		valid := map[string]bool{"on": true, "off": true, "stream": true}
		if !valid[mode] {
			return &OutgoingMessage{
				ChatID:    cmd.ChatID,
				Text:      "⚙️ 用法: /reasoning on|off|stream",
				ParseMode: "HTML",
			}, nil
		}
		if registry.sessionSettings != nil {
			registry.sessionSettings.SetReasoning(cmd.ChatID, mode)
		}
		return &OutgoingMessage{
			ChatID:    cmd.ChatID,
			Text:      fmt.Sprintf("💭 推理可见性: %s", mode),
			ParseMode: "HTML",
		}, nil
	})

	// /activation 命令 - 群组激活模式 (对标 OpenClaw handleActivationCommand)
	registry.Register("activation", func(ctx context.Context, cmd *Command) (*OutgoingMessage, error) {
		if len(cmd.Args) == 0 {
			current := "always"
			if registry.sessionSettings != nil {
				if v := registry.sessionSettings.GetActivation(cmd.ChatID); v != "" {
					current = v
				}
			}
			return &OutgoingMessage{
				ChatID: cmd.ChatID,
				Text: fmt.Sprintf("⚙️ <b>群组激活模式</b>\n\n当前: %s\n\n用法: /activation mention|always", current),
				ParseMode: "HTML",
			}, nil
		}
		mode := strings.ToLower(cmd.Args[0])
		if mode != "mention" && mode != "always" {
			return &OutgoingMessage{
				ChatID:    cmd.ChatID,
				Text:      "⚙️ 用法: /activation mention|always",
				ParseMode: "HTML",
			}, nil
		}
		if registry.sessionSettings != nil {
			registry.sessionSettings.SetActivation(cmd.ChatID, mode)
		}
		return &OutgoingMessage{
			ChatID:    cmd.ChatID,
			Text:      fmt.Sprintf("⚙️ 群组激活模式: %s", mode),
			ParseMode: "HTML",
		}, nil
	})

	// /sendpolicy 命令 - 发送策略 (对标 OpenClaw handleSendPolicyCommand)
	registry.Register("sendpolicy", func(ctx context.Context, cmd *Command) (*OutgoingMessage, error) {
		if len(cmd.Args) == 0 {
			current := "inherit"
			if registry.sessionSettings != nil {
				if v := registry.sessionSettings.GetSendPolicy(cmd.ChatID); v != "" {
					current = v
				}
			}
			return &OutgoingMessage{
				ChatID: cmd.ChatID,
				Text: fmt.Sprintf("⚙️ <b>发送策略</b>\n\n当前: %s\n\n用法: /sendpolicy on|off|inherit", current),
				ParseMode: "HTML",
			}, nil
		}
		arg := strings.ToLower(cmd.Args[0])
		// normalize: on→allow, off→deny
		policy := arg
		switch arg {
		case "on":
			policy = "allow"
		case "off":
			policy = "deny"
		}
		valid := map[string]bool{"allow": true, "deny": true, "inherit": true}
		if !valid[policy] {
			return &OutgoingMessage{
				ChatID:    cmd.ChatID,
				Text:      "⚙️ 用法: /sendpolicy on|off|inherit",
				ParseMode: "HTML",
			}, nil
		}
		if registry.sessionSettings != nil {
			registry.sessionSettings.SetSendPolicy(cmd.ChatID, policy)
		}
		label := policy
		if policy == "allow" {
			label = "on"
		} else if policy == "deny" {
			label = "off"
		}
		return &OutgoingMessage{
			ChatID:    cmd.ChatID,
			Text:      fmt.Sprintf("⚙️ 发送策略: %s", label),
			ParseMode: "HTML",
		}, nil
	})


	// /compact 命令 - 压缩上下文

	// Aliases
	registry.Alias("t", "think")
	registry.Alias("thinking", "think")
	registry.Alias("v", "verbose")
	registry.Alias("reason", "reasoning")
}

// buildThinkStatus builds the think level message with toggleable inline keyboard.
func buildThinkStatus(chatID int64, current string) *OutgoingMessage {
	labels := map[string]string{
		"off":    "关闭",
		"low":    "低",
		"medium": "中",
		"high":   "高",
	}
	currentLabel := labels[current]
	if currentLabel == "" {
		currentLabel = current
	}

	// Build checkmark icons
	icons := map[string]string{"off": "", "low": "", "medium": "", "high": ""}
	icons[current] = "✅ "

	text := fmt.Sprintf("🧠 <b>思考级别</b>\n\n当前: %s\n\n<i>点击下方按钮切换:</i>", currentLabel)

	keyboard := BuildInlineKeyboard([][]InlineButton{
		{
			{Text: icons["off"] + "关闭", CallbackData: "/_think_set off"},
			{Text: icons["low"] + "低", CallbackData: "/_think_set low"},
			{Text: icons["medium"] + "中", CallbackData: "/_think_set medium"},
			{Text: icons["high"] + "高", CallbackData: "/_think_set high"},
		},
	})

	return &OutgoingMessage{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: &keyboard,
	}
}
