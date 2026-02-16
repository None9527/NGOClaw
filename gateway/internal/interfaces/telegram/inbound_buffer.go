package telegram

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Inbound buffer constants — match OpenClaw thresholds
const (
	// Text fragment reassembly: Telegram splits pastes >4096 chars
	fragmentStartThreshold = 4000  // chars to consider a message as a potential fragment
	fragmentMaxGapMs       = 1500  // max ms between fragments
	fragmentMaxIDGap       = 1     // max message_id gap between fragments
	fragmentMaxParts       = 12    // max fragments to reassemble
	fragmentMaxTotalChars  = 50000 // max total chars after reassembly

	// Debounce: merge rapid short messages into one
	debounceWindowMs = 1500

	// Media group: buffer album messages by media_group_id
	mediaGroupTimeoutMs = 500
)

// InboundBuffer merges rapid-fire Telegram messages before forwarding
// to the message handler. Handles three scenarios:
//  1. Text fragments — long paste split by Telegram into multiple messages
//  2. Debounce — user sends several short messages quickly
//  3. Media groups — album (multiple photos/videos) sent as a group
type InboundBuffer struct {
	fragments   map[string]*fragmentEntry
	debounce    map[string]*debounceEntry
	mediaGroups map[string]*mediaGroupEntry
	handler     InboundHandler
	logger      *zap.Logger
	mu          sync.Mutex
}

// InboundHandler is called when a buffered message is ready
type InboundHandler func(ctx context.Context, msg *IncomingMessage)

type fragmentEntry struct {
	key      string
	messages []bufferedMessage
	timer    *time.Timer
}

type debounceEntry struct {
	key      string
	messages []bufferedMessage
	timer    *time.Timer
}

type mediaGroupEntry struct {
	groupID  string
	messages []bufferedMessage
	timer    *time.Timer
}

type bufferedMessage struct {
	ctx        context.Context
	msg        *IncomingMessage
	receivedAt time.Time
}

// NewInboundBuffer creates a new inbound buffer
func NewInboundBuffer(handler InboundHandler, logger *zap.Logger) *InboundBuffer {
	return &InboundBuffer{
		fragments:   make(map[string]*fragmentEntry),
		debounce:    make(map[string]*debounceEntry),
		mediaGroups: make(map[string]*mediaGroupEntry),
		handler:     handler,
		logger:      logger,
	}
}

// Submit processes an incoming message through the appropriate buffer
func (b *InboundBuffer) Submit(ctx context.Context, msg *IncomingMessage, mediaGroupID string) {
	// Media group messages are always buffered by group ID
	if mediaGroupID != "" {
		b.submitMediaGroup(ctx, msg, mediaGroupID)
		return
	}

	// Commands bypass all buffering
	if isCommand(msg.Text) {
		b.handler(ctx, msg)
		return
	}

	// Check if this is a text fragment (long message that may have been split)
	if b.tryAppendFragment(ctx, msg) {
		return
	}

	// Check if this starts a new fragment sequence
	if len(msg.Text) >= fragmentStartThreshold && msg.Media == nil {
		b.startFragment(ctx, msg)
		return
	}

	// Messages with media bypass debounce
	if msg.Media != nil {
		b.handler(ctx, msg)
		return
	}

	// Empty text — no point buffering
	if strings.TrimSpace(msg.Text) == "" {
		b.handler(ctx, msg)
		return
	}

	// Short text messages go through debounce
	b.submitDebounce(ctx, msg)
}

// isCommand checks if text starts with /
func isCommand(text string) bool {
	if len(text) == 0 {
		return false
	}
	return text[0] == '/'
}

// --- Text Fragment Reassembly ---

func (b *InboundBuffer) fragmentKey(msg *IncomingMessage) string {
	return fmt.Sprintf("frag:%d:%d", msg.ChatID, msg.UserID)
}

func (b *InboundBuffer) tryAppendFragment(ctx context.Context, msg *IncomingMessage) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	key := b.fragmentKey(msg)
	entry, exists := b.fragments[key]
	if !exists {
		return false
	}

	last := entry.messages[len(entry.messages)-1]
	idGap := msg.MessageID - last.msg.MessageID
	timeGap := time.Since(last.receivedAt)

	// Check if this message can be appended as a fragment
	canAppend := idGap > 0 && idGap <= fragmentMaxIDGap &&
		timeGap <= time.Duration(fragmentMaxGapMs)*time.Millisecond &&
		len(entry.messages) < fragmentMaxParts

	if canAppend {
		// Check total chars
		totalChars := 0
		for _, m := range entry.messages {
			totalChars += len(m.msg.Text)
		}
		if totalChars+len(msg.Text) > fragmentMaxTotalChars {
			canAppend = false
		}
	}

	if !canAppend {
		// Gap too large or timeout — flush accumulated fragments immediately
		entry.timer.Stop()
		delete(b.fragments, key)
		b.flushFragmentLocked(entry)
		return false
	}

	// Append fragment
	entry.messages = append(entry.messages, bufferedMessage{
		ctx:        ctx,
		msg:        msg,
		receivedAt: time.Now(),
	})

	// Reset timer
	entry.timer.Reset(time.Duration(fragmentMaxGapMs) * time.Millisecond)

	b.logger.Debug("Text fragment appended",
		zap.Int64("chat_id", msg.ChatID),
		zap.Int("part", len(entry.messages)),
		zap.Int("msg_id", msg.MessageID),
	)

	return true
}

func (b *InboundBuffer) startFragment(ctx context.Context, msg *IncomingMessage) {
	b.mu.Lock()
	defer b.mu.Unlock()

	key := b.fragmentKey(msg)

	// Flush any existing fragment for this key
	if existing, ok := b.fragments[key]; ok {
		existing.timer.Stop()
		b.flushFragmentLocked(existing)
	}

	entry := &fragmentEntry{
		key: key,
		messages: []bufferedMessage{{
			ctx:        ctx,
			msg:        msg,
			receivedAt: time.Now(),
		}},
	}
	entry.timer = time.AfterFunc(time.Duration(fragmentMaxGapMs)*time.Millisecond, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if e, ok := b.fragments[key]; ok {
			delete(b.fragments, key)
			b.flushFragmentLocked(e)
		}
	})

	b.fragments[key] = entry

	b.logger.Debug("Text fragment sequence started",
		zap.Int64("chat_id", msg.ChatID),
		zap.Int("text_len", len(msg.Text)),
	)
}

func (b *InboundBuffer) flushFragmentLocked(entry *fragmentEntry) {
	if len(entry.messages) == 0 {
		return
	}

	// Sort by message ID
	sortBuffered(entry.messages)

	// Combine text using strings.Builder for efficiency
	first := entry.messages[0]
	var sb strings.Builder
	for _, m := range entry.messages {
		sb.WriteString(m.msg.Text)
	}
	combined := sb.String()

	last := entry.messages[len(entry.messages)-1]

	// Create merged message — preserve ReplyToMessage from first
	merged := &IncomingMessage{
		MessageID:      last.msg.MessageID,
		ChatID:         first.msg.ChatID,
		UserID:         first.msg.UserID,
		Username:       first.msg.Username,
		Text:           combined,
		Timestamp:      first.msg.Timestamp,
		ReplyToMessage: first.msg.ReplyToMessage,
	}

	b.logger.Info("Text fragments reassembled",
		zap.Int64("chat_id", merged.ChatID),
		zap.Int("parts", len(entry.messages)),
		zap.Int("total_chars", len(combined)),
	)

	go b.handler(first.ctx, merged)
}

// --- Debounce ---

func (b *InboundBuffer) debounceKey(msg *IncomingMessage) string {
	return fmt.Sprintf("deb:%d:%d", msg.ChatID, msg.UserID)
}

func (b *InboundBuffer) submitDebounce(ctx context.Context, msg *IncomingMessage) {
	b.mu.Lock()
	defer b.mu.Unlock()

	key := b.debounceKey(msg)
	entry, exists := b.debounce[key]

	if exists {
		entry.messages = append(entry.messages, bufferedMessage{
			ctx:        ctx,
			msg:        msg,
			receivedAt: time.Now(),
		})
		entry.timer.Reset(time.Duration(debounceWindowMs) * time.Millisecond)
		return
	}

	entry = &debounceEntry{
		key: key,
		messages: []bufferedMessage{{
			ctx:        ctx,
			msg:        msg,
			receivedAt: time.Now(),
		}},
	}
	entry.timer = time.AfterFunc(time.Duration(debounceWindowMs)*time.Millisecond, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if e, ok := b.debounce[key]; ok {
			delete(b.debounce, key)
			b.flushDebounceLocked(e)
		}
	})

	b.debounce[key] = entry
}

func (b *InboundBuffer) flushDebounceLocked(entry *debounceEntry) {
	if len(entry.messages) == 0 {
		return
	}

	if len(entry.messages) == 1 {
		// Single message — no merging needed
		go b.handler(entry.messages[0].ctx, entry.messages[0].msg)
		return
	}

	// Sort and merge
	sortBuffered(entry.messages)

	first := entry.messages[0]
	last := entry.messages[len(entry.messages)-1]

	// Combine text with newlines
	parts := make([]string, 0, len(entry.messages))
	for _, m := range entry.messages {
		if m.msg.Text != "" {
			parts = append(parts, m.msg.Text)
		}
	}

	merged := &IncomingMessage{
		MessageID:      last.msg.MessageID,
		ChatID:         first.msg.ChatID,
		UserID:         first.msg.UserID,
		Username:       first.msg.Username,
		Text:           joinStrings(parts, "\n"),
		Timestamp:      first.msg.Timestamp,
		ReplyToMessage: first.msg.ReplyToMessage,
	}

	b.logger.Info("Debounced messages merged",
		zap.Int64("chat_id", merged.ChatID),
		zap.Int("count", len(entry.messages)),
	)

	go b.handler(first.ctx, merged)
}

// --- Media Group ---

func (b *InboundBuffer) submitMediaGroup(ctx context.Context, msg *IncomingMessage, groupID string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	entry, exists := b.mediaGroups[groupID]
	if exists {
		entry.messages = append(entry.messages, bufferedMessage{
			ctx:        ctx,
			msg:        msg,
			receivedAt: time.Now(),
		})
		entry.timer.Reset(time.Duration(mediaGroupTimeoutMs) * time.Millisecond)
		return
	}

	entry = &mediaGroupEntry{
		groupID: groupID,
		messages: []bufferedMessage{{
			ctx:        ctx,
			msg:        msg,
			receivedAt: time.Now(),
		}},
	}
	entry.timer = time.AfterFunc(time.Duration(mediaGroupTimeoutMs)*time.Millisecond, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if e, ok := b.mediaGroups[groupID]; ok {
			delete(b.mediaGroups, groupID)
			b.flushMediaGroupLocked(e)
		}
	})

	b.mediaGroups[groupID] = entry
}

func (b *InboundBuffer) flushMediaGroupLocked(entry *mediaGroupEntry) {
	if len(entry.messages) == 0 {
		return
	}

	sortBuffered(entry.messages)

	// Find the message with caption (primary message)
	var primary *bufferedMessage
	for i := range entry.messages {
		if entry.messages[i].msg.Text != "" {
			primary = &entry.messages[i]
			break
		}
	}
	if primary == nil {
		primary = &entry.messages[0]
	}

	// Collect all media into MediaGroup slice
	var mediaGroup []MediaInfo
	for _, m := range entry.messages {
		if m.msg.Media != nil {
			mediaGroup = append(mediaGroup, *m.msg.Media)
		}
	}

	merged := &IncomingMessage{
		MessageID:  primary.msg.MessageID,
		ChatID:     primary.msg.ChatID,
		UserID:     primary.msg.UserID,
		Username:   primary.msg.Username,
		Text:       primary.msg.Text,
		Timestamp:  primary.msg.Timestamp,
		Media:      primary.msg.Media,
		MediaData:  primary.msg.MediaData,
		MediaGroup: mediaGroup,
	}

	b.logger.Info("Media group merged",
		zap.Int64("chat_id", merged.ChatID),
		zap.String("group_id", entry.groupID),
		zap.Int("items", len(entry.messages)),
	)

	go b.handler(primary.ctx, merged)
}

// --- Helpers ---

func sortBuffered(msgs []bufferedMessage) {
	// Simple insertion sort (typically small arrays)
	for i := 1; i < len(msgs); i++ {
		key := msgs[i]
		j := i - 1
		for j >= 0 && msgs[j].msg.MessageID > key.msg.MessageID {
			msgs[j+1] = msgs[j]
			j--
		}
		msgs[j+1] = key
	}
}

func joinStrings(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	result := parts[0]
	for _, p := range parts[1:] {
		result += sep + p
	}
	return result
}
