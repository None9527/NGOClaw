package anthropic

// --- Anthropic Messages API Types ---
// Reference: https://docs.anthropic.com/en/docs/build-with-claude/tool-use
//
// Key differences from OpenAI:
// - Messages use content blocks ([]ContentBlock) instead of flat string content
// - Tool calls are content blocks with type "tool_use"
// - Tool results are sent as role "user" with type "tool_result"
// - System prompt is a separate top-level field, not a message

// Request is the Anthropic Messages API request format.
type Request struct {
	Model         string         `json:"model"`
	MaxTokens     int            `json:"max_tokens"`
	System        string         `json:"system,omitempty"`
	Messages      []Message      `json:"messages"`
	Tools         []Tool         `json:"tools,omitempty"`
	Temperature   float64        `json:"temperature,omitempty"`
	Stream        bool           `json:"stream,omitempty"`
}

// Message represents an Anthropic conversation message.
type Message struct {
	Role    string         `json:"role"` // "user" | "assistant"
	Content []ContentBlock `json:"content"`
}

// ContentBlock is a polymorphic content element.
type ContentBlock struct {
	Type string `json:"type"` // "text" | "tool_use" | "tool_result" | "thinking"

	// For type "text"
	Text string `json:"text,omitempty"`

	// For type "tool_use" (assistant requesting a tool call)
	ID    string                 `json:"id,omitempty"`
	Name  string                 `json:"name,omitempty"`
	Input map[string]interface{} `json:"input,omitempty"`

	// For type "tool_result" (user providing tool output)
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"` // text result from tool

	// For type "thinking" (extended thinking)
	Thinking string `json:"thinking,omitempty"`
}

// Tool is an Anthropic tool definition.
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

// Response is the Anthropic Messages API response.
type Response struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"` // "message"
	Role         string         `json:"role"` // "assistant"
	Content      []ContentBlock `json:"content"`
	Model        string         `json:"model"`
	StopReason   string         `json:"stop_reason"` // "end_turn" | "tool_use" | "max_tokens"
	Usage        Usage          `json:"usage"`
}

// Usage reports token consumption.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Total returns total token count.
func (u *Usage) Total() int {
	return u.InputTokens + u.OutputTokens
}

// --- Streaming Types ---
// Anthropic uses event-based SSE with typed events.

// StreamEvent represents a typed SSE event from the Anthropic streaming API.
type StreamEvent struct {
	Type  string `json:"type"` // event type
	Index int    `json:"index,omitempty"`

	// For content_block_start
	ContentBlock *ContentBlock `json:"content_block,omitempty"`

	// For content_block_delta
	Delta *DeltaBlock `json:"delta,omitempty"`

	// For message_delta
	Usage *Usage `json:"usage,omitempty"`

	// For message_start
	Message *Response `json:"message,omitempty"`
}

// DeltaBlock represents incremental content in a stream.
type DeltaBlock struct {
	Type       string `json:"type"` // "text_delta" | "input_json_delta" | "thinking_delta"
	Text       string `json:"text,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
	Thinking   string `json:"thinking,omitempty"`

	// For message_delta event
	StopReason string `json:"stop_reason,omitempty"`
}

// ConvertSchema ensures tool parameter schema has proper JSON Schema format.
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
