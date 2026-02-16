package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/entity"
	domaintool "github.com/ngoclaw/ngoclaw/gateway/internal/domain/tool"
	"go.uber.org/zap"
)

// AgentLoopConfig holds configuration for the agent's ReAct loop
type AgentLoopConfig struct {
	DoomLoopThreshold int     // Deprecated: use LoopDetectThreshold for sliding window
	MaxOutputChars    int     // Maximum characters per tool output before truncation (default: 32000)
	Temperature       float64 // LLM temperature
	Model             string  // LLM model identifier (e.g. "bailian/qwen3-coder-plus")

	// Per-model policy overrides from config.yaml.
	// Keys are matched by substring against model ID (e.g. "qwen3", "minimax").
	ModelPolicies map[string]*ModelPolicyOverride

	// Auto-retry configuration
	MaxRetries    int           // Max retries per LLM call (default: 3)
	RetryBaseWait time.Duration // Base wait between retries (default: 2s, exponential: 2s, 4s, 8s)

	// Context compaction
	CompactThreshold int // Deprecated: use ContextGuard for token-based compaction
	CompactKeepLast  int // Number of recent messages to preserve during compaction (default: 10)

	// Plan vs Act mode — LLM proposes a plan before executing tool calls
	PlanMode bool // When true, first turn forces planning; execution pauses until approval

	// Parallel tool execution
	MaxParallelTools int // Max concurrent tool executions (default: 4, 1 = sequential)

	// Guardrails (backported from EmbeddedRunner)
	MaxTokenBudget      int64         // Token budget limit (0 = disabled)
	RunTimeout          time.Duration // Max duration per Run (0 = disabled, default 10m)
	ToolTimeout         time.Duration // Per-tool execution timeout (default 30s)
	ContextMaxTokens    int           // Context window token limit (default 128000)
	ContextWarnRatio    float64       // Warn when context > this ratio (default 0.7)
	ContextHardRatio    float64       // Force compact when > this ratio (default 0.85)
	LoopWindowSize      int           // Sliding window size for loop detection (default 10)
	LoopDetectThreshold int           // Identical calls in window to trigger loop alarm (default 5)
}

// DefaultAgentLoopConfig returns production-ready defaults.
// Note: no MaxSteps — the loop runs until the model stops calling tools,
// guarded by RunTimeout + LoopDetector + ContextGuard (matching OpenClaw/Gemini CLI patterns).
func DefaultAgentLoopConfig() AgentLoopConfig {
	return AgentLoopConfig{
		DoomLoopThreshold:   3,
		MaxOutputChars:      32000,
		Temperature:         0.7,
		MaxRetries:          3,
		RetryBaseWait:       2 * time.Second,
		CompactThreshold:    40,
		CompactKeepLast:     10,
		MaxParallelTools:    4,
		RunTimeout:          10 * time.Minute,
		ToolTimeout:         30 * time.Second,
		ContextMaxTokens:    128000,
		ContextWarnRatio:    0.7,
		ContextHardRatio:    0.85,
		LoopWindowSize:      10,
		LoopDetectThreshold: 5,
	}
}

// LLMClient is the interface the agent loop uses to communicate with language models.
// It decouples the loop from gRPC/Sideload/direct HTTP transport.
type LLMClient interface {
	// Generate sends a prompt with tool definitions and history, returning a full response.
	Generate(ctx context.Context, req *LLMRequest) (*LLMResponse, error)

	// GenerateStream sends a prompt and streams back partial responses.
	// The channel is closed when the stream ends. The caller must drain it.
	// Returns the final accumulated LLMResponse after the channel is closed.
	GenerateStream(ctx context.Context, req *LLMRequest, deltaCh chan<- StreamChunk) (*LLMResponse, error)
}

// StreamChunk represents a single delta from a streaming LLM response.
type StreamChunk struct {
	DeltaText     string               // Incremental text content
	DeltaToolCall *entity.ToolCallInfo  // Incremental tool call (may arrive in fragments)
	FinishReason  string               // "stop", "tool_calls", "" (not yet finished)
}

// LLMRequest is the request sent to the language model
type LLMRequest struct {
	Messages    []LLMMessage           `json:"messages"`
	Tools       []domaintool.Definition `json:"tools,omitempty"`
	Model       string                 `json:"model"`
	MaxTokens   int                    `json:"max_tokens,omitempty"`
	Temperature float64                `json:"temperature"`
}

// LLMMessage represents a single message in the conversation
type LLMMessage struct {
	Role       string               `json:"role"` // "system", "user", "assistant", "tool"
	Content    string               `json:"content"`
	Parts      []ContentPart        `json:"parts,omitempty"`    // Multimodal content (takes precedence over Content)
	ToolCalls  []entity.ToolCallInfo `json:"tool_calls,omitempty"`
	ToolCallID string               `json:"tool_call_id,omitempty"`
	Name       string               `json:"name,omitempty"`
}

// ContentPart represents a multimodal content fragment.
type ContentPart struct {
	Type     string `json:"type"`               // "text", "image", "audio", "file"
	Text     string `json:"text,omitempty"`      // Content when Type="text"
	MediaURL string `json:"media_url,omitempty"` // URL when Type="image"/"audio"/"file"
	MimeType string `json:"mime_type,omitempty"` // e.g. "image/png"
	Data     []byte `json:"data,omitempty"`      // Inline binary data (optional)
}

// TextContent returns all text content, joining text parts or falling back to Content.
func (m *LLMMessage) TextContent() string {
	if len(m.Parts) == 0 {
		return m.Content
	}
	var texts []string
	for _, p := range m.Parts {
		if p.Type == "text" && p.Text != "" {
			texts = append(texts, p.Text)
		}
	}
	if len(texts) == 0 {
		return m.Content
	}
	return strings.Join(texts, "\n")
}

// HasMedia returns true if the message contains non-text content.
func (m *LLMMessage) HasMedia() bool {
	for _, p := range m.Parts {
		if p.Type != "text" {
			return true
		}
	}
	return false
}

// LLMResponse is the response from the language model
type LLMResponse struct {
	Content    string               `json:"content"`
	ToolCalls  []entity.ToolCallInfo `json:"tool_calls,omitempty"`
	ModelUsed  string               `json:"model_used"`
	TokensUsed int                  `json:"tokens_used"`
}

// ToolExecutor is the interface for executing tools within the agent loop
type ToolExecutor interface {
	Execute(ctx context.Context, name string, args map[string]interface{}) (*domaintool.Result, error)
	GetDefinitions() []domaintool.Definition
	// GetToolKind returns the Kind of a registered tool (defaults to "execute" if unknown)
	GetToolKind(name string) domaintool.Kind
}

// AgentLoop implements the ReAct (Reason + Act) agent loop with:
//   - Auto-retry with exponential backoff
//   - Context compaction for long conversations
//   - Graceful abort support
//   - Doom loop detection
type AgentLoop struct {
	llm        LLMClient
	tools      ToolExecutor
	config     AgentLoopConfig
	hooks      AgentHook
	middleware *MiddlewarePipeline
	toolCache  *ToolResultCache
	logger     *zap.Logger
}

// NewAgentLoop creates a new ReAct agent loop
func NewAgentLoop(llm LLMClient, tools ToolExecutor, config AgentLoopConfig, logger *zap.Logger) *AgentLoop {
	if config.DoomLoopThreshold <= 0 {
		config.DoomLoopThreshold = 3
	}
	if config.MaxOutputChars <= 0 {
		config.MaxOutputChars = 32000
	}
	if config.MaxRetries <= 0 {
		config.MaxRetries = 3
	}
	if config.RetryBaseWait <= 0 {
		config.RetryBaseWait = 2 * time.Second
	}
	if config.CompactThreshold <= 0 {
		config.CompactThreshold = 40
	}
	if config.CompactKeepLast <= 0 {
		config.CompactKeepLast = 10
	}
	if config.MaxParallelTools <= 0 {
		config.MaxParallelTools = 4
	}
	// Guardrail defaults
	if config.ToolTimeout <= 0 {
		config.ToolTimeout = 30 * time.Second
	}
	if config.ContextMaxTokens <= 0 {
		config.ContextMaxTokens = 128000
	}
	if config.ContextWarnRatio <= 0 {
		config.ContextWarnRatio = 0.7
	}
	if config.ContextHardRatio <= 0 {
		config.ContextHardRatio = 0.85
	}
	if config.LoopWindowSize <= 0 {
		config.LoopWindowSize = 10
	}
	if config.LoopDetectThreshold <= 0 {
		config.LoopDetectThreshold = 5
	}

	return &AgentLoop{
		llm:        llm,
		tools:      tools,
		config:     config,
		hooks:      &NoOpHook{},
		middleware: NewMiddlewarePipeline(logger),
		toolCache:  NewToolResultCache(30*time.Second, 100),
		logger:     logger,
	}
}

// SetHooks replaces the hook chain for this agent loop.
func (a *AgentLoop) SetHooks(hooks AgentHook) {
	if hooks != nil {
		a.hooks = hooks
	}
}

// SetMiddleware replaces the middleware pipeline for this agent loop.
func (a *AgentLoop) SetMiddleware(mw *MiddlewarePipeline) {
	if mw != nil {
		a.middleware = mw
	}
}

// AgentResult is the final result of the agent loop
type AgentResult struct {
	FinalContent string
	TotalSteps   int
	TotalTokens  int
	ModelUsed    string
	ToolsUsed    []string
}

// Run executes the ReAct loop, emitting events to the provided channel.
// The caller should read from eventCh until it's closed.
// approveCh is optional (nil = no plan approval needed). When PlanMode is on,
// the loop sends a plan_propose event and waits for true on approveCh to proceed.
func (a *AgentLoop) Run(ctx context.Context, systemPrompt string, userMessage string, history []LLMMessage, approveCh <-chan bool) (*AgentResult, <-chan entity.AgentEvent) {
	eventCh := make(chan entity.AgentEvent, 64)

	result := &AgentResult{}

	// Inject trace ID for structured logging
	ctx = WithTraceID(ctx, "")
	a.logger = a.logger.With(zap.String("trace_id", TraceIDFromContext(ctx)))

	// Clear tool cache for each new run
	a.toolCache.Clear()

	// Create a state machine for this run
	sm := NewStateMachine(0, a.logger) // 0 = unlimited steps (bounded by RunTimeout)

	// Wire hooks into state machine transitions
	sm.OnTransition(func(from, to AgentState, snap StateSnapshot) {
		a.hooks.OnStateChange(from, to, snap)
	})

	go func() {
		defer close(eventCh)
		defer func() {
			if r := recover(); r != nil {
				a.logger.Error("Agent loop panicked",
					zap.Any("panic", r),
					zap.Stack("stack"),
				)
				a.emitEvent(eventCh, entity.AgentEvent{
					Type:  entity.EventError,
					Error: fmt.Sprintf("Internal error: %v", r),
				})
				result.FinalContent = fmt.Sprintf("Internal error: %v", r)
			}
		}()
		a.runLoop(ctx, systemPrompt, userMessage, history, result, eventCh, approveCh, sm)
	}()

	return result, eventCh
}

// planInstruction is injected into the user message when PlanMode is active.
// Structures agent output into 4 phases: Understand → Plan → Execute → Verify.
const planInstruction = `

---

**Plan Mode 已激活。请严格遵循以下 4 阶段流程：**

## Phase 1: 理解 (Understand)
分析用户需求，阅读相关文件建立上下文。

## Phase 2: 计划 (Plan)
输出结构化方案：

### 方案
1. **目标**: [本次修改要达成的目标]
2. **影响范围**: [需要修改/创建/删除的文件列表]
3. **修改顺序**: [按依赖关系排序的执行步骤]
4. **风险评估**: [可能的副作用或破坏性变更]
5. **验证方式**: [完成后如何确认修改正确]

**在用户批准前，不要调用任何工具。只输出方案。**

## Phase 3: 执行 (Execute)
方案批准后，按计划逐步执行。

## Phase 4: 验证 (Verify)
执行完毕后，自动运行验证：
- 编译检查 (go build / npm run build)
- lint 检查
- 现有测试
- 总结变更清单
`

func (a *AgentLoop) runLoop(
	ctx context.Context,
	systemPrompt string,
	userMessage string,
	history []LLMMessage,
	result *AgentResult,
	eventCh chan<- entity.AgentEvent,
	approveCh <-chan bool,
	sm *StateMachine,
) {
	// Store user message in context for MemoryMiddleware
	ctx = WithUserMessage(ctx, userMessage)

	// Build initial messages
	messages := make([]LLMMessage, 0, len(history)+2)
	if systemPrompt != "" {
		messages = append(messages, LLMMessage{Role: "system", Content: systemPrompt})
	}
	messages = append(messages, history...)

	// Plan vs Act: inject plan instruction into user message on first turn
	actualUserMsg := userMessage
	if a.config.PlanMode {
		actualUserMsg = userMessage + planInstruction
	}
	messages = append(messages, LLMMessage{Role: "user", Content: actualUserMsg})

	toolDefs := a.tools.GetDefinitions()
	toolsUsedSet := make(map[string]bool)

	// Initialize guardrails for this run
	loopDetector := NewLoopDetector(a.config.LoopWindowSize, a.config.LoopDetectThreshold, a.logger)
	contextGuard := NewContextGuard(a.config.ContextMaxTokens, a.config.ContextWarnRatio, a.config.ContextHardRatio, a.logger)
	var costGuard *CostGuard
	if a.config.MaxTokenBudget > 0 || a.config.RunTimeout > 0 {
		costGuard = NewCostGuard(a.config.MaxTokenBudget, a.config.RunTimeout, a.logger)
	}

	// Apply RunTimeout to context
	if a.config.RunTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, a.config.RunTimeout)
		defer cancel()
	}

	planApproved := !a.config.PlanMode // If not plan mode, consider it already approved

	consecutiveFailures := 0 // Track consecutive tool failures for early abort

	// Resolve per-model policy for this run
	policy := ResolveModelPolicy(a.config.Model, a.config.ModelPolicies)
	a.logger.Info("Model policy resolved",
		zap.String("model", a.config.Model),
		zap.String("reasoning_format", policy.ReasoningFormat),
		zap.Int("progress_interval", policy.ProgressInterval),
		zap.String("prompt_style", policy.PromptStyle),
	)

	// No MaxSteps ceiling — loop runs until model stops calling tools.
	// Safety nets: RunTimeout, LoopDetector, ContextGuard, consecutiveFailures.
	for step := 1; ; step++ {
		sm.SetStep(step)

		// Check cancellation (RunTimeout or user abort)
		if err := ctx.Err(); err != nil {
			_ = sm.Transition(StateAborted)
			a.emitEvent(eventCh, entity.AgentEvent{
				Type:  entity.EventError,
				Error: "context cancelled",
			})
			return
		}

		a.logger.Info("Agent loop step",
			zap.Int("step", step),
			zap.Int("messages", len(messages)),
		)

		// === Progress injection: policy-driven interval with escalating urgency ===
		if policy.ProgressInterval > 0 && step > 1 && step%policy.ProgressInterval == 0 {
			if msg := policy.BuildProgressMessage(step); msg != "" {
				messages = append(messages, LLMMessage{
					Role:    "user",
					Content: msg,
				})
			}
		}

		// === Context compaction (token-based via ContextGuard) ===
		ctxCheck := contextGuard.Check(messages)
		if ctxCheck.NeedCompaction || len(messages) > a.config.CompactThreshold {
			_ = sm.Transition(StateCompacting)
			messages = a.compactMessages(messages)
			a.logger.Info("Context compacted",
				zap.Int("messages_after", len(messages)),
				zap.Int("estimated_tokens", ctxCheck.EstimatedTokens),
				zap.Float64("ratio", ctxCheck.Ratio),
			)
		}

		// === Sanitize messages (fix orphan tool_use blocks) ===
		messages = sanitizeMessages(messages)

		// === 1. Call LLM with auto-retry ===
		if !planApproved {
			_ = sm.Transition(StatePlanning)
		} else {
			_ = sm.Transition(StateStreaming)
		}

		// === Middleware: BeforeModel (transform messages) ===
		mwMessages := a.middleware.RunBeforeModel(ctx, messages, step)

		llmReq := &LLMRequest{
			Messages:    mwMessages,
			Tools:       toolDefs,
			Model:       a.config.Model,
			Temperature: a.config.Temperature,
		}

		a.hooks.BeforeLLMCall(ctx, llmReq, step)

		resp, err := a.callLLMWithRetry(ctx, llmReq, step, eventCh)
		if err != nil {
			// All retries exhausted
			sm.RecordError()
			_ = sm.Transition(StateError)
			a.hooks.OnError(ctx, err, step)
			a.emitEvent(eventCh, entity.AgentEvent{
				Type:  entity.EventError,
				Error: fmt.Sprintf("LLM error at step %d (after %d retries): %v", step, a.config.MaxRetries, err),
			})
			result.FinalContent = fmt.Sprintf("Error: %v", err)
			return
		}

		result.TotalTokens += resp.TokensUsed
		result.ModelUsed = resp.ModelUsed
		result.TotalSteps = step
		sm.AddTokens(resp.TokensUsed)
		sm.SetModel(resp.ModelUsed)

		// === CostGuard: check token + time budgets ===
		if costGuard != nil {
			if err := costGuard.AddTokens(int64(resp.TokensUsed)); err != nil {
				_ = sm.Transition(StateError)
				a.hooks.OnError(ctx, err, step)
				a.emitEvent(eventCh, entity.AgentEvent{
					Type:  entity.EventError,
					Error: fmt.Sprintf("Budget exceeded: %v", err),
				})
				result.FinalContent = fmt.Sprintf("Stopped: %v", err)
				return
			}
			if err := costGuard.CheckBudget(); err != nil {
				_ = sm.Transition(StateError)
				a.hooks.OnError(ctx, err, step)
				a.emitEvent(eventCh, entity.AgentEvent{
					Type:  entity.EventError,
					Error: fmt.Sprintf("Budget exceeded: %v", err),
				})
				result.FinalContent = fmt.Sprintf("Stopped: %v", err)
				return
			}
		}

		// === Middleware: AfterModel (transform response) ===
		resp = a.middleware.RunAfterModel(ctx, resp, step)

		a.hooks.AfterLLMCall(ctx, resp, step)

		// 2. Emit step info with state
		snap := sm.Snapshot()
		a.emitEvent(eventCh, entity.AgentEvent{
			Type: entity.EventStepDone,
			StepInfo: &entity.StepInfo{
				Step:       step,
				TokensUsed: resp.TokensUsed,
				ModelUsed:  resp.ModelUsed,
				State:      string(snap.State),
			},
		})

		// 3. Check if there are tool calls
		a.logger.Info("[DIAG] Post-LLM decision point",
			zap.Int("step", step),
			zap.Int("tool_calls", len(resp.ToolCalls)),
			zap.Int("content_len", len(resp.Content)),
			zap.Bool("plan_approved", planApproved),
			zap.Int("tokens", resp.TokensUsed),
		)
		if len(resp.ToolCalls) == 0 {
			// No tool calls
			if !planApproved && resp.Content != "" {
				// Plan mode: this is the plan proposal
				a.hooks.OnPlanProposed(ctx, resp.Content)
				a.emitEvent(eventCh, entity.AgentEvent{
					Type:    entity.EventPlanPropose,
					Content: resp.Content,
				})

				// Wait for approval
				if approveCh != nil {
					a.logger.Info("Plan proposed, waiting for approval")
					select {
					case approved := <-approveCh:
						if !approved {
							result.FinalContent = resp.Content
							a.emitEvent(eventCh, entity.AgentEvent{
								Type:    entity.EventTextDelta,
								Content: "Plan rejected by user.",
							})
							a.emitEvent(eventCh, entity.AgentEvent{Type: entity.EventDone})
							return
						}
					case <-ctx.Done():
						a.emitEvent(eventCh, entity.AgentEvent{
							Type:  entity.EventError,
							Error: "context cancelled while waiting for plan approval",
						})
						return
					}
				}

				planApproved = true
				a.logger.Info("Plan approved, proceeding to execution")

				// Add plan as assistant message, then inject "proceed + verify" user message
				messages = append(messages, LLMMessage{
					Role:    "assistant",
					Content: resp.Content,
				})
				messages = append(messages, LLMMessage{
					Role:    "user",
					Content: "方案已批准，请执行。完成后用文字告诉我结果。",
				})
				continue // Go back to LLM to execute the plan
			}

			// Normal final response
			// NOTE: text_delta events already emitted in real-time by streaming in callLLMWithRetry
			a.logger.Info("[DIAG] Final response path",
				zap.Int("step", step),
				zap.Int("content_len", len(resp.Content)),
			)

			finalContent := resp.Content

			// Summary fallback: some models (Qwen3) return empty content on the final step
			// because all text was emitted as intermediate narration during tool-calling steps.
			// In this case, ask the LLM for a brief summary.
			if finalContent == "" && step > 1 {
				a.logger.Info("[DIAG] Final content empty after tool execution, requesting summary")
				messages = append(messages, LLMMessage{
					Role:    "assistant",
					Content: resp.Content,
				})
				messages = append(messages, LLMMessage{
					Role:    "user",
					Content: "请用简洁的文字总结你刚才执行的操作和最终结果。不要重复方案，只说结果。",
				})
				summaryReq := &LLMRequest{
					Messages:    messages,
					Tools:       nil, // No tools — force text response
					Model:       a.config.Model,
					Temperature: a.config.Temperature,
				}
				summaryResp, err := a.callLLMWithRetry(ctx, summaryReq, step+1, eventCh)
				if err == nil && summaryResp.Content != "" {
					finalContent = summaryResp.Content
					a.logger.Info("[DIAG] Summary fallback succeeded",
						zap.Int("content_len", len(finalContent)),
					)
				}
			}

			result.FinalContent = finalContent
			_ = sm.Transition(StateComplete)
			a.hooks.OnComplete(ctx, result)
			a.emitEvent(eventCh, entity.AgentEvent{Type: entity.EventDone})
			a.logger.Info("[DIAG] EventDone emitted, returning")
			return
		}

		// NOTE: intermediate text already streamed in real-time by callLLMWithRetry

		// 4. Append assistant message with tool calls to history
		messages = append(messages, LLMMessage{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		// 5. Execute tool calls (parallel when multiple)
		_ = sm.Transition(StateToolExec)

		// Pre-check: sliding window loop detection (skip SafeKind tools — read/search/think)
		for _, tc := range resp.ToolCalls {
			kind := a.tools.GetToolKind(tc.Name)
			if domaintool.SafeKinds[kind] {
				continue // read-only tools don't count toward loop detection
			}
			// Build a deterministic args fingerprint so "bash(cmd1)" ≠ "bash(cmd2)"
			argsFingerprint := ""
			if tc.Arguments != nil {
				if raw, err := json.Marshal(tc.Arguments); err == nil {
					argsFingerprint = string(raw)
				}
			}
			if loopDetector.Record(tc.Name, argsFingerprint) {
				a.logger.Warn("Loop detected by sliding window",
					zap.String("tool", tc.Name),
				)
				_ = sm.Transition(StateError)
				a.emitEvent(eventCh, entity.AgentEvent{
					Type:  entity.EventError,
					Error: fmt.Sprintf("Loop detected: tool '%s' called %d times with identical args in recent window", tc.Name, a.config.LoopDetectThreshold),
				})
				result.FinalContent = fmt.Sprintf("Stopped: loop detected on tool '%s'", tc.Name)
				return
			}
		}

		// Emit all tool call events
		for _, tc := range resp.ToolCalls {
			a.emitEvent(eventCh, entity.AgentEvent{
				Type: entity.EventToolCall,
				ToolCall: &entity.ToolCallEvent{
					ID:        tc.ID,
					Name:      tc.Name,
					Arguments: tc.Arguments,
				},
			})
		}

		// Execute tools in parallel with semaphore
		type toolExecResult struct {
			Index    int
			TC       entity.ToolCallInfo
			Output   string
			Display  string // Rich UI output from tool (may be empty)
			Success  bool
			Duration time.Duration
		}

		results := make([]toolExecResult, len(resp.ToolCalls))
		var wg sync.WaitGroup
		sem := make(chan struct{}, a.config.MaxParallelTools)

		for i, tc := range resp.ToolCalls {
			wg.Add(1)
			go func(idx int, call entity.ToolCallInfo) {
				defer wg.Done()

				// Acquire semaphore slot
				select {
				case sem <- struct{}{}:
					defer func() { <-sem }()
				case <-ctx.Done():
					results[idx] = toolExecResult{
						Index:   idx,
						TC:      call,
						Output:  "context cancelled",
						Success: false,
					}
					return
				}

				// BeforeToolCall hook — veto check
				if !a.hooks.BeforeToolCall(ctx, call.Name, call.Arguments) {
					a.logger.Info("Tool call vetoed by hook",
						zap.String("tool", call.Name),
					)
					results[idx] = toolExecResult{
						Index:   idx,
						TC:      call,
						Output:  fmt.Sprintf("Tool '%s' was blocked by security policy", call.Name),
						Success: false,
					}
					return
				}

				start := time.Now()

				// Check tool cache for deduplication
				if cached, cachedSuccess, hit := a.toolCache.Get(call.Name, call.Arguments); hit {
					a.logger.Debug("Tool cache hit",
						zap.String("tool", call.Name),
					)
					results[idx] = toolExecResult{
						Index:    idx,
						TC:       call,
						Output:   cached,
						Success:  cachedSuccess,
						Duration: time.Since(start),
					}
					a.hooks.AfterToolCall(ctx, call.Name, cached, cachedSuccess)
					return
				}

				// Per-tool timeout
				toolCtx := ctx
				if a.config.ToolTimeout > 0 {
					var toolCancel context.CancelFunc
					toolCtx, toolCancel = context.WithTimeout(ctx, a.config.ToolTimeout)
					defer toolCancel()
				}

				toolResult, err := a.tools.Execute(toolCtx, call.Name, call.Arguments)
				duration := time.Since(start)

				var output string
				var success bool

				if err != nil {
					output = fmt.Sprintf("[TOOL_FAILED] %s\n[ERROR] %v\n[HINT] 工具执行出错。如果问题持续，请停止重试并告知用户。", call.Name, err)
					success = false
					a.logger.Error("Tool execution failed",
						zap.String("tool", call.Name),
						zap.Duration("duration", duration),
						zap.Error(err),
					)
				} else {
					success = toolResult.Success
					if !success {
						// Structured failure annotation — help model understand what went wrong
						errText := toolResult.Error
						if errText == "" {
							errText = toolResult.Output
						}
						exitCode := 1
						hint := "命令执行失败"
						if toolResult.Metadata != nil {
							if ec, ok := toolResult.Metadata["exit_code"].(int); ok {
								exitCode = ec
								hint = exitCodeHint(ec)
							}
						}
						output = fmt.Sprintf("[TOOL_FAILED] %s\n[EXIT_CODE] %d — %s\n[OUTPUT]\n%s",
							call.Name, exitCode, hint, errText)
					} else {
						output = toolResult.Output
					}
				}

				output = truncateOutput(output, a.config.MaxOutputChars)

				// Store result in cache for deduplication
				a.toolCache.Put(call.Name, call.Arguments, output, success)

				// Capture Display for UI rendering (may be empty)
				var display string
				if toolResult != nil {
					display = toolResult.Display
				}

				results[idx] = toolExecResult{
					Index:    idx,
					TC:       call,
					Output:   output,
					Display:  display,
					Success:  success,
					Duration: duration,
				}
			}(i, tc)
		}

		wg.Wait()

		// Process results in order (preserves message ordering for LLM)
		for _, r := range results {
			toolsUsedSet[r.TC.Name] = true
			sm.RecordToolExec(r.TC.Name)

			a.emitEvent(eventCh, entity.AgentEvent{
				Type: entity.EventToolResult,
				ToolCall: &entity.ToolCallEvent{
					ID:        r.TC.ID,
					Name:      r.TC.Name,
					Arguments: r.TC.Arguments,
					Output:    r.Output,
					Display:   r.Display,
					Success:   r.Success,
					Duration:  r.Duration,
				},
			})

			messages = append(messages, LLMMessage{
				Role:       "tool",
				Content:    r.Output,
				ToolCallID: r.TC.ID,
				Name:       r.TC.Name,
			})
		}

		// Track consecutive failures — if all tools in this step failed, count it
		allFailed := true
		for _, r := range results {
			if r.Success {
				allFailed = false
				break
			}
		}
		if allFailed && len(results) > 0 {
			consecutiveFailures++
		} else {
			consecutiveFailures = 0
		}

		// If 3 consecutive rounds of all-failed tools, force report
		if consecutiveFailures >= 3 {
			messages = append(messages, LLMMessage{
				Role:    "user",
				Content: "[SYSTEM] 工具已连续失败 3 轮。请停止重试，用中文告诉用户：遇到了什么问题、尝试了什么、建议的解决方案。",
			})
			consecutiveFailures = 0 // Reset to allow one more attempt after reporting
		}

		// Continue loop — go back to step 1 (call LLM again)
	}

	// This point is only reached if the infinite loop somehow exits without
	// returning (should not happen — all exits are via return statements above).
	a.logger.Error("Agent loop exited unexpectedly")
	for name := range toolsUsedSet {
		result.ToolsUsed = append(result.ToolsUsed, name)
	}
}

// exitCodeHint returns a human-readable Chinese explanation for common exit codes.
func exitCodeHint(code int) string {
	switch code {
	case 0:
		return "成功"
	case 1:
		return "一般错误 — 检查命令参数或文件路径"
	case 2:
		return "参数错误 — 命令语法不正确"
	case 124:
		return "超时被杀 (TIMEOUT) — 命令未在时限内完成，可能网络不通或服务无响应"
	case 126:
		return "权限不足 — 文件不可执行"
	case 127:
		return "命令未找到 — 检查命令名称或 PATH"
	case 128:
		return "信号退出 — 进程被异常终止"
	case 130:
		return "Ctrl+C 中断"
	case 137:
		return "被 SIGKILL 杀死 — 可能内存不足 (OOM)"
	case 139:
		return "段错误 (SIGSEGV)"
	case 143:
		return "被 SIGTERM 终止"
	case 255:
		return "SSH 连接失败 — 检查主机可达性、端口、认证"
	default:
		if code > 128 {
			return fmt.Sprintf("被信号 %d 终止", code-128)
		}
		return "未知错误"
	}
}
