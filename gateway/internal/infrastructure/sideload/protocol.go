package sideload

import (
	"encoding/json"
	"fmt"
)

// JSON-RPC 2.0 Protocol Implementation
// Spec: https://www.jsonrpc.org/specification

const jsonRPCVersion = "2.0"

// Request is a JSON-RPC 2.0 request
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"` // string | int | null (notification when absent)
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is a JSON-RPC 2.0 response
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError is a JSON-RPC 2.0 error object
type RPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("JSON-RPC error %d: %s", e.Code, e.Message)
}

// Standard JSON-RPC error codes
const (
	ErrParse          = -32700
	ErrInvalidRequest = -32600
	ErrMethodNotFound = -32601
	ErrInvalidParams  = -32602
	ErrInternal       = -32603
	// Application-defined errors
	ErrModuleNotReady = -32001
	ErrTimeout        = -32002
	ErrToolExecFailed = -32003
)

// NewRequest creates a new JSON-RPC request
func NewRequest(id interface{}, method string, params interface{}) (*Request, error) {
	var rawParams json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("marshal params: %w", err)
		}
		rawParams = b
	}
	return &Request{
		JSONRPC: jsonRPCVersion,
		ID:      id,
		Method:  method,
		Params:  rawParams,
	}, nil
}

// NewNotification creates a JSON-RPC notification (no id, no response expected)
func NewNotification(method string, params interface{}) (*Request, error) {
	var rawParams json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("marshal params: %w", err)
		}
		rawParams = b
	}
	return &Request{
		JSONRPC: jsonRPCVersion,
		Method:  method,
		Params:  rawParams,
	}, nil
}

// NewResponse creates a success response
func NewResponse(id interface{}, result interface{}) (*Response, error) {
	b, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("marshal result: %w", err)
	}
	return &Response{
		JSONRPC: jsonRPCVersion,
		ID:      id,
		Result:  b,
	}, nil
}

// NewErrorResponse creates an error response
func NewErrorResponse(id interface{}, code int, message string, data interface{}) *Response {
	return &Response{
		JSONRPC: jsonRPCVersion,
		ID:      id,
		Error: &RPCError{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
}

// IsNotification checks if the request is a notification (no id)
func (r *Request) IsNotification() bool {
	return r.ID == nil
}

// ParseParams decodes params into the given struct
func (r *Request) ParseParams(v interface{}) error {
	if r.Params == nil {
		return nil
	}
	return json.Unmarshal(r.Params, v)
}

// ParseResult decodes result into the given struct
func (r *Response) ParseResult(v interface{}) error {
	if r.Result == nil {
		return nil
	}
	return json.Unmarshal(r.Result, v)
}

// --- Protocol Method Constants ---

const (
	MethodInitialize      = "initialize"
	MethodShutdown        = "shutdown"
	MethodToolExecute     = "tool/execute"
	MethodProviderGenerate = "provider/generate"
	MethodProviderDelta   = "provider/generate/delta"
	MethodHookInvoke      = "hook/invoke"
	MethodPing            = "ping"
)

// --- Protocol Param/Result Types ---

// InitializeParams sent from Core → Module
type InitializeParams struct {
	Capabilities []string          `json:"capabilities"` // ["tool", "provider", "hook"]
	Config       map[string]string `json:"config,omitempty"`
}

// InitializeResult returned from Module → Core
type InitializeResult struct {
	Name         string         `json:"name"`
	Version      string         `json:"version"`
	Capabilities ModuleCaps     `json:"capabilities"`
}

// ModuleCaps describes what a module provides
type ModuleCaps struct {
	Providers []ProviderCap `json:"providers,omitempty"`
	Tools     []ToolCap     `json:"tools,omitempty"`
	Hooks     []string      `json:"hooks,omitempty"`
}

// ProviderCap describes a LLM provider capability
type ProviderCap struct {
	ID     string   `json:"id"`
	Models []string `json:"models"`
}

// ToolCap describes a tool capability
type ToolCap struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema,omitempty"`
}

// ToolExecuteParams sent from Core → Module for tool execution
type ToolExecuteParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
	Context   ToolExecContext        `json:"context,omitempty"`
}

// ToolExecContext provides session info to the tool
type ToolExecContext struct {
	SessionID string `json:"session_id,omitempty"`
	Agent     string `json:"agent,omitempty"`
	UserID    string `json:"user_id,omitempty"`
}

// ToolExecuteResult returned from Module → Core
type ToolExecuteResult struct {
	Output   string                 `json:"output"`
	Success  bool                   `json:"success"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
	Error    string                 `json:"error,omitempty"`
}

// GenerateParams sent from Core → Module for LLM generation
type GenerateParams struct {
	Provider string                   `json:"provider"`
	Model    string                   `json:"model"`
	Messages []GenerateMessage        `json:"messages"`
	Tools    []ToolCap                `json:"tools,omitempty"`
	Stream   bool                     `json:"stream"`
	Options  map[string]interface{}   `json:"options,omitempty"`
}

// GenerateMessage is a chat message
type GenerateMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Name    string `json:"name,omitempty"`
}

// GenerateResult returned from Module → Core
type GenerateResult struct {
	Content      string               `json:"content,omitempty"`
	FinishReason string               `json:"finish_reason,omitempty"`
	ToolCalls    []ToolCallResult     `json:"tool_calls,omitempty"`
	TokensUsed   int                  `json:"tokens_used,omitempty"`
	ModelUsed    string               `json:"model_used,omitempty"`
}

// ToolCallResult is a tool call returned by the LLM
type ToolCallResult struct {
	ID        string                 `json:"id"`
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

// StreamDelta is sent as notification during streaming
type StreamDelta struct {
	Text      string           `json:"text,omitempty"`
	ToolCalls []ToolCallResult `json:"tool_calls,omitempty"`
}

// HookInvokeParams sent from Core → Module for hook invocation
type HookInvokeParams struct {
	Hook   string                 `json:"hook"`
	Input  map[string]interface{} `json:"input"`
	Output map[string]interface{} `json:"output,omitempty"`
}

// HookInvokeResult returned from Module → Core
type HookInvokeResult struct {
	Modified bool                   `json:"modified"`
	Output   map[string]interface{} `json:"output,omitempty"`
}
