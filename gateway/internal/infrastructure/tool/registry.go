package tool

import (
	"os"
	"time"

	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/service"
	domaintool "github.com/ngoclaw/ngoclaw/gateway/internal/domain/tool"
	"github.com/ngoclaw/ngoclaw/gateway/internal/infrastructure/sandbox"
	"go.uber.org/zap"
)

// ToolLayerDeps aggregates all external dependencies needed by the tool layer.
// This is the single configuration point for the entire tool subsystem.
type ToolLayerDeps struct {
	// Required
	Registry domaintool.Registry
	Logger   *zap.Logger

	// Infrastructure
	Sandbox   *sandbox.ProcessSandbox // nil = tools run unsandboxed
	SkillExec SkillExecutor           // nil = browser tools disabled

	// Paths
	PythonEnv string // conda/venv path for Python-based tools
	SkillsDir string // ~/.ngoclaw/skills

	// Code Intelligence
	Workspace string // LSP workspace root

	// MCP
	MCPManager *MCPManager // nil = no MCP support

	// Media (nil = media tools not registered, e.g. CLI mode)
	MediaSender MediaSender

	// Sub-Agent (nil = sub_agent tool not registered)
	SubAgent *SubAgentDeps
}

// SubAgentDeps holds dependencies for the sub_agent tool.
type SubAgentDeps struct {
	LLMClient    service.LLMClient
	ToolExecutor service.ToolExecutor
	DefaultModel string
	MaxSteps     int
	Timeout      time.Duration
}

// RegisterAllTools registers all tools in one place. This is the ONLY
// tool registration entry point. Adding a new tool? Add it here.
//
// Registration order:
//  1. Core file operations (bash, read, write, edit, list, grep, glob)
//  2. Advanced (apply_patch, web_fetch)
//  3. Web & data (web_search, stock_analysis)
//  4. Browser (navigate, screenshot, click, type)
//  5. Code intelligence (repo_map, git, lint_fix, lsp)
//  6. Agent capabilities (save_memory, update_plan, sub_agent)
//  7. MCP management (mcp_manage + dynamic MCP server tools)
func RegisterAllTools(deps ToolLayerDeps) int {
	var tools []domaintool.Tool

	// ── 1. Core File Operations ──
	tools = append(tools,
		NewBashTool(deps.Sandbox, deps.Logger),
		NewReadFileTool(deps.Sandbox, deps.Logger),
		NewWriteFileTool(deps.Sandbox, deps.Logger),
		NewEditFileTool(deps.Sandbox, deps.Logger),
		NewListDirTool(deps.Sandbox, deps.Logger),
		NewSearchTool(deps.Sandbox, deps.Logger),
		NewGlobTool(deps.Sandbox, deps.Logger),
	)

	// ── 2. Advanced ──
	tools = append(tools,
		NewApplyPatchTool(deps.Sandbox, deps.Logger),
		NewWebFetchTool(deps.Sandbox, deps.Logger),
	)

	// ── 3. Web & Data ──
	tools = append(tools,
		NewWebSearchTool(deps.PythonEnv, deps.SkillsDir, deps.Logger),
		NewStockAnalysisTool(deps.PythonEnv, deps.SkillsDir, deps.Logger),
	)

	// ── 4. Browser (gRPC delegate) ──
	tools = append(tools,
		NewBrowserNavigateTool(deps.SkillExec, deps.Logger),
		NewBrowserScreenshotTool(deps.SkillExec, deps.Logger),
		NewBrowserClickTool(deps.SkillExec, deps.Logger),
		NewBrowserTypeTool(deps.SkillExec, deps.Logger),
	)

	// ── 5. Code Intelligence ──
	tools = append(tools, NewRepoMapTool(deps.Logger))

	workspace := deps.Workspace
	if workspace == "" {
		workspace, _ = os.Getwd()
	}
	tools = append(tools, NewLSPTool(workspace, deps.Logger))

	if deps.Sandbox != nil {
		tools = append(tools,
			NewGitTool(deps.Sandbox, deps.Logger),
			NewLintFixTool(deps.Sandbox, deps.Logger),
		)
	}

	// ── 6. Agent Capabilities ──
	tools = append(tools,
		NewSaveMemoryTool(deps.Logger),
		NewUpdatePlanTool(deps.Logger),
	)

	// ── 6b. Media (TG only) ──
	if deps.MediaSender != nil {
		tools = append(tools,
			NewSendPhotoTool(deps.MediaSender, deps.Logger),
			NewSendDocumentTool(deps.MediaSender, deps.Logger),
		)
	}

	if deps.SubAgent != nil {
		sa := deps.SubAgent
		tools = append(tools, NewSubAgentTool(
			sa.LLMClient,
			sa.ToolExecutor,
			sa.DefaultModel,
			sa.MaxSteps,
			sa.Timeout,
			deps.Logger,
		))
	}

	// ── 7. MCP Management ──
	if deps.MCPManager != nil {
		tools = append(tools, NewMCPManageTool(deps.MCPManager, deps.Logger))
	}

	// ── Register everything ──
	registered := 0
	for _, t := range tools {
		if err := deps.Registry.Register(t); err != nil {
			deps.Logger.Warn("Failed to register tool",
				zap.String("tool", t.Name()),
				zap.Error(err),
			)
		} else {
			deps.Logger.Info("Registered tool", zap.String("tool", t.Name()))
			registered++
		}
	}

	// ── MCP servers (hot-plugged from mcp.json) ──
	if deps.MCPManager != nil {
		deps.MCPManager.InitFromConfig()
	}

	deps.Logger.Info("Tool layer initialized",
		zap.Int("total_registered", registered),
	)

	return registered
}
