package anthropic

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

const anthropicVersion = "2023-06-01"

func init() {
	llm.RegisterFactory("anthropic", func(cfg llm.ProviderConfig, logger *zap.Logger) llm.Provider {
		return New(cfg, logger)
	})
}

// Provider implements the Anthropic Messages API natively.
type Provider struct {
	name    string
	baseURL string
	apiKey  string
	models  []string
	client  *http.Client
	logger  *zap.Logger
}

// New creates an Anthropic API provider.
func New(cfg llm.ProviderConfig, logger *zap.Logger) *Provider {
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
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
		client:  &http.Client{Transport: transport},
		logger:  logger.With(zap.String("provider", cfg.Name), zap.String("type", "anthropic")),
	}
}

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

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	p.setHeaders(httpReq)

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
		return nil, fmt.Errorf("Anthropic API error %d: %s", resp.StatusCode, string(respBody))
	}

	return p.parseAPIResponse(respBody)
}

// GenerateStream implements service.LLMClient with Anthropic SSE streaming.
func (p *Provider) GenerateStream(ctx context.Context, req *service.LLMRequest, deltaCh chan<- service.StreamChunk) (*service.LLMResponse, error) {
	apiReq := p.buildAPIRequest(req)
	apiReq.Stream = true

	body, err := json.Marshal(apiReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	p.setHeaders(httpReq)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Anthropic API error %d: %s", resp.StatusCode, string(respBody))
	}

	// Context cancellation watchdog
	streamDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			p.logger.Info("Context cancelled, force-closing Anthropic SSE stream",
				zap.Error(ctx.Err()))
			resp.Body.Close()
		case <-streamDone:
		}
	}()

	result, err := ParseSSEStream(ctx, resp.Body, deltaCh, p.logger)
	close(streamDone)
	return result, err
}

// --- Internal ---

func (p *Provider) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", anthropicVersion)
}

func (p *Provider) buildAPIRequest(req *service.LLMRequest) *Request {
	model := req.Model
	if idx := strings.Index(model, "/"); idx >= 0 {
		model = model[idx+1:]
	}

	apiReq := &Request{
		Model:       model,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
	}
	if apiReq.MaxTokens == 0 {
		apiReq.MaxTokens = 8192 // Anthropic requires explicit max_tokens
	}

	// Extract system prompt from messages
	var messages []Message
	for _, msg := range req.Messages {
		switch msg.Role {
		case "system":
			apiReq.System = msg.Content

		case "assistant":
			var blocks []ContentBlock
			if msg.Content != "" {
				blocks = append(blocks, ContentBlock{Type: "text", Text: msg.Content})
			}
			for _, tc := range msg.ToolCalls {
				blocks = append(blocks, ContentBlock{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Name,
					Input: tc.Arguments,
				})
			}
			if len(blocks) > 0 {
				messages = append(messages, Message{Role: "assistant", Content: blocks})
			}

		case "tool":
			// Anthropic: tool results go as user role with tool_result blocks
			messages = append(messages, Message{
				Role: "user",
				Content: []ContentBlock{{
					Type:      "tool_result",
					ToolUseID: msg.ToolCallID,
					Content:   msg.Content,
				}},
			})

		default: // user
			messages = append(messages, Message{
				Role:    "user",
				Content: []ContentBlock{{Type: "text", Text: msg.Content}},
			})
		}
	}
	apiReq.Messages = messages

	// Convert tool definitions
	for _, td := range req.Tools {
		apiReq.Tools = append(apiReq.Tools, Tool{
			Name:        td.Name,
			Description: td.Description,
			InputSchema: ConvertSchema(td.Parameters),
		})
	}

	return apiReq
}

func (p *Provider) parseAPIResponse(body []byte) (*service.LLMResponse, error) {
	var apiResp Response
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("parse Anthropic response: %w", err)
	}

	resp := &service.LLMResponse{
		ModelUsed:  apiResp.Model,
		TokensUsed: apiResp.Usage.Total(),
	}

	// Extract text and tool calls from content blocks
	for _, block := range apiResp.Content {
		switch block.Type {
		case "text":
			resp.Content += block.Text
		case "tool_use":
			resp.ToolCalls = append(resp.ToolCalls, entity.ToolCallInfo{
				ID:        block.ID,
				Name:      block.Name,
				Arguments: block.Input,
			})
		}
	}

	return resp, nil
}
