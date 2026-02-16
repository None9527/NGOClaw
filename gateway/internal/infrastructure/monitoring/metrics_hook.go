package monitoring

import (
	"context"
	"time"

	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/service"
)

// MetricsHook is an AgentHook that automatically instruments the AgentLoop
// with Monitor metrics. Embed NoOpHook for default method implementations.
// Wire it into the AgentLoop via SetHooks().
//
// Usage:
//
//	monitor := monitoring.NewMonitor(logger)
//	hook := monitoring.NewMetricsHook(monitor)
//	agentLoop.SetHooks(hook)
type MetricsHook struct {
	service.NoOpHook
	monitor  *Monitor
	stepTime time.Time // tracks per-step latency
}

// NewMetricsHook creates a metrics-collecting agent hook.
func NewMetricsHook(monitor *Monitor) *MetricsHook {
	return &MetricsHook{monitor: monitor}
}

// Compile-time interface check
var _ service.AgentHook = (*MetricsHook)(nil)

// BeforeLLMCall is called before each LLM request.
func (h *MetricsHook) BeforeLLMCall(ctx context.Context, req *service.LLMRequest, step int) {
	h.monitor.IncModelCall()
	h.monitor.IncRequestTotal()
	h.stepTime = time.Now()
}

// AfterLLMCall is called after each successful LLM response.
func (h *MetricsHook) AfterLLMCall(ctx context.Context, resp *service.LLMResponse, step int) {
	h.monitor.IncRequestSuccess()
	h.monitor.AddTokensUsed(resp.TokensUsed)
	if !h.stepTime.IsZero() {
		h.monitor.RecordRequestLatency(time.Since(h.stepTime))
	}
}

// BeforeToolCall is called before each tool execution.
// Always returns true (does not veto) — purely observational.
func (h *MetricsHook) BeforeToolCall(ctx context.Context, toolName string, args map[string]interface{}) bool {
	h.monitor.IncToolCallTotal()
	return true
}

// AfterToolCall is called after each tool execution completes.
func (h *MetricsHook) AfterToolCall(ctx context.Context, toolName string, output string, success bool) {
	if success {
		h.monitor.IncToolCallSuccess()
	} else {
		h.monitor.IncToolCallFailed()
	}
}

// OnError is called when an error occurs in the loop.
func (h *MetricsHook) OnError(ctx context.Context, err error, step int) {
	h.monitor.IncError()
	h.monitor.IncRequestFailed()
}

// OnComplete is called when the loop finishes successfully.
func (h *MetricsHook) OnComplete(ctx context.Context, result *service.AgentResult) {
	// No additional metrics needed — success already tracked per-step
}

// OnStateChange is called on each state machine transition.
func (h *MetricsHook) OnStateChange(from, to service.AgentState, snap service.StateSnapshot) {
	// Can be extended for state-specific metrics in the future
}
