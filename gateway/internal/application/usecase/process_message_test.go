package usecase_test

import (
	"context"
	"testing"

	"github.com/ngoclaw/ngoclaw/gateway/internal/application/usecase"
	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/entity"
	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/valueobject"
	"go.uber.org/zap"
)

// MockMessageRepository 模拟消息仓储
type MockMessageRepository struct {
	savedMessages []*entity.Message
}

func (m *MockMessageRepository) Save(ctx context.Context, message *entity.Message) error {
	m.savedMessages = append(m.savedMessages, message)
	return nil
}

func (m *MockMessageRepository) FindByID(ctx context.Context, id string) (*entity.Message, error) {
	return nil, nil
}

func (m *MockMessageRepository) FindByConversationID(ctx context.Context, conversationID string, limit, offset int) ([]*entity.Message, error) {
	return nil, nil
}

func (m *MockMessageRepository) Delete(ctx context.Context, id string) error {
	return nil
}

func (m *MockMessageRepository) Count(ctx context.Context, conversationID string) (int64, error) {
	return 0, nil
}

// MockMessageRouter 模拟消息路由
type MockMessageRouter struct {
	agent *entity.Agent
}

func (m *MockMessageRouter) Route(ctx context.Context, message *entity.Message) (*entity.Agent, error) {
	return m.agent, nil
}

// MockAIServiceClient 模拟AI服务客户端
type MockAIServiceClient struct {
	response *usecase.AIResponse
}

func (m *MockAIServiceClient) GenerateResponse(ctx context.Context, req *usecase.AIRequest) (*usecase.AIResponse, error) {
	return m.response, nil
}

func (m *MockAIServiceClient) GenerateStream(ctx context.Context, req *usecase.AIRequest) (<-chan *usecase.AIStreamChunk, <-chan error) {
	chunkCh := make(chan *usecase.AIStreamChunk, 1)
	errCh := make(chan error)
	go func() {
		chunkCh <- &usecase.AIStreamChunk{Content: m.response.Content, IsFinal: true}
		close(chunkCh)
		close(errCh)
	}()
	return chunkCh, errCh
}

func (m *MockAIServiceClient) ExecuteSkill(ctx context.Context, req *usecase.SkillRequest) (*usecase.SkillResponse, error) {
	return &usecase.SkillResponse{
		Output:  "Mock skill output",
		Success: true,
	}, nil
}

func TestProcessMessage_Execute_Success(t *testing.T) {
	// 1. Setup
	repo := &MockMessageRepository{}

	// Create a mock agent
	modelConfig := valueobject.NewModelConfig("test-provider", "test-model", 1000, 0.7, 0.9, false)
	agent, _ := entity.NewAgent("agent-1", "Test Agent", modelConfig)
	router := &MockMessageRouter{agent: agent}

	// Mock AI response
	aiResponse := &usecase.AIResponse{
		Content:    "Hello, user!",
		ModelUsed:  "test-provider/test-model",
		TokensUsed: 10,
	}
	aiClient := &MockAIServiceClient{response: aiResponse}

	logger := zap.NewNop()

	uc := usecase.NewProcessMessageUseCase(repo, router, aiClient, logger)

	// 2. Create input message
	user := valueobject.NewUser("user-1", "testuser", "user")
	content := valueobject.NewMessageContent("Hi", valueobject.ContentTypeText)
	msg, _ := entity.NewMessage("msg-1", "conv-1", content, user)

	// 3. Execute
	ctx := context.Background()
	respMsg, err := uc.Execute(ctx, msg)

	// 4. Verify
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Verify response message
	if respMsg.Content().Text() != "Hello, user!" {
		t.Errorf("Expected response 'Hello, user!', got '%s'", respMsg.Content().Text())
	}

	if respMsg.Sender().Type() != "bot" {
		t.Errorf("Expected sender type 'bot', got '%s'", respMsg.Sender().Type())
	}

	if val, ok := respMsg.GetMetadata("model_used"); !ok || val != "test-provider/test-model" {
		t.Errorf("Expected metadata model_used='test-provider/test-model', got %v", val)
	}

	// Verify repository interactions
	if len(repo.savedMessages) != 2 {
		t.Errorf("Expected 2 messages saved (user + bot), got %d", len(repo.savedMessages))
	}
}
