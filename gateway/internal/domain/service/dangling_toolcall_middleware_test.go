package service

import (
	"context"
	"testing"

	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/entity"
	"go.uber.org/zap"
)

func TestDanglingToolCallMiddleware_PatchesOrphans(t *testing.T) {
	logger := zap.NewNop()
	mw := NewDanglingToolCallMiddleware(logger)

	messages := []LLMMessage{
		{Role: "user", Content: "do something"},
		{
			Role:    "assistant",
			Content: "calling tools",
			ToolCalls: []entity.ToolCallInfo{
				{ID: "tc_1", Name: "bash", Arguments: map[string]interface{}{"cmd": "ls"}},
				{ID: "tc_2", Name: "read_file", Arguments: map[string]interface{}{"path": "/tmp"}},
			},
		},
		// Only tc_1 has a result — tc_2 is orphan
		{Role: "tool", ToolCallID: "tc_1", Content: "file1.go"},
	}

	result := mw.BeforeModel(context.Background(), messages, 1)

	// Should have injected a placeholder for tc_2
	if len(result) != 4 {
		t.Fatalf("expected 4 messages (original 3 + 1 injected), got %d", len(result))
	}

	last := result[3]
	if last.Role != "tool" || last.ToolCallID != "tc_2" {
		t.Errorf("expected injected tool result for tc_2, got role=%s callID=%s", last.Role, last.ToolCallID)
	}
}

func TestDanglingToolCallMiddleware_NoOrphans(t *testing.T) {
	logger := zap.NewNop()
	mw := NewDanglingToolCallMiddleware(logger)

	messages := []LLMMessage{
		{Role: "user", Content: "hello"},
		{
			Role:    "assistant",
			Content: "calling tool",
			ToolCalls: []entity.ToolCallInfo{
				{ID: "tc_1", Name: "bash"},
			},
		},
		{Role: "tool", ToolCallID: "tc_1", Content: "ok"},
	}

	result := mw.BeforeModel(context.Background(), messages, 1)

	// No orphans — should be unmodified
	if len(result) != 3 {
		t.Errorf("expected 3 messages (no change), got %d", len(result))
	}
}

func TestDanglingToolCallMiddleware_EmptyMessages(t *testing.T) {
	logger := zap.NewNop()
	mw := NewDanglingToolCallMiddleware(logger)

	result := mw.BeforeModel(context.Background(), nil, 1)
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}
