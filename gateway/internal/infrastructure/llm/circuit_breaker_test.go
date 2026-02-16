package llm

import (
	"testing"
	"time"
)

func TestCircuitBreaker_ClosedByDefault(t *testing.T) {
	cb := NewCircuitBreaker(3, 100*time.Millisecond)
	if cb.State() != CircuitClosed {
		t.Fatal("expected closed state by default")
	}
	if !cb.Allow() {
		t.Fatal("expected allow in closed state")
	}
}

func TestCircuitBreaker_OpensAfterThreshold(t *testing.T) {
	cb := NewCircuitBreaker(3, 100*time.Millisecond)

	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != CircuitClosed {
		t.Fatal("should still be closed after 2 failures")
	}

	cb.RecordFailure() // 3rd failure
	if cb.State() != CircuitOpen {
		t.Fatal("should be open after 3 failures")
	}
	if cb.Allow() {
		t.Fatal("should not allow when open")
	}
}

func TestCircuitBreaker_SuccessResetsFailureCount(t *testing.T) {
	cb := NewCircuitBreaker(3, 100*time.Millisecond)

	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordSuccess() // Resets failure count
	cb.RecordFailure()
	cb.RecordFailure()

	if cb.State() != CircuitClosed {
		t.Fatal("should still be closed — success reset the failure count")
	}
}

func TestCircuitBreaker_HalfOpenAfterTimeout(t *testing.T) {
	cb := NewCircuitBreaker(2, 10*time.Millisecond)

	cb.RecordFailure()
	cb.RecordFailure() // Opens
	if cb.State() != CircuitOpen {
		t.Fatal("should be open")
	}

	time.Sleep(15 * time.Millisecond)

	if !cb.Allow() {
		t.Fatal("should allow probe after recovery timeout")
	}
	if cb.State() != CircuitHalfOpen {
		t.Fatal("should be half-open after recovery timeout")
	}
}

func TestCircuitBreaker_HalfOpenClosesOnSuccess(t *testing.T) {
	cb := NewCircuitBreaker(2, 10*time.Millisecond)

	cb.RecordFailure()
	cb.RecordFailure()
	time.Sleep(15 * time.Millisecond)
	cb.Allow() // Transitions to half-open

	cb.RecordSuccess() // Should close
	if cb.State() != CircuitClosed {
		t.Fatal("should be closed after success in half-open")
	}
}

func TestCircuitBreaker_HalfOpenReopensOnFailure(t *testing.T) {
	cb := NewCircuitBreaker(2, 10*time.Millisecond)

	cb.RecordFailure()
	cb.RecordFailure()
	time.Sleep(15 * time.Millisecond)
	cb.Allow() // Transitions to half-open

	cb.RecordFailure() // Should re-open
	if cb.State() != CircuitOpen {
		t.Fatal("should re-open after failure in half-open")
	}
}

func TestCircuitBreaker_Reset(t *testing.T) {
	cb := NewCircuitBreaker(2, 100*time.Millisecond)

	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != CircuitOpen {
		t.Fatal("should be open")
	}

	cb.Reset()
	if cb.State() != CircuitClosed {
		t.Fatal("should be closed after reset")
	}
	if !cb.Allow() {
		t.Fatal("should allow after reset")
	}
}

func TestCircuitBreaker_StateStrings(t *testing.T) {
	tests := []struct {
		state CircuitState
		want  string
	}{
		{CircuitClosed, "closed"},
		{CircuitOpen, "open"},
		{CircuitHalfOpen, "half_open"},
		{CircuitState(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("CircuitState(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}
