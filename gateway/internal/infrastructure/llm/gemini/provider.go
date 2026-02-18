package gemini

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
	llm.RegisterFactory("gemini", func(cfg llm.ProviderConfig, logger *zap.Logger) llm.Provider {
		return New(cfg, logger)
	})
}

// Provider implements the Google Gemini API natively.
type Provider struct {
	name    string
	baseURL string
	apiKey  string
	models  []string
	client  *http.Client
	logger  *zap.Logger
}

// New creates a Google Gemini API provider.
func New(cfg llm.ProviderConfig, logger *zap.Logger) *Provider {
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com"
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
		logger:  logger.With(zap.String("provider", cfg.Name), zap.String("type", "gemini")),
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
	model := p.stripPrefix(req.Model)

	body, err := json.Marshal(apiReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent?key=%s", p.baseURL, model, p.apiKey)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

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
		return nil, fmt.Errorf("Gemini API error %d: %s", resp.StatusCode, string(respBody))
	}

	return p.parseAPIResponse(respBody)
}

// GenerateStream implements service.LLMClient with Gemini SSE streaming.
func (p *Provider) GenerateStream(ctx context.Context, req *service.LLMRequest, deltaCh chan<- service.StreamChunk) (*service.LLMResponse, error) {
	apiReq := p.buildAPIRequest(req)
	model := p.stripPrefix(req.Model)

	body, err := json.Marshal(apiReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/v1beta/models/%s:streamGenerateContent?alt=sse&key=%s", p.baseURL, model, p.apiKey)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Gemini API error %d: %s", resp.StatusCode, string(respBody))
	}

	streamDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			p.logger.Info("Context cancelled, force-closing Gemini SSE stream",
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

func (p *Provider) stripPrefix(model string) string {
	if idx := strings.Index(model, "/"); idx >= 0 {
		return model[idx+1:]
	}
	return model
}

func (p *Provider) buildAPIRequest(req *service.LLMRequest) *Request {
	apiReq := &Request{
		GenerationConfig: &GenerationConfig{
			Temperature:     req.Temperature,
			MaxOutputTokens: req.MaxTokens,
		},
	}

	// Convert messages to Gemini contents
	for _, msg := range req.Messages {
		switch msg.Role {
		case "system":
			apiReq.SystemInstruction = &Content{
				Parts: []Part{{Text: msg.Content}},
			}

		case "assistant":
			content := Content{Role: "model"}
			if msg.Content != "" {
				content.Parts = append(content.Parts, Part{Text: msg.Content})
			}
			for _, tc := range msg.ToolCalls {
				content.Parts = append(content.Parts, Part{
					FunctionCall: &FunctionCall{
						Name: tc.Name,
						Args: tc.Arguments,
					},
				})
			}
			if len(content.Parts) > 0 {
				apiReq.Contents = append(apiReq.Contents, content)
			}

		case "tool":
			// Gemini: tool results are functionResponse parts in a user turn
			result := map[string]interface{}{"output": msg.Content}
			apiReq.Contents = append(apiReq.Contents, Content{
				Role: "user",
				Parts: []Part{{
					FunctionResponse: &FunctionResponse{
						Name:     msg.Name,
						Response: result,
					},
				}},
			})

		default: // user
			apiReq.Contents = append(apiReq.Contents, Content{
				Role:  "user",
				Parts: []Part{{Text: msg.Content}},
			})
		}
	}

	// Convert tool definitions
	if len(req.Tools) > 0 {
		var decls []FunctionDeclarationSpec
		for _, td := range req.Tools {
			decls = append(decls, FunctionDeclarationSpec{
				Name:        td.Name,
				Description: td.Description,
				Parameters:  ConvertSchema(td.Parameters),
			})
		}
		apiReq.Tools = []ToolDeclaration{{FunctionDeclarations: decls}}
	}

	return apiReq
}

func (p *Provider) parseAPIResponse(body []byte) (*service.LLMResponse, error) {
	var apiResp Response
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("parse Gemini response: %w", err)
	}

	if len(apiResp.Candidates) == 0 {
		return nil, fmt.Errorf("empty Gemini response: no candidates")
	}

	candidate := apiResp.Candidates[0]
	resp := &service.LLMResponse{
		ModelUsed: apiResp.ModelVersion,
	}
	if apiResp.UsageMetadata != nil {
		resp.TokensUsed = apiResp.UsageMetadata.Total()
	}

	// Extract text and function calls from parts
	for _, part := range candidate.Content.Parts {
		if part.Text != "" {
			resp.Content += part.Text
		}
		if part.FunctionCall != nil {
			resp.ToolCalls = append(resp.ToolCalls, entity.ToolCallInfo{
				ID:        fmt.Sprintf("call_%s_%d", part.FunctionCall.Name, len(resp.ToolCalls)),
				Name:      part.FunctionCall.Name,
				Arguments: part.FunctionCall.Args,
			})
		}
	}

	return resp, nil
}
