package grpc

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ngoclaw/ngoclaw/gateway/internal/application/usecase"
	"go.uber.org/zap"
)

// mockAIClient for testing failover
type mockFailoverAIClient struct {
	failModels map[string]error // model -> error to return
	callLog    []string
}

func (m *mockFailoverAIClient) GenerateResponse(ctx context.Context, req *usecase.AIRequest) (*usecase.AIResponse, error) {
	m.callLog = append(m.callLog, req.Model)
	if err, ok := m.failModels[req.Model]; ok {
		return nil, err
	}
	return &usecase.AIResponse{
		Content:   "ok from " + req.Model,
		ModelUsed: req.Model,
	}, nil
}

func (m *mockFailoverAIClient) GenerateStream(ctx context.Context, req *usecase.AIRequest) (<-chan *usecase.AIStreamChunk, <-chan error) {
	return nil, nil
}

func (m *mockFailoverAIClient) ExecuteSkill(ctx context.Context, req *usecase.SkillRequest) (*usecase.SkillResponse, error) {
	return nil, nil
}

func testFailoverLogger() *zap.Logger {
	logger, _ := zap.NewDevelopment()
	return logger
}

func TestModelFailover_PrimarySucceeds(t *testing.T) {
	client := &mockFailoverAIClient{failModels: map[string]error{}}
	fo := NewModelFailover([]string{"model-a", "model-b"}, testFailoverLogger())

	req := &usecase.AIRequest{Model: "model-a", Prompt: "test"}
	resp, err := fo.ExecuteWithFailover(context.Background(), req, client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ModelUsed != "model-a" {
		t.Fatalf("expected model-a, got %s", resp.ModelUsed)
	}
	if len(client.callLog) != 1 {
		t.Fatalf("expected 1 call, got %d", len(client.callLog))
	}
}

func TestModelFailover_FallbackOnRetryableError(t *testing.T) {
	client := &mockFailoverAIClient{
		failModels: map[string]error{
			"model-a": fmt.Errorf("rate limit exceeded (429)"),
		},
	}
	fo := NewModelFailover([]string{"model-a", "model-b", "model-c"}, testFailoverLogger())

	req := &usecase.AIRequest{Model: "model-a", Prompt: "test"}
	resp, err := fo.ExecuteWithFailover(context.Background(), req, client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ModelUsed != "model-b" {
		t.Fatalf("expected model-b, got %s", resp.ModelUsed)
	}
	if len(client.callLog) != 2 {
		t.Fatalf("expected 2 calls, got %d: %v", len(client.callLog), client.callLog)
	}
}

func TestModelFailover_NonRetryableErrorStops(t *testing.T) {
	client := &mockFailoverAIClient{
		failModels: map[string]error{
			"model-a": fmt.Errorf("invalid prompt: content policy violation"),
		},
	}
	fo := NewModelFailover([]string{"model-a", "model-b"}, testFailoverLogger())

	req := &usecase.AIRequest{Model: "model-a", Prompt: "test"}
	_, err := fo.ExecuteWithFailover(context.Background(), req, client)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if len(client.callLog) != 1 {
		t.Fatalf("expected only 1 call (no fallback), got %d", len(client.callLog))
	}
}

func TestModelFailover_CooldownSkipsModel(t *testing.T) {
	client := &mockFailoverAIClient{failModels: map[string]error{}}
	fo := NewModelFailover([]string{"model-a", "model-b"}, testFailoverLogger())
	fo.SetCooldownDuration(1 * time.Second)

	// Manually set cooldown on model-a
	fo.setCooldown("model-a")

	req := &usecase.AIRequest{Model: "model-a", Prompt: "test"}
	resp, err := fo.ExecuteWithFailover(context.Background(), req, client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// model-a is cooled down, should use model-b
	if resp.ModelUsed != "model-b" {
		t.Fatalf("expected model-b (fallback), got %s", resp.ModelUsed)
	}
}

func TestModelFailover_AllModelsFail(t *testing.T) {
	client := &mockFailoverAIClient{
		failModels: map[string]error{
			"model-a": fmt.Errorf("503 unavailable"),
			"model-b": fmt.Errorf("timeout exceeded"),
			"model-c": fmt.Errorf("rate limit 429"),
		},
	}
	fo := NewModelFailover([]string{"model-a", "model-b", "model-c"}, testFailoverLogger())

	req := &usecase.AIRequest{Model: "model-a", Prompt: "test"}
	_, err := fo.ExecuteWithFailover(context.Background(), req, client)
	if err == nil {
		t.Fatal("expected error when all models fail")
	}
}

func TestModelFailover_ClearCooldown(t *testing.T) {
	fo := NewModelFailover([]string{"model-a"}, testFailoverLogger())
	fo.setCooldown("model-a")

	status := fo.GetCooldownStatus()
	if len(status) != 1 {
		t.Fatalf("expected 1 cooldown, got %d", len(status))
	}

	fo.ClearCooldown("model-a")
	status = fo.GetCooldownStatus()
	if len(status) != 0 {
		t.Fatalf("expected 0 cooldowns after clear, got %d", len(status))
	}
}

func TestIsRetryableError(t *testing.T) {
	tests := []struct {
		err      string
		expected bool
	}{
		{"rate limit exceeded", true},
		{"429 too many requests", true},
		{"401 unauthorized", true},
		{"timeout exceeded", true},
		{"connection refused", true},
		{"503 service unavailable", true},
		{"invalid prompt", false},
		{"content policy violation", false},
		{"malformed request", false},
	}

	for _, tt := range tests {
		result := isRetryableError(fmt.Errorf(tt.err))
		if result != tt.expected {
			t.Errorf("isRetryableError(%q) = %v, want %v", tt.err, result, tt.expected)
		}
	}
}
