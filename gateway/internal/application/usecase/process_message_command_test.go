package usecase

import (
	"context"
	"testing"

	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/service"
	"go.uber.org/zap"
)

// MockLLMClient for internal usecase tests
type mockLLMClient struct {
	generateFunc func(ctx context.Context, req *service.LLMRequest) (*service.LLMResponse, error)
}

func (m *mockLLMClient) Generate(ctx context.Context, req *service.LLMRequest) (*service.LLMResponse, error) {
	if m.generateFunc != nil {
		return m.generateFunc(ctx, req)
	}
	return &service.LLMResponse{Content: "Mock response"}, nil
}

func (m *mockLLMClient) GenerateStream(ctx context.Context, req *service.LLMRequest, deltaCh chan<- service.StreamChunk) (*service.LLMResponse, error) {
	resp, err := m.Generate(ctx, req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func TestProcessMessageUseCase_LLMClient(t *testing.T) {
	logger := zap.NewNop()
	client := &mockLLMClient{}

	if logger == nil {
		t.Error("Logger is nil")
	}
	if client == nil {
		t.Error("Client is nil")
	}

	// Verify mock implements LLMClient
	var _ service.LLMClient = client
}
