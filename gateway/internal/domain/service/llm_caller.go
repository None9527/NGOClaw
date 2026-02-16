package service

import (
	"fmt"
	"strings"
	"time"

	"context"

	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/entity"
	"go.uber.org/zap"
)

// callLLMWithRetry calls the LLM with automatic retry and exponential backoff.
// On transient errors (timeout, network), retries up to MaxRetries times.
// Emits retry events so the user knows what's happening.
func (a *AgentLoop) callLLMWithRetry(ctx context.Context, req *LLMRequest, step int, eventCh chan<- entity.AgentEvent) (*LLMResponse, error) {
	var lastErr error

	for attempt := 0; attempt <= a.config.MaxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 2s, 4s, 8s...
			wait := a.config.RetryBaseWait * (1 << (attempt - 1))

			a.logger.Info("Retrying LLM call",
				zap.Int("attempt", attempt),
				zap.Int("max_retries", a.config.MaxRetries),
				zap.Duration("wait", wait),
				zap.Error(lastErr),
			)

			a.emitEvent(eventCh, entity.AgentEvent{
				Type:    entity.EventThinking,
				Content: fmt.Sprintf("⚡ LLM call failed, retrying (%d/%d) in %s...", attempt, a.config.MaxRetries, wait),
			})

			// Wait with cancellation support
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		// Try streaming first — forward text deltas in real time
		deltaCh := make(chan StreamChunk, 128)

		// Forward deltas to event channel in a goroutine
		done := make(chan struct{})
		go func() {
			defer close(done)
			for chunk := range deltaCh {
				if chunk.DeltaText != "" {
					a.emitEvent(eventCh, entity.AgentEvent{
						Type:    entity.EventTextDelta,
						Content: chunk.DeltaText,
					})
				}
				// Tool call deltas are accumulated by GenerateStream
				// and returned in the final LLMResponse — no need to emit here
			}
		}()

		// Per-call timeout: prevent individual LLM calls from hanging forever.
		// SSE streams can stall after headers arrive (ResponseHeaderTimeout won't help).
		// 3 minutes is generous for any single LLM inference — retries handle transients.
		callCtx, callCancel := context.WithTimeout(ctx, 3*time.Minute)

		a.logger.Info("[DIAG] LLM GenerateStream starting",
			zap.Int("step", step),
			zap.Int("attempt", attempt),
			zap.String("model", req.Model),
		)

		resp, err := a.llm.GenerateStream(callCtx, req, deltaCh)

		a.logger.Info("[DIAG] LLM GenerateStream returned",
			zap.Int("step", step),
			zap.Bool("has_error", err != nil),
			zap.Error(err),
		)

		callCancel()
		close(deltaCh)
		<-done // Wait for delta forwarding to finish

		a.logger.Info("[DIAG] Delta forwarding complete",
			zap.Int("step", step),
		)

		if err == nil {
			if attempt > 0 {
				a.logger.Info("LLM retry succeeded",
					zap.Int("attempt", attempt),
					zap.Int("step", step),
				)
			}
			return resp, nil
		}

		lastErr = err
		a.logger.Warn("LLM streaming call failed",
			zap.Int("attempt", attempt),
			zap.Int("step", step),
			zap.Error(err),
		)

		// Check if error is retryable
		if !isRetryableError(err) {
			return nil, fmt.Errorf("non-retryable LLM error: %w", err)
		}
	}

	return nil, fmt.Errorf("LLM call failed after %d retries: %w", a.config.MaxRetries, lastErr)
}

// isRetryableError determines if an LLM error is worth retrying.
// Retryable: timeout, connection reset, 5xx server errors.
// Non-retryable: 401 auth, 400 bad request, context cancelled.
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	errStr := strings.ToLower(err.Error())

	// Non-retryable patterns
	nonRetryable := []string{
		"context canceled",
		"unauthorized",
		"invalid api key",
		"bad request",
		"invalid argument",
		"model not found",
	}
	for _, pattern := range nonRetryable {
		if strings.Contains(errStr, pattern) {
			return false
		}
	}

	// Retryable patterns
	retryable := []string{
		"timeout",
		"deadline exceeded",
		"connection reset",
		"connection refused",
		"eof",
		"server error",
		"502", "503", "504", "529",
		"rate limit",
		"too many requests",
		"overloaded",
		"temporarily unavailable",
	}
	for _, pattern := range retryable {
		if strings.Contains(errStr, pattern) {
			return true
		}
	}

	// Default: retry on unknown errors (conservative, but prevents single-point failures)
	return true
}
