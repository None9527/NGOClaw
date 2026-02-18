package usecase

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/entity"
	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/repository"
	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/service"
	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/valueobject"
	"go.uber.org/zap"
)

// ProcessMessageUseCase handles the legacy message-processing flow.
// The primary path is AgentLoop (ReAct engine); this use-case is the
// fallback for HTTP API and REPL interfaces that do not use AgentLoop.
type ProcessMessageUseCase struct {
	messageRepo repository.MessageRepository
	router      service.MessageRouter
	llm         service.LLMClient
	agentLoop   *service.AgentLoop
	logger      *zap.Logger
}

// NewProcessMessageUseCase creates a message processing use-case.
// The llm parameter is the same LLMClient (llmRouter) used by AgentLoop.
func NewProcessMessageUseCase(
	messageRepo repository.MessageRepository,
	router service.MessageRouter,
	llm service.LLMClient,
	logger *zap.Logger,
) *ProcessMessageUseCase {
	return &ProcessMessageUseCase{
		messageRepo: messageRepo,
		router:      router,
		llm:         llm,
		logger:      logger,
	}
}

// SetAgentLoop sets the ReAct agent loop for tool-calling conversations
func (uc *ProcessMessageUseCase) SetAgentLoop(loop *service.AgentLoop) {
	uc.agentLoop = loop
}

// Execute processes a user message and generates an AI response.
func (uc *ProcessMessageUseCase) Execute(ctx context.Context, message *entity.Message) (*entity.Message, error) {
	// 1. Save user message
	if err := uc.messageRepo.Save(ctx, message); err != nil {
		uc.logger.Error("Failed to save message", zap.Error(err))
		return nil, err
	}

	// 2. Route to agent
	agent, err := uc.router.Route(ctx, message)
	if err != nil {
		uc.logger.Error("Failed to route message", zap.Error(err))
		return nil, err
	}

	uc.logger.Info("Message routed to agent",
		zap.String("agent_id", agent.ID()),
		zap.String("agent_name", agent.Name()),
	)

	// 3. Get conversation history
	history, err := uc.messageRepo.FindByConversationID(ctx, message.ConversationID(), 50, 0)
	if err != nil {
		uc.logger.Warn("Failed to retrieve conversation history", zap.Error(err))
		history = []*entity.Message{}
	}

	// 4. Build LLM request
	modelConfig := agent.ModelConfig()

	// Convert entity messages to LLMMessages
	var llmHistory []service.LLMMessage
	for _, msg := range history {
		if msg.ID() == message.ID() {
			continue
		}
		if !msg.Content().IsTextOnly() {
			continue
		}
		role := "user"
		if msg.IsFromBot() {
			role = "assistant"
		}
		llmHistory = append(llmHistory, service.LLMMessage{
			Role:    role,
			Content: msg.Content().Text(),
		})
	}

	// Add the current user message
	llmHistory = append(llmHistory, service.LLMMessage{
		Role:    "user",
		Content: message.Content().Text(),
	})

	llmReq := &service.LLMRequest{
		Messages:    llmHistory,
		Model:       modelConfig.FullModelName(),
		MaxTokens:   modelConfig.MaxTokens(),
		Temperature: modelConfig.Temperature(),
	}

	// 5. Call LLM via llmRouter (same path as AgentLoop)
	llmResp, err := uc.llm.Generate(ctx, llmReq)
	if err != nil {
		uc.logger.Error("Failed to generate AI response", zap.Error(err))
		return nil, err
	}

	// 6. Build response message
	botUser := valueobject.NewUser(
		agent.ID(),
		agent.Name(),
		"bot",
	)

	content := valueobject.NewMessageContent(
		llmResp.Content,
		valueobject.ContentTypeText,
	)

	respID := fmt.Sprintf("msg_%d", time.Now().UnixNano())
	responseMsg, err := entity.NewMessage(
		respID,
		message.ConversationID(),
		content,
		botUser,
	)
	if err != nil {
		uc.logger.Error("Failed to create response message", zap.Error(err))
		return nil, err
	}

	responseMsg.SetMetadata("model_used", llmResp.ModelUsed)
	responseMsg.SetMetadata("tokens_used", llmResp.TokensUsed)

	// 7. Save response
	if err := uc.messageRepo.Save(ctx, responseMsg); err != nil {
		uc.logger.Error("Failed to save response message", zap.Error(err))
		return nil, err
	}

	uc.logger.Info("AI response generated and saved",
		zap.String("message_id", responseMsg.ID()),
		zap.String("model", llmResp.ModelUsed),
		zap.Int("tokens", llmResp.TokensUsed),
	)

	return responseMsg, nil
}

// createErrorMessage creates an error response message
func (uc *ProcessMessageUseCase) createErrorMessage(
	ctx context.Context,
	originalMsg *entity.Message,
	agent *entity.Agent,
	errorText string,
) (*entity.Message, error) {
	content := valueobject.NewMessageContent(errorText, valueobject.ContentTypeText)
	return uc.saveResponse(ctx, originalMsg, agent, content, map[string]interface{}{
		"is_error": true,
	})
}

func (uc *ProcessMessageUseCase) saveResponse(
	ctx context.Context,
	originalMsg *entity.Message,
	agent *entity.Agent,
	content valueobject.MessageContent,
	metadata map[string]interface{},
) (*entity.Message, error) {
	botUser := valueobject.NewUser(
		agent.ID(),
		agent.Name(),
		"bot",
	)

	respID := fmt.Sprintf("msg_%d", time.Now().UnixNano())
	responseMsg, err := entity.NewMessage(
		respID,
		originalMsg.ConversationID(),
		content,
		botUser,
	)
	if err != nil {
		return nil, err
	}

	for k, v := range metadata {
		responseMsg.SetMetadata(k, v)
	}

	if err := uc.messageRepo.Save(ctx, responseMsg); err != nil {
		uc.logger.Error("Failed to save response message", zap.Error(err))
		return nil, err
	}

	return responseMsg, nil
}

// Helper: build history string for compaction (used by domain compactor, kept here for reference)
func buildHistoryText(messages []*entity.Message) string {
	var sb strings.Builder
	for _, msg := range messages {
		role := "User"
		if msg.Sender().Type() == "bot" {
			role = "Assistant"
		}
		text := msg.Content().Text()
		if len(text) > 500 {
			text = text[:500] + "..."
		}
		sb.WriteString(fmt.Sprintf("[%s]: %s\n\n", role, text))
	}
	return sb.String()
}
