package cli

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// SlashCommand represents a parsed slash command
type SlashCommand struct {
	Name string
	Args []string
}

// ParseSlashCommand parses a slash command from user input
func ParseSlashCommand(input string) *SlashCommand {
	input = strings.TrimSpace(input)
	if !strings.HasPrefix(input, "/") {
		return nil
	}

	parts := strings.Fields(input)
	name := strings.TrimPrefix(parts[0], "/")
	var args []string
	if len(parts) > 1 {
		args = parts[1:]
	}

	return &SlashCommand{Name: name, Args: args}
}

// CommandResult is the output of executing a slash command
type CommandResult struct {
	Output  string
	IsQuit  bool
	IsReset bool
}

// ExecuteCommand handles slash commands and returns the result
func ExecuteCommand(cmd *SlashCommand, model string, toolCount int) CommandResult {
	switch cmd.Name {
	case "help", "h":
		return CommandResult{Output: renderHelp()}
	case "exit", "quit", "q":
		return CommandResult{IsQuit: true}
	case "new", "reset":
		return CommandResult{Output: "🔄 已清空对话历史", IsReset: true}
	case "status", "s":
		return CommandResult{Output: renderStatus(model, toolCount)}
	case "model", "m":
		if len(cmd.Args) == 0 {
			return CommandResult{Output: fmt.Sprintf("当前模型: %s\n用法: /model <model_name>", model)}
		}
		return CommandResult{Output: fmt.Sprintf("✓ 模型已切换为: %s", cmd.Args[0])}
	case "compact":
		return CommandResult{Output: "🗜 上下文已压缩"}
	case "think":
		level := "medium"
		if len(cmd.Args) > 0 {
			level = cmd.Args[0]
		}
		return CommandResult{Output: fmt.Sprintf("🧠 思考级别: %s", level)}
	case "version":
		return CommandResult{Output: fmt.Sprintf("NGOClaw v%s", appVersion)}
	default:
		return CommandResult{Output: fmt.Sprintf("未知命令: /%s  输入 /help 查看可用命令", cmd.Name)}
	}
}

func renderHelp() string {
	titleStyle := lipgloss.NewStyle().Foreground(colorCyan).Bold(true)
	cmdStyle := lipgloss.NewStyle().Foreground(colorGreen)
	descStyle := lipgloss.NewStyle().Foreground(colorGray)

	cmds := []struct {
		name string
		desc string
	}{
		{"/help", "显示此帮助"},
		{"/model [name]", "查看/切换模型"},
		{"/new", "清空对话历史"},
		{"/compact", "压缩上下文"},
		{"/status", "当前状态"},
		{"/think [level]", "思考级别 (off/low/medium/high)"},
		{"/version", "版本信息"},
		{"/exit", "退出"},
	}

	var sb strings.Builder
	sb.WriteString(titleStyle.Render("◇ 可用命令"))
	sb.WriteString("\n\n")

	for _, c := range cmds {
		sb.WriteString(fmt.Sprintf("  %s  %s\n",
			cmdStyle.Render(fmt.Sprintf("%-16s", c.name)),
			descStyle.Render(c.desc),
		))
	}

	return sb.String()
}

func renderStatus(model string, toolCount int) string {
	titleStyle := lipgloss.NewStyle().Foreground(colorCyan).Bold(true)
	labelStyle := lipgloss.NewStyle().Foreground(colorGray)
	valueStyle := lipgloss.NewStyle().Foreground(colorWhite)

	var sb strings.Builder
	sb.WriteString(titleStyle.Render("◇ 当前状态"))
	sb.WriteString("\n\n")
	sb.WriteString(fmt.Sprintf("  %s %s\n", labelStyle.Render("模型:"), valueStyle.Render(model)))
	sb.WriteString(fmt.Sprintf("  %s %s\n", labelStyle.Render("工具:"), valueStyle.Render(fmt.Sprintf("%d 已加载", toolCount))))

	return sb.String()
}
