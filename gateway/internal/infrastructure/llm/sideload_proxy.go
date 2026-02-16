package llm

import (
	"context"
	"fmt"

	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/entity"
	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/service"
	"github.com/ngoclaw/ngoclaw/gateway/internal/infrastructure/sideload"
	"go.uber.org/zap"
)

// SideloadProxyProvider forwards LLM requests to a Python sideload module
// via JSON-RPC 2.0. This is the primary LLM provider in NGOClaw —
// the Go builtin is only a fallback.
type SideloadProxyProvider struct {
	moduleMgr  *sideload.Manager
	providerID string
	models     []string
	logger     *zap.Logger
}

// NewSideloadProxyProvider creates a proxy that delegates to a sideload module
func NewSideloadProxyProvider(mgr *sideload.Manager, providerID string, models []string, logger *zap.Logger) *SideloadProxyProvider {
	return &SideloadProxyProvider{
		moduleMgr:  mgr,
		providerID: providerID,
		models:     models,
		logger: logger.With(
			zap.String("provider", "sideload:"+providerID),
		),
	}
}

// Compile-time interface check
var _ Provider = (*SideloadProxyProvider)(nil)

func (p *SideloadProxyProvider) Name() string    { return "sideload:" + p.providerID }
func (p *SideloadProxyProvider) Models() []string { return p.models }

func (p *SideloadProxyProvider) SupportsModel(model string) bool {
	if len(p.models) == 0 {
		return true
	}
	for _, m := range p.models {
		if m == model {
			return true
		}
	}
	return false
}

func (p *SideloadProxyProvider) IsAvailable(ctx context.Context) bool {
	module, ok := p.moduleMgr.GetProviderModule(p.providerID)
	return ok && module.State() == sideload.ModuleStateReady
}

// Generate implements service.LLMClient by forwarding to the sideload module
func (p *SideloadProxyProvider) Generate(ctx context.Context, req *service.LLMRequest) (*service.LLMResponse, error) {
	module, ok := p.moduleMgr.GetProviderModule(p.providerID)
	if !ok {
		return nil, fmt.Errorf("no sideload module provides '%s'", p.providerID)
	}

	// Convert LLMRequest messages to sideload GenerateMessage format
	var messages []sideload.GenerateMessage
	for _, msg := range req.Messages {
		messages = append(messages, sideload.GenerateMessage{
			Role:    msg.Role,
			Content: msg.Content,
			Name:    msg.Name,
		})
	}

	// Convert tool definitions to sideload ToolCap format
	var tools []sideload.ToolCap
	for _, td := range req.Tools {
		tools = append(tools, sideload.ToolCap{
			Name:        td.Name,
			Description: td.Description,
			InputSchema: td.Parameters,
		})
	}

	// Build sideload GenerateParams
	params := &sideload.GenerateParams{
		Provider: p.providerID,
		Model:    req.Model,
		Messages: messages,
		Tools:    tools,
		Stream:   false,
		Options: map[string]interface{}{
			"temperature": req.Temperature,
			"max_tokens":  req.MaxTokens,
		},
	}

	p.logger.Debug("Forwarding to sideload module",
		zap.String("provider", p.providerID),
		zap.String("model", req.Model),
		zap.Int("messages", len(messages)),
	)

	result, err := module.Generate(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("sideload generate: %w", err)
	}

	// Convert result
	resp := &service.LLMResponse{
		Content:    result.Content,
		ModelUsed:  result.ModelUsed,
		TokensUsed: result.TokensUsed,
	}

	// Convert tool calls
	for _, tc := range result.ToolCalls {
		resp.ToolCalls = append(resp.ToolCalls, entity.ToolCallInfo{
			ID:        tc.ID,
			Name:      tc.Name,
			Arguments: tc.Arguments,
		})
	}

	return resp, nil
}

// GenerateStream implements service.LLMClient.
// Sideload modules don't support native streaming yet, so this
// falls back to Generate and emits the full content as a single chunk.
func (p *SideloadProxyProvider) GenerateStream(ctx context.Context, req *service.LLMRequest, deltaCh chan<- service.StreamChunk) (*service.LLMResponse, error) {
	resp, err := p.Generate(ctx, req)
	if err != nil {
		return nil, err
	}

	// Emit full content as one delta (no incremental streaming from sideload)
	if resp.Content != "" {
		deltaCh <- service.StreamChunk{DeltaText: resp.Content}
	}
	deltaCh <- service.StreamChunk{FinishReason: "stop"}

	return resp, nil
}
