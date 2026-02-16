package telegram

import (
	"context"
	"fmt"
	"strings"
)

// registerAdminCommands registers admin/infrastructure: config, debug, restart, allowlist, subagents, plugin, tts
func (a *Adapter) registerAdminCommands(registry *CommandRegistry) {
	registry.Register("config", func(ctx context.Context, cmd *Command) (*OutgoingMessage, error) {
		if registry.configManager != nil && !registry.configManager.IsFeatureEnabled("config") {
			return &OutgoingMessage{
				ChatID:    cmd.ChatID,
				Text:      "⚠️ /config is disabled. Set commands.config=true to enable.",
				ParseMode: "HTML",
			}, nil
		}
		if len(cmd.Args) == 0 {
			// /config → show full config
			if registry.configManager == nil {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: "⚠️ Config manager not available."}, nil
			}
			json := registry.configManager.GetConfigJSON()
			if len(json) > 4000 {
				json = json[:4000] + "\n...(truncated)"
			}
			return &OutgoingMessage{
				ChatID:    cmd.ChatID,
				Text:      fmt.Sprintf("⚙️ Config (raw):\n<pre>%s</pre>", json),
				ParseMode: "HTML",
			}, nil
		}
		action := strings.ToLower(cmd.Args[0])
		switch action {
		case "show":
			path := ""
			if len(cmd.Args) > 1 {
				path = cmd.Args[1]
			}
			if registry.configManager == nil {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: "⚠️ Config manager not available."}, nil
			}
			value, err := registry.configManager.GetConfigValue(path)
			if err != nil {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: fmt.Sprintf("⚠️ %s", err.Error())}, nil
			}
			return &OutgoingMessage{
				ChatID:    cmd.ChatID,
				Text:      fmt.Sprintf("⚙️ Config %s:\n%v", path, value),
				ParseMode: "HTML",
			}, nil
		case "set":
			if len(cmd.Args) < 3 {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: "⚙️ Usage: /config set <path> <value>"}, nil
			}
			path := cmd.Args[1]
			value := strings.Join(cmd.Args[2:], " ")
			if registry.configManager == nil {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: "⚠️ Config manager not available."}, nil
			}
			if err := registry.configManager.SetConfigValue(path, value); err != nil {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: fmt.Sprintf("⚠️ %s", err.Error())}, nil
			}
			return &OutgoingMessage{
				ChatID: cmd.ChatID,
				Text:   fmt.Sprintf("⚙️ Config updated: %s=%s", path, value),
			}, nil
		case "unset":
			if len(cmd.Args) < 2 {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: "⚙️ Usage: /config unset <path>"}, nil
			}
			path := cmd.Args[1]
			if registry.configManager == nil {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: "⚠️ Config manager not available."}, nil
			}
			if err := registry.configManager.UnsetConfigValue(path); err != nil {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: fmt.Sprintf("⚠️ %s", err.Error())}, nil
			}
			return &OutgoingMessage{
				ChatID: cmd.ChatID,
				Text:   fmt.Sprintf("⚙️ Config updated: %s removed.", path),
			}, nil
		default:
			return &OutgoingMessage{
				ChatID: cmd.ChatID,
				Text:   "⚙️ Usage: /config [show <path>|set <path> <value>|unset <path>]",
			}, nil
		}
	})

	// /debug 命令 - 调试覆盖 (对标 OpenClaw handleDebugCommand)
	registry.Register("debug", func(ctx context.Context, cmd *Command) (*OutgoingMessage, error) {
		if registry.configManager != nil && !registry.configManager.IsFeatureEnabled("debug") {
			return &OutgoingMessage{
				ChatID: cmd.ChatID,
				Text:   "⚠️ /debug is disabled. Set commands.debug=true to enable.",
			}, nil
		}
		if len(cmd.Args) == 0 {
			// /debug → show overrides
			if registry.configManager == nil {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: "⚙️ Debug overrides: (none)"}, nil
			}
			overrides := registry.configManager.GetDebugOverrides()
			if len(overrides) == 0 {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: "⚙️ Debug overrides: (none)"}, nil
			}
			return &OutgoingMessage{
				ChatID: cmd.ChatID,
				Text:   fmt.Sprintf("⚙️ Debug overrides (memory-only):\n%v", overrides),
			}, nil
		}
		action := strings.ToLower(cmd.Args[0])
		switch action {
		case "show":
			if registry.configManager == nil {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: "⚙️ Debug overrides: (none)"}, nil
			}
			overrides := registry.configManager.GetDebugOverrides()
			if len(overrides) == 0 {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: "⚙️ Debug overrides: (none)"}, nil
			}
			return &OutgoingMessage{ChatID: cmd.ChatID, Text: fmt.Sprintf("⚙️ Debug overrides:\n%v", overrides)}, nil
		case "set":
			if len(cmd.Args) < 3 {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: "⚙️ Usage: /debug set <path> <value>"}, nil
			}
			path := cmd.Args[1]
			value := strings.Join(cmd.Args[2:], " ")
			if registry.configManager == nil {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: "⚠️ Config manager not available."}, nil
			}
			if err := registry.configManager.SetDebugOverride(path, value); err != nil {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: fmt.Sprintf("⚠️ %s", err.Error())}, nil
			}
			return &OutgoingMessage{ChatID: cmd.ChatID, Text: fmt.Sprintf("⚙️ Debug override set: %s=%s", path, value)}, nil
		case "unset":
			if len(cmd.Args) < 2 {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: "⚙️ Usage: /debug unset <path>"}, nil
			}
			path := cmd.Args[1]
			if registry.configManager == nil {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: "⚠️ Config manager not available."}, nil
			}
			if err := registry.configManager.UnsetDebugOverride(path); err != nil {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: fmt.Sprintf("⚠️ %s", err.Error())}, nil
			}
			return &OutgoingMessage{ChatID: cmd.ChatID, Text: fmt.Sprintf("⚙️ Debug override removed: %s", path)}, nil
		case "reset":
			if registry.configManager != nil {
				registry.configManager.ResetDebugOverrides()
			}
			return &OutgoingMessage{ChatID: cmd.ChatID, Text: "⚙️ Debug overrides cleared; using config on disk."}, nil
		default:
			return &OutgoingMessage{
				ChatID: cmd.ChatID,
				Text:   "⚙️ Usage: /debug [show|set <path> <value>|unset <path>|reset]",
			}, nil
		}
	})

	// /restart 命令 - 重启网关 (对标 OpenClaw handleRestartCommand)
	registry.Register("restart", func(ctx context.Context, cmd *Command) (*OutgoingMessage, error) {
		if registry.configManager != nil && !registry.configManager.IsFeatureEnabled("restart") {
			return &OutgoingMessage{
				ChatID: cmd.ChatID,
				Text:   "⚠️ /restart is disabled. Set commands.restart=true to enable.",
			}, nil
		}
		// Signal restart (actual restart handled by process supervisor)
		return &OutgoingMessage{
			ChatID: cmd.ChatID,
			Text:   "🔄 Restart requested. The gateway will restart shortly.",
		}, nil
	})

	// /allowlist 命令 - 白名单管理 (对标 OpenClaw handleAllowlistCommand)
	registry.Register("allowlist", func(ctx context.Context, cmd *Command) (*OutgoingMessage, error) {
		scope := "dm"
		if len(cmd.Args) == 0 {
			// /allowlist → list dm
			if registry.allowlistManager == nil {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: "⚠️ Allowlist manager not available."}, nil
			}
			entries, policy, err := registry.allowlistManager.ListAllowlist(cmd.ChatID, scope)
			if err != nil {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: fmt.Sprintf("⚠️ %s", err.Error())}, nil
			}
			text := fmt.Sprintf("🧾 Allowlist\nChannel: telegram\nPolicy: %s\nEntries: %s", policy, strings.Join(entries, ", "))
			if len(entries) == 0 {
				text = fmt.Sprintf("🧾 Allowlist\nChannel: telegram\nPolicy: %s\nEntries: (none)", policy)
			}
			return &OutgoingMessage{ChatID: cmd.ChatID, Text: text}, nil
		}
		action := strings.ToLower(cmd.Args[0])
		// Parse optional scope
		if len(cmd.Args) > 1 {
			s := strings.ToLower(cmd.Args[1])
			if s == "dm" || s == "group" || s == "all" {
				scope = s
			}
		}
		switch action {
		case "list":
			if registry.allowlistManager == nil {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: "⚠️ Allowlist manager not available."}, nil
			}
			entries, policy, err := registry.allowlistManager.ListAllowlist(cmd.ChatID, scope)
			if err != nil {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: fmt.Sprintf("⚠️ %s", err.Error())}, nil
			}
			list := "(none)"
			if len(entries) > 0 {
				list = strings.Join(entries, ", ")
			}
			return &OutgoingMessage{ChatID: cmd.ChatID, Text: fmt.Sprintf("🧾 Allowlist (%s)\nPolicy: %s\nEntries: %s", scope, policy, list)}, nil
		case "add":
			if len(cmd.Args) < 2 {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: "⚙️ Usage: /allowlist add <entry>"}, nil
			}
			entry := cmd.Args[len(cmd.Args)-1]
			if registry.allowlistManager == nil {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: "⚠️ Allowlist manager not available."}, nil
			}
			if err := registry.allowlistManager.AddAllowlist(cmd.ChatID, scope, entry); err != nil {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: fmt.Sprintf("⚠️ %s", err.Error())}, nil
			}
			return &OutgoingMessage{ChatID: cmd.ChatID, Text: fmt.Sprintf("✅ Allowlist added: %s", entry)}, nil
		case "remove":
			if len(cmd.Args) < 2 {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: "⚙️ Usage: /allowlist remove <entry>"}, nil
			}
			entry := cmd.Args[len(cmd.Args)-1]
			if registry.allowlistManager == nil {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: "⚠️ Allowlist manager not available."}, nil
			}
			if err := registry.allowlistManager.RemoveAllowlist(cmd.ChatID, scope, entry); err != nil {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: fmt.Sprintf("⚠️ %s", err.Error())}, nil
			}
			return &OutgoingMessage{ChatID: cmd.ChatID, Text: fmt.Sprintf("✅ Allowlist removed: %s", entry)}, nil
		default:
			return &OutgoingMessage{
				ChatID: cmd.ChatID,
				Text:   "⚙️ Usage: /allowlist [list|add|remove] [dm|group] [entry]",
			}, nil
		}
	})

	// /subagents 命令 - 子代理管理 (对标 OpenClaw commands-subagents.ts)
	registry.Register("subagents", func(ctx context.Context, cmd *Command) (*OutgoingMessage, error) {
		if len(cmd.Args) == 0 || strings.ToLower(cmd.Args[0]) == "list" {
			if registry.subagentManager == nil {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: "🧭 Subagents: none for this session."}, nil
			}
			runs := registry.subagentManager.ListSubagents(cmd.ChatID)
			if len(runs) == 0 {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: "🧭 Subagents: none for this session."}, nil
			}
			active := 0
			for _, r := range runs {
				if r.Status == "running" {
					active++
				}
			}
			lines := []string{fmt.Sprintf("🧭 Subagents (current session)\nActive: %d · Done: %d", active, len(runs)-active)}
			for _, r := range runs {
				lines = append(lines, fmt.Sprintf("%d) %s · %s · %s · run %s", r.Index, r.Status, r.Label, r.Runtime, r.RunID))
			}
			return &OutgoingMessage{ChatID: cmd.ChatID, Text: strings.Join(lines, "\n")}, nil
		}
		action := strings.ToLower(cmd.Args[0])
		switch action {
		case "help":
			return &OutgoingMessage{ChatID: cmd.ChatID, Text: "🧭 Subagents\nUsage:\n- /subagents list\n- /subagents stop <id|#|all>\n- /subagents log <id|#> [limit]\n- /subagents info <id|#>\n- /subagents send <id|#> <message>"}, nil
		case "stop":
			if len(cmd.Args) < 2 {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: "⚙️ Usage: /subagents stop <id|#|all>"}, nil
			}
			if registry.subagentManager == nil {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: "⚠️ Subagent manager not available."}, nil
			}
			target := cmd.Args[1]
			if target == "all" || target == "*" {
				stopped, err := registry.subagentManager.StopAllSubagents(ctx, cmd.ChatID)
				if err != nil {
					return &OutgoingMessage{ChatID: cmd.ChatID, Text: fmt.Sprintf("⚠️ %s", err.Error())}, nil
				}
				label := "subagent"
				if stopped != 1 {
					label = "subagents"
				}
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: fmt.Sprintf("⚙️ Stopped %d %s.", stopped, label)}, nil
			}
			msg, err := registry.subagentManager.StopSubagent(ctx, cmd.ChatID, target)
			if err != nil {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: fmt.Sprintf("⚠️ %s", err.Error())}, nil
			}
			return &OutgoingMessage{ChatID: cmd.ChatID, Text: msg}, nil
		case "info":
			if len(cmd.Args) < 2 {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: "ℹ️ Usage: /subagents info <id|#>"}, nil
			}
			if registry.subagentManager == nil {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: "⚠️ Subagent manager not available."}, nil
			}
			info, err := registry.subagentManager.SubagentInfo(cmd.ChatID, cmd.Args[1])
			if err != nil {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: fmt.Sprintf("⚠️ %s", err.Error())}, nil
			}
			return &OutgoingMessage{ChatID: cmd.ChatID, Text: info}, nil
		case "log":
			if len(cmd.Args) < 2 {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: "📜 Usage: /subagents log <id|#> [limit]"}, nil
			}
			if registry.subagentManager == nil {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: "⚠️ Subagent manager not available."}, nil
			}
			limit := 20
			if len(cmd.Args) > 2 {
				if n := parsePageNumber(cmd.Args[2]); n > 0 {
					limit = n
				}
			}
			log, err := registry.subagentManager.SubagentLog(cmd.ChatID, cmd.Args[1], limit)
			if err != nil {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: fmt.Sprintf("⚠️ %s", err.Error())}, nil
			}
			return &OutgoingMessage{ChatID: cmd.ChatID, Text: log}, nil
		case "send":
			if len(cmd.Args) < 3 {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: "✉️ Usage: /subagents send <id|#> <message>"}, nil
			}
			if registry.subagentManager == nil {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: "⚠️ Subagent manager not available."}, nil
			}
			target := cmd.Args[1]
			message := strings.Join(cmd.Args[2:], " ")
			reply, err := registry.subagentManager.SendToSubagent(ctx, cmd.ChatID, target, message)
			if err != nil {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: fmt.Sprintf("⚠️ Send failed: %s", err.Error())}, nil
			}
			return &OutgoingMessage{ChatID: cmd.ChatID, Text: reply}, nil
		default:
			return &OutgoingMessage{ChatID: cmd.ChatID, Text: "🧭 Subagents\nUsage:\n- /subagents list\n- /subagents stop <id|#|all>\n- /subagents log <id|#> [limit]\n- /subagents info <id|#>\n- /subagents send <id|#> <message>"}, nil
		}
	})

	// /commands 命令 - 列出所有已注册命令 (对标 OpenClaw handleCommandsCommand)
	registry.Register("plugin", func(ctx context.Context, cmd *Command) (*OutgoingMessage, error) {
		if registry.pluginManager == nil {
			return &OutgoingMessage{ChatID: cmd.ChatID, Text: "⚠️ Plugin manager not available."}, nil
		}
		normalized := "/plugin " + strings.Join(cmd.Args, " ")
		matched, args, ok := registry.pluginManager.MatchCommand(strings.TrimSpace(normalized))
		if !ok {
			return &OutgoingMessage{ChatID: cmd.ChatID, Text: "⚠️ No matching plugin command."}, nil
		}
		result, err := registry.pluginManager.ExecuteCommand(ctx, matched, args, cmd.ChatID)
		if err != nil {
			return &OutgoingMessage{ChatID: cmd.ChatID, Text: fmt.Sprintf("❌ Plugin error: %s", err.Error())}, nil
		}
		return &OutgoingMessage{ChatID: cmd.ChatID, Text: result}, nil
	})

	// /tts 命令 - TTS 控制 (对标 OpenClaw commands-tts.ts)
	registry.Register("tts", func(ctx context.Context, cmd *Command) (*OutgoingMessage, error) {
		action := "status"
		if len(cmd.Args) > 0 {
			action = strings.ToLower(cmd.Args[0])
		}
		switch action {
		case "help":
			return &OutgoingMessage{
				ChatID: cmd.ChatID,
				Text: "🔊 TTS Help\n\n" +
					"• /tts on — Enable TTS\n" +
					"• /tts off — Disable TTS\n" +
					"• /tts status — Show settings\n" +
					"• /tts provider [name] — View/change provider\n" +
					"• /tts limit [number] — View/change text limit\n" +
					"• /tts summary [on|off] — Auto-summary toggle\n" +
					"• /tts audio <text> — Generate audio",
			}, nil
		case "on":
			if registry.ttsController != nil {
				registry.ttsController.SetEnabled(cmd.ChatID, true)
			}
			return &OutgoingMessage{ChatID: cmd.ChatID, Text: "🔊 TTS enabled."}, nil
		case "off":
			if registry.ttsController != nil {
				registry.ttsController.SetEnabled(cmd.ChatID, false)
			}
			return &OutgoingMessage{ChatID: cmd.ChatID, Text: "🔇 TTS disabled."}, nil
		case "provider":
			if registry.ttsController == nil {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: "⚠️ TTS controller not available."}, nil
			}
			if len(cmd.Args) < 2 {
				current := registry.ttsController.GetProvider(cmd.ChatID)
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: fmt.Sprintf("🎙️ TTS provider: %s\nUsage: /tts provider openai|elevenlabs|edge", current)}, nil
			}
			provider := strings.ToLower(cmd.Args[1])
			if err := registry.ttsController.SetProvider(cmd.ChatID, provider); err != nil {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: fmt.Sprintf("⚠️ %s", err.Error())}, nil
			}
			return &OutgoingMessage{ChatID: cmd.ChatID, Text: fmt.Sprintf("✅ TTS provider set to %s.", provider)}, nil
		case "limit":
			if registry.ttsController == nil {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: "⚠️ TTS controller not available."}, nil
			}
			if len(cmd.Args) < 2 {
				current := registry.ttsController.GetLimit(cmd.ChatID)
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: fmt.Sprintf("📏 TTS limit: %d characters.\nRange: 100-4096.\nUsage: /tts limit <number>", current)}, nil
			}
			n := parsePageNumber(cmd.Args[1])
			if n < 100 || n > 4096 {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: "❌ Limit must be between 100 and 4096."}, nil
			}
			if err := registry.ttsController.SetLimit(cmd.ChatID, n); err != nil {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: fmt.Sprintf("⚠️ %s", err.Error())}, nil
			}
			return &OutgoingMessage{ChatID: cmd.ChatID, Text: fmt.Sprintf("✅ TTS limit set to %d characters.", n)}, nil
		case "summary":
			if registry.ttsController == nil {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: "⚠️ TTS controller not available."}, nil
			}
			if len(cmd.Args) < 2 {
				enabled := registry.ttsController.IsSummaryEnabled(cmd.ChatID)
				label := "off"
				if enabled {
					label = "on"
				}
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: fmt.Sprintf("📝 TTS auto-summary: %s.\nUsage: /tts summary on|off", label)}, nil
			}
			on := strings.ToLower(cmd.Args[1]) == "on"
			registry.ttsController.SetSummaryEnabled(cmd.ChatID, on)
			label := "disabled"
			if on {
				label = "enabled"
			}
			return &OutgoingMessage{ChatID: cmd.ChatID, Text: fmt.Sprintf("✅ TTS auto-summary %s.", label)}, nil
		case "audio":
			if len(cmd.Args) < 2 {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: "🎤 Usage: /tts audio <text>"}, nil
			}
			if registry.ttsController == nil {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: "⚠️ TTS controller not available."}, nil
			}
			text := strings.Join(cmd.Args[1:], " ")
			result, err := registry.ttsController.GenerateAudio(ctx, cmd.ChatID, text)
			if err != nil {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: fmt.Sprintf("❌ Error: %s", err.Error())}, nil
			}
			return &OutgoingMessage{ChatID: cmd.ChatID, Text: result}, nil
		case "status":
			if registry.ttsController == nil {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: "📊 TTS status\nState: ❌ disabled\nProvider: n/a"}, nil
			}
			status := registry.ttsController.GetStatus(cmd.ChatID)
			if status == nil {
				return &OutgoingMessage{ChatID: cmd.ChatID, Text: "📊 TTS status\nState: ❌ disabled"}, nil
			}
			state := "❌ disabled"
			if status.Enabled {
				state = "✅ enabled"
			}
			providerReady := "❌"
			if status.ProviderReady {
				providerReady = "✅"
			}
			summaryLabel := "off"
			if status.AutoSummary {
				summaryLabel = "on"
			}
			return &OutgoingMessage{
				ChatID: cmd.ChatID,
				Text: fmt.Sprintf("📊 TTS status\nState: %s\nProvider: %s (%s configured)\nText limit: %d chars\nAuto-summary: %s",
					state, status.Provider, providerReady, status.TextLimit, summaryLabel),
			}, nil
		default:
			return &OutgoingMessage{
				ChatID: cmd.ChatID,
				Text: "🔊 TTS Help\n\n" +
					"• /tts on|off\n" +
					"• /tts status\n" +
					"• /tts provider [name]\n" +
					"• /tts limit [number]\n" +
					"• /tts summary [on|off]\n" +
					"• /tts audio <text>",
			}, nil
		}
	})

	// 注册命令别名

	// Aliases
	registry.Alias("sa", "subagents")
	registry.Alias("ptt", "tts")
}
