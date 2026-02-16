package llm

import (
	"bufio"
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
	"go.uber.org/zap"
)

// OpenAIBuiltinProvider is a Go-native OpenAI-compatible HTTP client.
// It serves as a fallback when the Python sideload module is unavailable.
// Compatible with: OpenAI, Anthropic (via proxy), Bailian, MiniMax, Ollama, etc.
type OpenAIBuiltinProvider struct {
	name    string
	baseURL string
	apiKey  string
	models  []string
	client  *http.Client
	logger  *zap.Logger
}

// NewOpenAIBuiltinProvider creates a Go-native OpenAI-compatible LLM client
func NewOpenAIBuiltinProvider(cfg ProviderConfig, logger *zap.Logger) *OpenAIBuiltinProvider {
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	// Transport-level timeouts (industry pattern from Cline/OpenCode):
	// - DialContext: TCP connection timeout (fast failure on unreachable hosts)
	// - TLSHandshakeTimeout: TLS negotiation timeout
	// - ResponseHeaderTimeout: time until first response header (covers LLM think time)
	// - IdleConnTimeout: max time an idle connection stays in pool
	// NO total Timeout — long LLM inferences are not killed.
	// Cancellation is handled by context (agent_loop's run_timeout).
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 300 * time.Second, // allow up to 5min for LLM first token
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          10,
		MaxIdleConnsPerHost:   5,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
	}

	return &OpenAIBuiltinProvider{
		name:    cfg.Name,
		baseURL: baseURL,
		apiKey:  cfg.APIKey,
		models:  cfg.Models,
		client: &http.Client{
			Transport: transport,
			// No Timeout — rely on context cancellation and transport-level timeouts
		},
		logger: logger.With(zap.String("provider", cfg.Name)),
	}
}

// Compile-time interface check
var _ Provider = (*OpenAIBuiltinProvider)(nil)

func (p *OpenAIBuiltinProvider) Name() string      { return p.name }
func (p *OpenAIBuiltinProvider) Models() []string   { return p.models }

func (p *OpenAIBuiltinProvider) SupportsModel(model string) bool {
	if len(p.models) == 0 {
		return true // wildcard: accept any model
	}
	for _, m := range p.models {
		if m == model {
			return true
		}
	}
	return false
}

func (p *OpenAIBuiltinProvider) IsAvailable(ctx context.Context) bool {
	return p.apiKey != ""
}

// Generate implements service.LLMClient
func (p *OpenAIBuiltinProvider) Generate(ctx context.Context, req *service.LLMRequest) (*service.LLMResponse, error) {
	// Convert to OpenAI API format
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
// Sends stream:true to OpenAI-compatible API and emits deltas in real time.
func (p *OpenAIBuiltinProvider) GenerateStream(ctx context.Context, req *service.LLMRequest, deltaCh chan<- service.StreamChunk) (*service.LLMResponse, error) {
	apiReq := p.buildAPIRequest(req)

	// Build streaming request body
	streamBody := struct {
		*openaiRequest
		Stream bool `json:"stream"`
	}{openaiRequest: apiReq, Stream: true}

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

	// CRITICAL: Go's context cancellation does NOT interrupt resp.Body.Read().
	// The only way to abort a stalled SSE stream is to force-close the body.
	// This goroutine watches ctx.Done() and closes the body, which makes
	// scanner.Scan() return false with an error, unblocking parseSSEStream.
	streamDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			p.logger.Info("Context cancelled, force-closing SSE stream",
				zap.Error(ctx.Err()))
			resp.Body.Close() // Force unblock scanner.Scan()
		case <-streamDone:
			// Normal completion — no need to force close
		}
	}()

	result, err := p.parseSSEStream(ctx, resp.Body, deltaCh)
	close(streamDone) // Signal the watcher to exit
	return result, err
}

// openaiStreamChunk represents a single SSE chunk from the OpenAI streaming API
type openaiStreamChunk struct {
	ID      string                 `json:"id"`
	Choices []openaiStreamChoice   `json:"choices"`
	Usage   *openaiUsage           `json:"usage,omitempty"`
	Model   string                 `json:"model"`
}

type openaiStreamChoice struct {
	Delta        openaiStreamDelta `json:"delta"`
	FinishReason *string           `json:"finish_reason"`
}

type openaiStreamDelta struct {
	Role      string            `json:"role,omitempty"`
	Content   string            `json:"content,omitempty"`
	ToolCalls []openaiToolCall  `json:"tool_calls,omitempty"`
}

// parseSSEStream reads a text/event-stream response, emitting deltas and accumulating the final response.
//
// Three-tier termination protection (industry best practice):
//   L1: Break on finish_reason (don't wait for [DONE] — some APIs never send it)
//   L2: 60s read idle timeout (detect stale connections)
//   L3: Per-call context timeout (set by callLLMWithRetry)
func (p *OpenAIBuiltinProvider) parseSSEStream(ctx context.Context, reader io.Reader, deltaCh chan<- service.StreamChunk) (*service.LLMResponse, error) {
	// L2: Wrap reader with idle timeout — if no data for 60s, the read returns an error.
	// This catches silently-stalled API connections that send headers but then go silent.
	idleTimeout := 60 * time.Second
	tReader := &timedReader{r: reader, timeout: idleTimeout}

	scanner := bufio.NewScanner(tReader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // 1MB max line

	// Accumulators for the final response
	var contentBuilder strings.Builder
	toolCallMap := make(map[int]*toolCallAccumulator) // index → accumulator
	var modelUsed string
	var tokensUsed int
	var finishReason string

	for scanner.Scan() {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		line := scanner.Text()

		// SSE format: "data: {json}" or "data: [DONE]"
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk openaiStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			p.logger.Debug("Skip unparseable SSE chunk", zap.Error(err))
			continue
		}

		if chunk.Model != "" {
			modelUsed = chunk.Model
		}
		if chunk.Usage != nil {
			tokensUsed = chunk.Usage.TotalTokens
		}

		if len(chunk.Choices) == 0 {
			continue
		}

		choice := chunk.Choices[0]
		delta := choice.Delta

		// Accumulate finish reason
		if choice.FinishReason != nil {
			finishReason = *choice.FinishReason
		}

		// Text delta
		if delta.Content != "" {
			contentBuilder.WriteString(delta.Content)
			deltaCh <- service.StreamChunk{
				DeltaText: delta.Content,
			}
		}

		// Tool call deltas (may arrive in fragments across multiple chunks)
		for _, tc := range delta.ToolCalls {
			idx := tc.Index // Use explicit index from OpenAI API

			if _, ok := toolCallMap[idx]; !ok {
				// New tool call starting at this index
				toolCallMap[idx] = &toolCallAccumulator{
					ID:   tc.ID,
					Name: tc.Function.Name,
				}
			}

			acc := toolCallMap[idx]
			// Update ID/Name if provided (first chunk for this index)
			if tc.ID != "" {
				acc.ID = tc.ID
			}
			if tc.Function.Name != "" {
				acc.Name = tc.Function.Name
			}
			acc.ArgsBuilder.WriteString(tc.Function.Arguments)
		}

		// L1: finish_reason received — break immediately, don't wait for [DONE].
		// Some APIs (Bailian, MiniMax) send finish_reason but never send [DONE],
		// causing scanner.Scan() to block indefinitely.
		if finishReason != "" {
			deltaCh <- service.StreamChunk{
				FinishReason: finishReason,
			}
			p.logger.Debug("SSE stream: finish_reason received, breaking",
				zap.String("finish_reason", finishReason))
			break
		}
	}

	// L2: Distinguish idle timeout from real scan errors
	if err := scanner.Err(); err != nil {
		if isIdleTimeoutErr(err) {
			p.logger.Warn("SSE stream idle timeout — API stalled",
				zap.Duration("idle_timeout", idleTimeout),
				zap.String("content_so_far", truncateForLog(contentBuilder.String(), 100)),
			)
			// If we got content, return it as a partial success (better than nothing)
			if contentBuilder.Len() > 0 || len(toolCallMap) > 0 {
				p.logger.Info("Returning partial SSE response after idle timeout")
			} else {
				return nil, fmt.Errorf("SSE stream stalled: no data for %v", idleTimeout)
			}
		} else {
			return nil, fmt.Errorf("SSE scan error: %w", err)
		}
	}

	// Build final response
	resp := &service.LLMResponse{
		Content:    contentBuilder.String(),
		ModelUsed:  modelUsed,
		TokensUsed: tokensUsed,
	}

	// Assemble accumulated tool calls
	for i := 0; i < len(toolCallMap); i++ {
		acc := toolCallMap[i]
		var args map[string]interface{}
		if argsStr := acc.ArgsBuilder.String(); argsStr != "" {
			if err := json.Unmarshal([]byte(argsStr), &args); err != nil {
				p.logger.Warn("Failed to parse streamed tool call args",
					zap.String("tool", acc.Name),
					zap.Error(err),
				)
				continue
			}
		}
		tc := entity.ToolCallInfo{
			ID:        acc.ID,
			Name:      acc.Name,
			Arguments: args,
		}
		resp.ToolCalls = append(resp.ToolCalls, tc)

		// Emit final tool call as delta
		deltaCh <- service.StreamChunk{
			DeltaToolCall: &tc,
		}
	}

	return resp, nil
}

// toolCallAccumulator accumulates tool call fragments across SSE chunks
type toolCallAccumulator struct {
	ID          string
	Name        string
	ArgsBuilder strings.Builder
}

// --- OpenAI API Types ---

type openaiRequest struct {
	Model       string          `json:"model"`
	Messages    []openaiMessage `json:"messages"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Temperature float64         `json:"temperature,omitempty"`
	Tools       []openaiTool    `json:"tools,omitempty"`
}

type openaiMessage struct {
	Role       string            `json:"role"`
	Content    string            `json:"content"`
	ToolCalls  []openaiToolCall  `json:"tool_calls,omitempty"`
	ToolCallID string            `json:"tool_call_id,omitempty"`
	Name       string            `json:"name,omitempty"`
}

type openaiTool struct {
	Type     string              `json:"type"`
	Function openaiToolFunction  `json:"function"`
}

type openaiToolFunction struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

type openaiToolCall struct {
	Index    int                  `json:"index"` // Explicit index from SSE streaming (0-based)
	ID       string               `json:"id"`
	Type     string               `json:"type"`
	Function openaiToolCallFunc   `json:"function"`
}

type openaiToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON string
}

type openaiResponse struct {
	ID      string           `json:"id"`
	Choices []openaiChoice   `json:"choices"`
	Usage   openaiUsage      `json:"usage"`
	Model   string           `json:"model"`
}

type openaiChoice struct {
	Message      openaiMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

type openaiUsage struct {
	TotalTokens int `json:"total_tokens"`
}

// --- Conversion ---

func (p *OpenAIBuiltinProvider) buildAPIRequest(req *service.LLMRequest) *openaiRequest {
	// Strip provider prefix (e.g. "bailian/qwen3-max" → "qwen3-max")
	model := req.Model
	if idx := strings.Index(model, "/"); idx >= 0 {
		model = model[idx+1:]
	}

	apiReq := &openaiRequest{
		Model:       model,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	}

	// Convert messages
	for _, msg := range req.Messages {
		apiMsg := openaiMessage{
			Role:       msg.Role,
			Content:    msg.Content,
			ToolCallID: msg.ToolCallID,
			Name:       msg.Name,
		}

		// Convert tool calls in assistant messages
		for _, tc := range msg.ToolCalls {
			argsJSON, _ := json.Marshal(tc.Arguments)
			apiMsg.ToolCalls = append(apiMsg.ToolCalls, openaiToolCall{
				ID:   tc.ID,
				Type: "function",
				Function: openaiToolCallFunc{
					Name:      tc.Name,
					Arguments: string(argsJSON),
				},
			})
		}

		apiReq.Messages = append(apiReq.Messages, apiMsg)
	}

	// Convert tool definitions
	for _, td := range req.Tools {
		apiReq.Tools = append(apiReq.Tools, openaiTool{
			Type: "function",
			Function: openaiToolFunction{
				Name:        td.Name,
				Description: td.Description,
				Parameters:  convertSchema(td.Parameters),
			},
		})
	}

	return apiReq
}

func (p *OpenAIBuiltinProvider) parseAPIResponse(body []byte) (*service.LLMResponse, error) {
	var apiResp openaiResponse
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
		TokensUsed: apiResp.Usage.TotalTokens,
	}

	// Convert tool calls
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

// convertSchema converts a domaintool.Definition.Schema to OpenAI parameter format
func convertSchema(schema map[string]interface{}) map[string]interface{} {
	if schema == nil {
		return map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}
	}

	// domaintool.Definition.Schema is already in JSON Schema format
	result := make(map[string]interface{})
	for k, v := range schema {
		result[k] = v
	}

	// Ensure "type" is set
	if _, ok := result["type"]; !ok {
		result["type"] = "object"
	}

	return result
}

// --- Convenience Constructors ---

// NewAntigravityProvider creates a provider for the Antigravity proxy
func NewAntigravityProvider(apiKey string, logger *zap.Logger) *OpenAIBuiltinProvider {
	return NewOpenAIBuiltinProvider(ProviderConfig{
		Name:    "antigravity",
		BaseURL: "https://api.antigravity.wiki/v1",
		APIKey:  apiKey,
		Models:  []string{"gemini-3-pro-low", "gemini-3-flash-low", "claude-sonnet-4-20250514"},
	}, logger)
}

// NewBailianProvider creates a provider for Aliyun Bailian (Qwen)
func NewBailianProvider(apiKey string, logger *zap.Logger) *OpenAIBuiltinProvider {
	return NewOpenAIBuiltinProvider(ProviderConfig{
		Name:    "bailian",
		BaseURL: "https://coding.dashscope.aliyuncs.com/v1",
		APIKey:  apiKey,
		Models:  []string{"qwen3-coder-plus"},
	}, logger)
}

// NewOllamaProvider creates a provider for local Ollama
func NewOllamaProvider(baseURL string, logger *zap.Logger) *OpenAIBuiltinProvider {
	if !strings.HasSuffix(baseURL, "/v1") {
		baseURL += "/v1"
	}
	return NewOpenAIBuiltinProvider(ProviderConfig{
		Name:    "ollama",
		BaseURL: baseURL,
		APIKey:  "ollama",
		Models:  []string{}, // wildcard
	}, logger)
}

// --- SSE idle timeout support ---

// errIdleTimeout is the sentinel error returned when timedReader's deadline expires.
var errIdleTimeout = fmt.Errorf("SSE read idle timeout")

// timedReader wraps an io.Reader and applies a per-Read deadline.
// If a single Read blocks longer than `timeout`, it returns errIdleTimeout.
// This detects stalled SSE streams where the API stops sending data mid-stream.
type timedReader struct {
	r       io.Reader
	timeout time.Duration
}

func (t *timedReader) Read(p []byte) (int, error) {
	type result struct {
		n   int
		err error
	}
	ch := make(chan result, 1)
	go func() {
		n, err := t.r.Read(p)
		ch <- result{n, err}
	}()

	select {
	case res := <-ch:
		return res.n, res.err
	case <-time.After(t.timeout):
		return 0, errIdleTimeout
	}
}

// isIdleTimeoutErr checks if an error is our SSE idle timeout sentinel.
func isIdleTimeoutErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "SSE read idle timeout")
}

// truncateForLog truncates a string for safe logging.
func truncateForLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
