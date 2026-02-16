// Copyright 2026 NGOClaw Authors. All rights reserved.
package service

import (
	"context"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// MemoryPersister is the interface for persisting extracted memory facts.
// This decouples the middleware from the infrastructure/tool package (avoids import cycle).
type MemoryPersister interface {
	SaveFact(content, category string, confidence float64, source string) error
	IsDuplicate(content string) bool
}

// MemoryMiddleware extracts facts from conversation endings and persists them
// to structured memory (memory.json) after a debounce period.
//
// Source: Deer-Flow memory_middleware.py + queue.py — 30s debounce + background LLM extraction.
//
// Complementary to P1.7 compaction extraction:
//   - P1.7: extracts from long-conversation compaction summaries
//   - P3.16: extracts from normal conversation endings (no tool calls = final response)
type MemoryMiddleware struct {
	NoOpMiddleware
	llm       LLMClient
	persister MemoryPersister
	logger    *zap.Logger

	// Debounce queue: threadID → pending messages
	mu      sync.Mutex
	pending map[string][]conversationPair
	timers  map[string]*time.Timer

	debounce time.Duration
}

// conversationPair represents a user + assistant exchange.
type conversationPair struct {
	User      string
	Assistant string
}

// NewMemoryMiddleware creates the memory extraction middleware.
func NewMemoryMiddleware(llm LLMClient, persister MemoryPersister, logger *zap.Logger) *MemoryMiddleware {
	return &MemoryMiddleware{
		llm:       llm,
		persister: persister,
		logger:    logger,
		pending:   make(map[string][]conversationPair),
		timers:    make(map[string]*time.Timer),
		debounce:  30 * time.Second,
	}
}

func (m *MemoryMiddleware) Name() string { return "memory_extraction" }

// AfterModel checks if the conversation has ended (no tool calls = final response).
// If so, it queues the user+assistant pair for debounced background extraction.
func (m *MemoryMiddleware) AfterModel(ctx context.Context, resp *LLMResponse, step int) *LLMResponse {
	// Only trigger on final responses (no tool calls)
	if len(resp.ToolCalls) > 0 || resp.Content == "" {
		return resp
	}

	// Skip very short exchanges (step 1 = likely simple Q&A, not worth extracting)
	if step <= 1 {
		return resp
	}

	// Thread ID from context (for dedup)
	threadID := "default"
	if tid, ok := ctx.Value(threadIDKey{}).(string); ok && tid != "" {
		threadID = tid
	}

	// User message from context
	userMsg := ""
	if um, ok := ctx.Value(userMessageKey{}).(string); ok {
		userMsg = um
	}
	if userMsg == "" {
		return resp
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.pending[threadID] = append(m.pending[threadID], conversationPair{
		User:      userMsg,
		Assistant: resp.Content,
	})

	// Reset debounce timer
	if t, ok := m.timers[threadID]; ok {
		t.Stop()
	}
	m.timers[threadID] = time.AfterFunc(m.debounce, func() {
		m.flush(threadID)
	})

	return resp
}

// flush processes the pending conversation pairs for a thread.
func (m *MemoryMiddleware) flush(threadID string) {
	m.mu.Lock()
	pairs := m.pending[threadID]
	delete(m.pending, threadID)
	delete(m.timers, threadID)
	m.mu.Unlock()

	if len(pairs) == 0 {
		return
	}

	// Build conversation text for LLM extraction
	var sb strings.Builder
	for _, p := range pairs {
		sb.WriteString("User: " + p.User + "\n")
		sb.WriteString("Assistant: " + p.Assistant + "\n\n")
	}

	m.logger.Info("Memory extraction triggered",
		zap.String("thread", threadID),
		zap.Int("pairs", len(pairs)),
	)

	extractPrompt := `Analyze the following conversation and extract important facts worth remembering.
Focus on: user preferences, environment details, project decisions, corrections, behavior patterns, goals.
Output ONLY facts as bullet points starting with "- ". If nothing worth remembering, output "NONE".

Conversation:
` + sb.String()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	resp, err := m.llm.Generate(ctx, &LLMRequest{
		Messages:    []LLMMessage{{Role: "user", Content: extractPrompt}},
		MaxTokens:   500,
		Temperature: 0.2,
	})
	if err != nil {
		m.logger.Debug("Memory extraction LLM call failed", zap.Error(err))
		return
	}

	if resp.Content == "" || strings.TrimSpace(resp.Content) == "NONE" {
		return
	}

	// Parse and save facts
	var saved int
	for _, line := range strings.Split(resp.Content, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "- ")
		line = strings.TrimPrefix(line, "* ")
		line = strings.TrimPrefix(line, "• ")
		line = strings.TrimSpace(line)
		if line == "" || len(line) < 5 || strings.EqualFold(line, "NONE") {
			continue
		}

		if m.persister.IsDuplicate(line) {
			continue
		}
		if err := m.persister.SaveFact(line, "knowledge", 0.7, "agent"); err != nil {
			m.logger.Debug("Failed to save extracted memory", zap.Error(err))
			continue
		}
		saved++
	}

	if saved > 0 {
		m.logger.Info("Memory extraction completed",
			zap.String("thread", threadID),
			zap.Int("facts_saved", saved),
		)
	}
}

// --- Context keys ---

type threadIDKey struct{}
type userMessageKey struct{}

// WithThreadID stores a thread ID in context.
func WithThreadID(ctx context.Context, tid string) context.Context {
	return context.WithValue(ctx, threadIDKey{}, tid)
}

// WithUserMessage stores the current user message in context for MemoryMiddleware.
func WithUserMessage(ctx context.Context, msg string) context.Context {
	return context.WithValue(ctx, userMessageKey{}, msg)
}

// Compile-time check
var _ Middleware = (*MemoryMiddleware)(nil)
