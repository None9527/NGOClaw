package service

import (
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// AgentState represents the discrete states of the agent loop state machine.
type AgentState string

const (
	StateIdle       AgentState = "idle"        // Waiting for input

	StateStreaming   AgentState = "streaming"   // Streaming LLM response
	StateToolExec   AgentState = "tool_exec"   // Executing a tool call
	StateCompacting AgentState = "compacting"  // Compacting context (summarizing old messages)
	StateRetrying   AgentState = "retrying"    // Waiting between retry attempts
	StateComplete   AgentState = "complete"    // Successfully completed
	StateError      AgentState = "error"       // Terminated with error
	StateAborted    AgentState = "aborted"     // Cancelled by user or context
)

// validTransitions defines the allowed state transitions.
// Key = from state, Value = set of allowed target states.
var validTransitions = map[AgentState]map[AgentState]bool{
	StateIdle: {
		StateStreaming: true,
	},
	StateStreaming: {
		StateToolExec:   true,
		StateCompacting: true,
		StateRetrying:   true,
		StateComplete:   true,
		StateError:      true,
		StateAborted:    true,
	},
	StateToolExec: {
		StateStreaming:   true, // Next LLM call after tool result
		StateCompacting: true,
		StateError:      true,
		StateAborted:    true,
	},
	StateCompacting: {
		StateStreaming: true,
		StateError:    true,
		StateAborted:  true,
	},
	StateRetrying: {
		StateStreaming: true,
		StateError:    true,
		StateAborted:  true,
	},
	// Terminal states — no transitions out
	StateComplete: {},
	StateError:    {},
	StateAborted:  {},
}

// StateSnapshot captures the agent's runtime state at a point in time.
type StateSnapshot struct {
	State         AgentState    `json:"state"`
	Step          int           `json:"step"`
	MaxSteps      int           `json:"max_steps"`      // 0 = unlimited
	TokensUsed    int           `json:"tokens_used"`
	ToolsExecuted int           `json:"tools_executed"`
	RetryCount    int           `json:"retry_count"`
	ErrorCount    int           `json:"error_count"`
	Elapsed       time.Duration `json:"elapsed"`
	ModelUsed     string        `json:"model_used,omitempty"`
	LastTool      string        `json:"last_tool,omitempty"`
}

// StateMachine manages state transitions for an agent loop run.
// Thread-safe — multiple goroutines can read state concurrently.
type StateMachine struct {
	mu            sync.RWMutex
	state         AgentState
	step          int
	maxSteps      int
	tokensUsed    int
	toolsExecuted int
	retryCount    int
	errorCount    int
	startTime     time.Time
	modelUsed     string
	lastTool      string
	logger        *zap.Logger

	// Listeners notified on each state transition
	listeners []func(from, to AgentState, snap StateSnapshot)
}

// NewStateMachine creates a state machine starting in Idle.
func NewStateMachine(maxSteps int, logger *zap.Logger) *StateMachine {
	return &StateMachine{
		state:     StateIdle,
		maxSteps:  maxSteps,
		startTime: time.Now(),
		logger:    logger,
	}
}

// State returns the current state (thread-safe).
func (sm *StateMachine) State() AgentState {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.state
}

// Snapshot returns a full copy of the current runtime state.
func (sm *StateMachine) Snapshot() StateSnapshot {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return StateSnapshot{
		State:         sm.state,
		Step:          sm.step,
		MaxSteps:      sm.maxSteps,
		TokensUsed:    sm.tokensUsed,
		ToolsExecuted: sm.toolsExecuted,
		RetryCount:    sm.retryCount,
		ErrorCount:    sm.errorCount,
		Elapsed:       time.Since(sm.startTime),
		ModelUsed:     sm.modelUsed,
		LastTool:      sm.lastTool,
	}
}

// Transition attempts to move to a new state.
// Returns error if the transition is not allowed.
func (sm *StateMachine) Transition(to AgentState) error {
	sm.mu.Lock()
	from := sm.state

	allowed, ok := validTransitions[from]
	if !ok || !allowed[to] {
		sm.mu.Unlock()
		err := fmt.Errorf("invalid state transition: %s → %s", from, to)
		sm.logger.Error("State machine violation", zap.Error(err))
		return err
	}

	sm.state = to
	snap := StateSnapshot{
		State:         to,
		Step:          sm.step,
		MaxSteps:      sm.maxSteps,
		TokensUsed:    sm.tokensUsed,
		ToolsExecuted: sm.toolsExecuted,
		RetryCount:    sm.retryCount,
		ErrorCount:    sm.errorCount,
		Elapsed:       time.Since(sm.startTime),
		ModelUsed:     sm.modelUsed,
		LastTool:      sm.lastTool,
	}
	listeners := make([]func(from, to AgentState, snap StateSnapshot), len(sm.listeners))
	copy(listeners, sm.listeners)
	sm.mu.Unlock()

	sm.logger.Debug("State transition",
		zap.String("from", string(from)),
		zap.String("to", string(to)),
		zap.Int("step", snap.Step),
	)

	// Notify listeners outside lock
	for _, fn := range listeners {
		fn(from, to, snap)
	}

	return nil
}

// OnTransition registers a listener called on every state change.
func (sm *StateMachine) OnTransition(fn func(from, to AgentState, snap StateSnapshot)) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.listeners = append(sm.listeners, fn)
}

// --- Mutation helpers (all thread-safe) ---

// SetStep updates the current step counter.
func (sm *StateMachine) SetStep(step int) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.step = step
}

// AddTokens increments the token counter.
func (sm *StateMachine) AddTokens(n int) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.tokensUsed += n
}

// RecordToolExec records a tool execution.
func (sm *StateMachine) RecordToolExec(toolName string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.toolsExecuted++
	sm.lastTool = toolName
}

// RecordRetry increments the retry counter.
func (sm *StateMachine) RecordRetry() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.retryCount++
}

// RecordError increments the error counter.
func (sm *StateMachine) RecordError() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.errorCount++
}

// SetModel sets the model identifier.
func (sm *StateMachine) SetModel(model string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.modelUsed = model
}

// IsTerminal returns true if the state machine is in a terminal state.
func (sm *StateMachine) IsTerminal() bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	switch sm.state {
	case StateComplete, StateError, StateAborted:
		return true
	}
	return false
}
