package eventbus

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestPersistentBus_PublishAndReplay(t *testing.T) {
	dir := t.TempDir()
	logger := zap.NewNop()

	// Phase 1: Publish events
	bus, err := NewPersistentBus(PersistentBusConfig{
		WALDir:     dir,
		BufferSize: 64,
	}, logger)
	if err != nil {
		t.Fatalf("failed to create bus: %v", err)
	}

	ctx := context.Background()
	bus.Publish(ctx, NewEvent("test.created", map[string]string{"id": "1"}))
	bus.Publish(ctx, NewEvent("test.updated", map[string]string{"id": "2"}))
	bus.Publish(ctx, NewEvent("test.deleted", map[string]string{"id": "3"}))
	time.Sleep(50 * time.Millisecond) // Wait for dispatch
	bus.Close()

	// Verify WAL file exists
	walPath := filepath.Join(dir, "events.wal")
	info, err := os.Stat(walPath)
	if err != nil {
		t.Fatalf("WAL file not found: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("WAL file is empty")
	}

	// Phase 2: Replay events into a new bus
	bus2, err := NewPersistentBus(PersistentBusConfig{
		WALDir:     dir,
		BufferSize: 64,
	}, logger)
	if err != nil {
		t.Fatalf("failed to create bus2: %v", err)
	}
	defer bus2.Close()

	var mu sync.Mutex
	replayed := make([]string, 0)
	bus2.Subscribe("*", func(ctx context.Context, event Event) {
		mu.Lock()
		replayed = append(replayed, event.Type())
		mu.Unlock()
	})

	count, err := bus2.Replay(ctx)
	if err != nil {
		t.Fatalf("replay error: %v", err)
	}
	time.Sleep(50 * time.Millisecond) // Wait for dispatch

	if count != 3 {
		t.Fatalf("expected 3 replayed events, got %d", count)
	}

	mu.Lock()
	if len(replayed) != 3 {
		t.Fatalf("expected 3 handler calls, got %d", len(replayed))
	}
	mu.Unlock()
}

func TestPersistentBus_Truncate(t *testing.T) {
	dir := t.TempDir()
	logger := zap.NewNop()

	bus, err := NewPersistentBus(PersistentBusConfig{
		WALDir:     dir,
		BufferSize: 64,
	}, logger)
	if err != nil {
		t.Fatalf("failed to create bus: %v", err)
	}
	defer bus.Close()

	ctx := context.Background()
	bus.Publish(ctx, NewEvent("test.event", nil))
	time.Sleep(20 * time.Millisecond)

	if bus.WALSize() == 0 {
		t.Fatal("expected non-zero WAL size after publish")
	}

	if err := bus.Truncate(); err != nil {
		t.Fatalf("truncate error: %v", err)
	}

	if bus.WALSize() != 0 {
		t.Fatal("expected zero WAL size after truncate")
	}
}

func TestPersistentBus_WALRotation(t *testing.T) {
	dir := t.TempDir()
	logger := zap.NewNop()

	// Set tiny max size to trigger rotation
	bus, err := NewPersistentBus(PersistentBusConfig{
		WALDir:     dir,
		BufferSize: 256,
		MaxWALSize: 100, // 100 bytes — will rotate almost immediately
	}, logger)
	if err != nil {
		t.Fatalf("failed to create bus: %v", err)
	}
	defer bus.Close()

	ctx := context.Background()
	// Write enough events to trigger rotation
	for i := 0; i < 10; i++ {
		bus.Publish(ctx, NewEvent("test.rotation", map[string]int{"i": i}))
	}
	time.Sleep(50 * time.Millisecond)

	// Check .old file exists
	oldPath := filepath.Join(dir, "events.wal.old")
	if _, err := os.Stat(oldPath); os.IsNotExist(err) {
		t.Fatal("expected .old WAL file after rotation")
	}
}

func TestPersistentBus_ImplementsBusInterface(t *testing.T) {
	dir := t.TempDir()
	logger := zap.NewNop()

	bus, err := NewPersistentBus(PersistentBusConfig{
		WALDir: dir,
	}, logger)
	if err != nil {
		t.Fatalf("failed to create bus: %v", err)
	}
	defer bus.Close()

	// Compile-time check: PersistentBus implements Bus
	var _ Bus = bus
}
