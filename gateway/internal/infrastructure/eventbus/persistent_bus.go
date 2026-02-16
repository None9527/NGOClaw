package eventbus

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.uber.org/zap"
)

// PersistentBus wraps InMemoryBus with a Write-Ahead Log (WAL) for event persistence.
//
// Events are serialized as JSON lines to a WAL file before dispatch.
// On recovery, Replay() reads the WAL and re-emits events to handlers.
// Rotation keeps the WAL from growing unbounded.
type PersistentBus struct {
	inner   *InMemoryBus
	walFile *os.File
	writer  *bufio.Writer
	walPath string
	mu      sync.Mutex // protects file writes
	logger  *zap.Logger

	// Rotation config
	maxWALSize int64 // bytes; 0 = no rotation (default: 10MB)
	written    int64
}

// walEntry is the JSON-serializable form of an event on disk.
type walEntry struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"ts"`
	Payload   any       `json:"payload"`
}

// PersistentBusConfig configures the persistent event bus.
type PersistentBusConfig struct {
	WALDir     string // Directory for WAL files (required)
	BufferSize int    // Channel buffer size for InMemoryBus (default: 256)
	MaxWALSize int64  // Max WAL file size before rotation (default: 10MB, 0 = disabled)
}

// NewPersistentBus creates a persistent event bus backed by a WAL file.
func NewPersistentBus(cfg PersistentBusConfig, logger *zap.Logger) (*PersistentBus, error) {
	if cfg.WALDir == "" {
		return nil, fmt.Errorf("WALDir is required")
	}
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = 256
	}
	if cfg.MaxWALSize <= 0 {
		cfg.MaxWALSize = 10 * 1024 * 1024 // 10MB
	}

	// Ensure WAL directory exists
	if err := os.MkdirAll(cfg.WALDir, 0755); err != nil {
		return nil, fmt.Errorf("create WAL dir: %w", err)
	}

	walPath := filepath.Join(cfg.WALDir, "events.wal")
	f, err := os.OpenFile(walPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("open WAL file: %w", err)
	}

	// Get current file size for rotation tracking
	stat, _ := f.Stat()
	var currentSize int64
	if stat != nil {
		currentSize = stat.Size()
	}

	inner := NewInMemoryBus(logger, cfg.BufferSize)

	return &PersistentBus{
		inner:      inner,
		walFile:    f,
		writer:     bufio.NewWriterSize(f, 64*1024), // 64KB write buffer
		walPath:    walPath,
		logger:     logger.With(zap.String("component", "persistent-bus")),
		maxWALSize: cfg.MaxWALSize,
		written:    currentSize,
	}, nil
}

// Publish persists the event to the WAL, then delegates to InMemoryBus for dispatch.
func (b *PersistentBus) Publish(ctx context.Context, event Event) {
	// Write to WAL first (write-ahead)
	entry := walEntry{
		Type:      event.Type(),
		Timestamp: event.Timestamp(),
		Payload:   event.Payload(),
	}

	data, err := json.Marshal(entry)
	if err != nil {
		b.logger.Error("Failed to marshal event for WAL",
			zap.String("type", event.Type()),
			zap.Error(err),
		)
	} else {
		b.mu.Lock()
		n, writeErr := b.writer.Write(append(data, '\n'))
		if writeErr != nil {
			b.logger.Error("WAL write failed",
				zap.String("type", event.Type()),
				zap.Error(writeErr),
			)
		}
		b.written += int64(n)

		// Flush periodically for durability
		_ = b.writer.Flush()

		// Check rotation
		if b.maxWALSize > 0 && b.written >= b.maxWALSize {
			b.rotateLocked()
		}
		b.mu.Unlock()
	}

	// Dispatch to in-memory bus
	b.inner.Publish(ctx, event)
}

// Subscribe delegates to InMemoryBus.
func (b *PersistentBus) Subscribe(eventType string, handler Handler) {
	b.inner.Subscribe(eventType, handler)
}

// Unsubscribe delegates to InMemoryBus.
func (b *PersistentBus) Unsubscribe(eventType string, handler Handler) {
	b.inner.Unsubscribe(eventType, handler)
}

// Close flushes the WAL and shuts down the bus.
func (b *PersistentBus) Close() {
	b.mu.Lock()
	_ = b.writer.Flush()
	_ = b.walFile.Sync()
	_ = b.walFile.Close()
	b.mu.Unlock()

	b.inner.Close()
	b.logger.Info("Persistent event bus closed")
}

// Replay reads the WAL file and re-emits events to registered handlers.
// This should be called after Subscribe but before normal operation.
// Returns the number of events replayed.
func (b *PersistentBus) Replay(ctx context.Context) (int, error) {
	f, err := os.Open(b.walPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil // No WAL file, nothing to replay
		}
		return 0, fmt.Errorf("open WAL for replay: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // 1MB max line

	count := 0
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return count, ctx.Err()
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var entry walEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			b.logger.Warn("Skipping corrupt WAL entry",
				zap.Error(err),
			)
			continue
		}

		event := &BaseEvent{
			EventType:      entry.Type,
			EventTimestamp: entry.Timestamp,
			EventPayload:   entry.Payload,
		}

		b.inner.Publish(ctx, event)
		count++
	}

	if err := scanner.Err(); err != nil {
		return count, fmt.Errorf("WAL scan error: %w", err)
	}

	b.logger.Info("WAL replay complete",
		zap.Int("events_replayed", count),
	)
	return count, nil
}

// Truncate clears the WAL file, resetting the log.
// Useful after a clean snapshot or checkpoint.
func (b *PersistentBus) Truncate() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	_ = b.writer.Flush()
	_ = b.walFile.Close()

	f, err := os.Create(b.walPath) // truncate and reopen
	if err != nil {
		return fmt.Errorf("truncate WAL: %w", err)
	}

	b.walFile = f
	b.writer = bufio.NewWriterSize(f, 64*1024)
	b.written = 0

	b.logger.Info("WAL truncated")
	return nil
}

// rotateLocked rotates the WAL file (must be called with b.mu held).
func (b *PersistentBus) rotateLocked() {
	_ = b.writer.Flush()
	_ = b.walFile.Close()

	// Rename current WAL to .old (simple single-file rotation)
	oldPath := b.walPath + ".old"
	_ = os.Remove(oldPath)
	_ = os.Rename(b.walPath, oldPath)

	f, err := os.OpenFile(b.walPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		b.logger.Error("WAL rotation failed", zap.Error(err))
		return
	}

	b.walFile = f
	b.writer = bufio.NewWriterSize(f, 64*1024)
	b.written = 0

	b.logger.Info("WAL rotated",
		zap.String("old_path", oldPath),
	)
}

// WALSize returns the current WAL file size in bytes.
func (b *PersistentBus) WALSize() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.written
}
