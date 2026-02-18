package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/entity"
)

// Renderer handles all output rendering: markdown, tool calls, diffs
type Renderer struct {
	glamour *glamour.TermRenderer
	width   int
}

// NewRenderer creates a renderer with the given terminal width
func NewRenderer(width int) *Renderer {
	if width <= 0 {
		width = 80
	}
	r, _ := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width-4),
	)
	return &Renderer{
		glamour: r,
		width:   width,
	}
}

// RenderMarkdown renders markdown text to styled terminal output
func (r *Renderer) RenderMarkdown(md string) string {
	if r.glamour == nil {
		return md
	}
	out, err := r.glamour.Render(md)
	if err != nil {
		return md
	}
	return strings.TrimSpace(out)
}

// RenderToolCall renders a tool call summary with spinner
func (r *Renderer) RenderToolCall(tc *entity.ToolCallEvent, spinnerFrame string) string {
	if tc == nil {
		return ""
	}

	iconStyle := lipgloss.NewStyle().Foreground(colorYellow).Bold(true)
	nameStyle := lipgloss.NewStyle().Foreground(colorCyan).Bold(true)
	argStyle := lipgloss.NewStyle().Foreground(colorGray)

	icon := iconStyle.Render(spinnerFrame)
	name := nameStyle.Render(tc.Name)

	// Extract key arguments for display
	argSummary := summarizeArgs(tc.Arguments)

	return fmt.Sprintf("  %s %s %s", icon, name, argStyle.Render(argSummary))
}

// RenderToolResult renders a completed tool call result
func (r *Renderer) RenderToolResult(tc *entity.ToolCallEvent) string {
	if tc == nil {
		return ""
	}

	var icon string
	if tc.Success {
		icon = lipgloss.NewStyle().Foreground(colorGreen).Render("✓")
	} else {
		icon = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF5F5F")).Render("✗")
	}

	nameStyle := lipgloss.NewStyle().Foreground(colorCyan)
	durStyle := lipgloss.NewStyle().Foreground(colorGray)

	dur := ""
	if tc.Duration > 0 {
		dur = durStyle.Render(fmt.Sprintf(" (%s)", formatDuration(tc.Duration)))
	}

	return fmt.Sprintf("  %s %s%s", icon, nameStyle.Render(tc.Name), dur)
}

// RenderApproval renders the approval prompt for a tool call
func (r *Renderer) RenderApproval(tc *entity.ToolCallEvent) string {
	if tc == nil {
		return ""
	}

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorYellow).
		Padding(0, 1).
		Width(r.width - 4)

	titleStyle := lipgloss.NewStyle().Foreground(colorYellow).Bold(true)
	nameStyle := lipgloss.NewStyle().Foreground(colorCyan).Bold(true)
	argStyle := lipgloss.NewStyle().Foreground(colorWhite)
	hintStyle := lipgloss.NewStyle().Foreground(colorGray)

	title := titleStyle.Render("⚠ 工具审批")
	content := fmt.Sprintf("%s\n\n工具: %s\n", title, nameStyle.Render(tc.Name))

	// Show key arguments
	for k, v := range tc.Arguments {
		valStr := fmt.Sprintf("%v", v)
		if len(valStr) > 200 {
			valStr = valStr[:200] + "..."
		}
		content += fmt.Sprintf("%s: %s\n",
			lipgloss.NewStyle().Foreground(colorGray).Render(k),
			argStyle.Render(valStr),
		)
	}

	content += "\n" + hintStyle.Render("[Y]es  [N]o  [A]lways")

	return boxStyle.Render(content)
}

// RenderThinking renders a thinking indicator
func (r *Renderer) RenderThinking(frame string) string {
	style := lipgloss.NewStyle().Foreground(colorDimCyan).Italic(true)
	return style.Render(fmt.Sprintf("  %s thinking...", frame))
}

// summarizeArgs extracts key args for compact display
func summarizeArgs(args map[string]interface{}) string {
	if len(args) == 0 {
		return ""
	}

	// Priority args to show
	priority := []string{"command", "file_path", "path", "query", "url", "content"}
	var parts []string

	for _, key := range priority {
		if v, ok := args[key]; ok {
			valStr := fmt.Sprintf("%v", v)
			if len(valStr) > 60 {
				valStr = valStr[:60] + "…"
			}
			parts = append(parts, valStr)
		}
	}

	if len(parts) == 0 {
		// Show first arg
		for _, v := range args {
			valStr := fmt.Sprintf("%v", v)
			if len(valStr) > 60 {
				valStr = valStr[:60] + "…"
			}
			parts = append(parts, valStr)
			break
		}
	}

	return strings.Join(parts, " ")
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}
