package application

import (
	"context"
	"fmt"

	domaintool "github.com/ngoclaw/ngoclaw/gateway/internal/domain/tool"
)

// toolBridge adapts domaintool.Registry → service.ToolExecutor.
// This allows the AgentLoop to discover and execute tools through the shared registry.
type toolBridge struct {
	registry domaintool.Registry
}

// Execute implements service.ToolExecutor.Execute
func (b *toolBridge) Execute(ctx context.Context, name string, args map[string]interface{}) (*domaintool.Result, error) {
	tool, ok := b.registry.Get(name)
	if !ok {
		return &domaintool.Result{
			Output:  fmt.Sprintf("Tool '%s' not found", name),
			Success: false,
			Error:   fmt.Sprintf("tool '%s' not registered", name),
		}, nil
	}
	return tool.Execute(ctx, args)
}

// GetDefinitions implements service.ToolExecutor.GetDefinitions
func (b *toolBridge) GetDefinitions() []domaintool.Definition {
	return b.registry.List()
}

// GetToolKind implements service.ToolExecutor.GetToolKind
func (b *toolBridge) GetToolKind(name string) domaintool.Kind {
	tool, ok := b.registry.Get(name)
	if !ok {
		return domaintool.KindExecute
	}
	return tool.Kind()
}
