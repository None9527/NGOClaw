package tool

import (
	"context"
	"fmt"
	"time"

	domaintool "github.com/ngoclaw/ngoclaw/gateway/internal/domain/tool"
	"github.com/ngoclaw/ngoclaw/gateway/internal/infrastructure/sandbox"
	"go.uber.org/zap"
)

// Executor 工具执行器 - 适配 Runner 接口
type Executor struct {
	registry      domaintool.Registry
	policy        *domaintool.Policy
	sandbox       *sandbox.ProcessSandbox
	skillExec     SkillExecutor
	logger        *zap.Logger
	execContext   domaintool.ExecutionContext
	pythonEnv     string // 全局 Python 环境路径
	skillsDir     string // 技能脚本目录
}

// NewExecutor 创建工具执行器
func NewExecutor(
	registry domaintool.Registry,
	policy *domaintool.Policy,
	sandbox *sandbox.ProcessSandbox,
	skillExec SkillExecutor,
	logger *zap.Logger,
	pythonEnv string,
	skillsDir string,
) *Executor {
	return &Executor{
		registry:    registry,
		policy:      policy,
		sandbox:     sandbox,
		skillExec:   skillExec,
		logger:      logger,
		execContext: domaintool.ExecContextSandbox,
		pythonEnv:   pythonEnv,
		skillsDir:   skillsDir,
	}
}

// ToolCall 工具调用 (与 runner 包中的定义兼容)
type ToolCall struct {
	ID        string
	Name      string
	Arguments map[string]interface{}
}

// ToolResult 工具结果
type ToolResult struct {
	ToolCallID string
	Output     string
	Success    bool
	Error      error
}

// ToolDef 工具定义
type ToolDef struct {
	Name        string
	Description string
	Parameters  map[string]interface{}
}

// Execute 执行工具调用
func (e *Executor) Execute(ctx context.Context, call ToolCall) (*ToolResult, error) {
	startTime := time.Now()

	// 检查策略
	if !e.policy.IsAllowed(call.Name) {
		e.logger.Warn("Tool execution denied by policy",
			zap.String("tool", call.Name),
		)
		return &ToolResult{
			ToolCallID: call.ID,
			Output:     fmt.Sprintf("Tool '%s' is not allowed by current policy", call.Name),
			Success:    false,
			Error:      fmt.Errorf("tool not allowed: %s", call.Name),
		}, nil
	}

	// 获取工具
	tool, exists := e.registry.Get(call.Name)
	if !exists {
		e.logger.Warn("Tool not found",
			zap.String("tool", call.Name),
		)
		return &ToolResult{
			ToolCallID: call.ID,
			Output:     fmt.Sprintf("Tool '%s' not found", call.Name),
			Success:    false,
			Error:      fmt.Errorf("tool not found: %s", call.Name),
		}, nil
	}

	e.logger.Info("Executing tool",
		zap.String("tool", call.Name),
		zap.String("call_id", call.ID),
		zap.String("context", e.execContext.String()),
	)

	// 执行工具
	result, err := tool.Execute(ctx, call.Arguments)
	
	duration := time.Since(startTime)

	if err != nil {
		e.logger.Error("Tool execution error",
			zap.String("tool", call.Name),
			zap.Duration("duration", duration),
			zap.Error(err),
		)
		return &ToolResult{
			ToolCallID: call.ID,
			Output:     err.Error(),
			Success:    false,
			Error:      err,
		}, nil
	}

	e.logger.Info("Tool execution completed",
		zap.String("tool", call.Name),
		zap.Duration("duration", duration),
		zap.Bool("success", result.Success),
	)

	return &ToolResult{
		ToolCallID: call.ID,
		Output:     result.Output,
		Success:    result.Success,
		Error:      nil,
	}, nil
}

// GetToolDefs 获取所有工具定义
func (e *Executor) GetToolDefs() []ToolDef {
	// 获取策略过滤后的工具列表
	enforcer := domaintool.NewPolicyEnforcer(e.policy, e.registry)
	filtered := enforcer.FilteredList()

	defs := make([]ToolDef, len(filtered))
	for i, def := range filtered {
		defs[i] = ToolDef{
			Name:        def.Name,
			Description: def.Description,
			Parameters:  def.Parameters,
		}
	}

	return defs
}

// SetExecutionContext 设置执行上下文
func (e *Executor) SetExecutionContext(ctx domaintool.ExecutionContext) {
	e.execContext = ctx
}

// RegisterBuiltinTools 注册内置工具
func (e *Executor) RegisterBuiltinTools() error {
	builtins := []domaintool.Tool{
		// Core file operations
		NewBashTool(e.sandbox, e.logger),
		NewReadFileTool(e.sandbox, e.logger),
		NewWriteFileTool(e.sandbox, e.logger),
		NewEditFileTool(e.sandbox, e.logger),
		NewListDirTool(e.sandbox, e.logger),
		NewSearchTool(e.sandbox, e.logger),
		NewGlobTool(e.sandbox, e.logger),
		// Advanced tools
		NewApplyPatchTool(e.sandbox, e.logger),
		NewWebFetchTool(e.sandbox, e.logger),
		// Web search (SearXNG + deep scraping)
		NewWebSearchTool(e.pythonEnv, e.skillsDir, e.logger),
		// Stock analysis
		NewStockAnalysisTool(e.pythonEnv, e.skillsDir, e.logger),
		// Browser tools (delegate to Python AI Service via gRPC)
		NewBrowserNavigateTool(e.skillExec, e.logger),
		NewBrowserScreenshotTool(e.skillExec, e.logger),
		NewBrowserClickTool(e.skillExec, e.logger),
		NewBrowserTypeTool(e.skillExec, e.logger),
	}

	for _, tool := range builtins {
		if err := e.registry.Register(tool); err != nil {
			e.logger.Warn("Failed to register builtin tool",
				zap.String("tool", tool.Name()),
				zap.Error(err),
			)
		} else {
			e.logger.Info("Registered builtin tool",
				zap.String("tool", tool.Name()),
			)
		}
	}

	return nil
}

// NeedsApproval 检查是否需要用户批准
func (e *Executor) NeedsApproval() bool {
	return e.policy.AskMode
}
