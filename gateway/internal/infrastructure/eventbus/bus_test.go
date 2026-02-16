package eventbus

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
)

func testLogger() *zap.Logger {
	logger, _ := zap.NewDevelopment()
	return logger
}

// === NewEvent ===

func TestNewEvent(t *testing.T) {
	ev := NewEvent("test_event", "payload_data")
	if ev.Type() != "test_event" {
		t.Errorf("Type: got %q, want %q", ev.Type(), "test_event")
	}
	if ev.Payload().(string) != "payload_data" {
		t.Errorf("Payload: got %v", ev.Payload())
	}
	if ev.Timestamp().IsZero() {
		t.Error("Timestamp should not be zero")
	}
}

// === InMemoryBus Publish/Subscribe ===

func TestInMemoryBus_PublishSubscribe(t *testing.T) {
	bus := NewInMemoryBus(testLogger(), 100)
	defer bus.Close()

	var received atomic.Int32
	bus.Subscribe("test", func(ctx context.Context, ev Event) {
		received.Add(1)
	})

	bus.Publish(context.Background(), NewEvent("test", nil))
	bus.Publish(context.Background(), NewEvent("test", nil))
	bus.Publish(context.Background(), NewEvent("test", nil))

	// Wait for async dispatch
	time.Sleep(50 * time.Millisecond)

	if got := received.Load(); got != 3 {
		t.Errorf("expected 3 events received, got %d", got)
	}
}

// === Wildcard subscriber ===

func TestInMemoryBus_WildcardSubscriber(t *testing.T) {
	bus := NewInMemoryBus(testLogger(), 100)
	defer bus.Close()

	var received atomic.Int32
	bus.Subscribe("*", func(ctx context.Context, ev Event) {
		received.Add(1)
	})

	bus.Publish(context.Background(), NewEvent("type_a", nil))
	bus.Publish(context.Background(), NewEvent("type_b", nil))
	bus.Publish(context.Background(), NewEvent("type_c", nil))

	time.Sleep(50 * time.Millisecond)

	if got := received.Load(); got != 3 {
		t.Errorf("wildcard should receive all events, got %d", got)
	}
}

// === Multiple subscribers ===

func TestInMemoryBus_MultipleSubscribers(t *testing.T) {
	bus := NewInMemoryBus(testLogger(), 100)
	defer bus.Close()

	var count1, count2 atomic.Int32
	bus.Subscribe("event", func(ctx context.Context, ev Event) {
		count1.Add(1)
	})
	bus.Subscribe("event", func(ctx context.Context, ev Event) {
		count2.Add(1)
	})

	bus.Publish(context.Background(), NewEvent("event", nil))
	time.Sleep(50 * time.Millisecond)

	if count1.Load() != 1 || count2.Load() != 1 {
		t.Errorf("both subscribers should receive: %d, %d", count1.Load(), count2.Load())
	}
}

// === No subscriber for event type ===

func TestInMemoryBus_NoSubscriber(t *testing.T) {
	bus := NewInMemoryBus(testLogger(), 100)
	defer bus.Close()

	// Should not panic
	bus.Publish(context.Background(), NewEvent("unhandled", nil))
	time.Sleep(20 * time.Millisecond)
}

// === Close prevents publish ===

func TestInMemoryBus_ClosePreventsPublish(t *testing.T) {
	bus := NewInMemoryBus(testLogger(), 100)
	bus.Close()

	// Should not panic after close
	bus.Publish(context.Background(), NewEvent("test", nil))
}

// === Handler panic recovery ===

func TestInMemoryBus_HandlerPanicRecovery(t *testing.T) {
	bus := NewInMemoryBus(testLogger(), 100)
	defer bus.Close()

	var safeReceived atomic.Int32

	// Panicking handler
	bus.Subscribe("test", func(ctx context.Context, ev Event) {
		panic("handler crash")
	})
	// Safe handler
	bus.Subscribe("test", func(ctx context.Context, ev Event) {
		safeReceived.Add(1)
	})

	bus.Publish(context.Background(), NewEvent("test", nil))
	time.Sleep(50 * time.Millisecond)

	if safeReceived.Load() != 1 {
		t.Errorf("safe handler should still run after panic, got %d", safeReceived.Load())
	}
}

// === Concurrent publish ===

func TestInMemoryBus_ConcurrentPublish(t *testing.T) {
	bus := NewInMemoryBus(testLogger(), 1000)
	defer bus.Close()

	var received atomic.Int32
	bus.Subscribe("concurrent", func(ctx context.Context, ev Event) {
		received.Add(1)
	})

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bus.Publish(context.Background(), NewEvent("concurrent", nil))
		}()
	}
	wg.Wait()
	time.Sleep(100 * time.Millisecond)

	if got := received.Load(); got != 100 {
		t.Errorf("expected 100 concurrent events, got %d", got)
	}
}

// === Event payload types ===

func TestInMemoryBus_PayloadTypes(t *testing.T) {
	bus := NewInMemoryBus(testLogger(), 100)
	defer bus.Close()

	var receivedPayload any
	done := make(chan struct{})
	bus.Subscribe("typed", func(ctx context.Context, ev Event) {
		receivedPayload = ev.Payload()
		close(done)
	})

	payload := StateChangePayload{
		SessionID: "sess_123",
		FromState: "idle",
		ToState:   "processing",
		Trigger:   "message_received",
	}
	bus.Publish(context.Background(), NewEvent("typed", payload))

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}

	got, ok := receivedPayload.(StateChangePayload)
	if !ok {
		t.Fatalf("payload type mismatch: %T", receivedPayload)
	}
	if got.SessionID != "sess_123" || got.FromState != "idle" {
		t.Errorf("payload content wrong: %+v", got)
	}
}
