package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"
)

// compactMessages summarizes older messages to reduce context length.
// Preserves:
//   - System prompt (first message)
//   - Last N messages (recent context)
//
// Replaces middle section with a summary message.
func (a *AgentLoop) compactMessages(messages []LLMMessage) []LLMMessage {
	keepLast := a.config.CompactKeepLast
	if keepLast >= len(messages) {
		return messages // Nothing to compact
	}

	// Find system message boundary
	firstNonSystem := 0
	if len(messages) > 0 && messages[0].Role == "system" {
		firstNonSystem = 1
	}

	// If we don't have enough messages to compact meaningfully, skip
	middleEnd := len(messages) - keepLast
	if middleEnd <= firstNonSystem {
		return messages
	}

	// Try LLM-based summarization first
	summary := a.tryLLMSummarize(messages[firstNonSystem:middleEnd])

	// Fallback to truncation-based summary if LLM summarization fails
	if summary == "" {
		summary = a.truncationSummary(messages[firstNonSystem:middleEnd])
	}

	// Reconstruct: system + summary + last N messages
	compacted := make([]LLMMessage, 0, 2+keepLast)

	// Keep system prompt
	if firstNonSystem > 0 {
		compacted = append(compacted, messages[0])
	}

	// Add summary as a user message
	compacted = append(compacted, LLMMessage{
		Role:    "user",
		Content: summary,
	})

	// Keep last N messages
	compacted = append(compacted, messages[len(messages)-keepLast:]...)

	a.logger.Info("Context compaction completed",
		zap.Int("before", len(messages)),
		zap.Int("after", len(compacted)),
		zap.Int("compacted_messages", middleEnd-firstNonSystem),
	)

	return compacted
}

// tryLLMSummarize uses the LLM to generate a structured XML <state_snapshot>
// summary of older messages. Returns empty string if summarization fails.
func (a *AgentLoop) tryLLMSummarize(messages []LLMMessage) string {
	if a.llm == nil {
		return ""
	}

	// Build a concise representation of the conversation for summarization
	var parts []string
	for _, msg := range messages {
		text := msg.TextContent()
		if text == "" {
			continue
		}
		// Truncate individual messages to save tokens
		if len(text) > 500 {
			text = text[:500] + "..."
		}
		parts = append(parts, fmt.Sprintf("[%s]: %s", msg.Role, text))
	}

	if len(parts) == 0 {
		return ""
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	const compressionPrompt = `You are a conversation state compressor. Analyze the following conversation and produce a structured XML snapshot.

Output format:
<state_snapshot>
  <task_description>Current task being executed</task_description>
  <progress>
    <completed>List of completed steps</completed>
    <in_progress>Current step</in_progress>
    <remaining>Remaining steps</remaining>
  </progress>
  <key_decisions>Key technical decisions and reasons</key_decisions>
  <modified_files>
    <file path="path/to/file" action="created|modified|deleted">Change summary</file>
  </modified_files>
  <current_context>
    <working_directory>Current working directory</working_directory>
    <relevant_findings>Key findings and constraints</relevant_findings>
  </current_context>
  <memory_candidates>Facts worth remembering long-term (user preferences, environment info, project decisions)</memory_candidates>
</state_snapshot>

Rules:
- Preserve ALL unfinished task state
- Keep key decisions and reasons
- Drop specific code content (only keep file paths + change summaries)
- Drop intermediate debugging
- Extract memory-worthy facts into <memory_candidates>`

	summaryReq := &LLMRequest{
		Model:       a.config.Model,
		Temperature: 0.2,
		MaxTokens:   800,
		Messages: []LLMMessage{
			{
				Role:    "system",
				Content: compressionPrompt,
			},
			{
				Role:    "user",
				Content: fmt.Sprintf("Compress this conversation (%d messages):\n\n%s", len(parts), strings.Join(parts, "\n")),
			},
		},
	}

	resp, err := a.llm.Generate(ctx, summaryReq)
	if err != nil {
		a.logger.Debug("LLM summarization failed, using fallback",
			zap.Error(err),
		)
		return ""
	}

	if resp.Content == "" {
		return ""
	}

	// Flush conversation state to daily log before context is discarded
	go a.flushToDailyLog(resp.Content, len(messages))

	// P1.7: Auto-extract memory candidates from compaction
	go a.extractMemoriesFromCompaction(resp.Content)

	return fmt.Sprintf("[Context compacted — %d messages → state_snapshot]\n\n%s", len(messages), resp.Content)
}

// extractMemoriesFromCompaction extracts <memory_candidates> from compaction output
// and appends them to ~/.ngoclaw/memory.md. Runs async to not block compaction.
func (a *AgentLoop) extractMemoriesFromCompaction(snapshot string) {
	// Extract <memory_candidates>...</memory_candidates>
	start := strings.Index(snapshot, "<memory_candidates>")
	end := strings.Index(snapshot, "</memory_candidates>")
	if start == -1 || end == -1 || end <= start {
		return
	}

	candidates := strings.TrimSpace(snapshot[start+len("<memory_candidates>") : end])
	if candidates == "" {
		return
	}

	// Parse bullet points
	lines := strings.Split(candidates, "\n")
	var facts []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "- ")
		line = strings.TrimPrefix(line, "* ")
		line = strings.TrimPrefix(line, "• ")
		line = strings.TrimSpace(line)
		if line != "" && len(line) > 5 {
			facts = append(facts, line)
		}
	}

	if len(facts) == 0 {
		return
	}

	// Use save_memory tool to persist each fact
	for _, fact := range facts {
		_, err := a.tools.Execute(context.Background(), "save_memory", map[string]interface{}{
			"fact": fact,
		})
		if err != nil {
			a.logger.Debug("Auto-extract memory failed",
				zap.String("fact", fact),
				zap.Error(err),
			)
		}
	}

	a.logger.Info("Auto-extracted memories from compaction",
		zap.Int("facts", len(facts)),
	)
}

// flushToDailyLog writes a compact summary of the compacted conversation to
// the daily log file (memory/YYYY-MM-DD.md). This preserves context that
// would otherwise be lost after compaction.
func (a *AgentLoop) flushToDailyLog(snapshot string, messageCount int) {
	// Extract <task_description> for a one-line summary
	taskDesc := extractXMLTag(snapshot, "task_description")
	inProgress := extractXMLTag(snapshot, "in_progress")

	var entry string
	switch {
	case taskDesc != "" && inProgress != "":
		entry = fmt.Sprintf("[compaction] %s — 进行中: %s (%d msgs compacted)", taskDesc, inProgress, messageCount)
	case taskDesc != "":
		entry = fmt.Sprintf("[compaction] %s (%d msgs compacted)", taskDesc, messageCount)
	default:
		entry = fmt.Sprintf("[compaction] %d messages compacted", messageCount)
	}

	// Write directly to avoid import cycle (service ← tool → service)
	home, err := os.UserHomeDir()
	if err != nil {
		a.logger.Warn("Failed to get home dir for daily log", zap.Error(err))
		return
	}
	dir := filepath.Join(home, ".ngoclaw", "memory")
	if err := os.MkdirAll(dir, 0755); err != nil {
		a.logger.Warn("Failed to create daily log dir", zap.Error(err))
		return
	}
	logPath := filepath.Join(dir, time.Now().Format("2006-01-02")+".md")
	line := fmt.Sprintf("- [%s] %s\n", time.Now().Format("15:04"), entry)
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		a.logger.Warn("Failed to open daily log", zap.Error(err))
		return
	}
	defer f.Close()
	if _, err := f.WriteString(line); err != nil {
		a.logger.Warn("Failed to write daily log", zap.Error(err))
	}
}

// extractXMLTag extracts the text content of a simple XML tag from a string.
func extractXMLTag(s, tag string) string {
	open := "<" + tag + ">"
	close := "</" + tag + ">"
	start := strings.Index(s, open)
	end := strings.Index(s, close)
	if start == -1 || end == -1 || end <= start {
		return ""
	}
	return strings.TrimSpace(s[start+len(open) : end])
}

// truncationSummary builds a simple truncation-based summary as fallback.
func (a *AgentLoop) truncationSummary(messages []LLMMessage) string {
	var summaryParts []string
	toolCallCount := 0
	assistantMsgCount := 0
	userMsgCount := 0

	for _, msg := range messages {
		switch msg.Role {
		case "assistant":
			assistantMsgCount++
			if msg.Content != "" {
				text := msg.Content
				if len(text) > 200 {
					text = text[:200] + "..."
				}
				summaryParts = append(summaryParts, fmt.Sprintf("Assistant: %s", text))
			}
			toolCallCount += len(msg.ToolCalls)
		case "user":
			userMsgCount++
			text := msg.Content
			if len(text) > 100 {
				text = text[:100] + "..."
			}
			summaryParts = append(summaryParts, fmt.Sprintf("User: %s", text))
		case "tool":
			// Skip tool results in summary (they're implicit from tool calls)
		}
	}

	return fmt.Sprintf(
		"[Context compacted: %d messages summarized (%d user, %d assistant, %d tool calls)]\n\n%s",
		len(messages),
		userMsgCount,
		assistantMsgCount,
		toolCallCount,
		strings.Join(summaryParts, "\n"),
	)
}
