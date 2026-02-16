package telegram

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// StagedReply implements Antigravity-style staged output for TG cards:
//
//	Phase 1 (Status): Show a single status message that updates in-place
//	  "🤔 思考中..."  →  "⚙️ bash_exec..."  →  "🔄 Step 2..."
//	Phase 2 (Deliver): Delete status message → send final complete reply
//
// This avoids the flickering edit-in-place streaming that breaks TG card UX.
type StagedReply struct {
	bot       *tgbotapi.BotAPI
	chatID    int64
	statusID  int    // message ID of the status message (0 = not yet sent)
	lastText  string // last status text (dedup)
	parseMode string
	mu        sync.Mutex

	// Throttle for status updates (avoid TG rate limit)
	throttleMs int64
	lastUpdate int64

	// Accumulated tool history for status display
	toolHistory []string
	activeTool  string
	toolCount   int
	stepInfo    string
}

// NewStagedReply creates a staged reply handler
func NewStagedReply(bot *tgbotapi.BotAPI, chatID int64) *StagedReply {
	return &StagedReply{
		bot:        bot,
		chatID:     chatID,
		throttleMs: 1500, // 1.5s — status updates don't need to be fast
		parseMode:  "HTML",
	}
}

// SetThrottle sets the throttle interval for status updates
func (s *StagedReply) SetThrottle(ms int64) {
	s.throttleMs = ms
}

// StatusThinking shows the initial "thinking" status
func (s *StagedReply) StatusThinking() error {
	return s.updateStatus("🤔 _思考中..._")
}

// StatusToolStart shows that a tool is being executed with human-readable label
func (s *StagedReply) StatusToolStart(toolName string, args map[string]interface{}) error {
	s.mu.Lock()
	s.activeTool = toolDisplayLabel(toolName, args)
	s.mu.Unlock()
	return s.forceStatusRefresh()
}

// StatusToolDone marks a tool as completed with human-readable label
func (s *StagedReply) StatusToolDone(toolName string, args map[string]interface{}, success bool) error {
	s.mu.Lock()
	icon := "✅"
	if !success {
		icon = "❌"
	}
	s.toolHistory = append(s.toolHistory, fmt.Sprintf("%s %s", icon, toolDisplayLabel(toolName, args)))
	s.toolCount++
	s.activeTool = ""
	s.mu.Unlock()
	return s.forceStatusRefresh()
}

// StatusStep shows step progress
func (s *StagedReply) StatusStep(step, maxSteps int) error {
	s.mu.Lock()
	if maxSteps > 0 {
		s.stepInfo = fmt.Sprintf("Step %d/%d", step, maxSteps)
	}
	s.mu.Unlock()
	return s.forceStatusRefresh()
}

// StatusCustom sets an arbitrary status message (throttled)
func (s *StagedReply) StatusCustom(text string) error {
	return s.updateStatus(text)
}

// buildStatusText composes the current status display with numbered steps.
// Output format like Antigravity progress:
//   1. ✅ 搜索: searxng docker compose
//   2. ✅ webfetch
//   🔄 3. 写入: searxng-docker-compose.yml
func (s *StagedReply) buildStatusText() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	var lines []string

	totalTools := len(s.toolHistory)
	startIdx := 0

	// Show at most last 6 completed tools
	if totalTools > 6 {
		startIdx = totalTools - 6
		lines = append(lines, fmt.Sprintf("<i>... +%d</i>", startIdx))
	}

	// Completed tools with step numbers
	for i := startIdx; i < totalTools; i++ {
		lines = append(lines, fmt.Sprintf("%d. %s", i+1, s.toolHistory[i]))
	}

	// Active tool with spinner
	if s.activeTool != "" {
		stepNum := totalTools + 1
		lines = append(lines, fmt.Sprintf("⚙️ %d. <i>%s</i>", stepNum, s.activeTool))
	} else if totalTools == 0 {
		lines = append(lines, "🤔 <i>思考中...</i>")
	}

	return strings.Join(lines, "\n")
}

// updateStatus updates the status message (throttled)
func (s *StagedReply) updateStatus(text string) error {
	s.mu.Lock()
	now := time.Now().UnixMilli()
	// Throttle check
	if now-s.lastUpdate < s.throttleMs {
		s.mu.Unlock()
		return nil
	}
	if text == s.lastText {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()
	return s.doSendOrEdit(text, now)
}

// forceStatusRefresh rebuilds + sends status (ignores throttle for phase changes)
func (s *StagedReply) forceStatusRefresh() error {
	text := s.buildStatusText()
	now := time.Now().UnixMilli()
	return s.doSendOrEdit(text, now)
}

// doSendOrEdit sends a new message or edits the existing one
func (s *StagedReply) doSendOrEdit(text string, now int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if text == s.lastText {
		return nil
	}

	if s.statusID == 0 {
		// First send
		msg := tgbotapi.NewMessage(s.chatID, text)
		if s.parseMode != "" {
			msg.ParseMode = s.parseMode
		}
		sent, err := s.bot.Send(msg)
		if err != nil {
			return err
		}
		s.statusID = sent.MessageID
	} else {
		// Edit existing status message
		editMsg := tgbotapi.NewEditMessageText(s.chatID, s.statusID, text)
		if s.parseMode != "" {
			editMsg.ParseMode = s.parseMode
		}
		_, err := s.bot.Send(editMsg)
		if err != nil && !isMessageNotModifiedError(err) {
			return err
		}
	}

	s.lastText = text
	s.lastUpdate = now
	return nil
}

// Deliver deletes the status message and sends the final complete reply.
// For long texts, it splits into multiple messages with pagination.
func (s *StagedReply) Deliver(adapter *Adapter, finalText string) error {
	// Delete the status message
	s.deleteStatus()

	// Send final text as properly formatted message(s)
	return s.sendFinalChunked(adapter, finalText)
}

// DeliverWithSuffix delivers with a suffix appended to the last chunk.
// Converts Markdown → TG HTML before sending.
func (s *StagedReply) DeliverWithSuffix(adapter *Adapter, finalText, suffix string) error {
	s.deleteStatus()

	// Convert LLM Markdown → Telegram HTML
	htmlText := MarkdownToTelegramHTML(finalText)

	chunks := ChunkMarkdown(htmlText)
	if len(chunks) == 0 {
		chunks = []string{htmlText}
	}

	for i, chunk := range chunks {
		text := chunk
		isLast := i == len(chunks)-1

		// Add pagination marker for multi-chunk messages
		if len(chunks) > 1 {
			text += fmt.Sprintf("\n\n📄 <i>(%d/%d)</i>", i+1, len(chunks))
		}

		// Append suffix to the last chunk
		if isLast && suffix != "" {
			text += "\n\n" + suffix
		}

		err := adapter.SendMessage(&OutgoingMessage{
			ChatID:    s.chatID,
			Text:      text,
			ParseMode: s.parseMode,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// deleteStatus removes the status message
func (s *StagedReply) deleteStatus() {
	s.mu.Lock()
	msgID := s.statusID
	s.mu.Unlock()

	if msgID == 0 {
		return
	}

	deleteMsg := tgbotapi.NewDeleteMessage(s.chatID, msgID)
	s.bot.Request(deleteMsg)

	s.mu.Lock()
	s.statusID = 0
	s.mu.Unlock()
}

// sendFinalChunked sends the final text in properly formatted chunks
func (s *StagedReply) sendFinalChunked(adapter *Adapter, text string) error {
	chunks := ChunkMarkdown(text)
	if len(chunks) == 0 {
		chunks = []string{text}
	}

	for i, chunk := range chunks {
		displayText := chunk
		if len(chunks) > 1 {
			displayText += fmt.Sprintf("\n\n📄 <i>(%d/%d)</i>", i+1, len(chunks))
		}
		err := adapter.SendMessage(&OutgoingMessage{
			ChatID:    s.chatID,
			Text:      displayText,
			ParseMode: s.parseMode,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// GetStatusMessageID returns the current status message ID
func (s *StagedReply) GetStatusMessageID() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.statusID
}

// toolDisplayLabel generates a human-readable label for a tool invocation.
// Instead of showing bare "bash", it shows "执行命令: ls -la" etc.
func toolDisplayLabel(toolName string, args map[string]interface{}) string {
	switch toolName {
	case "bash", "bash_exec", "shell":
		if cmd := argStr(args, "command"); cmd != "" {
			return fmt.Sprintf("执行命令: %s", truncateLabel(cmd, 48))
		}
		return "执行命令"

	case "read_file":
		if p := argStr(args, "path"); p != "" {
			return fmt.Sprintf("读取: %s", filepath.Base(p))
		}
		return "读取文件"

	case "write_file":
		if p := argStr(args, "path"); p != "" {
			return fmt.Sprintf("写入: %s", filepath.Base(p))
		}
		return "写入文件"

	case "list_dir", "list_directory":
		if p := argStr(args, "path"); p != "" {
			return fmt.Sprintf("查看目录: %s", truncateLabel(p, 40))
		}
		return "查看目录"

	case "web_search", "search":
		if q := argStr(args, "query"); q != "" {
			return fmt.Sprintf("搜索: %s", truncateLabel(q, 48))
		}
		return "网络搜索"

	case "browser", "browse":
		if u := argStr(args, "url"); u != "" {
			return fmt.Sprintf("浏览: %s", truncateLabel(u, 48))
		}
		return "浏览网页"

	case "git":
		if sub := argStr(args, "subcommand"); sub != "" {
			return fmt.Sprintf("Git: %s", sub)
		}
		if cmd := argStr(args, "command"); cmd != "" {
			return fmt.Sprintf("Git: %s", truncateLabel(cmd, 40))
		}
		return "Git 操作"

	case "memory_search", "memory_store":
		if q := argStr(args, "query"); q != "" {
			return fmt.Sprintf("记忆检索: %s", truncateLabel(q, 40))
		}
		return "记忆操作"

	case "stock_analysis", "stock_query":
		if code := argStr(args, "code"); code != "" {
			return fmt.Sprintf("股票分析: %s", code)
		}
		return "股票分析"

	case "subagent":
		if task := argStr(args, "task"); task != "" {
			return fmt.Sprintf("子任务: %s", truncateLabel(task, 40))
		}
		return "子任务执行"

	case "lsp_diagnostics", "lsp_hover", "lsp_definition":
		return "代码分析"

	case "lint_fix":
		return "代码修复"

	case "repomap":
		return "仓库结构分析"

	default:
		// Fallback: capitalize tool name
		return toolName
	}
}

// argStr safely extracts a string argument from the args map
func argStr(args map[string]interface{}, key string) string {
	if args == nil {
		return ""
	}
	v, ok := args[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// truncateLabel shortens text to maxLen, adding ellipsis if truncateLabeld
func truncateLabel(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-1]) + "…"
}

