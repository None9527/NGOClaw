package llm

import (
	"context"
	"strings"
	"testing"

	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/service"
	"go.uber.org/zap"
)

// helper: create a minimal provider for test access to parseSSEStream
func newTestProvider() *OpenAIBuiltinProvider {
	return &OpenAIBuiltinProvider{
		name:   "test",
		logger: zap.NewNop(),
	}
}

// helper: collect all emitted StreamChunks from a channel
func drainChunks(ch <-chan service.StreamChunk) []service.StreamChunk {
	var result []service.StreamChunk
	for c := range ch {
		result = append(result, c)
	}
	return result
}

// === Test: Pure text streaming ===

func TestParseSSEStream_TextOnly(t *testing.T) {
	p := newTestProvider()

	sseData := `data: {"id":"chatcmpl-1","choices":[{"delta":{"role":"assistant","content":"Hello"},"finish_reason":null}],"model":"gpt-4"}

data: {"id":"chatcmpl-1","choices":[{"delta":{"content":" world"},"finish_reason":null}],"model":"gpt-4"}

data: {"id":"chatcmpl-1","choices":[{"delta":{"content":"!"},"finish_reason":"stop"}],"model":"gpt-4","usage":{"total_tokens":42}}

data: [DONE]
`

	reader := strings.NewReader(sseData)
	deltaCh := make(chan service.StreamChunk, 64)

	resp, err := p.parseSSEStream(context.Background(), reader, deltaCh)
	close(deltaCh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check accumulated response
	if resp.Content != "Hello world!" {
		t.Fatalf("expected 'Hello world!', got %q", resp.Content)
	}
	if resp.ModelUsed != "gpt-4" {
		t.Fatalf("expected model 'gpt-4', got %q", resp.ModelUsed)
	}
	if resp.TokensUsed != 42 {
		t.Fatalf("expected 42 tokens, got %d", resp.TokensUsed)
	}
	if len(resp.ToolCalls) != 0 {
		t.Fatalf("expected no tool calls, got %d", len(resp.ToolCalls))
	}

	// Check emitted deltas
	chunks := drainChunks(deltaCh)
	textChunks := 0
	for _, c := range chunks {
		if c.DeltaText != "" {
			textChunks++
		}
	}
	if textChunks != 3 {
		t.Fatalf("expected 3 text delta chunks, got %d", textChunks)
	}
}

// === Test: Single tool call with fragmented arguments ===

func TestParseSSEStream_SingleToolCall(t *testing.T) {
	p := newTestProvider()

	sseData := `data: {"id":"chatcmpl-2","choices":[{"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_abc","type":"function","function":{"name":"read_file","arguments":""}}]},"finish_reason":null}],"model":"gpt-4"}

data: {"id":"chatcmpl-2","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"path\":"}}]},"finish_reason":null}],"model":"gpt-4"}

data: {"id":"chatcmpl-2","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"main.go\"}"}}]},"finish_reason":null}],"model":"gpt-4"}

data: {"id":"chatcmpl-2","choices":[{"delta":{},"finish_reason":"tool_calls"}],"model":"gpt-4","usage":{"total_tokens":100}}

data: [DONE]
`

	reader := strings.NewReader(sseData)
	deltaCh := make(chan service.StreamChunk, 64)

	resp, err := p.parseSSEStream(context.Background(), reader, deltaCh)
	close(deltaCh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}

	tc := resp.ToolCalls[0]
	if tc.ID != "call_abc" {
		t.Fatalf("expected ID 'call_abc', got %q", tc.ID)
	}
	if tc.Name != "read_file" {
		t.Fatalf("expected name 'read_file', got %q", tc.Name)
	}
	if tc.Arguments["path"] != "main.go" {
		t.Fatalf("expected path 'main.go', got %v", tc.Arguments["path"])
	}
}

// === Test: Parallel tool calls (multiple indices) ===

func TestParseSSEStream_ParallelToolCalls(t *testing.T) {
	p := newTestProvider()

	// Two tool calls, interleaved by index
	sseData := `data: {"id":"chatcmpl-3","choices":[{"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read_file","arguments":""}},{"index":1,"id":"call_2","type":"function","function":{"name":"write_file","arguments":""}}]},"finish_reason":null}],"model":"gpt-4"}

data: {"id":"chatcmpl-3","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"path\":\"a.go\"}"}},{"index":1,"function":{"arguments":"{\"path\":\"b.go\"}"}}]},"finish_reason":null}],"model":"gpt-4"}

data: {"id":"chatcmpl-3","choices":[{"delta":{},"finish_reason":"tool_calls"}],"model":"gpt-4","usage":{"total_tokens":200}}

data: [DONE]
`

	reader := strings.NewReader(sseData)
	deltaCh := make(chan service.StreamChunk, 64)

	resp, err := p.parseSSEStream(context.Background(), reader, deltaCh)
	close(deltaCh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(resp.ToolCalls))
	}

	// Verify order by index
	if resp.ToolCalls[0].Name != "read_file" {
		t.Fatalf("expected tool 0 = 'read_file', got %q", resp.ToolCalls[0].Name)
	}
	if resp.ToolCalls[1].Name != "write_file" {
		t.Fatalf("expected tool 1 = 'write_file', got %q", resp.ToolCalls[1].Name)
	}

	// Verify arguments parsed correctly
	if resp.ToolCalls[0].Arguments["path"] != "a.go" {
		t.Fatalf("expected tool 0 path='a.go', got %v", resp.ToolCalls[0].Arguments["path"])
	}
	if resp.ToolCalls[1].Arguments["path"] != "b.go" {
		t.Fatalf("expected tool 1 path='b.go', got %v", resp.ToolCalls[1].Arguments["path"])
	}
}

// === Test: Empty stream (just [DONE]) ===

func TestParseSSEStream_EmptyStream(t *testing.T) {
	p := newTestProvider()

	sseData := `data: [DONE]
`

	reader := strings.NewReader(sseData)
	deltaCh := make(chan service.StreamChunk, 64)

	resp, err := p.parseSSEStream(context.Background(), reader, deltaCh)
	close(deltaCh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Content != "" {
		t.Fatalf("expected empty content, got %q", resp.Content)
	}
	if len(resp.ToolCalls) != 0 {
		t.Fatalf("expected no tool calls, got %d", len(resp.ToolCalls))
	}
}

// === Test: Context cancellation during parse ===

func TestParseSSEStream_ContextCancel(t *testing.T) {
	p := newTestProvider()

	// Use a reader that blocks forever — but cancel the context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	sseData := `data: {"id":"chatcmpl-1","choices":[{"delta":{"content":"Hello"},"finish_reason":null}],"model":"gpt-4"}

data: [DONE]
`

	reader := strings.NewReader(sseData)
	deltaCh := make(chan service.StreamChunk, 64)

	_, err := p.parseSSEStream(ctx, reader, deltaCh)
	close(deltaCh)

	if err == nil {
		t.Fatal("expected context error")
	}
}

// === Test: Non-SSE lines are skipped ===

func TestParseSSEStream_SkipsNonDataLines(t *testing.T) {
	p := newTestProvider()

	sseData := `: this is a comment
event: message
id: 123
data: {"id":"chatcmpl-1","choices":[{"delta":{"content":"OK"},"finish_reason":"stop"}],"model":"gpt-4"}

data: [DONE]
`

	reader := strings.NewReader(sseData)
	deltaCh := make(chan service.StreamChunk, 64)

	resp, err := p.parseSSEStream(context.Background(), reader, deltaCh)
	close(deltaCh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Content != "OK" {
		t.Fatalf("expected 'OK', got %q", resp.Content)
	}
}

// === Test: Mixed text + tool calls ===

func TestParseSSEStream_TextThenToolCall(t *testing.T) {
	p := newTestProvider()

	sseData := `data: {"id":"chatcmpl-4","choices":[{"delta":{"role":"assistant","content":"Let me check "},"finish_reason":null}],"model":"gpt-4"}

data: {"id":"chatcmpl-4","choices":[{"delta":{"content":"the file."},"finish_reason":null}],"model":"gpt-4"}

data: {"id":"chatcmpl-4","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_xyz","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"test.go\"}"}}]},"finish_reason":null}],"model":"gpt-4"}

data: {"id":"chatcmpl-4","choices":[{"delta":{},"finish_reason":"tool_calls"}],"model":"gpt-4"}

data: [DONE]
`

	reader := strings.NewReader(sseData)
	deltaCh := make(chan service.StreamChunk, 64)

	resp, err := p.parseSSEStream(context.Background(), reader, deltaCh)
	close(deltaCh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Content != "Let me check the file." {
		t.Fatalf("expected 'Let me check the file.', got %q", resp.Content)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "read_file" {
		t.Fatalf("expected 'read_file', got %q", resp.ToolCalls[0].Name)
	}
}

// === Test: Malformed JSON chunks are skipped gracefully ===

func TestParseSSEStream_MalformedJSON(t *testing.T) {
	p := newTestProvider()

	sseData := `data: {"id":"chatcmpl-1","choices":[{"delta":{"content":"Hello"},"finish_reason":null}],"model":"gpt-4"}

data: {this is not valid json}

data: {"id":"chatcmpl-1","choices":[{"delta":{"content":" world"},"finish_reason":"stop"}],"model":"gpt-4"}

data: [DONE]
`

	reader := strings.NewReader(sseData)
	deltaCh := make(chan service.StreamChunk, 64)

	resp, err := p.parseSSEStream(context.Background(), reader, deltaCh)
	close(deltaCh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should still accumulate the valid chunks
	if resp.Content != "Hello world" {
		t.Fatalf("expected 'Hello world', got %q", resp.Content)
	}
}
