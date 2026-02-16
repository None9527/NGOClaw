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

// LoopDetector uses a sliding window to detect repeated tool call patterns.
type LoopDetector struct {
	recentCalls []string // stores "name|argsHash" signatures
	windowSize  int
	threshold   int
	logger      *zap.Logger
}

// NewLoopDetector creates a sliding window loop detector.
func NewLoopDetector(windowSize, threshold int, logger *zap.Logger) *LoopDetector {
	return &LoopDetector{
		recentCalls: make([]string, 0, windowSize),
		windowSize:  windowSize,
		threshold:   threshold,
		logger:      logger,
	}
}

// Record adds a tool call and returns true if a loop is detected.
// A loop means the same tool with the SAME arguments appears >= threshold
// times consecutively in the recent window. Different arguments = different call.
func (d *LoopDetector) Record(toolName string, args ...string) bool {
	// Build signature: "toolName" or "toolName|argsHash" when args are provided
	sig := toolName
	if len(args) > 0 && args[0] != "" {
		sig = toolName + "|" + args[0]
	}

	d.recentCalls = append(d.recentCalls, sig)
	if len(d.recentCalls) > d.windowSize {
		d.recentCalls = d.recentCalls[1:]
	}

	if len(d.recentCalls) < d.threshold {
		return false
	}

	// Check if the last `threshold` calls are all the same signature
	tail := d.recentCalls[len(d.recentCalls)-d.threshold:]
	allSame := true
	for _, name := range tail {
		if name != tail[0] {
			allSame = false
			break
		}
	}

	if allSame {
		d.logger.Warn("Tool call loop detected",
			zap.String("tool", toolName),
			zap.String("signature", sig),
			zap.Int("consecutive_calls", d.threshold),
		)
		return true
	}
	return false
}

// Reset clears the sliding window (call at start of each Run).
func (d *LoopDetector) Reset() {
	d.recentCalls = d.recentCalls[:0]
}
