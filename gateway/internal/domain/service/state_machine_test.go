package service

import (
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
)

func testLogger() *zap.Logger {
	logger, _ := zap.NewDevelopment()
	return logger
}

// === StateMachine creation ===

func TestNewStateMachine(t *testing.T) {
	sm := NewStateMachine(10, testLogger())
	if sm.State() != StateIdle {
		t.Errorf("expected initial state Idle, got %s", sm.State())
	}
	if sm.IsTerminal() {
		t.Error("new state machine should not be terminal")
	}
	snap := sm.Snapshot()
	if snap.MaxSteps != 10 {
		t.Errorf("expected MaxSteps=10, got %d", snap.MaxSteps)
	}
}

// === Valid transitions ===

func TestTransition_ValidPaths(t *testing.T) {
	tests := []struct {
		name string
		path []AgentState
	}{
		{
			name: "idle -> planning -> streaming -> complete",
			path: []AgentState{StatePlanning, StateStreaming, StateComplete},
		},
		{
			name: "idle -> streaming -> tool_exec -> streaming -> complete",
			path: []AgentState{StateStreaming, StateToolExec, StateStreaming, StateComplete},
		},
		{
			name: "idle -> streaming -> compacting -> streaming -> complete",
			path: []AgentState{StateStreaming, StateCompacting, StateStreaming, StateComplete},
		},
		{
			name: "idle -> streaming -> retrying -> streaming -> complete",
			path: []AgentState{StateStreaming, StateRetrying, StateStreaming, StateComplete},
		},
		{
			name: "idle -> streaming -> error",
			path: []AgentState{StateStreaming, StateError},
		},
		{
			name: "idle -> planning -> aborted",
			path: []AgentState{StatePlanning, StateAborted},
		},
		{
			name: "idle -> streaming -> tool_exec -> aborted",
			path: []AgentState{StateStreaming, StateToolExec, StateAborted},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := NewStateMachine(25, testLogger())
			for _, state := range tt.path {
				if err := sm.Transition(state); err != nil {
					t.Fatalf("failed transition to %s: %v", state, err)
				}
			}
			last := tt.path[len(tt.path)-1]
			if sm.State() != last {
				t.Errorf("expected state %s, got %s", last, sm.State())
			}
		})
	}
}

// === Invalid transitions ===

func TestTransition_InvalidPaths(t *testing.T) {
	tests := []struct {
		name string
		from AgentState
		to   AgentState
	}{
		{"idle -> complete", StateIdle, StateComplete},
		{"idle -> tool_exec", StateIdle, StateToolExec},
		{"idle -> error", StateIdle, StateError},
		{"planning -> tool_exec", StatePlanning, StateToolExec},
		{"complete -> idle (terminal)", StateComplete, StateIdle},
		{"error -> idle (terminal)", StateError, StateIdle},
		{"aborted -> streaming (terminal)", StateAborted, StateStreaming},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := NewStateMachine(10, testLogger())
			// Navigate to the 'from' state
			switch tt.from {
			case StatePlanning:
				_ = sm.Transition(StatePlanning)
			case StateStreaming:
				_ = sm.Transition(StateStreaming)
			case StateToolExec:
				_ = sm.Transition(StateStreaming)
				_ = sm.Transition(StateToolExec)
			case StateComplete:
				_ = sm.Transition(StateStreaming)
				_ = sm.Transition(StateComplete)
			case StateError:
				_ = sm.Transition(StateStreaming)
				_ = sm.Transition(StateError)
			case StateAborted:
				_ = sm.Transition(StatePlanning)
				_ = sm.Transition(StateAborted)
			}

			err := sm.Transition(tt.to)
			if err == nil {
				t.Errorf("expected error for %s → %s, got nil", tt.from, tt.to)
			}
		})
	}
}

// === Terminal states ===

func TestIsTerminal(t *testing.T) {
	tests := []struct {
		state    AgentState
		terminal bool
	}{
		{StateIdle, false},
		{StatePlanning, false},
		{StateStreaming, false},
		{StateToolExec, false},
		{StateCompacting, false},
		{StateRetrying, false},
		{StateComplete, true},
		{StateError, true},
		{StateAborted, true},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			sm := NewStateMachine(10, testLogger())
			// Navigate to state
			switch tt.state {
			case StatePlanning:
				_ = sm.Transition(StatePlanning)
			case StateStreaming:
				_ = sm.Transition(StateStreaming)
			case StateToolExec:
				_ = sm.Transition(StateStreaming)
				_ = sm.Transition(StateToolExec)
			case StateCompacting:
				_ = sm.Transition(StateStreaming)
				_ = sm.Transition(StateCompacting)
			case StateRetrying:
				_ = sm.Transition(StateStreaming)
				_ = sm.Transition(StateRetrying)
			case StateComplete:
				_ = sm.Transition(StateStreaming)
				_ = sm.Transition(StateComplete)
			case StateError:
				_ = sm.Transition(StateStreaming)
				_ = sm.Transition(StateError)
			case StateAborted:
				_ = sm.Transition(StatePlanning)
				_ = sm.Transition(StateAborted)
			}

			if sm.IsTerminal() != tt.terminal {
				t.Errorf("IsTerminal() for %s: got %v, want %v", tt.state, sm.IsTerminal(), tt.terminal)
			}
		})
	}
}

// === Mutation helpers ===

func TestMutationHelpers(t *testing.T) {
	sm := NewStateMachine(10, testLogger())

	sm.SetStep(5)
	sm.AddTokens(1000)
	sm.AddTokens(500)
	sm.RecordToolExec("shell_exec")
	sm.RecordToolExec("file_read")
	sm.RecordRetry()
	sm.RecordError()
	sm.SetModel("gpt-4o")

	snap := sm.Snapshot()
	if snap.Step != 5 {
		t.Errorf("Step: got %d, want 5", snap.Step)
	}
	if snap.TokensUsed != 1500 {
		t.Errorf("TokensUsed: got %d, want 1500", snap.TokensUsed)
	}
	if snap.ToolsExecuted != 2 {
		t.Errorf("ToolsExecuted: got %d, want 2", snap.ToolsExecuted)
	}
	if snap.LastTool != "file_read" {
		t.Errorf("LastTool: got %s, want file_read", snap.LastTool)
	}
	if snap.RetryCount != 1 {
		t.Errorf("RetryCount: got %d, want 1", snap.RetryCount)
	}
	if snap.ErrorCount != 1 {
		t.Errorf("ErrorCount: got %d, want 1", snap.ErrorCount)
	}
	if snap.ModelUsed != "gpt-4o" {
		t.Errorf("ModelUsed: got %s, want gpt-4o", snap.ModelUsed)
	}
	if snap.Elapsed <= 0 {
		t.Error("Elapsed should be positive")
	}
}

// === OnTransition listener ===

func TestOnTransitionListener(t *testing.T) {
	sm := NewStateMachine(10, testLogger())

	var transitions []struct{ from, to AgentState }
	sm.OnTransition(func(from, to AgentState, snap StateSnapshot) {
		transitions = append(transitions, struct{ from, to AgentState }{from, to})
	})

	_ = sm.Transition(StateStreaming)
	_ = sm.Transition(StateToolExec)
	_ = sm.Transition(StateStreaming)
	_ = sm.Transition(StateComplete)

	if len(transitions) != 4 {
		t.Fatalf("expected 4 transitions, got %d", len(transitions))
	}
	expected := []struct{ from, to AgentState }{
		{StateIdle, StateStreaming},
		{StateStreaming, StateToolExec},
		{StateToolExec, StateStreaming},
		{StateStreaming, StateComplete},
	}
	for i, exp := range expected {
		if transitions[i].from != exp.from || transitions[i].to != exp.to {
			t.Errorf("transition[%d]: got %s→%s, want %s→%s",
				i, transitions[i].from, transitions[i].to, exp.from, exp.to)
		}
	}
}

// === Thread safety ===

func TestStateMachine_ConcurrentAccess(t *testing.T) {
	sm := NewStateMachine(100, testLogger())
	_ = sm.Transition(StateStreaming)

	var wg sync.WaitGroup
	// Concurrent readers
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = sm.State()
			_ = sm.Snapshot()
			_ = sm.IsTerminal()
		}()
	}
	// Concurrent writers
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			sm.AddTokens(100)
			sm.SetStep(n)
			sm.RecordToolExec("test_tool")
		}(i)
	}
	wg.Wait()

	snap := sm.Snapshot()
	if snap.TokensUsed != 2000 {
		t.Errorf("concurrent TokensUsed: got %d, want 2000", snap.TokensUsed)
	}
	if snap.ToolsExecuted != 20 {
		t.Errorf("concurrent ToolsExecuted: got %d, want 20", snap.ToolsExecuted)
	}
}

// === Snapshot isolation ===

func TestSnapshot_Isolation(t *testing.T) {
	sm := NewStateMachine(10, testLogger())
	sm.SetStep(3)
	sm.AddTokens(500)

	snap1 := sm.Snapshot()

	// Mutate after snapshot
	sm.SetStep(8)
	sm.AddTokens(1000)

	snap2 := sm.Snapshot()

	if snap1.Step != 3 || snap1.TokensUsed != 500 {
		t.Error("snap1 was mutated after capture")
	}
	if snap2.Step != 8 || snap2.TokensUsed != 1500 {
		t.Errorf("snap2 wrong: step=%d tokens=%d", snap2.Step, snap2.TokensUsed)
	}
}

// === Elapsed increases ===

func TestSnapshot_ElapsedIncreases(t *testing.T) {
	sm := NewStateMachine(10, testLogger())
	snap1 := sm.Snapshot()
	time.Sleep(5 * time.Millisecond)
	snap2 := sm.Snapshot()
	if snap2.Elapsed <= snap1.Elapsed {
		t.Errorf("elapsed should increase: %v <= %v", snap2.Elapsed, snap1.Elapsed)
	}
}
