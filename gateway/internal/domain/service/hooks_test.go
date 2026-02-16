package service

import (
	"context"
	"errors"
	"testing"
)

// === NoOpHook implements AgentHook ===

func TestNoOpHook_ImplementsInterface(t *testing.T) {
	var _ AgentHook = NoOpHook{}
}

func TestNoOpHook_BeforeToolCall_ReturnsTrue(t *testing.T) {
	h := NoOpHook{}
	if !h.BeforeToolCall(context.Background(), "test", nil) {
		t.Error("NoOpHook.BeforeToolCall should return true")
	}
}

// === HookChain ===

func TestHookChain_ImplementsInterface(t *testing.T) {
	var _ AgentHook = (*HookChain)(nil)
}

func TestHookChain_CallsAllHooks(t *testing.T) {
	var calls []string

	hook1 := &trackingHook{id: "h1", calls: &calls}
	hook2 := &trackingHook{id: "h2", calls: &calls}

	chain := NewHookChain(hook1, hook2)
	ctx := context.Background()

	chain.BeforeLLMCall(ctx, &LLMRequest{}, 1)
	chain.AfterLLMCall(ctx, &LLMResponse{}, 1)
	chain.BeforeToolCall(ctx, "shell_exec", nil)
	chain.AfterToolCall(ctx, "shell_exec", "ok", true)
	chain.OnPlanProposed(ctx, "plan text")
	chain.OnError(ctx, errors.New("test error"), 2)
	chain.OnComplete(ctx, &AgentResult{FinalContent: "done"})
	chain.OnStateChange(StateIdle, StateStreaming, StateSnapshot{})

	// Each of 8 methods should be called for each hook = 16 calls
	if len(calls) != 16 {
		t.Errorf("expected 16 hook calls, got %d: %v", len(calls), calls)
	}
}

func TestHookChain_Add(t *testing.T) {
	chain := NewHookChain()
	var calls []string
	chain.Add(&trackingHook{id: "added", calls: &calls})

	chain.BeforeLLMCall(context.Background(), &LLMRequest{}, 1)
	if len(calls) != 1 || calls[0] != "added:BeforeLLMCall" {
		t.Errorf("Add hook was not called: %v", calls)
	}
}

// === BeforeToolCall veto ===

func TestHookChain_BeforeToolCall_VetoStopsChain(t *testing.T) {
	var calls []string
	allow := &trackingHook{id: "allow", calls: &calls}
	deny := &vetoHook{calls: &calls}
	after := &trackingHook{id: "after", calls: &calls}

	chain := NewHookChain(allow, deny, after)
	result := chain.BeforeToolCall(context.Background(), "dangerous_tool", nil)

	if result {
		t.Error("expected BeforeToolCall to return false (vetoed)")
	}
	// "allow" should be called, "deny" should veto, "after" should NOT be called
	expected := []string{"allow:BeforeToolCall", "deny:BeforeToolCall:VETO"}
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d: %v", len(calls), calls)
	}
	for i, exp := range expected {
		if calls[i] != exp {
			t.Errorf("call[%d]: got %q, want %q", i, calls[i], exp)
		}
	}
}

func TestHookChain_BeforeToolCall_AllAllow(t *testing.T) {
	var calls []string
	chain := NewHookChain(
		&trackingHook{id: "h1", calls: &calls},
		&trackingHook{id: "h2", calls: &calls},
	)
	result := chain.BeforeToolCall(context.Background(), "safe_tool", nil)
	if !result {
		t.Error("expected BeforeToolCall to return true when all hooks allow")
	}
	if len(calls) != 2 {
		t.Errorf("expected 2 calls, got %d", len(calls))
	}
}

// === MetricsHook ===

func TestMetricsHook_Counters(t *testing.T) {
	m := &MetricsHook{}
	ctx := context.Background()

	m.AfterLLMCall(ctx, &LLMResponse{}, 1)
	m.AfterLLMCall(ctx, &LLMResponse{}, 2)
	m.AfterToolCall(ctx, "tool1", "ok", true)
	m.AfterToolCall(ctx, "tool2", "ok", true)
	m.AfterToolCall(ctx, "tool3", "fail", false)
	m.OnError(ctx, errors.New("err"), 1)

	if m.LLMCallCount != 2 {
		t.Errorf("LLMCallCount: got %d, want 2", m.LLMCallCount)
	}
	if m.ToolCallCount != 3 {
		t.Errorf("ToolCallCount: got %d, want 3", m.ToolCallCount)
	}
	if m.ErrorCount != 1 {
		t.Errorf("ErrorCount: got %d, want 1", m.ErrorCount)
	}
}

// === Empty chain ===

func TestHookChain_EmptyChain(t *testing.T) {
	chain := NewHookChain()
	ctx := context.Background()

	// Should not panic
	chain.BeforeLLMCall(ctx, &LLMRequest{}, 0)
	chain.AfterLLMCall(ctx, &LLMResponse{}, 0)
	result := chain.BeforeToolCall(ctx, "test", nil)
	chain.AfterToolCall(ctx, "test", "", true)
	chain.OnPlanProposed(ctx, "")
	chain.OnError(ctx, nil, 0)
	chain.OnComplete(ctx, nil)
	chain.OnStateChange(StateIdle, StateStreaming, StateSnapshot{})

	if !result {
		t.Error("empty chain BeforeToolCall should return true")
	}
}

// === Test helpers ===

// trackingHook records all method calls
type trackingHook struct {
	NoOpHook
	id    string
	calls *[]string
}

func (h *trackingHook) BeforeLLMCall(_ context.Context, _ *LLMRequest, _ int) {
	*h.calls = append(*h.calls, h.id+":BeforeLLMCall")
}
func (h *trackingHook) AfterLLMCall(_ context.Context, _ *LLMResponse, _ int) {
	*h.calls = append(*h.calls, h.id+":AfterLLMCall")
}
func (h *trackingHook) BeforeToolCall(_ context.Context, _ string, _ map[string]interface{}) bool {
	*h.calls = append(*h.calls, h.id+":BeforeToolCall")
	return true
}
func (h *trackingHook) AfterToolCall(_ context.Context, _ string, _ string, _ bool) {
	*h.calls = append(*h.calls, h.id+":AfterToolCall")
}
func (h *trackingHook) OnPlanProposed(_ context.Context, _ string) {
	*h.calls = append(*h.calls, h.id+":OnPlanProposed")
}
func (h *trackingHook) OnError(_ context.Context, _ error, _ int) {
	*h.calls = append(*h.calls, h.id+":OnError")
}
func (h *trackingHook) OnComplete(_ context.Context, _ *AgentResult) {
	*h.calls = append(*h.calls, h.id+":OnComplete")
}
func (h *trackingHook) OnStateChange(_, _ AgentState, _ StateSnapshot) {
	*h.calls = append(*h.calls, h.id+":OnStateChange")
}

// vetoHook denies all tool calls
type vetoHook struct {
	NoOpHook
	calls *[]string
}

func (h *vetoHook) BeforeToolCall(_ context.Context, _ string, _ map[string]interface{}) bool {
	*h.calls = append(*h.calls, "deny:BeforeToolCall:VETO")
	return false
}
