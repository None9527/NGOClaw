package tool

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/service"
	domaintool "github.com/ngoclaw/ngoclaw/gateway/internal/domain/tool"
	"go.uber.org/zap"
)

// depthKey is the context key for tracking sub-agent nesting depth.
type depthKey struct{}

// SubAgentTool allows the main agent to delegate sub-tasks to a new AgentLoop instance.
type SubAgentTool struct {
	llm             service.LLMClient
	tools           service.ToolExecutor
	defaultModel    string
	defaultMaxSteps int
	timeout         time.Duration
	logger          *zap.Logger
}

func NewSubAgentTool(llm service.LLMClient, tools service.ToolExecutor, defaultModel string, maxSteps int, timeout time.Duration, logger *zap.Logger) *SubAgentTool {
	if maxSteps <= 0 {
		maxSteps = 25
	}
	if timeout <= 0 {
		timeout = 3 * time.Minute
	}
	return &SubAgentTool{
		llm:             llm,
		tools:           tools,
		defaultModel:    defaultModel,
		defaultMaxSteps: maxSteps,
		timeout:         timeout,
		logger:          logger,
	}
}

func (t *SubAgentTool) Name() string        { return "spawn_agent" }
func (t *SubAgentTool) Kind() domaintool.Kind { return domaintool.KindExecute }

func (t *SubAgentTool) Description() string {
	return "Delegate a sub-task to an independent agent that has access to all the same tools. " +
		"Use this for complex tasks that benefit from focused, isolated execution. " +
		"The sub-agent runs its own ReAct loop and returns the final result. " +
		"Example: spawning an agent to audit a codebase, research a topic, or execute a multi-step procedure."
}

func (t *SubAgentTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"task": map[string]interface{}{
				"type":        "string",
				"description": "A clear description of the sub-task for the agent to complete",
			},
			"system_prompt": map[string]interface{}{
				"type":        "string",
				"description": "Optional system prompt to give the sub-agent a specific role or context",
			},
			"max_steps": map[string]interface{}{
				"type":        "integer",
				"description": fmt.Sprintf("Maximum reasoning steps for the sub-agent (default: %d)", t.defaultMaxSteps),
			},
		},
		"required": []string{"task"},
	}
}

func (t *SubAgentTool) Execute(ctx context.Context, args map[string]interface{}) (*domaintool.Result, error) {
	task, ok := args["task"].(string)
	if !ok || task == "" {
		return &domaintool.Result{Success: false, Error: "task is required"}, nil
	}

	// Enforce nesting depth limit (max 2 levels)
	depth := 0
	if d, ok := ctx.Value(depthKey{}).(int); ok {
		depth = d
	}
	if depth >= 2 {
		return &domaintool.Result{
			Success: false,
			Error:   "sub-agent nesting depth limit reached (max 2 levels)",
		}, nil
	}

	systemPrompt := ""
	if sp, ok := args["system_prompt"].(string); ok {
		systemPrompt = sp
	}

	maxSteps := t.defaultMaxSteps
	if ms, ok := args["max_steps"].(float64); ok && ms > 0 {
		maxSteps = int(ms)
		if maxSteps > t.defaultMaxSteps*2 {
			maxSteps = t.defaultMaxSteps * 2
		}
	}

	t.logger.Info("Spawning sub-agent",
		zap.String("task_preview", truncateStr(task, 100)),
		zap.Int("max_steps", maxSteps),
		zap.Int("depth", depth+1),
	)

	// Create sub-agent config (no MaxSteps — bounded by timeout like the main agent)
	cfg := service.AgentLoopConfig{
		DoomLoopThreshold: 3,
		MaxOutputChars:    32000,
		Temperature:       0.7,
		Model:             t.defaultModel,
		RunTimeout:        t.timeout,
	}

	subAgent := service.NewAgentLoop(t.llm, t.tools, cfg, t.logger.Named("sub-agent"))

	// Inject incremented depth into context
	subCtx := context.WithValue(ctx, depthKey{}, depth+1)

	// Set a timeout for the sub-agent (from config)
	subCtx, cancel := context.WithTimeout(subCtx, t.timeout)
	defer cancel()

	result, eventCh := subAgent.Run(subCtx, systemPrompt, task, nil, nil)

	// Drain events (we don't stream them to the parent, just wait for completion)
	var toolsUsed []string
	for ev := range eventCh {
		if ev.ToolCall != nil {
			toolsUsed = append(toolsUsed, ev.ToolCall.Name)
		}
	}

	t.logger.Info("Sub-agent completed",
		zap.Int("steps", result.TotalSteps),
		zap.Int("tokens", result.TotalTokens),
		zap.String("model", result.ModelUsed),
		zap.Int("tools_used", len(toolsUsed)),
	)

	// Format output
	var sb strings.Builder
	sb.WriteString("=== Sub-Agent Result ===\n\n")
	sb.WriteString(result.FinalContent)
	sb.WriteString("\n\n--- Execution Summary ---\n")
	sb.WriteString(fmt.Sprintf("Steps: %d | Tokens: %d | Model: %s\n", result.TotalSteps, result.TotalTokens, result.ModelUsed))
	if len(toolsUsed) > 0 {
		sb.WriteString(fmt.Sprintf("Tools used: %s\n", strings.Join(uniqueStrings(toolsUsed), ", ")))
	}

	return &domaintool.Result{
		Output:  sb.String(),
		Success: true,
		Metadata: map[string]interface{}{
			"steps":      result.TotalSteps,
			"tokens":     result.TotalTokens,
			"model":      result.ModelUsed,
			"tools_used": toolsUsed,
		},
	}, nil
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func uniqueStrings(ss []string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
