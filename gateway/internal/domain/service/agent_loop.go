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

	// Parallel tool execution
	MaxParallelTools int // Max concurrent tool executions (default: 4, 1 = sequential)

	// Guardrails — OpenClaw/Continue aligned: token budget is the only natural limit.
	// No MaxSteps, no RunTimeout. Loop runs until LLM stops calling tools or tokens exhaust.
	MaxTokenBudget      int64         // Token budget limit (0 = disabled)
	ToolTimeout         time.Duration // Per-tool execution timeout (default 30s)
	ContextMaxTokens    int           // Context window token limit (default 128000)
	ContextWarnRatio    float64       // Warn when context > this ratio (default 0.7)
	ContextHardRatio    float64       // Force compact when > this ratio (default 0.85)
	LoopWindowSize      int           // Sliding window size for exact-match loop detection (default 10)
	LoopDetectThreshold int           // Identical calls in window to trigger reflection (default 5)
	LoopNameThreshold   int           // Same tool name consecutive calls to trigger reflection (default 8)
}

// DefaultAgentLoopConfig returns production-ready defaults.
// OpenClaw/Continue aligned: no MaxSteps, no RunTimeout.
// Loop runs until LLM stops calling tools, guarded by token budget + ContextGuard.
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
		ToolTimeout:         30 * time.Second,
		ContextMaxTokens:    128000,
		ContextWarnRatio:    0.7,
		ContextHardRatio:    0.85,
		LoopWindowSize:      10,
		LoopDetectThreshold: 5,
		LoopNameThreshold:   8,
	}
}

// LLMClient is the interface the agent loop uses to communicate with language models.
// It decouples the loop from specific LLM provider implementations.
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
// modelOverride, when non-empty, overrides the default model for this run
// (used by TG /models command to switch models per-session).
func (a *AgentLoop) Run(ctx context.Context, systemPrompt string, userMessage string, history []LLMMessage, modelOverride string) (*AgentResult, <-chan entity.AgentEvent) {
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
		a.runLoop(ctx, systemPrompt, userMessage, history, result, eventCh, sm, modelOverride)
	}()

	return result, eventCh
}

func (a *AgentLoop) runLoop(
	ctx context.Context,
	systemPrompt string,
	userMessage string,
	history []LLMMessage,
	result *AgentResult,
	eventCh chan<- entity.AgentEvent,
	sm *StateMachine,
	modelOverride string,
) {
	// Store user message in context for MemoryMiddleware
	ctx = WithUserMessage(ctx, userMessage)

	// Build initial messages
	messages := make([]LLMMessage, 0, len(history)+2)
	if systemPrompt != "" {
		messages = append(messages, LLMMessage{Role: "system", Content: systemPrompt})
	}
	messages = append(messages, history...)
	messages = append(messages, LLMMessage{Role: "user", Content: userMessage})

	toolDefs := a.tools.GetDefinitions()
	toolsUsedSet := make(map[string]bool)

	// Initialize guardrails for this run
	loopDetector := NewLoopDetector(a.config.LoopWindowSize, a.config.LoopDetectThreshold, a.config.LoopNameThreshold, a.logger)
	contextGuard := NewContextGuard(a.config.ContextMaxTokens, a.config.ContextWarnRatio, a.config.ContextHardRatio, a.logger)
	var costGuard *CostGuard
	if a.config.MaxTokenBudget > 0 {
		costGuard = NewCostGuard(a.config.MaxTokenBudget, 0, a.logger)
	}

	// OpenClaw/Continue aligned: no RunTimeout. Token budget is the natural limit.


	consecutiveFailures := 0    // Track consecutive tool failures for early abort
	overflowCompactions := 0    // Track auto-compaction retries on context overflow (max 3)
	compactionThisTurn := false // OpenClaw pattern: auto-continue once after compaction

	// OpenClaw pattern: collect cleaned text from every assistant turn.
	// Many models (MiniMax, Qwen3) emit ALL useful text during intermediate
	// tool-calling steps and return empty content on the final step.
	// This slice captures each non-empty assistant response so we can use
	// the last one as a fallback when the final step's content is empty.
	var assistantTexts []string

	// Determine effective model for this run
	model := a.config.Model
	if modelOverride != "" {
		model = modelOverride
		a.logger.Info("Model override active", zap.String("override", modelOverride))
	}

	// Resolve per-model policy for this run
	policy := ResolveModelPolicy(model, a.config.ModelPolicies)
	a.logger.Info("Model policy resolved",
		zap.String("model", model),
		zap.String("reasoning_format", policy.ReasoningFormat),
		zap.Int("progress_interval", policy.ProgressInterval),
		zap.String("prompt_style", policy.PromptStyle),
	)

	// OpenClaw/Continue pattern: no MaxSteps, no RunTimeout.
	// Loop runs until LLM stops calling tools. Safety nets: token budget, ContextGuard.
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

		// === Context compaction (token-based only — no fixed message count threshold) ===
		// Aligned with OpenClaw/Gemini CLI: trigger ONLY on token ratio, never on message count.
		ctxCheck := contextGuard.Check(messages)
		if ctxCheck.NeedCompaction {
			_ = sm.Transition(StateCompacting)
			messages = a.compactMessages(messages)
			compactionThisTurn = true
			a.logger.Info("Context compacted (token threshold)",
				zap.Int("messages_after", len(messages)),
				zap.Int("estimated_tokens", ctxCheck.EstimatedTokens),
				zap.Float64("ratio", ctxCheck.Ratio),
			)
		}

		// === Sanitize messages (fix orphan tool_use blocks) ===
		messages = sanitizeMessages(messages)

		// === 1. Call LLM with auto-retry ===
		_ = sm.Transition(StateStreaming)

		// === Middleware: BeforeModel (transform messages) ===
		mwMessages := a.middleware.RunBeforeModel(ctx, messages, step)

		llmReq := &LLMRequest{
			Messages:    mwMessages,
			Tools:       toolDefs,
			Model:       model,
			Temperature: a.config.Temperature,
		}

		a.hooks.BeforeLLMCall(ctx, llmReq, step)

		resp, err := a.callLLMWithRetry(ctx, llmReq, step, eventCh)
		if err != nil {
			// OpenClaw pattern: reactive overflow detection.
			// If the API returns a context overflow error, auto-compact and retry
			// instead of failing immediately. Max 3 attempts.
			if IsContextOverflowError(err) && overflowCompactions < 3 {
				overflowCompactions++
				a.logger.Warn("Context overflow detected, auto-compacting",
					zap.Int("attempt", overflowCompactions),
					zap.Int("messages", len(messages)),
					zap.Error(err),
				)
				_ = sm.Transition(StateCompacting)
				messages = a.compactMessages(messages)
				a.logger.Info("Auto-compaction complete, retrying LLM call",
					zap.Int("messages_after", len(messages)),
				)
				continue // retry the loop iteration with compacted context
			}

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
			zap.Int("tokens", resp.TokensUsed),
		)
		if len(resp.ToolCalls) == 0 {
			// OpenClaw/Continue pattern: auto-continue once after compaction.
			// If compaction happened this turn, the LLM might stop prematurely because
			// it lost context. Give it one more chance by injecting "continue".
			if compactionThisTurn {
				compactionThisTurn = false // only continue once, preventing infinite loop
				a.logger.Info("Auto-continue after compaction (OpenClaw pattern)",
					zap.Int("step", step),
				)
				messages = append(messages, LLMMessage{
					Role:    "assistant",
					Content: resp.Content,
				})
				messages = append(messages, LLMMessage{
					Role:    "user",
					Content: "continue",
				})
				continue // retry the loop — LLM gets fresh context after compaction
			}

			// No tool calls — final response
			a.logger.Info("[DIAG] Final response path",
				zap.Int("step", step),
				zap.Int("content_len", len(resp.Content)),
			)

			finalContent := StripReasoningTags(resp.Content)

			// Fallback 1: if final step content is empty after multi-step execution,
			// request a proper summary from the model. This produces a coherent answer
			// rather than reusing intermediate narration ("让我检查…") which is just
			// the model's plan announcement, not a useful result.
			if strings.TrimSpace(finalContent) == "" && step > 1 {
				a.logger.Info("[DIAG] Final content empty, requesting summary")
				// Ensure proper role alternation. The last message in history is a
				// tool-result (role=tool) from the final tool call. We need to add
				// a user message. Some APIs require assistant-then-user alternation,
				// so insert a minimal assistant acknowledgment if the last message
				// isn't already from the assistant.
				if last := messages[len(messages)-1]; last.Role != "assistant" {
					messages = append(messages, LLMMessage{
						Role:    "assistant",
						Content: "好的，已完成工具调用。",
					})
				}
				messages = append(messages, LLMMessage{
					Role:    "user",
					Content: "请用简洁的文字总结你刚才执行的操作和最终结果。不要重复方案，只说结果。",
				})
				summaryReq := &LLMRequest{
					Messages:    messages,
					Tools:       nil, // No tools — force text response
					Model:       model,
					Temperature: a.config.Temperature,
				}
				summaryResp, err := a.callLLMWithRetry(ctx, summaryReq, step+1, eventCh)
				if err == nil && strings.TrimSpace(summaryResp.Content) != "" {
					finalContent = StripReasoningTags(summaryResp.Content)
					a.logger.Info("[DIAG] Summary fallback succeeded",
						zap.Int("content_len", len(finalContent)),
					)
				}
			}

			// Fallback 2: if summary also failed, use the last collected assistant text.
			// This is better than returning nothing, even though intermediate narration
			// is not ideal as a final answer.
			if strings.TrimSpace(finalContent) == "" && len(assistantTexts) > 0 {
				finalContent = assistantTexts[len(assistantTexts)-1]
				a.logger.Info("[DIAG] Using last assistant text as final content (last resort)",
					zap.Int("content_len", len(finalContent)),
					zap.Int("total_assistant_texts", len(assistantTexts)),
				)
			}

			result.FinalContent = finalContent
			_ = sm.Transition(StateComplete)
			a.hooks.OnComplete(ctx, result)
			a.emitEvent(eventCh, entity.AgentEvent{Type: entity.EventDone})
			a.logger.Info("[DIAG] EventDone emitted, returning")
			return
		}

		// OpenClaw pattern: collect intermediate assistant text during tool-calling steps.
		// This captures useful narration that some models produce alongside tool calls,
		// so we can use it as fallback if the final step returns empty content.
		if cleaned := strings.TrimSpace(StripReasoningTags(resp.Content)); cleaned != "" {
			assistantTexts = append(assistantTexts, cleaned)
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

		// Loop detection: inject reflection prompts instead of hard-terminating.
		// OpenClaw/Continue philosophy: let the LLM self-correct.
		var reflectionPrompts []string
		for _, tc := range resp.ToolCalls {
			kind := a.tools.GetToolKind(tc.Name)
			if domaintool.SafeKinds[kind] {
				continue // read-only tools don't count toward loop detection
			}

			// Name-only consecutive tracking (catches bash with different args)
			if prompt := loopDetector.RecordName(tc.Name); prompt != "" {
				reflectionPrompts = append(reflectionPrompts, prompt)
			}

			// Exact-match sliding window (catches identical repeated calls)
			argsFingerprint := ""
			if tc.Arguments != nil {
				if raw, err := json.Marshal(tc.Arguments); err == nil {
					argsFingerprint = string(raw)
				}
			}
			if prompt := loopDetector.Record(tc.Name, argsFingerprint); prompt != "" {
				reflectionPrompts = append(reflectionPrompts, prompt)
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

		// If 3 consecutive rounds of all-failed tools, inject reflection
		if consecutiveFailures >= 3 {
			messages = append(messages, LLMMessage{
				Role:    "user",
				Content: "[SYSTEM] 工具已连续失败 3 轮。请停止重试，用中文告诉用户：遇到了什么问题、尝试了什么、建议的解决方案。",
			})
			consecutiveFailures = 0
		}

		// Inject loop detection reflection prompts (if any)
		for _, prompt := range reflectionPrompts {
			messages = append(messages, LLMMessage{
				Role:    "user",
				Content: prompt,
			})
		}

		// === Post-tool context check (OpenClaw/Continue pattern) ===
		// If tool outputs pushed us over the hard ratio, force compaction now.
		postToolCheck := contextGuard.Check(messages)
		if postToolCheck.NeedCompaction {
			a.logger.Warn("Post-tool context overflow, forcing compaction",
				zap.Int("estimated_tokens", postToolCheck.EstimatedTokens),
				zap.Float64("ratio", postToolCheck.Ratio),
			)
			_ = sm.Transition(StateCompacting)
			messages = a.compactMessages(messages)
			compactionThisTurn = true
			a.logger.Info("Post-tool compaction complete",
				zap.Int("messages_after", len(messages)),
			)
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
