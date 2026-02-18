package service

import (
	"fmt"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// Guardrail sentinel errors
var (
	ErrTokenBudgetExceeded = fmt.Errorf("token budget exceeded")
	ErrTimeBudgetExceeded  = fmt.Errorf("run time budget exceeded")
	ErrContextOverflow     = fmt.Errorf("context window overflow")
)

// CostGuard prevents token/time budget overruns.
// Thread-safe — can be safely read from multiple goroutines.
type CostGuard struct {
	maxTokens     int64
	currentTokens atomic.Int64
	maxDuration   time.Duration
	startTime     time.Time
	logger        *zap.Logger
}

// NewCostGuard creates a cost guard for the current run.
func NewCostGuard(maxTokens int64, maxDuration time.Duration, logger *zap.Logger) *CostGuard {
	return &CostGuard{
		maxTokens:   maxTokens,
		maxDuration: maxDuration,
		startTime:   time.Now(),
		logger:      logger,
	}
}

// AddTokens accumulates token usage; returns error if budget exceeded.
func (g *CostGuard) AddTokens(n int64) error {
	current := g.currentTokens.Add(n)
	if g.maxTokens > 0 && current > g.maxTokens {
		g.logger.Warn("Token budget exceeded",
			zap.Int64("current", current),
			zap.Int64("max", g.maxTokens),
		)
		return ErrTokenBudgetExceeded
	}
	return nil
}

// CheckBudget returns error if time budget exceeded.
func (g *CostGuard) CheckBudget() error {
	if g.maxDuration > 0 && time.Since(g.startTime) > g.maxDuration {
		return ErrTimeBudgetExceeded
	}
	return nil
}

// GetUsage returns current token count and elapsed time.
func (g *CostGuard) GetUsage() (tokens int64, elapsed time.Duration) {
	return g.currentTokens.Load(), time.Since(g.startTime)
}

// ContextGuard monitors context window usage and triggers compaction.
type ContextGuard struct {
	maxTokens int
	warnRatio float64
	hardRatio float64
	logger    *zap.Logger
}

// NewContextGuard creates a context window guard.
func NewContextGuard(maxTokens int, warnRatio, hardRatio float64, logger *zap.Logger) *ContextGuard {
	return &ContextGuard{
		maxTokens: maxTokens,
		warnRatio: warnRatio,
		hardRatio: hardRatio,
		logger:    logger,
	}
}

// ContextCheckResult holds the result of a context window check.
type ContextCheckResult struct {
	EstimatedTokens int
	MaxTokens       int
	Ratio           float64
	NeedCompaction  bool // Hard threshold exceeded — must compact
	Warning         bool // Warn threshold exceeded — approaching limit
}

// Check estimates token usage for LLMMessages and returns compaction signals.
func (g *ContextGuard) Check(messages []LLMMessage) ContextCheckResult {
	estimated := g.estimateTokens(messages)
	ratio := float64(estimated) / float64(g.maxTokens)

	result := ContextCheckResult{
		EstimatedTokens: estimated,
		MaxTokens:       g.maxTokens,
		Ratio:           ratio,
	}

	if ratio > g.hardRatio {
		result.NeedCompaction = true
		g.logger.Warn("Context window exceeds hard threshold",
			zap.Int("tokens", estimated),
			zap.Int("max", g.maxTokens),
			zap.Float64("ratio", ratio),
		)
	} else if ratio > g.warnRatio {
		result.Warning = true
		g.logger.Info("Context window approaching limit",
			zap.Int("tokens", estimated),
			zap.Int("max", g.maxTokens),
			zap.Float64("ratio", ratio),
		)
	}

	return result
}

// estimateTokens roughly estimates token count.
// Heuristic: ~3 chars/token (blend of English ~4, CJK ~2).
func (g *ContextGuard) estimateTokens(messages []LLMMessage) int {
	total := 0
	for _, msg := range messages {
		total += len(msg.Content) / 3
		// ContentParts: count text parts
		for _, p := range msg.Parts {
			if p.Type == "text" {
				total += len(p.Text) / 3
			} else {
				total += 85 // image/media tokens (~85 for a typical image descriptor)
			}
		}
		// Tool call arguments overhead
		for _, tc := range msg.ToolCalls {
			total += len(tc.Name) + 50
		}
	}
	// Per-message formatting overhead
	total += len(messages) * 4
	return total
}

// LoopDetector detects repeated tool call patterns using two strategies:
//   1. Name-only: same tool name called consecutively (regardless of args)
//   2. Exact match: same tool name + identical args in sliding window
//
// Neither strategy terminates the loop. Instead, they return reflection prompts
// for injection into the conversation, letting the LLM self-correct.
// This aligns with OpenClaw/Continue's LLM-driven termination philosophy.
type LoopDetector struct {
	recentCalls []string // stores "name|argsHash" signatures
	windowSize  int
	threshold   int      // exact-match threshold (sliding window)

	// Name-only sliding window tracking (separate from exact-match window)
	nameThreshold int
	nameHistory   []string // tool names only, for frequency counting

	logger *zap.Logger
}

// NewLoopDetector creates a loop detector with both name-only and exact-match detection.
// nameThreshold: consecutive same-name calls before reflection (e.g. 8)
// windowSize/threshold: sliding window for exact-match detection
func NewLoopDetector(windowSize, threshold, nameThreshold int, logger *zap.Logger) *LoopDetector {
	return &LoopDetector{
		recentCalls:   make([]string, 0, windowSize),
		windowSize:    windowSize,
		threshold:     threshold,
		nameThreshold: nameThreshold,
		logger:        logger,
	}
}

// RecordName tracks tool name frequency in the sliding window (ignoring args).
// Returns a non-empty reflection prompt when the same tool appears >= nameThreshold
// times within the window — even if other tools are interleaved.
// This catches patterns like: bash×7 → web_search → bash (not strictly consecutive).
func (d *LoopDetector) RecordName(toolName string) string {
	// recentCalls is already maintained by Record(), so we count tool name
	// occurrences in the existing window. We also track via separate name window.
	d.nameHistory = append(d.nameHistory, toolName)
	if len(d.nameHistory) > d.windowSize {
		d.nameHistory = d.nameHistory[1:]
	}

	// Count how many times this tool name appears in the window
	count := 0
	for _, name := range d.nameHistory {
		if name == toolName {
			count++
		}
	}

	if count >= d.nameThreshold {
		d.logger.Warn("Same tool dominates sliding window",
			zap.String("tool", toolName),
			zap.Int("count_in_window", count),
			zap.Int("window_size", len(d.nameHistory)),
			zap.Int("threshold", d.nameThreshold),
		)
		return fmt.Sprintf(
			"[SYSTEM] ⚠️ 严重警告：工具 %s 在最近 %d 次调用中出现了 %d 次。"+
				"你很可能陷入了重试循环。你必须立即停止调用工具，"+
				"直接用中文回复用户：(1) 你在尝试做什么 (2) 遇到了什么困难 (3) 建议用户如何解决。"+
				"不要再调用任何工具。",
			toolName, len(d.nameHistory), count,
		)
	}
	return ""
}

// Record adds a tool call to the sliding window and returns a non-empty reflection
// prompt if the EXACT same call (name + args) appears >= threshold times consecutively.
func (d *LoopDetector) Record(toolName string, args ...string) string {
	sig := toolName
	if len(args) > 0 && args[0] != "" {
		sig = toolName + "|" + args[0]
	}

	d.recentCalls = append(d.recentCalls, sig)
	if len(d.recentCalls) > d.windowSize {
		d.recentCalls = d.recentCalls[1:]
	}

	if len(d.recentCalls) < d.threshold {
		return ""
	}

	tail := d.recentCalls[len(d.recentCalls)-d.threshold:]
	allSame := true
	for _, name := range tail {
		if name != tail[0] {
			allSame = false
			break
		}
	}

	if allSame {
		d.logger.Warn("Exact tool call loop detected",
			zap.String("tool", toolName),
			zap.String("signature", sig),
			zap.Int("consecutive_calls", d.threshold),
		)
		return fmt.Sprintf(
			"[SYSTEM] 工具 %s 以完全相同的参数被调用了 %d 次，结果不会改变。"+
				"请停止重复调用，改用其他方法或直接告知用户结果。",
			toolName, d.threshold,
		)
	}
	return ""
}

// Reset clears all tracking state (call at start of each Run).
func (d *LoopDetector) Reset() {
	d.recentCalls = d.recentCalls[:0]
	d.nameHistory = d.nameHistory[:0]
}
