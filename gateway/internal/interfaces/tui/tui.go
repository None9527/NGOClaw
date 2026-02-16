package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/entity"
	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/service"
	"go.uber.org/zap"
)

// TUI is a rich terminal user interface for the NGOClaw agent.
// It processes agent events and renders them with ANSI styling.
//
// Bubbletea integration deferred — this provides production-grade
// formatted output compatible with both raw terminal and pipe mode.
type TUI struct {
	agentLoop  *service.AgentLoop
	toolExec   service.ToolExecutor
	model      string
	sessionID  string
	logger     *zap.Logger
}

// ANSI styling constants
const (
	reset   = "\033[0m"
	bold    = "\033[1m"
	dim     = "\033[2m"
	italic  = "\033[3m"

	fgCyan    = "\033[36m"
	fgGreen   = "\033[32m"
	fgYellow  = "\033[33m"
	fgRed     = "\033[31m"
	fgMagenta = "\033[35m"
	fgGray    = "\033[90m"
	fgWhite   = "\033[97m"

	bgCyan    = "\033[46m"
	bgMagenta = "\033[45m"
)

// Config holds TUI configuration
type Config struct {
	Model     string
	SessionID string
	UserName  string
}

// New creates a new TUI instance
func New(agentLoop *service.AgentLoop, toolExec service.ToolExecutor, cfg Config, logger *zap.Logger) *TUI {
	session := cfg.SessionID
	if session == "" {
		session = fmt.Sprintf("tui_%d", time.Now().UnixNano())
	}

	return &TUI{
		agentLoop: agentLoop,
		toolExec:  toolExec,
		model:     cfg.Model,
		sessionID: session,
		logger:    logger,
	}
}

// PrintBanner displays the NGOClaw TUI header
func (t *TUI) PrintBanner() {
	fmt.Printf("\n%s%s ╔═══════════════════════════════════╗ %s\n", bold, bgCyan, reset)
	fmt.Printf("%s%s ║     🐾 NGOClaw Agent v0.2.0       ║ %s\n", bold, bgCyan, reset)
	fmt.Printf("%s%s ╚═══════════════════════════════════╝ %s\n", bold, bgCyan, reset)
	fmt.Printf("%s Model: %s │ Session: %s%s\n\n", fgGray, t.model, t.sessionID[:16], reset)
}

// RunMessage sends a message through the agent loop and renders events
func (t *TUI) RunMessage(ctx context.Context, systemPrompt, userMessage string, history []service.LLMMessage) (*service.AgentResult, error) {
	// Print user message
	fmt.Printf("%s%s▶ You%s\n", bold, fgGreen, reset)
	fmt.Printf("  %s\n\n", userMessage)

	result, eventCh := t.agentLoop.Run(ctx, systemPrompt, userMessage, history, nil)

	// Render each event
	for event := range eventCh {
		t.renderEvent(event)
	}

	// Print summary
	t.renderSummary(result)
	return result, nil
}

func (t *TUI) renderEvent(event entity.AgentEvent) {
	switch event.Type {
	case entity.EventThinking:
		fmt.Printf("%s%s💭 Thinking%s\n", dim, fgMagenta, reset)
		for _, line := range strings.Split(event.Content, "\n") {
			fmt.Printf("  %s%s%s\n", fgGray, line, reset)
		}
		fmt.Println()

	case entity.EventTextDelta:
		fmt.Print(event.Content) // Stream inline

	case entity.EventToolCall:
		if event.ToolCall != nil {
			fmt.Printf("\n%s%s🔧 %s%s", bold, fgYellow, event.ToolCall.Name, reset)
			if len(event.ToolCall.Arguments) > 0 {
				fmt.Printf(" %s(", fgGray)
				i := 0
				for k, v := range event.ToolCall.Arguments {
					if i > 0 {
						fmt.Print(", ")
					}
					vStr := fmt.Sprintf("%v", v)
					if len(vStr) > 60 {
						vStr = vStr[:57] + "..."
					}
					fmt.Printf("%s=%s", k, vStr)
					i++
				}
				fmt.Printf(")%s", reset)
			}
			fmt.Println()
		}

	case entity.EventToolResult:
		if event.ToolCall != nil {
			icon := "✅"
			color := fgGreen
			if !event.ToolCall.Success {
				icon = "❌"
				color = fgRed
			}
			fmt.Printf("  %s%s %s%s", color, icon, event.ToolCall.Name, reset)
			if event.ToolCall.Duration > 0 {
				fmt.Printf(" %s(%s)%s", fgGray, event.ToolCall.Duration.Round(time.Millisecond), reset)
			}
			fmt.Println()

			// Show output (truncated for TUI)
			output := event.ToolCall.Output
			if len(output) > 500 {
				output = output[:497] + "..."
			}
			if output != "" {
				lines := strings.Split(output, "\n")
				maxLines := 10
				if len(lines) > maxLines {
					for _, line := range lines[:maxLines] {
						fmt.Printf("  %s│ %s%s\n", fgGray, line, reset)
					}
					fmt.Printf("  %s│ ... (%d more lines)%s\n", fgGray, len(lines)-maxLines, reset)
				} else {
					for _, line := range lines {
						fmt.Printf("  %s│ %s%s\n", fgGray, line, reset)
					}
				}
			}
			fmt.Println()
		}

	case entity.EventStepDone:
		if event.StepInfo != nil {
			fmt.Printf("%s  ── step %d │ %d tokens │ %s ──%s\n",
				fgGray, event.StepInfo.Step,
				event.StepInfo.TokensUsed, event.StepInfo.ModelUsed, reset)
		}

	case entity.EventError:
		fmt.Printf("\n%s%s⚠ Error: %s%s\n\n", bold, fgRed, event.Error, reset)

	case entity.EventDone:
		fmt.Printf("\n%s%s🤖 Assistant%s\n", bold, fgCyan, reset)
	}
}

func (t *TUI) renderSummary(result *service.AgentResult) {
	fmt.Printf("\n%s%s────────────────────────────────────%s\n", dim, fgGray, reset)
	fmt.Printf("%s  Steps: %d │ Tokens: %d │ Model: %s%s\n",
		fgGray, result.TotalSteps, result.TotalTokens, result.ModelUsed, reset)
	if len(result.ToolsUsed) > 0 {
		fmt.Printf("%s  Tools: %s%s\n", fgGray, strings.Join(result.ToolsUsed, ", "), reset)
	}
	fmt.Printf("%s────────────────────────────────────%s\n\n", fgGray, reset)
}
