package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	domaintool "github.com/ngoclaw/ngoclaw/gateway/internal/domain/tool"
	"go.uber.org/zap"
)

// MCPManageTool is a builtin tool that allows the Agent to manage MCP servers
// at runtime: add, remove, list, and refresh. This tool enables the Agent to
// self-manage its MCP connections without requiring a gateway restart.
type MCPManageTool struct {
	manager *MCPManager
	logger  *zap.Logger
}

// NewMCPManageTool creates the mcp_manage builtin tool.
func NewMCPManageTool(manager *MCPManager, logger *zap.Logger) *MCPManageTool {
	return &MCPManageTool{manager: manager, logger: logger}
}

var _ domaintool.Tool = (*MCPManageTool)(nil)

func (t *MCPManageTool) Name() string { return "mcp_manage" }
func (t *MCPManageTool) Kind() domaintool.Kind { return domaintool.KindFetch }

func (t *MCPManageTool) Description() string {
	return "Manage MCP (Model Context Protocol) servers. " +
		"Actions: add, remove, list, refresh. Config persisted to ~/.ngoclaw/mcp.json."
}

func (t *MCPManageTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"add", "remove", "list", "refresh"},
				"description": "The action to perform",
			},
			"name": map[string]interface{}{
				"type":        "string",
				"description": "MCP server name (required for add/remove/refresh)",
			},
			"endpoint": map[string]interface{}{
				"type":        "string",
				"description": "MCP server endpoint URL (required for add, e.g. http://host:port)",
			},
		},
		"required": []string{"action"},
	}
}

func (t *MCPManageTool) Execute(ctx context.Context, args map[string]interface{}) (*domaintool.Result, error) {
	action, _ := args["action"].(string)
	name, _ := args["name"].(string)
	endpoint, _ := args["endpoint"].(string)

	switch strings.ToLower(action) {
	case "add":
		return t.executeAdd(name, endpoint)
	case "remove":
		return t.executeRemove(name)
	case "list":
		return t.executeList()
	case "refresh":
		return t.executeRefresh(name)
	default:
		return &domaintool.Result{
			Output:  fmt.Sprintf("Unknown action '%s'. Valid: add, remove, list, refresh", action),
			Success: false,
		}, nil
	}
}

func (t *MCPManageTool) executeAdd(name, endpoint string) (*domaintool.Result, error) {
	if name == "" || endpoint == "" {
		return &domaintool.Result{
			Output:  "Both 'name' and 'endpoint' are required for add action",
			Success: false,
		}, nil
	}

	if err := t.manager.AddServer(name, endpoint); err != nil {
		return &domaintool.Result{
			Output:  fmt.Sprintf("Failed to add MCP server '%s': %s", name, err),
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	// Get tool count
	servers := t.manager.ListServers()
	var toolCount int
	for _, s := range servers {
		if s.Name == name {
			toolCount = s.ToolCount
			break
		}
	}

	return &domaintool.Result{
		Output: fmt.Sprintf("MCP server '%s' added successfully.\n"+
			"Endpoint: %s\n"+
			"Tools discovered: %d\n"+
			"Config saved to: ~/.ngoclaw/mcp.json",
			name, endpoint, toolCount),
		Success: true,
	}, nil
}

func (t *MCPManageTool) executeRemove(name string) (*domaintool.Result, error) {
	if name == "" {
		return &domaintool.Result{
			Output:  "'name' is required for remove action",
			Success: false,
		}, nil
	}

	if err := t.manager.RemoveServer(name); err != nil {
		return &domaintool.Result{
			Output:  fmt.Sprintf("Failed to remove MCP server '%s': %s", name, err),
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	return &domaintool.Result{
		Output:  fmt.Sprintf("MCP server '%s' removed. All its tools have been unregistered.", name),
		Success: true,
	}, nil
}

func (t *MCPManageTool) executeList() (*domaintool.Result, error) {
	servers := t.manager.ListServers()
	if len(servers) == 0 {
		return &domaintool.Result{
			Output:  "No MCP servers configured.\nUse action 'add' with name and endpoint to register one.",
			Success: true,
		}, nil
	}

	data, _ := json.MarshalIndent(servers, "", "  ")
	return &domaintool.Result{
		Output:  fmt.Sprintf("Configured MCP servers (%d):\n%s", len(servers), string(data)),
		Success: true,
	}, nil
}

func (t *MCPManageTool) executeRefresh(name string) (*domaintool.Result, error) {
	if name == "" {
		return &domaintool.Result{
			Output:  "'name' is required for refresh action",
			Success: false,
		}, nil
	}

	if err := t.manager.RefreshServer(name); err != nil {
		return &domaintool.Result{
			Output:  fmt.Sprintf("Failed to refresh MCP server '%s': %s", name, err),
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	return &domaintool.Result{
		Output:  fmt.Sprintf("MCP server '%s' refreshed. Tools re-discovered.", name),
		Success: true,
	}, nil
}
