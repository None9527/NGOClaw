package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/chzyer/readline"

	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/entity"
	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/service"
	"github.com/ngoclaw/ngoclaw/gateway/internal/infrastructure/prompt"
	"golang.org/x/term"
)

// ─── ANSI Helpers ───

const (
	reset    = "\033[0m"
	bold     = "\033[1m"
	dim      = "\033[2m"
	italic   = "\033[3m"
	cyan     = "\033[96m"
	cyanBold = "\033[96m\033[1m"
	green    = "\033[92m"
	yellow   = "\033[93m"
	red      = "\033[91m"
	redBold  = "\033[91m\033[1m"
	dimText  = "\033[90m"
	white    = "\033[97m"
	clearLn  = "\033[2K\r"
)

// Braille spinner frames (Gemini CLI style)
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// REPLConfig holds CLI runtime config
type REPLConfig struct {
	Model      string
	Workspace  string
	ToolCount  int
	NoApprove  bool
	InitPrompt string
}

// RunREPL starts the interactive REPL loop
func RunREPL(
	agentLoop *service.AgentLoop,
	promptEngine *prompt.PromptEngine,
	cfg REPLConfig,
) error {
	w := termWidth()
	banner := RenderBanner(BannerInfo{
		Model:      cfg.Model,
		ToolCount:  cfg.ToolCount,
		Workspace:  cfg.Workspace,
		ProjectLng: DetectProjectLanguage(cfg.Workspace),
	}, w)
	fmt.Println(banner)

	// Readline for proper line editing (backspace, arrows, history)
	rl, err := readline.NewEx(&readline.Config{
		Prompt:          "\001\033[1;36m\002❯\001\033[0m\002 ",
		HistoryFile:      "",
		InterruptPrompt: "^C",
		EOFPrompt:       "exit",
	})
	if err != nil {
		return fmt.Errorf("readline init: %w", err)
	}
	defer rl.Close()

	var history []service.LLMMessage

	// Handle Ctrl+C for clean exit
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Printf("\n%s👋 再见%s\n", dimText, reset)
		rl.Close()
		os.Exit(0)
	}()

	// If initial prompt provided, run it first
	if cfg.InitPrompt != "" {
		history = runAgent(agentLoop, promptEngine, cfg, cfg.InitPrompt, history)
	}

	// REPL loop
	for {
		input, err := rl.Readline()
		if err != nil {
			if err == readline.ErrInterrupt {
				fmt.Printf("%s👋 再见%s\n", dimText, reset)
				return nil
			}
			if err == io.EOF {
				fmt.Printf("\n%s👋 再见%s\n", dimText, reset)
				return nil
			}
			return nil
		}
		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}

		// Slash command
		if cmd := ParseSlashCommand(input); cmd != nil {
			result := ExecuteCommand(cmd, cfg.Model, cfg.ToolCount)
			if result.IsQuit {
				fmt.Printf("%s👋 再见%s\n", dimText, reset)
				return nil
			}
			if result.IsReset {
				history = nil
			}
			if result.Output != "" {
				fmt.Println(result.Output)
			}
			continue
		}

		// Agent query
		history = runAgent(agentLoop, promptEngine, cfg, input, history)
	}
}

// ─── Agent Execution ───

func runAgent(
	agentLoop *service.AgentLoop,
	promptEngine *prompt.PromptEngine,
	cfg REPLConfig,
	userMessage string,
	history []service.LLMMessage,
) []service.LLMMessage {
	// Build system prompt
	systemPrompt := ""
	if promptEngine != nil {
		systemPrompt = promptEngine.Assemble(prompt.PromptContext{
			Channel:     "cli",
			ModelName:   cfg.Model,
			UserMessage: userMessage,
			Workspace:   cfg.Workspace,
		})
	}

	// Context with cancel for Ctrl+C during streaming
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGINT)
		select {
		case <-ch:
			cancel()
			fmt.Printf("\n%s⏹ 已中断%s\n", yellow, reset)
		case <-ctx.Done():
		}
	}()

	result, eventCh := agentLoop.Run(ctx, systemPrompt, userMessage, history, "")

	var textBuf strings.Builder
	stepCount := 0
	totalTokens := 0
	w := termWidth()

	// Spinner state
	spinner := newSpinner()

	for event := range eventCh {
		switch event.Type {
		case entity.EventTextDelta:
			spinner.Stop()
			fmt.Print(event.Content)
			textBuf.WriteString(event.Content)

		case entity.EventThinking:
			if event.Content != "" {
				first := firstLine(event.Content, 50)
				spinner.Update(fmt.Sprintf("thinking: %s", first))
			} else {
				spinner.Update("thinking...")
			}

		case entity.EventToolCall:
			spinner.Stop()
			if event.ToolCall != nil {
				printToolHeader(event.ToolCall, w)
				spinner.Update(fmt.Sprintf("%s running...", event.ToolCall.Name))
			}

		case entity.EventToolResult:
			spinner.Stop()
			if event.ToolCall != nil {
				printToolFooter(event.ToolCall, w)
			}

		case entity.EventStepDone:
			if event.StepInfo != nil {
				stepCount = event.StepInfo.Step
				totalTokens = event.StepInfo.TokensUsed
			}



		case entity.EventError:
			spinner.Stop()
			fmt.Printf("\n%s✗ %s%s\n", redBold, event.Error, reset)

		case entity.EventDone:
			spinner.Stop()
		}
	}
	spinner.Stop()

	// Ensure trailing newline
	if textBuf.Len() > 0 && !strings.HasSuffix(textBuf.String(), "\n") {
		fmt.Println()
	}

	// Summary line
	if result != nil && result.TotalSteps > 0 {
		fmt.Printf("\n%s─── %d steps · %s tokens · %s ───%s\n",
			dimText, result.TotalSteps, fmtTokens(result.TotalTokens), result.ModelUsed, reset)
	} else if stepCount > 0 {
		fmt.Printf("\n%s─── %d steps · %s tokens ───%s\n",
			dimText, stepCount, fmtTokens(totalTokens), reset)
	}

	// Update history
	finalContent := textBuf.String()
	if finalContent != "" {
		history = append(history,
			service.LLMMessage{Role: "user", Content: userMessage},
			service.LLMMessage{Role: "assistant", Content: finalContent},
		)
	}

	return history
}

// ─── Tool Display (Gemini CLI style) ───

// printToolHeader renders: ╭─ ⊷ tool_name description ──────
func printToolHeader(tc *entity.ToolCallEvent, width int) {
	if tc == nil {
		return
	}
	icon := toolIcon(tc.Name)
	args := summarizeToolArgs(tc.Arguments)

	// Header line
	label := fmt.Sprintf(" %s %s %s ", icon, tc.Name, args)
	lineW := width - len([]rune(label)) - 2
	if lineW < 3 {
		lineW = 3
	}
	line := strings.Repeat("─", lineW)

	fmt.Printf("\n%s╭─%s%s%s%s%s%s%s\n",
		dimText, reset,
		yellow, icon, reset,
		" "+cyanBold+tc.Name+reset+" "+dimText+args,
		" "+dimText+line,
		reset)
}

// printToolFooter renders: ╰─ ✓ tool_name (duration) ──────
func printToolFooter(tc *entity.ToolCallEvent, width int) {
	if tc == nil {
		return
	}

	var statusIcon, statusColor string
	if tc.Success {
		statusIcon = "✓"
		statusColor = green
	} else {
		statusIcon = "✗"
		statusColor = red
	}

	dur := ""
	if tc.Duration > 0 {
		dur = fmt.Sprintf(" %s(%s)%s", dimText, fmtDur(tc.Duration), reset)
	}

	label := fmt.Sprintf(" %s %s%s ", statusIcon, tc.Name, dur)
	lineW := width - len([]rune(label)) - 2
	if lineW < 3 {
		lineW = 3
	}
	line := strings.Repeat("─", lineW)

	fmt.Printf("%s╰─%s %s%s%s %s%s%s %s\n",
		dimText, reset,
		statusColor, statusIcon, reset,
		dimText, tc.Name, reset,
		dur+dimText+line+reset)
}

// printPlan renders a plan proposal in a box
func printPlan(content string, width int) {
	boxW := width - 4
	if boxW < 20 {
		boxW = 20
	}
	topLine := "╭─ 📋 Plan " + strings.Repeat("─", boxW-12) + "╮"
	botLine := "╰" + strings.Repeat("─", boxW) + "╯"

	fmt.Printf("\n%s%s%s\n", cyanBold, topLine, reset)

	for _, line := range strings.Split(content, "\n") {
		// Truncate if needed
		if len([]rune(line)) > boxW-4 {
			line = string([]rune(line)[:boxW-7]) + "..."
		}
		pad := boxW - 2 - len([]rune(line))
		if pad < 0 {
			pad = 0
		}
		fmt.Printf("%s│%s %s%s%s│%s\n",
			dimText, reset,
			line, strings.Repeat(" ", pad),
			dimText, reset)
	}

	fmt.Printf("%s%s%s\n", dimText, botLine, reset)
}

func toolIcon(name string) string {
	icons := map[string]string{
		"bash":         "$",
		"read_file":    "→",
		"write_file":   "←",
		"edit_file":    "←",
		"apply_patch":  "←",
		"list_dir":     "→",
		"search_files": "✱",
		"search_code":  "✱",
		"web_search":   "◈",
		"web_fetch":    "%",
		"python_exec":  "⟐",
		"create_file":  "+",
		"delete_file":  "×",
	}
	if icon, ok := icons[name]; ok {
		return icon
	}
	return "⚙"
}

func summarizeToolArgs(args map[string]interface{}) string {
	if len(args) == 0 {
		return ""
	}
	priority := []string{"command", "file_path", "path", "query", "url", "pattern"}
	for _, key := range priority {
		if v, ok := args[key]; ok {
			s := fmt.Sprintf("%v", v)
			if len(s) > 60 {
				s = s[:60] + "…"
			}
			return s
		}
	}
	for _, v := range args {
		s := fmt.Sprintf("%v", v)
		if len(s) > 60 {
			s = s[:60] + "…"
		}
		return s
	}
	return ""
}

// ─── Braille Spinner ───

type asyncSpinner struct {
	mu      sync.Mutex
	running bool
	msg     string
	stopCh  chan struct{}
	doneCh  chan struct{}
}

func newSpinner() *asyncSpinner {
	return &asyncSpinner{}
}

func (s *asyncSpinner) Update(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.msg = msg
	if !s.running {
		s.running = true
		s.stopCh = make(chan struct{})
		s.doneCh = make(chan struct{})
		go s.run()
	}
}

func (s *asyncSpinner) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	close(s.stopCh)
	doneCh := s.doneCh
	s.mu.Unlock()

	<-doneCh
	fmt.Print(clearLn) // Clear spinner line
}

func (s *asyncSpinner) run() {
	defer close(s.doneCh)

	frame := 0
	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.mu.Lock()
			msg := s.msg
			s.mu.Unlock()

			f := spinnerFrames[frame%len(spinnerFrames)]
			fmt.Printf("%s%s%s %s%s%s", clearLn, cyanBold, f, dimText, msg, reset)
			frame++
		}
	}
}

// ─── Helpers ───

func termWidth() int {
	w, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w <= 0 {
		return 80
	}
	return w
}

func firstLine(s string, maxLen int) string {
	first := strings.SplitN(s, "\n", 2)[0]
	r := []rune(first)
	if len(r) > maxLen {
		return string(r[:maxLen]) + "…"
	}
	return first
}

func fmtTokens(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000.0)
	}
	return fmt.Sprintf("%d", n)
}

func fmtDur(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}
