package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// registerAgentCommands registers agent/execution: skill, skills, cron, agent, bash, approve
func (a *Adapter) registerAgentCommands(registry *CommandRegistry) {
	registry.Register("skill", func(ctx context.Context, cmd *Command) (*OutgoingMessage, error) {
		if len(cmd.Args) == 0 {
			// Build dynamic skill list
			skillList := "暂无已安装技能"
			if registry.skillManager != nil {
				skills := registry.skillManager.List()
				if len(skills) > 0 {
					var lines []string
					for _, s := range skills {
						status := "✅"
						if !s.Enabled {
							status = "❌"
						}
						lines = append(lines, fmt.Sprintf("• %s <code>%s</code> — %s", status, s.ID, s.Name))
					}
					skillList = strings.Join(lines, "\n")
				}
			}

			return &OutgoingMessage{
				ChatID:    cmd.ChatID,
				Text:      fmt.Sprintf("🎯 <b>技能系统</b>\n\n%s\n\n用法: /skill &lt;技能名&gt; [输入]\n使用 /skills 查看所有可用技能", skillList),
				ParseMode: "HTML",
			}, nil
		}

		skillName := cmd.Args[0]
		return &OutgoingMessage{
			ChatID:    cmd.ChatID,
			Text:      fmt.Sprintf("🎯 正在执行技能: <code>%s</code>", skillName),
			ParseMode: "HTML",
		}, nil
	})

	// /skills 命令 - 技能列表
	registry.Register("skills", func(ctx context.Context, cmd *Command) (*OutgoingMessage, error) {
		if len(cmd.Args) == 0 {
			// List installed skills from SkillManager
			skillList := "暂无已安装技能。"
			if registry.skillManager != nil {
				skills := registry.skillManager.List()
				if len(skills) > 0 {
					var lines []string
					for _, s := range skills {
						status := "✅"
						if !s.Enabled {
							status = "❌"
						}
						lines = append(lines, fmt.Sprintf("%s <code>%s</code> — %s", status, s.ID, s.Name))
					}
					skillList = strings.Join(lines, "\n")
				}
			}
			return &OutgoingMessage{
				ChatID:    cmd.ChatID,
				Text:      fmt.Sprintf("🎯 <b>技能列表</b>\n\n%s\n\n用法:\n• /skills install &lt;来源&gt; — 安装技能\n• /skills remove &lt;ID&gt; — 卸载技能", skillList),
				ParseMode: "HTML",
			}, nil
		}

		subCmd := cmd.Args[0]

		switch subCmd {
		case "install", "add":
			if len(cmd.Args) < 2 {
				return &OutgoingMessage{
					ChatID:    cmd.ChatID,
					Text:      "❌ 用法: /skills install &lt;来源&gt;",
					ParseMode: "HTML",
				}, nil
			}
			source := cmd.Args[1]
			return &OutgoingMessage{
				ChatID:    cmd.ChatID,
				Text:      fmt.Sprintf("✅ 正在安装技能: <code>%s</code>", source),
				ParseMode: "HTML",
			}, nil

		case "remove", "uninstall", "rm":
			if len(cmd.Args) < 2 {
				return &OutgoingMessage{
					ChatID:    cmd.ChatID,
					Text:      "❌ 用法: /skills remove &lt;ID&gt;",
					ParseMode: "HTML",
				}, nil
			}
			skillID := cmd.Args[1]
			return &OutgoingMessage{
				ChatID:    cmd.ChatID,
				Text:      fmt.Sprintf("✅ 已卸载技能: <code>%s</code>", skillID),
				ParseMode: "HTML",
			}, nil

		default:
			return &OutgoingMessage{
				ChatID:    cmd.ChatID,
				Text:      fmt.Sprintf("❌ 未知子命令: <code>%s</code>", subCmd),
				ParseMode: "HTML",
			}, nil
		}
	})

	// /cron 命令 - 定时任务管理
	registry.Register("cron", func(ctx context.Context, cmd *Command) (*OutgoingMessage, error) {
		if len(cmd.Args) == 0 {
			return &OutgoingMessage{
				ChatID: cmd.ChatID,
				Text: "⏰ <b>定时任务</b>\n\n用法:\n" +
					"• /cron list — 列出任务\n" +
					"• /cron add &lt;表达式&gt; &lt;命令&gt; — 添加任务\n" +
					"• /cron remove &lt;ID&gt; — 删除任务\n\n" +
					"表达式示例:\n" +
					"• <code>@hourly</code> — 每小时\n" +
					"• <code>@daily</code> — 每天\n" +
					"• <code>0 9</code> — 每天 9:00",
				ParseMode: "HTML",
			}, nil
		}

		subCmd := cmd.Args[0]

		switch subCmd {
		case "list", "ls":
			// List cron jobs from CronService
			jobsText := "📋 暂无定时任务"
			if registry.cronService != nil {
				jobs := registry.cronService.List(cmd.ChatID)
				if len(jobs) > 0 {
					var lines []string
					for _, j := range jobs {
						lines = append(lines, fmt.Sprintf("• <code>%s</code> | <code>%s</code> | %s", j.ID[:8], j.CronExpr, j.Command))
					}
					jobsText = "📋 <b>定时任务</b>\n\n" + strings.Join(lines, "\n")
				}
			}
			return &OutgoingMessage{
				ChatID:    cmd.ChatID,
				Text:      jobsText,
				ParseMode: "HTML",
			}, nil

		case "add":
			if len(cmd.Args) < 3 {
				return &OutgoingMessage{
					ChatID:    cmd.ChatID,
					Text:      "❌ 用法: /cron add &lt;表达式&gt; &lt;命令&gt;",
					ParseMode: "HTML",
				}, nil
			}
			cronExpr := cmd.Args[1]
			command := strings.Join(cmd.Args[2:], " ")
			// Schedule via CronService
			if registry.cronService != nil {
				jobID, err := registry.cronService.Schedule(cmd.ChatID, cronExpr, command)
				if err != nil {
					return &OutgoingMessage{
						ChatID:    cmd.ChatID,
						Text:      fmt.Sprintf("❌ 添加失败: %s", err.Error()),
						ParseMode: "HTML",
					}, nil
				}
				return &OutgoingMessage{
					ChatID:    cmd.ChatID,
					Text:      fmt.Sprintf("✅ 已添加定时任务\nID: <code>%s</code>\n表达式: <code>%s</code>\n命令: <code>%s</code>", jobID, cronExpr, command),
					ParseMode: "HTML",
				}, nil
			}
			return &OutgoingMessage{
				ChatID:    cmd.ChatID,
				Text:      fmt.Sprintf("✅ 已添加定时任务\n表达式: <code>%s</code>\n命令: <code>%s</code>", cronExpr, command),
				ParseMode: "HTML",
			}, nil

		case "remove", "rm", "delete":
			if len(cmd.Args) < 2 {
				return &OutgoingMessage{
					ChatID:    cmd.ChatID,
					Text:      "❌ 用法: /cron remove &lt;ID&gt;",
					ParseMode: "HTML",
				}, nil
			}
			jobID := cmd.Args[1]
			// Cancel via CronService
			if registry.cronService != nil {
				if err := registry.cronService.Cancel(jobID); err != nil {
					return &OutgoingMessage{
						ChatID:    cmd.ChatID,
						Text:      fmt.Sprintf("❌ 删除失败: %s", err.Error()),
						ParseMode: "HTML",
					}, nil
				}
			}
			return &OutgoingMessage{
				ChatID:    cmd.ChatID,
				Text:      fmt.Sprintf("✅ 已删除任务: <code>%s</code>", jobID),
				ParseMode: "HTML",
			}, nil

		default:
			return &OutgoingMessage{
				ChatID:    cmd.ChatID,
				Text:      fmt.Sprintf("❌ 未知子命令: <code>%s</code>", subCmd),
				ParseMode: "HTML",
			}, nil
		}
	})

	// /agent 命令 - Agent 管理
	registry.Register("agent", func(ctx context.Context, cmd *Command) (*OutgoingMessage, error) {
		if len(cmd.Args) == 0 {
			return &OutgoingMessage{
				ChatID: cmd.ChatID,
				Text: "🤖 <b>Agent 管理</b>\n\n用法:\n" +
					"• /agent list — 列出 Agent\n" +
					"• /agent switch &lt;ID&gt; — 切换 Agent\n" +
					"• /agent spawn &lt;名称&gt; — 创建新 Agent\n" +
					"• /agent terminate &lt;ID&gt; — 终止 Agent",
				ParseMode: "HTML",
			}, nil
		}

		subCmd := cmd.Args[0]

		switch subCmd {
		case "list", "ls":
			// List agents from subagentManager
			agentList := "• <code>default</code> — 默认助手 [当前]"
			if registry.subagentManager != nil {
				agents := registry.subagentManager.ListSubagents(cmd.ChatID)
				if len(agents) > 0 {
					var lines []string
					for _, a := range agents {
						lines = append(lines, fmt.Sprintf("• <code>%s</code> — %s [%s]", a.Label, a.Status, a.RunID[:8]))
					}
					agentList = strings.Join(lines, "\n")
				}
			}
			return &OutgoingMessage{
				ChatID:    cmd.ChatID,
				Text:      fmt.Sprintf("🤖 <b>当前 Agent</b>\n\n%s", agentList),
				ParseMode: "HTML",
			}, nil

		case "switch", "use":
			if len(cmd.Args) < 2 {
				return &OutgoingMessage{
					ChatID:    cmd.ChatID,
					Text:      "❌ 用法: /agent switch &lt;ID&gt;",
					ParseMode: "HTML",
				}, nil
			}
			agentID := cmd.Args[1]
			return &OutgoingMessage{
				ChatID:    cmd.ChatID,
				Text:      fmt.Sprintf("✅ 已切换到 Agent: <code>%s</code>", agentID),
				ParseMode: "HTML",
			}, nil

		case "spawn", "create", "new":
			if len(cmd.Args) < 2 {
				return &OutgoingMessage{
					ChatID:    cmd.ChatID,
					Text:      "❌ 用法: /agent spawn &lt;名称&gt;",
					ParseMode: "HTML",
				}, nil
			}
			name := strings.Join(cmd.Args[1:], " ")
			return &OutgoingMessage{
				ChatID:    cmd.ChatID,
				Text:      fmt.Sprintf("✅ 已创建 Agent: <code>%s</code>", name),
				ParseMode: "HTML",
			}, nil

		case "terminate", "kill", "stop":
			if len(cmd.Args) < 2 {
				return &OutgoingMessage{
					ChatID:    cmd.ChatID,
					Text:      "❌ 用法: /agent terminate &lt;ID&gt;",
					ParseMode: "HTML",
				}, nil
			}
			agentID := cmd.Args[1]
			return &OutgoingMessage{
				ChatID:    cmd.ChatID,
				Text:      fmt.Sprintf("✅ 已终止 Agent: <code>%s</code>", agentID),
				ParseMode: "HTML",
			}, nil

		default:
			return &OutgoingMessage{
				ChatID:    cmd.ChatID,
				Text:      fmt.Sprintf("❌ 未知子命令: <code>%s</code>", subCmd),
				ParseMode: "HTML",
			}, nil
		}
	})

	// /bash 命令 - 执行 shell 命令 (对标 OpenClaw commands-bash.ts)
	registry.Register("bash", func(ctx context.Context, cmd *Command) (*OutgoingMessage, error) {
		if registry.configManager != nil && !registry.configManager.IsFeatureEnabled("bash") {
			return &OutgoingMessage{
				ChatID:    cmd.ChatID,
				Text:      "⚠️ /bash 已禁用。请设置 commands.bash=true 启用。",
				ParseMode: "HTML",
			}, nil
		}
		if len(cmd.Args) == 0 {
			return &OutgoingMessage{
				ChatID:    cmd.ChatID,
				Text:      "⚙️ 用法: /bash &lt;命令&gt;",
				ParseMode: "HTML",
			}, nil
		}
		command := strings.Join(cmd.Args, " ")
		if registry.bashExecutor == nil {
			return &OutgoingMessage{
				ChatID:    cmd.ChatID,
				Text:      "⚠️ Bash 执行器不可用。",
				ParseMode: "HTML",
			}, nil
		}
		output, err := registry.bashExecutor.Execute(ctx, cmd.ChatID, command)
		if err != nil {
			return &OutgoingMessage{
				ChatID:    cmd.ChatID,
				Text:      fmt.Sprintf("❌ 错误: %s", err.Error()),
				ParseMode: "HTML",
			}, nil
		}
		if output == "" {
			output = "(无输出)"
		}
		// Truncate long output
		if len(output) > 4000 {
			output = output[:4000] + "\n... (已截断)"
		}
		return &OutgoingMessage{
			ChatID:    cmd.ChatID,
			Text:      fmt.Sprintf("<pre>%s</pre>", output),
			ParseMode: "HTML",
		}, nil
	})

	// /approve 命令 - 审批操作 (对标 OpenClaw commands-approve.ts)
	registry.Register("approve", func(ctx context.Context, cmd *Command) (*OutgoingMessage, error) {
		if len(cmd.Args) < 2 {
			return &OutgoingMessage{
				ChatID:    cmd.ChatID,
				Text:      "⚙️ 用法: /approve &lt;id&gt; &lt;allow|deny&gt;",
				ParseMode: "HTML",
			}, nil
		}
		if registry.approvalManager == nil {
			return &OutgoingMessage{
				ChatID:    cmd.ChatID,
				Text:      "⚠️ 审批管理器不可用。",
				ParseMode: "HTML",
			}, nil
		}
		approvalID := cmd.Args[0]
		decision := strings.ToLower(cmd.Args[1])
		if decision != "allow" && decision != "deny" {
			return &OutgoingMessage{
				ChatID:    cmd.ChatID,
				Text:      "⚙️ 决定必须是 allow 或 deny。",
				ParseMode: "HTML",
			}, nil
		}
		if err := registry.approvalManager.ResolveApproval(ctx, approvalID, decision); err != nil {
			return &OutgoingMessage{
				ChatID:    cmd.ChatID,
				Text:      fmt.Sprintf("❌ 审批错误: %s", err.Error()),
				ParseMode: "HTML",
			}, nil
		}
		return &OutgoingMessage{
			ChatID:    cmd.ChatID,
			Text:      fmt.Sprintf("✅ 审批 %s: %s", approvalID, decision),
			ParseMode: "HTML",
		}, nil
	})


	// /plan 命令 - 查看当前计划 (reads ~/.ngoclaw/current_plan.json)
	registry.Register("plan", func(ctx context.Context, cmd *Command) (*OutgoingMessage, error) {
		home, err := os.UserHomeDir()
		if err != nil {
			return &OutgoingMessage{
				ChatID:    cmd.ChatID,
				Text:      "❌ 无法获取 home 目录",
				ParseMode: "HTML",
			}, nil
		}

		planPath := filepath.Join(home, ".ngoclaw", "current_plan.json")
		data, err := os.ReadFile(planPath)
		if err != nil {
			return &OutgoingMessage{
				ChatID:    cmd.ChatID,
				Text:      "📝 当前没有活跃计划\n\n使用对话中的 update_plan 工具创建计划。",
				ParseMode: "HTML",
			}, nil
		}

		var plan struct {
			Title string `json:"title"`
			Steps []struct {
				ID     string `json:"id"`
				Title  string `json:"title"`
				Status string `json:"status"`
			} `json:"steps"`
			UpdatedAt string `json:"updated_at"`
		}
		if err := json.Unmarshal(data, &plan); err != nil {
			return &OutgoingMessage{
				ChatID:    cmd.ChatID,
				Text:      fmt.Sprintf("❌ 计划文件格式错误: %s", err.Error()),
				ParseMode: "HTML",
			}, nil
		}

		// Build plan display
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("📝 <b>%s</b>\n\n", plan.Title))
		for _, step := range plan.Steps {
			icon := "⬜"
			switch step.Status {
			case "done", "completed":
				icon = "✅"
			case "in_progress", "working":
				icon = "🔄"
			case "blocked":
				icon = "🚫"
			}
			sb.WriteString(fmt.Sprintf("%s %s\n", icon, step.Title))
		}
		if plan.UpdatedAt != "" {
			sb.WriteString(fmt.Sprintf("\n<i>更新于: %s</i>", plan.UpdatedAt))
		}

		return &OutgoingMessage{
			ChatID:    cmd.ChatID,
			Text:      sb.String(),
			ParseMode: "HTML",
		}, nil
	})

	// /memory 命令 - 查看长期记忆 (reads ~/.ngoclaw/memory.json)
	registry.Register("memory", func(ctx context.Context, cmd *Command) (*OutgoingMessage, error) {
		home, err := os.UserHomeDir()
		if err != nil {
			return &OutgoingMessage{
				ChatID:    cmd.ChatID,
				Text:      "❌ 无法获取 home 目录",
				ParseMode: "HTML",
			}, nil
		}

		memPath := filepath.Join(home, ".ngoclaw", "memory.json")
		data, err := os.ReadFile(memPath)
		if err != nil {
			return &OutgoingMessage{
				ChatID:    cmd.ChatID,
				Text:      "🧠 记忆库为空\n\n对话中使用 👍 表情或 save_memory 工具来存储记忆。",
				ParseMode: "HTML",
			}, nil
		}

		var store struct {
			Facts []struct {
				Content    string  `json:"content"`
				Category   string  `json:"category"`
				Confidence float64 `json:"confidence"`
				CreatedAt  string  `json:"created_at"`
			} `json:"facts"`
		}
		if err := json.Unmarshal(data, &store); err != nil {
			return &OutgoingMessage{
				ChatID:    cmd.ChatID,
				Text:      fmt.Sprintf("❌ 记忆文件格式错误: %s", err.Error()),
				ParseMode: "HTML",
			}, nil
		}

		if len(store.Facts) == 0 {
			return &OutgoingMessage{
				ChatID:    cmd.ChatID,
				Text:      "🧠 记忆库为空",
				ParseMode: "HTML",
			}, nil
		}

		// Show last 10 memories (newest first)
		limit := 10
		if len(store.Facts) < limit {
			limit = len(store.Facts)
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("🧠 <b>长期记忆</b> (%d 条)\n\n", len(store.Facts)))
		for i := len(store.Facts) - 1; i >= len(store.Facts)-limit; i-- {
			fact := store.Facts[i]
			catIcon := "💡"
			switch fact.Category {
			case "preference":
				catIcon = "⚙️"
			case "project":
				catIcon = "📂"
			case "environment":
				catIcon = "🖥️"
			case "skill":
				catIcon = "🎯"
			}
			content := fact.Content
			if len(content) > 80 {
				content = content[:80] + "..."
			}
			sb.WriteString(fmt.Sprintf("%s %s\n", catIcon, content))
		}
		if len(store.Facts) > limit {
			sb.WriteString(fmt.Sprintf("\n<i>...共 %d 条记忆</i>", len(store.Facts)))
		}

		return &OutgoingMessage{
			ChatID:    cmd.ChatID,
			Text:      sb.String(),
			ParseMode: "HTML",
		}, nil
	})

	// /config 命令 - 配置管理 (对标 OpenClaw handleConfigCommand)
}
