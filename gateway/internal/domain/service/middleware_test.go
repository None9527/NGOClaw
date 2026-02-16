package service

import (
	"context"
	"testing"

	"go.uber.org/zap"
)

// mockMW implements the Middleware interface for testing.
type mockMW struct {
	NoOpMiddleware
	name          string
	beforeCalled  bool
	afterCalled   bool
	beforeMutator func([]LLMMessage) []LLMMessage
}

func (m *mockMW) Name() string { return m.name }

func (m *mockMW) BeforeModel(_ context.Context, msgs []LLMMessage, _ int) []LLMMessage {
	m.beforeCalled = true
	if m.beforeMutator != nil {
		return m.beforeMutator(msgs)
	}
	return msgs
}

func (m *mockMW) AfterModel(_ context.Context, resp *LLMResponse, _ int) *LLMResponse {
	m.afterCalled = true
	return resp
}

func TestMiddlewarePipeline_RunBeforeModel(t *testing.T) {
	logger := zap.NewNop()
	pipe := NewMiddlewarePipeline(logger)

	mw1 := &mockMW{name: "mw1"}
	mw2 := &mockMW{name: "mw2"}
	pipe.Use(mw1, mw2)

	msgs := []LLMMessage{{Role: "user", Content: "hello"}}
	result := pipe.RunBeforeModel(context.Background(), msgs, 1)

	if !mw1.beforeCalled {
		t.Error("mw1.BeforeModel was not called")
	}
	if !mw2.beforeCalled {
		t.Error("mw2.BeforeModel was not called")
	}
	if len(result) != 1 || result[0].Content != "hello" {
		t.Errorf("unexpected messages: %+v", result)
	}
}

func TestMiddlewarePipeline_RunAfterModel_ReverseOrder(t *testing.T) {
	logger := zap.NewNop()
	pipe := NewMiddlewarePipeline(logger)

	var order []string
	mw1 := &orderTracker{name: "mw1", order: &order}
	mw2 := &orderTracker{name: "mw2", order: &order}

	pipe.Use(mw1, mw2)

	resp := &LLMResponse{Content: "test"}
	pipe.RunAfterModel(context.Background(), resp, 1)

	// AfterModel should run in reverse order
	if len(order) != 2 || order[0] != "mw2" || order[1] != "mw1" {
		t.Errorf("expected reverse order [mw2, mw1], got %v", order)
	}
}

func TestMiddlewarePipeline_BeforeModel_MutatesMessages(t *testing.T) {
	logger := zap.NewNop()
	pipe := NewMiddlewarePipeline(logger)

	injector := &mockMW{
		name: "injector",
		beforeMutator: func(msgs []LLMMessage) []LLMMessage {
			return append(msgs, LLMMessage{Role: "system", Content: "injected"})
		},
	}
	pipe.Use(injector)

	msgs := []LLMMessage{{Role: "user", Content: "hello"}}
	result := pipe.RunBeforeModel(context.Background(), msgs, 1)

	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}
	if result[1].Content != "injected" {
		t.Errorf("expected injected message, got %q", result[1].Content)
	}
}

func TestMiddlewarePipeline_Empty(t *testing.T) {
	logger := zap.NewNop()
	pipe := NewMiddlewarePipeline(logger)

	msgs := []LLMMessage{{Role: "user", Content: "hello"}}
	result := pipe.RunBeforeModel(context.Background(), msgs, 1)

	if len(result) != 1 {
		t.Errorf("expected 1 message, got %d", len(result))
	}
}

// --- helpers ---

type orderTracker struct {
	NoOpMiddleware
	name  string
	order *[]string
}

func (m *orderTracker) Name() string { return m.name }

func (m *orderTracker) BeforeModel(_ context.Context, msgs []LLMMessage, _ int) []LLMMessage {
	return msgs
}

func (m *orderTracker) AfterModel(_ context.Context, resp *LLMResponse, _ int) *LLMResponse {
	*m.order = append(*m.order, m.name)
	return resp
}
