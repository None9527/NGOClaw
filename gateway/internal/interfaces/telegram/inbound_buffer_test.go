package telegram

import (
	"context"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
)

// collectHandler collects messages into a thread-safe slice
type collectHandler struct {
	mu   sync.Mutex
	msgs []*IncomingMessage
	done chan struct{}
}

func newCollectHandler() *collectHandler {
	return &collectHandler{
		done: make(chan struct{}, 100),
	}
}

func (h *collectHandler) handler() InboundHandler {
	return func(ctx context.Context, msg *IncomingMessage) {
		h.mu.Lock()
		h.msgs = append(h.msgs, msg)
		h.mu.Unlock()
		h.done <- struct{}{}
	}
}

func (h *collectHandler) waitN(n int, timeout time.Duration) []*IncomingMessage {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for i := 0; i < n; i++ {
		select {
		case <-h.done:
		case <-timer.C:
			break
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	result := make([]*IncomingMessage, len(h.msgs))
	copy(result, h.msgs)
	return result
}

func (h *collectHandler) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.msgs)
}

func testLogger() *zap.Logger {
	logger, _ := zap.NewDevelopment()
	return logger
}

// --- Test: Commands bypass buffer ---

func TestInboundBuffer_CommandBypassesBuffer(t *testing.T) {
	h := newCollectHandler()
	buf := NewInboundBuffer(h.handler(), testLogger())

	msg := &IncomingMessage{
		MessageID: 1,
		ChatID:    100,
		UserID:    1,
		Text:      "/help",
	}

	buf.Submit(context.Background(), msg, "")

	// Commands should be delivered immediately (no buffering)
	msgs := h.waitN(1, 500*time.Millisecond)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Text != "/help" {
		t.Fatalf("expected '/help', got '%s'", msgs[0].Text)
	}
}

// --- Test: Debounce merges rapid messages ---

func TestInboundBuffer_DebounceMergesMessages(t *testing.T) {
	h := newCollectHandler()
	buf := NewInboundBuffer(h.handler(), testLogger())

	ctx := context.Background()

	// Send 3 rapid messages
	buf.Submit(ctx, &IncomingMessage{MessageID: 1, ChatID: 100, UserID: 1, Text: "Hello"}, "")
	buf.Submit(ctx, &IncomingMessage{MessageID: 2, ChatID: 100, UserID: 1, Text: "How are you"}, "")
	buf.Submit(ctx, &IncomingMessage{MessageID: 3, ChatID: 100, UserID: 1, Text: "Today"}, "")

	// Wait for debounce window + processing
	msgs := h.waitN(1, 3*time.Second)

	if len(msgs) != 1 {
		t.Fatalf("expected 1 debounced message, got %d", len(msgs))
	}

	// Should be merged with newlines
	expected := "Hello\nHow are you\nToday"
	if msgs[0].Text != expected {
		t.Fatalf("expected '%s', got '%s'", expected, msgs[0].Text)
	}
}

// --- Test: Text fragment reassembly ---

func TestInboundBuffer_TextFragmentReassembly(t *testing.T) {
	h := newCollectHandler()
	buf := NewInboundBuffer(h.handler(), testLogger())

	ctx := context.Background()

	// Create a long text (>4000 chars) — this starts a fragment sequence
	longText := make([]byte, 4100)
	for i := range longText {
		longText[i] = 'A'
	}

	// First fragment
	buf.Submit(ctx, &IncomingMessage{
		MessageID: 100,
		ChatID:    200,
		UserID:    1,
		Text:      string(longText),
	}, "")

	// Second fragment (adjacent message_id, within time window)
	buf.Submit(ctx, &IncomingMessage{
		MessageID: 101,
		ChatID:    200,
		UserID:    1,
		Text:      "BBBB",
	}, "")

	// Wait for fragment timeout
	msgs := h.waitN(1, 3*time.Second)

	if len(msgs) != 1 {
		t.Fatalf("expected 1 reassembled message, got %d", len(msgs))
	}

	expectedLen := 4100 + 4
	if len(msgs[0].Text) != expectedLen {
		t.Fatalf("expected %d chars, got %d", expectedLen, len(msgs[0].Text))
	}
}

// --- Test: Media group merging ---

func TestInboundBuffer_MediaGroupMerge(t *testing.T) {
	h := newCollectHandler()
	buf := NewInboundBuffer(h.handler(), testLogger())

	ctx := context.Background()
	groupID := "album123"

	// Send 3 photos as a media group
	buf.Submit(ctx, &IncomingMessage{
		MessageID: 1,
		ChatID:    100,
		UserID:    1,
		Text:      "My vacation photos",
		Media:     &MediaInfo{Type: MediaTypePhoto, FileID: "file1", MimeType: "image/jpeg"},
	}, groupID)

	buf.Submit(ctx, &IncomingMessage{
		MessageID: 2,
		ChatID:    100,
		UserID:    1,
		Media:     &MediaInfo{Type: MediaTypePhoto, FileID: "file2", MimeType: "image/jpeg"},
	}, groupID)

	buf.Submit(ctx, &IncomingMessage{
		MessageID: 3,
		ChatID:    100,
		UserID:    1,
		Media:     &MediaInfo{Type: MediaTypePhoto, FileID: "file3", MimeType: "image/jpeg"},
	}, groupID)

	// Wait for media group timeout
	msgs := h.waitN(1, 2*time.Second)

	if len(msgs) != 1 {
		t.Fatalf("expected 1 merged media group, got %d", len(msgs))
	}

	if msgs[0].Text != "My vacation photos" {
		t.Fatalf("expected caption 'My vacation photos', got '%s'", msgs[0].Text)
	}

	if len(msgs[0].MediaGroup) != 3 {
		t.Fatalf("expected 3 media items, got %d", len(msgs[0].MediaGroup))
	}
}

// --- Test: Media bypasses debounce ---

func TestInboundBuffer_MediaBypassesDebounce(t *testing.T) {
	h := newCollectHandler()
	buf := NewInboundBuffer(h.handler(), testLogger())

	ctx := context.Background()

	// Single media message with no group_id should pass through immediately
	buf.Submit(ctx, &IncomingMessage{
		MessageID: 1,
		ChatID:    100,
		UserID:    1,
		Text:      "Check this out",
		Media:     &MediaInfo{Type: MediaTypePhoto, FileID: "file1"},
	}, "")

	msgs := h.waitN(1, 500*time.Millisecond)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
}

// --- Test: isCommand ---

func TestIsCommand(t *testing.T) {
	tests := []struct {
		text     string
		expected bool
	}{
		{"/help", true},
		{"/model gpt-4", true},
		{"hello", false},
		{"", false},
		{"not a /command", false},
		{" /help", false}, // space before /
	}

	for _, tt := range tests {
		result := isCommand(tt.text)
		if result != tt.expected {
			t.Errorf("isCommand(%q) = %v, want %v", tt.text, result, tt.expected)
		}
	}
}
