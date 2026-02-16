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

// ProcessMessageUseCase 处理消息用例
type ProcessMessageUseCase struct {
	messageRepo repository.MessageRepository
	router      service.MessageRouter
	aiClient    AIServiceClient
	compactor   *Compactor
	failover    RequestFailover
	agentLoop   *service.AgentLoop
	logger      *zap.Logger
}

// AIServiceClient AI服务客户端接口
type AIServiceClient interface {
	GenerateResponse(ctx context.Context, req *AIRequest) (*AIResponse, error)
	GenerateStream(ctx context.Context, req *AIRequest) (<-chan *AIStreamChunk, <-chan error)
	ExecuteSkill(ctx context.Context, req *SkillRequest) (*SkillResponse, error)
}

// RequestFailover wraps AI requests with model failover logic.
// Implemented by grpc.ModelFailover.
type RequestFailover interface {
	ExecuteWithFailover(ctx context.Context, req *AIRequest, client AIServiceClient) (*AIResponse, error)
}

// AIRequest AI请求
type AIRequest struct {
	Prompt      string
	Model       string
	MaxTokens   int
	Temperature float64
	History     []*entity.Message
	Tools       []ToolDefinition // Tool definitions for function calling
}

// ToolDefinition represents a tool schema for LLM function calling
type ToolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// AIResponse AI响应
type AIResponse struct {
	Content    string
	ModelUsed  string
	TokensUsed int
	ToolCalls  []entity.ToolCallInfo // Tool calls from the LLM
}

// AIStreamChunk AI流式响应块
type AIStreamChunk struct {
	Content    string
	IsFinal    bool
	ModelUsed  string // 模型名称 (由 final chunk 携带)
	TokensUsed int    // token 用量 (由 final chunk 携带)
}


// SkillRequest 技能执行请求
type SkillRequest struct {
	SkillID string
	Input   string
	Config  map[string]string
}

// SkillResponse 技能执行响应
type SkillResponse struct {
	Output       string
	Success      bool
	ErrorMessage string
}

// NewProcessMessageUseCase 创建消息处理用例
func NewProcessMessageUseCase(
	messageRepo repository.MessageRepository,
	router service.MessageRouter,
	aiClient AIServiceClient,
	logger *zap.Logger,
) *ProcessMessageUseCase {
	return &ProcessMessageUseCase{
		messageRepo: messageRepo,
		router:      router,
		aiClient:    aiClient,
		compactor:   NewCompactor(aiClient, logger),
		logger:      logger,
	}
}

// SetFailover sets an optional model failover handler
func (uc *ProcessMessageUseCase) SetFailover(f RequestFailover) {
	uc.failover = f
}

// SetAgentLoop sets the ReAct agent loop for tool-calling conversations
func (uc *ProcessMessageUseCase) SetAgentLoop(loop *service.AgentLoop) {
	uc.agentLoop = loop
}

// Execute 执行消息处理
func (uc *ProcessMessageUseCase) Execute(ctx context.Context, message *entity.Message) (*entity.Message, error) {
	// 1. 保存用户消息
	if err := uc.messageRepo.Save(ctx, message); err != nil {
		uc.logger.Error("Failed to save message", zap.Error(err))
		return nil, err
	}

	// 2. 路由到合适的代理
	agent, err := uc.router.Route(ctx, message)
	if err != nil {
		uc.logger.Error("Failed to route message", zap.Error(err))
		return nil, err
	}

	uc.logger.Info("Message routed to agent",
		zap.String("agent_id", agent.ID()),
		zap.String("agent_name", agent.Name()),
	)

	// Check for commands
	text := message.Content().Text()
	if strings.HasPrefix(text, "/skill ") {
		args := strings.TrimSpace(strings.TrimPrefix(text, "/skill "))
		if args != "" {
			parts := strings.SplitN(args, " ", 2)
			skillID := parts[0]
			input := ""
			if len(parts) > 1 {
				input = parts[1]
			}
			return uc.handleSkillExecution(ctx, message, agent, skillID, input)
		}
	}

	// 3. 获取历史消息
	// 获取最近的50条消息作为上下文
	history, err := uc.messageRepo.FindByConversationID(ctx, message.ConversationID(), 50, 0)
	if err != nil {
		uc.logger.Warn("Failed to retrieve conversation history", zap.Error(err))
		history = []*entity.Message{}
	}

	// 3.5 Get model config for compaction and AI request
	modelConfig := agent.ModelConfig()

	// 3.5 Auto-compact if history is too long
	var compactSummary string
	if uc.compactor != nil && len(history) > 0 {
		compactResult, compactErr := uc.compactor.CompactIfNeeded(ctx, history, modelConfig.FullModelName())
		if compactErr == nil && compactResult.WasCompacted {
			history = compactResult.RecentMessages
			compactSummary = compactResult.Summary
		}
	}

	// 4. 构建AI请求

	// 从历史中过滤掉当前消息
	var contextHistory []*entity.Message
	for _, msg := range history {
		if msg.ID() == message.ID() {
			continue
		}
		if msg.Content().IsTextOnly() {
			contextHistory = append(contextHistory, msg)
		}
	}

	// Build prompt with optional compaction summary
	promptText := message.Content().Text()
	if compactSummary != "" {
		promptText = fmt.Sprintf("[Previous conversation summary: %s]\n\n%s", compactSummary, promptText)
	}

	aiReq := &AIRequest{
		Prompt:      promptText,
		Model:       modelConfig.FullModelName(),
		MaxTokens:   modelConfig.MaxTokens(),
		Temperature: modelConfig.Temperature(),
		History:     contextHistory,
	}

	// 5. 调用AI服务
	aiResp := &AIResponse{}
	var aiErr error

	if modelConfig.Stream() {
		// 流式生成
		streamChan, errChan := uc.aiClient.GenerateStream(ctx, aiReq)
		var contentBuilder strings.Builder
		modelUsed := ""
		tokensUsed := 0

		for {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case chunk, ok := <-streamChan:
				if !ok {
					streamChan = nil // Mark channel as closed
					break
				}
				contentBuilder.WriteString(chunk.Content)
				// Extract model and token info from final chunk
				if chunk.ModelUsed != "" {
					modelUsed = chunk.ModelUsed
				}
				if chunk.TokensUsed > 0 {
					tokensUsed = chunk.TokensUsed
				}
				if chunk.IsFinal {
					break
				}
			case streamErr, ok := <-errChan:
				if ok {
					aiErr = streamErr
				}
				errChan = nil // Mark channel as closed
				break
			}

			if streamChan == nil && errChan == nil {
				break
			}
		}

		if aiErr != nil {
			uc.logger.Error("Failed to generate AI stream response", zap.Error(aiErr))
			return nil, aiErr
		}

		aiResp.Content = contentBuilder.String()
		if modelUsed == "" {
			modelUsed = aiReq.Model // Fallback to request model
		}
		aiResp.ModelUsed = modelUsed
		aiResp.TokensUsed = tokensUsed

	} else {
		// 非流式生成 — with optional failover
		if uc.failover != nil {
			aiResp, aiErr = uc.failover.ExecuteWithFailover(ctx, aiReq, uc.aiClient)
		} else {
			aiResp, aiErr = uc.aiClient.GenerateResponse(ctx, aiReq)
		}
		if aiErr != nil {
			uc.logger.Error("Failed to generate AI response", zap.Error(aiErr))
			return nil, aiErr
		}
	}

	// 6. 创建响应消息
	// 构建Bot用户
	botUser := valueobject.NewUser(
		agent.ID(),
		agent.Name(),
		"bot",
	)

	// 构建消息内容
	content := valueobject.NewMessageContent(
		aiResp.Content,
		valueobject.ContentTypeText,
	)

	// 生成消息ID (简单实现)
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

	responseMsg.SetMetadata("model_used", aiResp.ModelUsed)
	responseMsg.SetMetadata("tokens_used", aiResp.TokensUsed)

	// 7. 保存响应消息
	if err := uc.messageRepo.Save(ctx, responseMsg); err != nil {
		uc.logger.Error("Failed to save response message", zap.Error(err))
		return nil, err
	}

	uc.logger.Info("AI response generated and saved",
		zap.String("message_id", responseMsg.ID()),
		zap.String("model", aiResp.ModelUsed),
		zap.Int("tokens", aiResp.TokensUsed),
	)

	return responseMsg, nil
}


func (uc *ProcessMessageUseCase) handleSkillExecution(
	ctx context.Context,
	originalMsg *entity.Message,
	agent *entity.Agent,
	skillID string,
	input string,
) (*entity.Message, error) {
	uc.logger.Info("Handling skill execution", zap.String("skill_id", skillID))

	req := &SkillRequest{
		SkillID: skillID,
		Input:   input,
	}

	resp, err := uc.aiClient.ExecuteSkill(ctx, req)
	if err != nil {
		uc.logger.Error("Failed to execute skill", zap.Error(err))
		return uc.createErrorMessage(ctx, originalMsg, agent, "Failed to execute skill: "+err.Error())
	}

	responseText := resp.Output
	if !resp.Success {
		responseText = fmt.Sprintf("Skill execution failed: %s", resp.ErrorMessage)
	}

	content := valueobject.NewMessageContent(
		responseText,
		valueobject.ContentTypeText,
	)

	return uc.saveResponse(ctx, originalMsg, agent, content, map[string]interface{}{
		"skill_id": skillID,
		"success":  resp.Success,
		"type":     "skill_execution",
	})
}

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
