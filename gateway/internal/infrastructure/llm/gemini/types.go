package gemini

// --- Google Gemini API Types ---
// Reference: https://ai.google.dev/api/rest/v1beta/models/generateContent
//
// Key differences from OpenAI:
// - Messages use contents[].parts[] instead of messages[].content
// - Tool calls are parts[].functionCall
// - Tool results are parts[].functionResponse
// - System instruction is a separate field

// Request is the Gemini generateContent request format.
type Request struct {
	Contents          []Content          `json:"contents"`
	Tools             []ToolDeclaration  `json:"tools,omitempty"`
	SystemInstruction *Content           `json:"systemInstruction,omitempty"`
	GenerationConfig  *GenerationConfig  `json:"generationConfig,omitempty"`
}

// Content represents a conversation turn.
type Content struct {
	Role  string `json:"role,omitempty"` // "user" | "model"
	Parts []Part `json:"parts"`
}

// Part is a polymorphic content element within a Content.
type Part struct {
	// For text content
	Text string `json:"text,omitempty"`

	// For function call (model requesting tool execution)
	FunctionCall *FunctionCall `json:"functionCall,omitempty"`

	// For function response (user providing tool result)
	FunctionResponse *FunctionResponse `json:"functionResponse,omitempty"`

	// For thinking content (Gemini 2.5+ thinking)
	Thought   *bool  `json:"thought,omitempty"`
}

// FunctionCall represents a model's request to call a function.
type FunctionCall struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args"`
}

// FunctionResponse provides the result of a function call back to the model.
type FunctionResponse struct {
	Name     string                 `json:"name"`
	Response map[string]interface{} `json:"response"`
}

// ToolDeclaration wraps function declarations for the API.
type ToolDeclaration struct {
	FunctionDeclarations []FunctionDeclarationSpec `json:"functionDeclarations"`
}

// FunctionDeclarationSpec defines a callable function.
type FunctionDeclarationSpec struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

// GenerationConfig controls generation parameters.
type GenerationConfig struct {
	Temperature     float64 `json:"temperature,omitempty"`
	MaxOutputTokens int     `json:"maxOutputTokens,omitempty"`
	CandidateCount  int     `json:"candidateCount,omitempty"`
}

// Response is the Gemini generateContent response format.
type Response struct {
	Candidates    []Candidate    `json:"candidates"`
	UsageMetadata *UsageMetadata `json:"usageMetadata,omitempty"`
	ModelVersion  string         `json:"modelVersion,omitempty"`
}

// Candidate is a single response candidate.
type Candidate struct {
	Content       Content `json:"content"`
	FinishReason  string  `json:"finishReason,omitempty"` // "STOP" | "MAX_TOKENS" | "SAFETY"
}

// UsageMetadata reports token consumption.
type UsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

// Total returns the total token count.
func (u *UsageMetadata) Total() int {
	if u.TotalTokenCount > 0 {
		return u.TotalTokenCount
	}
	return u.PromptTokenCount + u.CandidatesTokenCount
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
