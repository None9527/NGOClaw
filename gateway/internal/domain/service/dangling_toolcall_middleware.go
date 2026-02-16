// Copyright 2026 NGOClaw Authors. All rights reserved.
package service

import (
	"context"

	"go.uber.org/zap"
)

// DanglingToolCallMiddleware detects assistant messages containing tool_calls
// that lack a corresponding tool-role response. This typically happens when
// a user interrupts the agent loop mid-execution or after context compaction.
//
// Without this fix, the next LLM call would fail with a malformed-messages
// error from most providers (OpenAI, Anthropic, Qwen all require every
// tool_use to have a matching tool_result).
//
// Source: Deer-Flow DanglingToolCallMiddleware pattern.
type DanglingToolCallMiddleware struct {
	NoOpMiddleware
	logger *zap.Logger
}

// NewDanglingToolCallMiddleware creates the middleware.
func NewDanglingToolCallMiddleware(logger *zap.Logger) *DanglingToolCallMiddleware {
	return &DanglingToolCallMiddleware{logger: logger}
}

func (d *DanglingToolCallMiddleware) Name() string {
	return "dangling_toolcall"
}

// BeforeModel scans the message history for orphan tool_calls and injects
// placeholder ToolMessage responses for any that are missing.
func (d *DanglingToolCallMiddleware) BeforeModel(ctx context.Context, messages []LLMMessage, step int) []LLMMessage {
	// Build a set of tool_call IDs that have corresponding tool responses
	respondedIDs := make(map[string]bool)
	for _, msg := range messages {
		if msg.Role == "tool" && msg.ToolCallID != "" {
			respondedIDs[msg.ToolCallID] = true
		}
	}

	// Find assistant messages with tool_calls lacking responses
	var patches []LLMMessage
	for _, msg := range messages {
		if msg.Role != "assistant" || len(msg.ToolCalls) == 0 {
			continue
		}
		for _, tc := range msg.ToolCalls {
			if tc.ID != "" && !respondedIDs[tc.ID] {
				d.logger.Info("Patching dangling tool_call",
					zap.String("tool_call_id", tc.ID),
					zap.String("tool", tc.Name),
					zap.Int("step", step),
				)
				patches = append(patches, LLMMessage{
					Role:       "tool",
					Content:    `{"output": "[tool call interrupted by user]", "success": false}`,
					ToolCallID: tc.ID,
					Name:       tc.Name,
				})
			}
		}
	}

	if len(patches) == 0 {
		return messages
	}

	// Append patches to the end of messages
	result := make([]LLMMessage, 0, len(messages)+len(patches))
	result = append(result, messages...)
	result = append(result, patches...)
	return result
}

// Compile-time check
var _ Middleware = (*DanglingToolCallMiddleware)(nil)
