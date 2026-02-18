package tool

import (
	"context"
	"fmt"

	domaintool "github.com/ngoclaw/ngoclaw/gateway/internal/domain/tool"
	"go.uber.org/zap"
)

// MCPTool adapts a single MCP server tool to the domaintool.Tool interface,
// enabling MCP-discovered tools to be registered in the standard ToolRegistry
// alongside builtin tools, skills, etc.
type MCPTool struct {
	adapter     *MCPAdapter
	toolDef     MCPToolDef
	logger      *zap.Logger
}

// NewMCPTool creates a domaintool.Tool wrapper for a single MCP tool.
func NewMCPTool(adapter *MCPAdapter, def MCPToolDef, logger *zap.Logger) *MCPTool {
	return &MCPTool{
		adapter: adapter,
		toolDef: def,
		logger:  logger,
	}
}

// Compile-time interface check
var _ domaintool.Tool = (*MCPTool)(nil)

func (t *MCPTool) Name() string {
	// Prefix with MCP server name to avoid collisions (e.g. "newsnow_get_news")
	return fmt.Sprintf("%s_%s", t.adapter.Name(), t.toolDef.Name)
}

func (t *MCPTool) Description() string {
	return fmt.Sprintf("[MCP:%s] %s", t.adapter.Name(), t.toolDef.Description)
}

func (t *MCPTool) Kind() domaintool.Kind {
	return domaintool.KindFetch // MCP tools are remote calls
}

func (t *MCPTool) Schema() map[string]interface{} {
	if t.toolDef.InputSchema != nil {
		return t.toolDef.InputSchema
	}
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}

func (t *MCPTool) Execute(ctx context.Context, args map[string]interface{}) (*domaintool.Result, error) {
	t.logger.Info("Executing MCP tool",
		zap.String("server", t.adapter.Name()),
		zap.String("tool", t.toolDef.Name),
	)

	output, err := t.adapter.CallTool(ctx, t.toolDef.Name, args)
	if err != nil {
		return &domaintool.Result{
			Output:  err.Error(),
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	return &domaintool.Result{
		Output:  output,
		Success: true,
	}, nil
}

// RegisterMCPTools discovers tools from an MCPAdapter and registers them
// into the provided tool registry. Returns the count of registered tools.
func RegisterMCPTools(ctx context.Context, adapter *MCPAdapter, registry domaintool.Registry, logger *zap.Logger) (int, error) {
	tools, err := adapter.DiscoverTools(ctx)
	if err != nil {
		return 0, fmt.Errorf("MCP discovery failed for %s: %w", adapter.Name(), err)
	}

	registered := 0
	for _, def := range tools {
		mcpTool := NewMCPTool(adapter, def, logger)
		if err := registry.Register(mcpTool); err != nil {
			logger.Warn("Failed to register MCP tool",
				zap.String("server", adapter.Name()),
				zap.String("tool", def.Name),
				zap.Error(err),
			)
			continue
		}
		registered++
		logger.Info("Registered MCP tool",
			zap.String("name", mcpTool.Name()),
			zap.String("description", def.Description),
		)
	}

	return registered, nil
}
