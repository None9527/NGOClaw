package service

import (
	"context"

	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/entity"
)

// AgentHook defines lifecycle hooks for extending agent loop behavior.
// All methods are optional — embed NoOpHook to only implement what you need.
// Hooks execute synchronously; keep them fast to avoid blocking the loop.
type AgentHook interface {
	// BeforeLLMCall is called before each LLM request.
	// The hook can modify the request (e.g., inject metadata).
	BeforeLLMCall(ctx context.Context, req *LLMRequest, step int)

	// AfterLLMCall is called after each successful LLM response.
	AfterLLMCall(ctx context.Context, resp *LLMResponse, step int)

	// BeforeToolCall is called before each tool execution.
	// Return false to skip the tool call (e.g., for sandboxing/permission checks).
	BeforeToolCall(ctx context.Context, toolName string, args map[string]interface{}) bool

	// AfterToolCall is called after each tool execution completes.
	AfterToolCall(ctx context.Context, toolName string, output string, success bool)


	// OnError is called when an error occurs in the loop.
	OnError(ctx context.Context, err error, step int)

	// OnComplete is called when the loop finishes successfully.
	OnComplete(ctx context.Context, result *AgentResult)

	// OnStateChange is called on each state machine transition.
	OnStateChange(from, to AgentState, snap StateSnapshot)
}

// NoOpHook provides a default no-op implementation of all hooks.
// Embed this in your custom hook to only override methods you care about.
type NoOpHook struct{}

func (NoOpHook) BeforeLLMCall(_ context.Context, _ *LLMRequest, _ int)                         {}
func (NoOpHook) AfterLLMCall(_ context.Context, _ *LLMResponse, _ int)                          {}
func (NoOpHook) BeforeToolCall(_ context.Context, _ string, _ map[string]interface{}) bool       { return true }
func (NoOpHook) AfterToolCall(_ context.Context, _ string, _ string, _ bool)                     {}

func (NoOpHook) OnError(_ context.Context, _ error, _ int)                                       {}
func (NoOpHook) OnComplete(_ context.Context, _ *AgentResult)                                    {}
func (NoOpHook) OnStateChange(_, _ AgentState, _ StateSnapshot)                                  {}

// HookChain aggregates multiple hooks — all hooks are called in order.
type HookChain struct {
	hooks []AgentHook
}

// NewHookChain creates a hook chain from the given hooks.
func NewHookChain(hooks ...AgentHook) *HookChain {
	return &HookChain{hooks: hooks}
}

// Add appends a hook to the chain.
func (c *HookChain) Add(h AgentHook) {
	c.hooks = append(c.hooks, h)
}

func (c *HookChain) BeforeLLMCall(ctx context.Context, req *LLMRequest, step int) {
	for _, h := range c.hooks {
		h.BeforeLLMCall(ctx, req, step)
	}
}

func (c *HookChain) AfterLLMCall(ctx context.Context, resp *LLMResponse, step int) {
	for _, h := range c.hooks {
		h.AfterLLMCall(ctx, resp, step)
	}
}

func (c *HookChain) BeforeToolCall(ctx context.Context, toolName string, args map[string]interface{}) bool {
	for _, h := range c.hooks {
		if !h.BeforeToolCall(ctx, toolName, args) {
			return false // Any hook can veto a tool call
		}
	}
	return true
}

func (c *HookChain) AfterToolCall(ctx context.Context, toolName string, output string, success bool) {
	for _, h := range c.hooks {
		h.AfterToolCall(ctx, toolName, output, success)
	}
}



func (c *HookChain) OnError(ctx context.Context, err error, step int) {
	for _, h := range c.hooks {
		h.OnError(ctx, err, step)
	}
}

func (c *HookChain) OnComplete(ctx context.Context, result *AgentResult) {
	for _, h := range c.hooks {
		h.OnComplete(ctx, result)
	}
}

func (c *HookChain) OnStateChange(from, to AgentState, snap StateSnapshot) {
	for _, h := range c.hooks {
		h.OnStateChange(from, to, snap)
	}
}

// Compile-time check: HookChain implements AgentHook
var _ AgentHook = (*HookChain)(nil)

// --- Built-in Hooks ---

// LoggingHook provides basic logging for all lifecycle events.
type LoggingHook struct {
	NoOpHook
	events []entity.AgentEvent
}

// MetricsHook tracks timing and count metrics.
type MetricsHook struct {
	NoOpHook
	LLMCallCount  int
	ToolCallCount int
	ErrorCount    int
}

func (h *MetricsHook) AfterLLMCall(_ context.Context, _ *LLMResponse, _ int)   { h.LLMCallCount++ }
func (h *MetricsHook) AfterToolCall(_ context.Context, _ string, _ string, _ bool) { h.ToolCallCount++ }
func (h *MetricsHook) OnError(_ context.Context, _ error, _ int)                { h.ErrorCount++ }
