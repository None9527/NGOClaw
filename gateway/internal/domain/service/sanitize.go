package service

import (
	"fmt"
	"strings"
	"time"

	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/entity"
	"go.uber.org/zap"
)

// sanitizeMessages fixes orphan tool_use blocks in the message history.
// An "orphan" is an assistant message with ToolCalls but no subsequent tool result.
// This can happen after context compaction or error recovery.
func sanitizeMessages(messages []LLMMessage) []LLMMessage {
	if len(messages) == 0 {
		return messages
	}

	// Collect IDs of tool results present
	resultIDs := make(map[string]bool)
	for _, msg := range messages {
		if msg.Role == "tool" && msg.ToolCallID != "" {
			resultIDs[msg.ToolCallID] = true
		}
	}

	// Check last assistant message — if it has tool_calls without corresponding results, strip them
	result := make([]LLMMessage, len(messages))
	copy(result, messages)

	for i := len(result) - 1; i >= 0; i-- {
		if result[i].Role == "assistant" && len(result[i].ToolCalls) > 0 {
			// Check if all tool call IDs have results
			allHaveResults := true
			for _, tc := range result[i].ToolCalls {
				if !resultIDs[tc.ID] {
					allHaveResults = false
					break
				}
			}
			if !allHaveResults {
				// Strip tool calls — keep only the text content
				result[i].ToolCalls = nil
			}
			break // Only check the last assistant message with tool calls
		}
	}

	return result
}

// truncateOutput trims tool output to maxChars, appending a notice if truncated
func truncateOutput(output string, maxChars int) string {
	if maxChars <= 0 || len(output) <= maxChars {
		return output
	}

	// Find a good break point (newline near the limit)
	breakAt := maxChars
	lastNewline := strings.LastIndex(output[:maxChars], "\n")
	if lastNewline > maxChars*3/4 {
		breakAt = lastNewline
	}

	truncated := output[:breakAt]
	remaining := len(output) - breakAt
	return fmt.Sprintf("%s\n\n[... truncated %d characters. Use read_file with line ranges for full content.]", truncated, remaining)
}

// emitEvent sends an event to the event channel with timestamp.
func (a *AgentLoop) emitEvent(ch chan<- entity.AgentEvent, event entity.AgentEvent) {
	event.Timestamp = time.Now()
	select {
	case ch <- event:
	default:
		a.logger.Warn("Event channel full, dropping event",
			zap.String("type", string(event.Type)),
		)
	}
}
