package telegram

import (
	"context"
	"fmt"
	"strings"
)

// registerModelCommands registers model selection: models, usage
func (a *Adapter) registerModelCommands(registry *CommandRegistry) {
	// _setmodel — internal handler for inline keyboard callbacks only (not user-facing)
	registry.Register("_setmodel", func(ctx context.Context, cmd *Command) (*OutgoingMessage, error) {
		modelArg := strings.Join(cmd.Args, " ")
		if modelArg == "" {
			return &OutgoingMessage{ChatID: cmd.ChatID, Text: "⚠️ 未指定模型", ParseMode: "HTML"}, nil
		}

		if registry.sessionManager != nil {
			if err := registry.sessionManager.SetModel(cmd.ChatID, modelArg); err != nil {
				return &OutgoingMessage{
					ChatID:    cmd.ChatID,
					Text:      fmt.Sprintf("❌ 切换模型失败: %s", err.Error()),
					ParseMode: "HTML",
				}, nil
			}
		}

		return &OutgoingMessage{
			ChatID:    cmd.ChatID,
			Text:      fmt.Sprintf("✅ 已切换到模型: <code>%s</code>", modelArg),
			ParseMode: "HTML",
		}, nil
	})

	// /models 命令 - 浏览和切换模型 (inline keyboard)
	registry.Register("models", func(ctx context.Context, cmd *Command) (*OutgoingMessage, error) {
		var models []ModelInfo
		var currentModel string
		if registry.sessionManager != nil {
			models = registry.sessionManager.GetAvailableModels()
			currentModel = registry.sessionManager.GetCurrentModel(cmd.ChatID)
		}

		if len(models) == 0 {
			return &OutgoingMessage{
				ChatID:    cmd.ChatID,
				Text:      "📋 <b>可用模型</b>\n\n当前没有配置模型列表。\n\n请在配置文件中设置模型，或联系管理员。",
				ParseMode: "HTML",
			}, nil
		}

		// 按提供商分组
		byProvider := make(map[string][]ModelInfo)
		var providers []string
		for _, m := range models {
			if _, exists := byProvider[m.Provider]; !exists {
				providers = append(providers, m.Provider)
			}
			byProvider[m.Provider] = append(byProvider[m.Provider], m)
		}

		// 解析参数
		page := 0
		provider := ""
		if len(cmd.Args) > 0 {
			provider = cmd.Args[0]
		}
		if len(cmd.Args) > 1 {
			if p := parsePageNumber(cmd.Args[1]); p >= 0 {
				page = p
			}
		}

		// 无 provider 参数：显示当前模型 + 提供商选择
		if provider == "" {
			keyboard := BuildProviderKeyboard(providers)
			text := fmt.Sprintf("🤖 当前: <code>%s</code>\n\n📋 选择提供商:", currentModel)
			return &OutgoingMessage{
				ChatID:      cmd.ChatID,
				Text:        text,
				ParseMode:   "HTML",
				ReplyMarkup: &keyboard,
			}, nil
		}

		// 有 provider：显示该提供商的模型
		providerModels, exists := byProvider[provider]
		if !exists {
			return &OutgoingMessage{
				ChatID:    cmd.ChatID,
				Text:      fmt.Sprintf("❌ 未知提供商: <code>%s</code>", provider),
				ParseMode: "HTML",
			}, nil
		}

		const pageSize = 6
		keyboard := BuildModelsKeyboard(provider, providerModels, currentModel, page, pageSize)

		return &OutgoingMessage{
			ChatID:      cmd.ChatID,
			Text:        fmt.Sprintf("📋 <b>%s</b> 模型:", provider),
			ParseMode:   "HTML",
			ReplyMarkup: &keyboard,
		}, nil
	})

	// /usage 命令
	registry.Register("usage", func(ctx context.Context, cmd *Command) (*OutgoingMessage, error) {
		if len(cmd.Args) > 0 {
			arg := strings.ToLower(cmd.Args[0])
			if arg == "cost" {
				return &OutgoingMessage{
					ChatID:    cmd.ChatID,
					Text:      "💸 用量统计\n本次会话 n/a\n今日 n/a\n近30天 n/a",
					ParseMode: "HTML",
				}, nil
			}
			validModes := map[string]bool{"off": true, "tokens": true, "full": true}
			if !validModes[arg] {
				return &OutgoingMessage{
					ChatID:    cmd.ChatID,
					Text:      "⚙️ 用法: /usage off|tokens|full|cost",
					ParseMode: "HTML",
				}, nil
			}
			if registry.sessionSettings != nil {
				registry.sessionSettings.SetUsageMode(cmd.ChatID, arg)
			}
			return &OutgoingMessage{
				ChatID:    cmd.ChatID,
				Text:      fmt.Sprintf("⚙️ 用量显示: %s", arg),
				ParseMode: "HTML",
			}, nil
		}
		current := "off"
		if registry.sessionSettings != nil {
			current = registry.sessionSettings.GetUsageMode(cmd.ChatID)
		}
		next := "tokens"
		switch current {
		case "tokens":
			next = "full"
		case "full":
			next = "off"
		}
		if registry.sessionSettings != nil {
			registry.sessionSettings.SetUsageMode(cmd.ChatID, next)
		}
		return &OutgoingMessage{
			ChatID:    cmd.ChatID,
			Text:      fmt.Sprintf("⚙️ 用量显示: %s", next),
			ParseMode: "HTML",
		}, nil
	})

	// Aliases — /model redirects to /models for backward compat
	registry.Alias("m", "models")
	registry.Alias("model", "models")
}
