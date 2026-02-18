package openai

import "encoding/json"

// --- OpenAI API Request/Response Types ---
// These types represent the OpenAI chat completions API format.
// Compatible with: OpenAI, Bailian (Qwen), MiniMax, DeepSeek, Ollama, vLLM, etc.

type Request struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
	Tools       []Tool    `json:"tools,omitempty"`
}

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

type ToolCall struct {
	Index    int          `json:"index"` // Explicit index from SSE streaming (0-based)
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function ToolCallFunc `json:"function"`
}

type ToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON string
}

type Response struct {
	ID      string   `json:"id"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
	Model   string   `json:"model"`
}

type Choice struct {
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

type Usage struct {
	TotalTokens      int `json:"total_tokens"`
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	InputTokens      int `json:"input_tokens"`
	OutputTokens     int `json:"output_tokens"`
}

// Total returns the best available total token count.
func (u *Usage) Total() int {
	if u.TotalTokens > 0 {
		return u.TotalTokens
	}
	if u.PromptTokens+u.CompletionTokens > 0 {
		return u.PromptTokens + u.CompletionTokens
	}
	if u.InputTokens+u.OutputTokens > 0 {
		return u.InputTokens + u.OutputTokens
	}
	return 0
}

// --- Streaming Types ---

type StreamChunkData struct {
	ID      string         `json:"id"`
	Choices []StreamChoice `json:"choices"`
	Usage   *Usage         `json:"usage,omitempty"`
	Model   string         `json:"model"`
}

type StreamChoice struct {
	Delta        StreamDelta `json:"delta"`
	FinishReason *string     `json:"finish_reason"`
}

type StreamDelta struct {
	Role      string     `json:"role,omitempty"`
	Content   string     `json:"content,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// --- Stream Request Wrapper ---

type StreamRequest struct {
	*Request
	Stream        bool                   `json:"stream"`
	StreamOptions map[string]interface{} `json:"stream_options,omitempty"`
}

// ConvertSchema ensures a tool parameter schema has proper JSON Schema format.
func ConvertSchema(schema map[string]interface{}) map[string]interface{} {
	if schema == nil {
		return map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}
	}

	result := make(map[string]interface{})
	for k, v := range schema {
		result[k] = v
	}
	if _, ok := result["type"]; !ok {
		result["type"] = "object"
	}
	return result
}

// MarshalToolCallArgs marshals tool call arguments to JSON string.
func MarshalToolCallArgs(args map[string]interface{}) string {
	if args == nil {
		return "{}"
	}
	b, _ := json.Marshal(args)
	return string(b)
}
