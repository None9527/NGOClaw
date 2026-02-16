package ngoclaw

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client is the Go SDK client for the NGOClaw Agent Platform.
// It connects via HTTP/SSE to stream agent events in real time.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// NewClient creates a new NGOClaw SDK client
func NewClient(baseURL string, opts ...Option) *Client {
	c := &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 300 * time.Second,
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Option configures the client
type Option func(*Client)

// WithAPIKey sets the API key for authentication
func WithAPIKey(key string) Option {
	return func(c *Client) {
		c.apiKey = key
	}
}

// WithTimeout sets the HTTP client timeout
func WithTimeout(d time.Duration) Option {
	return func(c *Client) {
		c.httpClient.Timeout = d
	}
}

// AgentRequest is the request to run the agent loop
type AgentRequest struct {
	Message      string            `json:"message"`
	SystemPrompt string            `json:"system_prompt,omitempty"`
	Model        string            `json:"model,omitempty"`
	SessionID    string            `json:"session_id,omitempty"`
	History      []map[string]string `json:"history,omitempty"`
}

// AgentEvent is an event streamed from the agent loop
type AgentEvent struct {
	Event string                 `json:"event"`
	Data  map[string]interface{} `json:"data"`
}

// Content returns the text content of the event
func (e *AgentEvent) Content() string {
	if c, ok := e.Data["content"].(string); ok {
		return c
	}
	return ""
}

// IsText returns true for text-producing events
func (e *AgentEvent) IsText() bool {
	return e.Event == "text_delta" || e.Event == "thinking"
}

// IsDone returns true when the agent loop is complete
func (e *AgentEvent) IsDone() bool {
	return e.Event == "done" || e.Event == "complete"
}

// IsError returns true for error events
func (e *AgentEvent) IsError() bool {
	return e.Event == "error"
}

// ErrorMessage returns the error message if present
func (e *AgentEvent) ErrorMessage() string {
	if msg, ok := e.Data["error"].(string); ok {
		return msg
	}
	return ""
}

// AgentResult is the final result after the agent loop completes
type AgentResult struct {
	Content     string   `json:"content"`
	TotalSteps  int      `json:"total_steps"`
	TotalTokens int      `json:"total_tokens"`
	ModelUsed   string   `json:"model_used"`
	ToolsUsed   []string `json:"tools_used"`
}

// ToolDefinition describes an available tool
type ToolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// Run executes the agent and streams events via a channel.
// The channel is closed when the agent loop completes.
func (c *Client) Run(ctx context.Context, req *AgentRequest) (<-chan *AgentEvent, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/v1/agent", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	ch := make(chan *AgentEvent, 32)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		c.readSSEStream(resp.Body, ch)
	}()

	return ch, nil
}

// RunSync executes the agent and blocks until completion,
// returning the collected result.
func (c *Client) RunSync(ctx context.Context, req *AgentRequest) (*AgentResult, error) {
	ch, err := c.Run(ctx, req)
	if err != nil {
		return nil, err
	}

	result := &AgentResult{}
	for event := range ch {
		if event.IsText() {
			result.Content += event.Content()
		}
		if event.IsDone() {
			if steps, ok := event.Data["total_steps"].(float64); ok {
				result.TotalSteps = int(steps)
			}
			if tokens, ok := event.Data["total_tokens"].(float64); ok {
				result.TotalTokens = int(tokens)
			}
			if model, ok := event.Data["model_used"].(string); ok {
				result.ModelUsed = model
			}
		}
	}

	return result, nil
}

// ListTools returns the available tool definitions
func (c *Client) ListTools(ctx context.Context) ([]ToolDefinition, error) {
	httpReq, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/api/v1/agent/tools", nil)
	if err != nil {
		return nil, err
	}
	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Tools []ToolDefinition `json:"tools"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Tools, nil
}

// Health checks if the server is healthy
func (c *Client) Health(ctx context.Context) bool {
	httpReq, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/health", nil)
	if err != nil {
		return false
	}
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
}

func (c *Client) readSSEStream(r io.Reader, ch chan<- *AgentEvent) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		dataStr := strings.TrimSpace(line[5:])
		if dataStr == "[DONE]" {
			ch <- &AgentEvent{Event: "done", Data: make(map[string]interface{})}
			return
		}

		var data map[string]interface{}
		if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
			continue
		}

		event := &AgentEvent{Data: data}
		if e, ok := data["event"].(string); ok {
			event.Event = e
		}
		ch <- event
	}
}
