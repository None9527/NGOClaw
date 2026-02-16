package telegram

import (
	"context"
	"fmt"
	"strings"
)

// registerContextCommands registers context management: compact, context
func (a *Adapter) registerContextCommands(registry *CommandRegistry) {
	registry.Register("compact", func(ctx context.Context, cmd *Command) (*OutgoingMessage, error) {
		if registry.contextController == nil {
			return &OutgoingMessage{
				ChatID:    cmd.ChatID,
				Text:      "⚙️ 上下文压缩不可用",
				ParseMode: "HTML",
			}, nil
		}

		// 先中止活跃运行 (对标 OpenClaw: abort active run before compacting)
		if registry.runController != nil {
			registry.runController.AbortRun(cmd.ChatID)
		}

		instructions := strings.Join(cmd.Args, " ")
		tokensBefore, tokensAfter, err := registry.contextController.CompactContext(ctx, cmd.ChatID, instructions)
		if err != nil {
			return &OutgoingMessage{
				ChatID:    cmd.ChatID,
				Text:      fmt.Sprintf("⚙️ 压缩失败: %s", err.Error()),
				ParseMode: "HTML",
			}, nil
		}

		var label string
		if tokensBefore > 0 && tokensAfter > 0 {
			label = fmt.Sprintf("已压缩 (%s → %s)", formatTokenCount(tokensBefore), formatTokenCount(tokensAfter))
		} else {
			label = "已压缩"
		}

		return &OutgoingMessage{
			ChatID:    cmd.ChatID,
			Text:      fmt.Sprintf("⚙️ %s", label),
			ParseMode: "HTML",
		}, nil
	})

	// /context 命令 - 上下文统计 (对标 OpenClaw handleContextCommand)
	registry.Register("context", func(ctx context.Context, cmd *Command) (*OutgoingMessage, error) {
		stats := &ContextStats{MaxTokens: 128000}
		if registry.contextController != nil {
			if s := registry.contextController.GetContextStats(cmd.ChatID); s != nil {
				stats = s
			}
		}

		usagePercent := 0.0
		if stats.MaxTokens > 0 {
			usagePercent = float64(stats.TokenCount) / float64(stats.MaxTokens) * 100
		}

		text := fmt.Sprintf("📝 <b>上下文</b>\n\n"+
			"消息数: %d\n"+
			"Tokens: %s / %s (%.1f%%)\n"+
			"\n使用 /compact 压缩上下文",
			stats.MessageCount,
			formatTokenCount(stats.TokenCount),
			formatTokenCount(stats.MaxTokens),
			usagePercent)

		return &OutgoingMessage{
			ChatID:    cmd.ChatID,
			Text:      text,
			ParseMode: "HTML",
		}, nil
	})

	// /skill 命令 - 运行技能

	// Aliases
	registry.Alias("c", "compact")
	registry.Alias("ctx", "context")
}
