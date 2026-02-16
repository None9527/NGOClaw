package service

import (
	"errors"
	"testing"
	"time"

	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/entity"
	"go.uber.org/zap"
)

// === CostGuard Tests ===

func TestCostGuard_TokenBudget(t *testing.T) {
	logger := zap.NewNop()
	cg := NewCostGuard(1000, 0, logger)

	// Should be fine under budget
	if err := cg.AddTokens(500); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Adding more tokens should trip the budget at AddTokens level
	if err := cg.AddTokens(600); err == nil {
		t.Fatal("expected budget exceeded error from AddTokens")
	}
}

func TestCostGuard_NoBudget(t *testing.T) {
	logger := zap.NewNop()
	cg := NewCostGuard(0, 0, logger) // Budget disabled

	if err := cg.AddTokens(999999); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := cg.CheckBudget(); err != nil {
		t.Fatalf("expected no error when budget disabled: %v", err)
	}
}

func TestCostGuard_TimeoutBudget(t *testing.T) {
	logger := zap.NewNop()
	cg := NewCostGuard(0, 10*time.Millisecond, logger)

	// Should be OK immediately
	if err := cg.CheckBudget(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Wait for timeout
	time.Sleep(15 * time.Millisecond)
	if err := cg.CheckBudget(); err == nil {
		t.Fatal("expected time budget exceeded error")
	}
}

// === ContextGuard Tests ===

func TestContextGuard_BelowThreshold(t *testing.T) {
	logger := zap.NewNop()
	cg := NewContextGuard(10000, 0.7, 0.85, logger)

	messages := []LLMMessage{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Hello"},
	}

	result := cg.Check(messages)
	if result.NeedCompaction {
		t.Fatal("should not need compaction for small context")
	}
	if result.Ratio > 0.1 {
		t.Fatalf("ratio too high: %f", result.Ratio)
	}
}

func TestContextGuard_HardCompaction(t *testing.T) {
	logger := zap.NewNop()
	// Very small window to trigger compaction easily
	cg := NewContextGuard(100, 0.7, 0.85, logger)

	// Create messages that exceed the token limit (100 tokens ~= 300 chars)
	messages := []LLMMessage{
		{Role: "system", Content: string(make([]byte, 200))},
		{Role: "user", Content: string(make([]byte, 200))},
	}

	result := cg.Check(messages)
	if !result.NeedCompaction {
		t.Fatalf("should need compaction, ratio: %f", result.Ratio)
	}
}

func TestContextGuard_MultimodalAware(t *testing.T) {
	logger := zap.NewNop()
	cg := NewContextGuard(1000, 0.7, 0.85, logger)

	messages := []LLMMessage{
		{Role: "user", Parts: []ContentPart{
			{Type: "text", Text: "What is this?"},
			{Type: "image", MediaURL: "http://example.com/img.png"},
		}},
	}

	result := cg.Check(messages)
	// Image adds significant token estimation (varies by implementation)
	if result.EstimatedTokens < 50 {
		t.Fatalf("expected multimodal to add significant tokens, got: %d", result.EstimatedTokens)
	}
}

// === LoopDetector Tests ===

func TestLoopDetector_NoLoop(t *testing.T) {
	logger := zap.NewNop()
	ld := NewLoopDetector(5, 3, logger)

	// Different tools should not trigger
	if ld.Record("read_file") {
		t.Fatal("should not detect loop on first call")
	}
	if ld.Record("write_file") {
		t.Fatal("should not detect loop on different tool")
	}
	if ld.Record("search") {
		t.Fatal("should not detect loop on different tool")
	}
}

func TestLoopDetector_DetectsLoop(t *testing.T) {
	logger := zap.NewNop()
	ld := NewLoopDetector(5, 3, logger)

	// Same tool 3 times in window of 5 should trigger
	ld.Record("read_file")
	ld.Record("read_file")
	if !ld.Record("read_file") {
		t.Fatal("should detect loop after 3 identical calls")
	}
}

func TestLoopDetector_SlidingWindow(t *testing.T) {
	logger := zap.NewNop()
	ld := NewLoopDetector(3, 2, logger) // Window=3, threshold=2

	ld.Record("read_file")
	ld.Record("write_file")
	ld.Record("search")

	// Window is now [write_file, search, ???] — read_file has slid out
	// One more read_file should NOT trigger
	if ld.Record("read_file") {
		t.Fatal("should not trigger — read_file only once in current window")
	}
}

// === sanitizeMessages Tests ===

func TestSanitizeMessages_Empty(t *testing.T) {
	result := sanitizeMessages(nil)
	if result != nil {
		t.Fatal("should return nil for nil input")
	}
}

func TestSanitizeMessages_NoOrphans(t *testing.T) {
	messages := []LLMMessage{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "", ToolCalls: []entity.ToolCallInfo{{ID: "tc1", Name: "read"}}},
		{Role: "tool", ToolCallID: "tc1", Content: "result"},
	}
	result := sanitizeMessages(messages)
	if result[1].ToolCalls == nil {
		t.Fatal("should preserve tool calls with matching results")
	}
}

func TestSanitizeMessages_StripsOrphan(t *testing.T) {
	messages := []LLMMessage{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "let me check", ToolCalls: []entity.ToolCallInfo{{ID: "orphan1", Name: "read"}}},
		// No tool result for "orphan1"
	}
	result := sanitizeMessages(messages)
	if result[1].ToolCalls != nil {
		t.Fatal("should strip orphan tool calls")
	}
	if result[1].Content != "let me check" {
		t.Fatal("should preserve text content when stripping tool calls")
	}
}

// === LLMError Classification Tests ===

func TestClassifyError_AuthError(t *testing.T) {
	err := errors.New("Unauthorized: invalid API key")
	classified := ClassifyError(err, "openai", "gpt-4")
	if classified.Kind != ErrKindAuth {
		t.Fatalf("expected auth, got %s", classified.Kind)
	}
	if classified.IsRetryable() {
		t.Fatal("auth errors should not be retryable")
	}
}

func TestClassifyError_ContentFilter(t *testing.T) {
	err := errors.New("content policy violation: message blocked by safety filter")
	classified := ClassifyError(err, "openai", "gpt-4")
	if classified.Kind != ErrKindContentFilter {
		t.Fatalf("expected content_filter, got %s", classified.Kind)
	}
}

func TestClassifyError_TransientDefault(t *testing.T) {
	err := errors.New("connection reset by peer")
	classified := ClassifyError(err, "openai", "gpt-4")
	if classified.Kind != ErrKindTransient {
		t.Fatalf("expected transient, got %s", classified.Kind)
	}
	if !classified.IsRetryable() {
		t.Fatal("transient errors should be retryable")
	}
}

func TestClassifyError_BadRequest(t *testing.T) {
	err := errors.New("400 Bad Request: model not found")
	classified := ClassifyError(err, "openai", "gpt-4")
	if classified.Kind != ErrKindBadRequest {
		t.Fatalf("expected bad_request, got %s", classified.Kind)
	}
}

func TestClassifyError_AlreadyClassified(t *testing.T) {
	original := &LLMError{Kind: ErrKindBudget, Message: "budget exceeded"}
	classified := ClassifyError(original, "openai", "gpt-4")
	if classified.Kind != ErrKindBudget {
		t.Fatalf("expected budget, got %s", classified.Kind)
	}
}

func TestClassifyError_Unwrap(t *testing.T) {
	cause := errors.New("connection refused")
	llmErr := &LLMError{Kind: ErrKindTransient, Message: "transient", Cause: cause}
	if !errors.Is(llmErr, cause) {
		t.Fatal("Unwrap should expose the cause")
	}
}
