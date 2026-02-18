package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/entity"
	domaintool "github.com/ngoclaw/ngoclaw/gateway/internal/domain/tool"
	"go.uber.org/zap"
)

// AIClientAdapter adapts any LLM calling function to the AgentLoop's LLMClient interface.
// Used to bridge the ProcessMessageUseCase and other non-AgentLoop callers.
type AIClientAdapter struct {
	generateFn func(ctx context.Context, req *LLMRequest) (*LLMResponse, error)
	logger     *zap.Logger
}

// NewAIClientAdapter creates an adapter that wraps the existing AI calling mechanism
func NewAIClientAdapter(
	generateFn func(ctx context.Context, req *LLMRequest) (*LLMResponse, error),
	logger *zap.Logger,
) *AIClientAdapter {
	return &AIClientAdapter{
		generateFn: generateFn,
		logger:     logger,
	}
}

// Generate implements LLMClient interface
func (a *AIClientAdapter) Generate(ctx context.Context, req *LLMRequest) (*LLMResponse, error) {
	return a.generateFn(ctx, req)
}

// ToolExecutorAdapter adapts the existing tool.Executor to the AgentLoop's ToolExecutor interface
type ToolExecutorAdapter struct {
	registry domaintool.Registry
	policy   *domaintool.Policy
	logger   *zap.Logger
}

// NewToolExecutorAdapter creates a tool executor adapter from the existing registry
func NewToolExecutorAdapter(
	registry domaintool.Registry,
	policy *domaintool.Policy,
	logger *zap.Logger,
) *ToolExecutorAdapter {
	return &ToolExecutorAdapter{
		registry: registry,
		policy:   policy,
		logger:   logger,
	}
}

// Execute implements ToolExecutor interface
func (t *ToolExecutorAdapter) Execute(ctx context.Context, name string, args map[string]interface{}) (*domaintool.Result, error) {
	// Policy check
	if t.policy != nil && !t.policy.IsAllowed(name) {
		return &domaintool.Result{
			Output:  fmt.Sprintf("Tool '%s' is not allowed by current policy", name),
			Success: false,
			Error:   "tool not allowed",
		}, nil
	}

	tool, exists := t.registry.Get(name)
	if !exists {
		return &domaintool.Result{
			Output:  fmt.Sprintf("Tool '%s' not found in registry", name),
			Success: false,
			Error:   "tool not found",
		}, nil
	}

	return tool.Execute(ctx, args)
}

// GetToolKind implements ToolExecutor interface — returns the Kind of a registered tool.
func (t *ToolExecutorAdapter) GetToolKind(name string) domaintool.Kind {
	tool, exists := t.registry.Get(name)
	if !exists {
		return domaintool.KindExecute // unknown tools treated as dangerous
	}
	return tool.Kind()
}

// GetDefinitions implements ToolExecutor interface
func (t *ToolExecutorAdapter) GetDefinitions() []domaintool.Definition {
	if t.policy != nil {
		enforcer := domaintool.NewPolicyEnforcer(t.policy, t.registry)
		return enforcer.FilteredList()
	}
	return t.registry.List()
}

// ParseToolCallsFromText extracts tool calls from text-based responses.
// Some models (especially smaller ones) don't use native function calling
// and instead emit tool calls as formatted text.
//
// Supported formats:
//   - [TOOL_CALL] name({"arg":"val"}) [/TOOL_CALL]
//   - ```tool_call\n{"name":"...","arguments":{...}}\n```
func ParseToolCallsFromText(text string) (string, []entity.ToolCallInfo) {
	var toolCalls []entity.ToolCallInfo
	cleanedText := text

	// Pattern 1: [TOOL_CALL] name({"arg":"val"}) [/TOOL_CALL]
	for {
		startIdx := strings.Index(cleanedText, "[TOOL_CALL]")
		if startIdx == -1 {
			break
		}
		endIdx := strings.Index(cleanedText[startIdx:], "[/TOOL_CALL]")
		if endIdx == -1 {
			break
		}
		endIdx += startIdx

		callStr := strings.TrimSpace(cleanedText[startIdx+len("[TOOL_CALL]") : endIdx])

		// Parse name(args)
		parenIdx := strings.Index(callStr, "(")
		if parenIdx > 0 && strings.HasSuffix(callStr, ")") {
			name := strings.TrimSpace(callStr[:parenIdx])
			argsStr := callStr[parenIdx+1 : len(callStr)-1]

			var args map[string]interface{}
			if err := json.Unmarshal([]byte(argsStr), &args); err == nil {
				toolCalls = append(toolCalls, entity.ToolCallInfo{
					ID:        fmt.Sprintf("tc_%d", len(toolCalls)),
					Name:      name,
					Arguments: args,
				})
			}
		}

		cleanedText = cleanedText[:startIdx] + cleanedText[endIdx+len("[/TOOL_CALL]"):]
	}

	// Pattern 2: ```tool_call\n{...}\n```
	for {
		startMarker := "```tool_call\n"
		startIdx := strings.Index(cleanedText, startMarker)
		if startIdx == -1 {
			break
		}
		rest := cleanedText[startIdx+len(startMarker):]
		endIdx := strings.Index(rest, "\n```")
		if endIdx == -1 {
			break
		}

		jsonStr := strings.TrimSpace(rest[:endIdx])
		var call struct {
			Name      string                 `json:"name"`
			Arguments map[string]interface{} `json:"arguments"`
		}
		if err := json.Unmarshal([]byte(jsonStr), &call); err == nil {
			toolCalls = append(toolCalls, entity.ToolCallInfo{
				ID:        fmt.Sprintf("tc_%d", len(toolCalls)),
				Name:      call.Name,
				Arguments: call.Arguments,
			})
		}

		cleanedText = cleanedText[:startIdx] + cleanedText[startIdx+len(startMarker)+endIdx+len("\n```"):]
	}

	return strings.TrimSpace(cleanedText), toolCalls
}
