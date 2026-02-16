package repl

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ngoclaw/ngoclaw/gateway/internal/application/usecase"
	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/entity"
	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/valueobject"
	"go.uber.org/zap"
)

// ANSI color codes
const (
	colorReset  = "\033[0m"
	colorGreen  = "\033[32m"
	colorCyan   = "\033[36m"
	colorYellow = "\033[33m"
	colorGray   = "\033[90m"
	colorBold   = "\033[1m"
)

// REPL interactive command-line agent session
type REPL struct {
	usecase        *usecase.ProcessMessageUseCase
	logger         *zap.Logger
	conversationID string
	currentModel   string
	userName       string
}

// Config REPL configuration
type Config struct {
	DefaultModel string
	UserName     string
}

// New creates a new REPL instance
func New(uc *usecase.ProcessMessageUseCase, logger *zap.Logger, cfg Config) *REPL {
	model := cfg.DefaultModel
	if model == "" {
		model = "default"
	}
	userName := cfg.UserName
	if userName == "" {
		userName = "user"
	}

	return &REPL{
		usecase:        uc,
		logger:         logger,
		conversationID: fmt.Sprintf("repl_%d", time.Now().UnixNano()),
		currentModel:   model,
		userName:       userName,
	}
}

// Run starts the REPL loop
func (r *REPL) Run(ctx context.Context) error {
	r.printBanner()

	scanner := bufio.NewScanner(os.Stdin)
	// Allow long input lines
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for {
		fmt.Printf("%s%s> %s", colorGreen, r.userName, colorReset)

		if !scanner.Scan() {
			// EOF or error
			break
		}

		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}

		// Handle built-in commands
		if handled, shouldExit := r.handleCommand(input); handled {
			if shouldExit {
				return nil
			}
			continue
		}

		// Process message through usecase
		if err := r.processMessage(ctx, input); err != nil {
			fmt.Printf("%sError: %v%s\n", colorYellow, err, colorReset)
			r.logger.Error("REPL message processing failed", zap.Error(err))
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scanner error: %w", err)
	}

	fmt.Println("\nGoodbye!")
	return nil
}

// handleCommand processes built-in REPL commands
// Returns (handled, shouldExit)
func (r *REPL) handleCommand(input string) (bool, bool) {
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return false, false
	}

	cmd := strings.ToLower(parts[0])

	switch cmd {
	case "/exit", "/quit", "/q":
		fmt.Println("Goodbye!")
		return true, true

	case "/new":
		r.conversationID = fmt.Sprintf("repl_%d", time.Now().UnixNano())
		fmt.Printf("%s✓ New conversation started%s\n", colorCyan, colorReset)
		return true, false

	case "/model":
		if len(parts) > 1 {
			r.currentModel = parts[1]
			fmt.Printf("%s✓ Model switched to: %s%s\n", colorCyan, r.currentModel, colorReset)
		} else {
			fmt.Printf("%sCurrent model: %s%s\n", colorCyan, r.currentModel, colorReset)
		}
		return true, false

	case "/status":
		fmt.Printf("%s── Status ──%s\n", colorCyan, colorReset)
		fmt.Printf("  Conversation: %s\n", r.conversationID)
		fmt.Printf("  Model:        %s\n", r.currentModel)
		fmt.Printf("  User:         %s\n", r.userName)
		return true, false

	case "/help":
		r.printHelp()
		return true, false

	default:
		return false, false
	}
}

// processMessage sends user input through the ProcessMessageUseCase
func (r *REPL) processMessage(ctx context.Context, input string) error {
	user := valueobject.NewUser("repl_user", r.userName, "repl")
	content := valueobject.NewMessageContent(input, valueobject.ContentTypeText)

	msgID := fmt.Sprintf("repl_%d", time.Now().UnixNano())
	msg, err := entity.NewMessage(msgID, r.conversationID, content, user)
	if err != nil {
		return fmt.Errorf("create message: %w", err)
	}

	startTime := time.Now()
	response, err := r.usecase.Execute(ctx, msg)
	elapsed := time.Since(startTime)

	if err != nil {
		return err
	}

	if response == nil {
		fmt.Printf("%s(empty response)%s\n", colorGray, colorReset)
		return nil
	}

	// Print response
	fmt.Printf("\n%s%s🤖 Assistant%s\n", colorBold, colorCyan, colorReset)
	fmt.Println(response.Content().Text())
	fmt.Printf("%s(%s)%s\n\n", colorGray, elapsed.Round(time.Millisecond), colorReset)

	return nil
}

// printBanner displays the REPL welcome message
func (r *REPL) printBanner() {
	fmt.Printf("\n%s%s╔══════════════════════════════════╗%s\n", colorBold, colorCyan, colorReset)
	fmt.Printf("%s%s║       NGOClaw REPL v0.1.0        ║%s\n", colorBold, colorCyan, colorReset)
	fmt.Printf("%s%s╚══════════════════════════════════╝%s\n", colorBold, colorCyan, colorReset)
	fmt.Printf("%sModel: %s | Type /help for commands%s\n\n", colorGray, r.currentModel, colorReset)
}

// printHelp displays available commands
func (r *REPL) printHelp() {
	fmt.Printf("\n%s── Commands ──%s\n", colorCyan, colorReset)
	fmt.Println("  /new          Start a new conversation")
	fmt.Println("  /model [name] Show or switch current model")
	fmt.Println("  /status       Show current session status")
	fmt.Println("  /image <p>    Generate an image")
	fmt.Println("  /skill <id>   Execute a skill")
	fmt.Println("  /help         Show this help")
	fmt.Println("  /exit         Exit REPL")
	fmt.Println()
}
