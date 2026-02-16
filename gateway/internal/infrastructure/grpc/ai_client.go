package grpc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/ngoclaw/ngoclaw/gateway/internal/application/usecase"
	pb "github.com/ngoclaw/ngoclaw/gateway/pkg/pb"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// AIClient AI服务gRPC客户端
type AIClient struct {
	conn   *grpc.ClientConn
	client pb.AIServiceClient
	logger *zap.Logger
}

// NewAIClient 创建AI服务客户端
func NewAIClient(host string, port int, logger *zap.Logger) (*AIClient, error) {
	addr := fmt.Sprintf("%s:%d", host, port)

	// 创建gRPC连接
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC client for AI service at %s: %w", addr, err)
	}

	logger.Info("Created AI service client", zap.String("address", addr))

	return &AIClient{
		conn:   conn,
		client: pb.NewAIServiceClient(conn),
		logger: logger,
	}, nil
}

// GenerateResponse 生成AI响应
func (c *AIClient) GenerateResponse(ctx context.Context, req *usecase.AIRequest) (*usecase.AIResponse, error) {
	// 解析模型名称 (format: provider/model)
	var provider, model string
	parts := strings.SplitN(req.Model, "/", 2)
	if len(parts) == 2 {
		provider = parts[0]
		model = parts[1]
	} else {
		provider = "antigravity" // 默认提供商
		model = req.Model
	}

	// 构建历史消息
	var pbHistory []*pb.ChatMessage
	for _, msg := range req.History {
		role := "user"
		if msg.IsFromBot() {
			role = "model"
		}

		content := msg.Content().Text()

		// 多模态: 有附件时编码为 JSON wrapper
		if msg.Content().HasAttachments() {
			parts := []map[string]string{{"type": "text", "text": content}}
			for _, att := range msg.Content().Attachments() {
				parts = append(parts, map[string]string{
					"type":      "media",
					"url":       att.URL,
					"mime_type": att.MimeType,
				})
			}
			if encoded, err := json.Marshal(parts); err == nil {
				content = string(encoded)
			}
		}

		pbHistory = append(pbHistory, &pb.ChatMessage{
			Role:    role,
			Content: content,
			Name:    msg.Sender().Username(),
		})
	}

	// 构建gRPC请求
	grpcReq := &pb.GenerateRequest{
		Prompt:      req.Prompt,
		Model:       model,
		Provider:    provider,
		MaxTokens:   int32(req.MaxTokens),
		Temperature: req.Temperature,
		History:     pbHistory,
	}

	// 调用远程服务
	resp, err := c.client.Generate(ctx, grpcReq)
	if err != nil {
		c.logger.Error("gRPC Generate call failed",
			zap.Error(err),
			zap.String("provider", provider),
			zap.String("model", model),
		)
		return nil, err
	}

	return &usecase.AIResponse{
		Content:    resp.Content,
		ModelUsed:  resp.ModelUsed,
		TokensUsed: int(resp.TokensUsed),
	}, nil
}

	// GenerateStream 流式生成AI响应
func (c *AIClient) GenerateStream(ctx context.Context, req *usecase.AIRequest) (<-chan *usecase.AIStreamChunk, <-chan error) {
	streamChan := make(chan *usecase.AIStreamChunk)
	errChan := make(chan error, 1)

	// 解析模型名称 (format: provider/model)
	var provider, model string
	parts := strings.SplitN(req.Model, "/", 2)
	if len(parts) == 2 {
		provider = parts[0]
		model = parts[1]
	} else {
		provider = "antigravity" // 默认提供商
		model = req.Model
	}

	// 构建历史消息
	var pbHistory []*pb.ChatMessage
	for _, msg := range req.History {
		role := "user"
		if msg.IsFromBot() {
			role = "model"
		}

		content := msg.Content().Text()

		// 多模态: 有附件时编码为 JSON wrapper
		if msg.Content().HasAttachments() {
			mediaParts := []map[string]string{{"type": "text", "text": content}}
			for _, att := range msg.Content().Attachments() {
				mediaParts = append(mediaParts, map[string]string{
					"type":      "media",
					"url":       att.URL,
					"mime_type": att.MimeType,
				})
			}
			if encoded, err := json.Marshal(mediaParts); err == nil {
				content = string(encoded)
			}
		}

		pbHistory = append(pbHistory, &pb.ChatMessage{
			Role:    role,
			Content: content,
			Name:    msg.Sender().Username(),
		})
	}

	// 构建gRPC请求
	grpcReq := &pb.GenerateRequest{
		Prompt:      req.Prompt,
		Model:       model,
		Provider:    provider,
		MaxTokens:   int32(req.MaxTokens),
		Temperature: req.Temperature,
		History:     pbHistory,
	}

	go func() {
		defer close(streamChan)
		defer close(errChan)

		// 调用远程流式服务
		streamClient, err := c.client.GenerateStream(ctx, grpcReq)
		if err != nil {
			c.logger.Error("gRPC GenerateStream call failed to establish",
				zap.Error(err),
				zap.String("provider", provider),
				zap.String("model", model),
			)
			errChan <- fmt.Errorf("failed to establish stream: %w", err)
			return
		}

		for {
			chunk, err := streamClient.Recv()
			if err == nil {
				streamChan <- &usecase.AIStreamChunk{
					Content: chunk.Content,
					IsFinal: chunk.IsFinal,
				}
				if chunk.IsFinal {
					break
				}
				continue
			}

			if err == io.EOF {
				c.logger.Info("gRPC GenerateStream completed successfully")
				break
			}

			c.logger.Error("gRPC GenerateStream received error",
				zap.Error(err),
				zap.String("provider", provider),
				zap.String("model", model),
			)
			errChan <- fmt.Errorf("stream receive error: %w", err)
			return
		}
	}()

	return streamChan, errChan
}

// ExecuteSkill 执行技能
func (c *AIClient) ExecuteSkill(ctx context.Context, req *usecase.SkillRequest) (*usecase.SkillResponse, error) {
	grpcReq := &pb.SkillRequest{
		SkillId: req.SkillID,
		Input:   req.Input,
		Config:  req.Config,
	}

	resp, err := c.client.ExecuteSkill(ctx, grpcReq)
	if err != nil {
		c.logger.Error("gRPC ExecuteSkill call failed", zap.Error(err))
		return nil, err
	}

	return &usecase.SkillResponse{
		Output:       resp.Output,
		Success:      resp.Success,
		ErrorMessage: resp.ErrorMessage,
	}, nil
}

// Close 关闭连接
func (c *AIClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// AIClientSkillAdapter adapts AIClient to the tool.SkillExecutor interface
type AIClientSkillAdapter struct {
	client *AIClient
}

// NewSkillExecutor creates a SkillExecutor adapter from an AIClient
func NewSkillExecutor(client *AIClient) *AIClientSkillAdapter {
	return &AIClientSkillAdapter{client: client}
}

// ExecuteSkill satisfies the tool.SkillExecutor interface
func (a *AIClientSkillAdapter) ExecuteSkill(ctx context.Context, skillID string, input string, config map[string]string) (string, error) {
	resp, err := a.client.ExecuteSkill(ctx, &usecase.SkillRequest{
		SkillID: skillID,
		Input:   input,
		Config:  config,
	})
	if err != nil {
		return "", err
	}
	if !resp.Success {
		return "", fmt.Errorf("skill %s failed: %s", skillID, resp.ErrorMessage)
	}
	return resp.Output, nil
}
