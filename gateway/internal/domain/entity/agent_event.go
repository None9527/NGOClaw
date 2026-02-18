package entity

import "time"

// AgentEventType defines the type of event emitted during an agent loop
type AgentEventType string

const (
	EventTextDelta   AgentEventType = "text_delta"
	EventToolCall    AgentEventType = "tool_call"
	EventToolResult  AgentEventType = "tool_result"
	EventThinking    AgentEventType = "thinking"
	EventStepDone    AgentEventType = "step_done"
	EventDone        AgentEventType = "done"
	EventError       AgentEventType = "error"
)

// AgentEvent represents a single event in the agent's ReAct loop.
// Consumers (TG adapter, CLI, WebChat) subscribe to a channel of these events.
type AgentEvent struct {
	Type      AgentEventType `json:"type"`
	Content   string         `json:"content,omitempty"`
	ToolCall  *ToolCallEvent `json:"tool_call,omitempty"`
	StepInfo  *StepInfo      `json:"step_info,omitempty"`
	Error     string         `json:"error,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
}

// ToolCallEvent describes a tool invocation within the agent loop
type ToolCallEvent struct {
	ID        string                 `json:"id"`
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
	Output    string                 `json:"output,omitempty"`
	Display   string                 `json:"display,omitempty"` // Rich UI output (fallback to Output)
	Success   bool                   `json:"success"`
	Duration  time.Duration          `json:"duration,omitempty"`
}

// StepInfo provides metadata about the current agent step
type StepInfo struct {
	Step       int    `json:"step"`
	TokensUsed int    `json:"tokens_used"`
	ModelUsed  string `json:"model_used"`
	State      string `json:"state,omitempty"` // Current state machine state
}

// ToolCallInfo represents a tool call parsed from LLM response
type ToolCallInfo struct {
	ID        string                 `json:"id"`
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}
