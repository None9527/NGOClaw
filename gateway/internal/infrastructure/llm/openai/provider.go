package openai

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/entity"
	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/service"
	llm "github.com/ngoclaw/ngoclaw/gateway/internal/infrastructure/llm"
	"go.uber.org/zap"
)

func init() {
	llm.RegisterFactory("openai", func(cfg llm.ProviderConfig, logger *zap.Logger) llm.Provider {
		return New(cfg, logger)
	})
}

// Provider is a Go-native OpenAI-compatible HTTP client.
// Compatible with: OpenAI, Bailian (Qwen), MiniMax, DeepSeek, Ollama, vLLM, etc.
type Provider struct {
	name    string
	baseURL string
	apiKey  string
	models  []string
	client  *http.Client
	logger  *zap.Logger
}

// New creates a Go-native OpenAI-compatible LLM provider.
func New(cfg llm.ProviderConfig, logger *zap.Logger) *Provider {
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 300 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          10,
		MaxIdleConnsPerHost:   5,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
	}

	return &Provider{
		name:    cfg.Name,
		baseURL: baseURL,
		apiKey:  cfg.APIKey,
		models:  cfg.Models,
		client: &http.Client{
			Transport: transport,
		},
		logger: logger.With(zap.String("provider", cfg.Name), zap.String("type", "openai")),
	}
}

// Compile-time interface check
var _ llm.Provider = (*Provider)(nil)

func (p *Provider) Name() string    { return p.name }
func (p *Provider) Models() []string { return p.models }

func (p *Provider) SupportsModel(model string) bool {
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

func (p *Provider) IsAvailable(ctx context.Context) bool {
	return p.apiKey != ""
}

// Generate implements service.LLMClient (non-streaming).
func (p *Provider) Generate(ctx context.Context, req *service.LLMRequest) (*service.LLMResponse, error) {
	apiReq := p.buildAPIRequest(req)

	body, err := json.Marshal(apiReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	return p.parseAPIResponse(respBody)
}

// GenerateStream implements service.LLMClient with SSE streaming.
func (p *Provider) GenerateStream(ctx context.Context, req *service.LLMRequest, deltaCh chan<- service.StreamChunk) (*service.LLMResponse, error) {
	apiReq := p.buildAPIRequest(req)

	streamBody := StreamRequest{
		Request:       apiReq,
		Stream:        true,
		StreamOptions: map[string]interface{}{"include_usage": true},
	}

	body, err := json.Marshal(streamBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	// Context cancellation body-close watchdog
	streamDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			p.logger.Info("Context cancelled, force-closing SSE stream",
				zap.Error(ctx.Err()))
			resp.Body.Close()
		case <-streamDone:
		}
	}()

	result, err := ParseSSEStream(ctx, resp.Body, deltaCh, p.logger)
	close(streamDone)
	return result, err
}

// --- Internal conversion methods ---

func (p *Provider) buildAPIRequest(req *service.LLMRequest) *Request {
	// Strip provider prefix (e.g. "bailian/qwen3-max" → "qwen3-max")
	model := req.Model
	if idx := strings.Index(model, "/"); idx >= 0 {
		model = model[idx+1:]
	}

	apiReq := &Request{
		Model:       model,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	}

	for _, msg := range req.Messages {
		apiMsg := Message{
			Role:       msg.Role,
			Content:    msg.Content,
			ToolCallID: msg.ToolCallID,
			Name:       msg.Name,
		}

		for _, tc := range msg.ToolCalls {
			apiMsg.ToolCalls = append(apiMsg.ToolCalls, ToolCall{
				ID:   tc.ID,
				Type: "function",
				Function: ToolCallFunc{
					Name:      tc.Name,
					Arguments: MarshalToolCallArgs(tc.Arguments),
				},
			})
		}

		apiReq.Messages = append(apiReq.Messages, apiMsg)
	}

	for _, td := range req.Tools {
		apiReq.Tools = append(apiReq.Tools, Tool{
			Type: "function",
			Function: ToolFunction{
				Name:        td.Name,
				Description: td.Description,
				Parameters:  ConvertSchema(td.Parameters),
			},
		})
	}

	return apiReq
}

func (p *Provider) parseAPIResponse(body []byte) (*service.LLMResponse, error) {
	var apiResp Response
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if len(apiResp.Choices) == 0 {
		return nil, fmt.Errorf("empty response: no choices")
	}

	choice := apiResp.Choices[0]
	resp := &service.LLMResponse{
		Content:    choice.Message.Content,
		ModelUsed:  apiResp.Model,
		TokensUsed: apiResp.Usage.Total(),
	}

	for _, tc := range choice.Message.ToolCalls {
		var args map[string]interface{}
		if tc.Function.Arguments != "" {
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				return nil, fmt.Errorf("parse tool call arguments for %s: %w", tc.Function.Name, err)
			}
		}
		resp.ToolCalls = append(resp.ToolCalls, entity.ToolCallInfo{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: args,
		})
	}

	return resp, nil
}
