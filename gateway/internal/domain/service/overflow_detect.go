package service

import (
	"strings"
)

// IsContextOverflowError checks if an error indicates the context window was
// exceeded. Aligned with OpenClaw's isContextOverflowError — detects common
// error patterns from Anthropic, OpenAI, Google, MiniMax, and proxy APIs.
func IsContextOverflowError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())

	return strings.Contains(msg, "context length exceeded") ||
		strings.Contains(msg, "maximum context length") ||
		strings.Contains(msg, "request_too_large") ||
		strings.Contains(msg, "request exceeds the maximum size") ||
		strings.Contains(msg, "prompt is too long") ||
		strings.Contains(msg, "exceeds model context window") ||
		strings.Contains(msg, "context overflow") ||
		(strings.Contains(msg, "request size exceeds") && strings.Contains(msg, "context window")) ||
		(strings.Contains(msg, "413") && strings.Contains(msg, "too large"))
}
