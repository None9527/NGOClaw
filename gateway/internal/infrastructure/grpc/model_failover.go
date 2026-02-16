package grpc

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ngoclaw/ngoclaw/gateway/internal/application/usecase"
	"go.uber.org/zap"
)

// Failover cooldown and retry settings
const (
	DefaultCooldownDuration = 5 * time.Minute
	MaxFailoverAttempts     = 3
)

// ModelFailover wraps AI requests with automatic model failover.
// When a request fails with a retryable error, it tries the next model
// in the fallback chain. Models that fail enter a cooldown period.
type ModelFailover struct {
	fallbackChain []string
	cooldowns     map[string]time.Time
	cooldownDur   time.Duration
	logger        *zap.Logger
	mu            sync.RWMutex
}

// NewModelFailover creates a new model failover handler
func NewModelFailover(fallbackChain []string, logger *zap.Logger) *ModelFailover {
	return &ModelFailover{
		fallbackChain: fallbackChain,
		cooldowns:     make(map[string]time.Time),
		cooldownDur:   DefaultCooldownDuration,
		logger:        logger,
	}
}

// SetCooldownDuration sets the cooldown duration for failed models
func (f *ModelFailover) SetCooldownDuration(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cooldownDur = d
}

// ExecuteWithFailover attempts a request with the primary model, falling back
// through the chain on retryable errors
func (f *ModelFailover) ExecuteWithFailover(
	ctx context.Context,
	req *usecase.AIRequest,
	client usecase.AIServiceClient,
) (*usecase.AIResponse, error) {
	// Build ordered model list: primary + fallbacks (skip cooled-down models)
	models := f.buildModelList(req.Model)
	if len(models) == 0 {
		return nil, fmt.Errorf("all models are in cooldown, try again later")
	}

	var lastErr error
	for i, model := range models {
		if i >= MaxFailoverAttempts {
			break
		}

		// Clone request with new model
		attemptReq := *req
		attemptReq.Model = model

		resp, err := client.GenerateResponse(ctx, &attemptReq)
		if err == nil {
			if i > 0 {
				f.logger.Info("Failover succeeded",
					zap.String("failed_model", req.Model),
					zap.String("success_model", model),
					zap.Int("attempt", i+1),
				)
			}
			return resp, nil
		}

		lastErr = err

		// Check if error is retryable
		if !isRetryableError(err) {
			f.logger.Warn("Non-retryable error, not failing over",
				zap.String("model", model),
				zap.Error(err),
			)
			return nil, err
		}

		// Put model in cooldown
		f.setCooldown(model)

		f.logger.Warn("Model failed, trying fallback",
			zap.String("failed_model", model),
			zap.Error(err),
			zap.Int("attempt", i+1),
		)
	}

	return nil, fmt.Errorf("all models failed after failover: %w", lastErr)
}

// buildModelList returns ordered list of models to try, skipping cooled-down ones
func (f *ModelFailover) buildModelList(primary string) []string {
	f.mu.RLock()
	defer f.mu.RUnlock()

	now := time.Now()
	var models []string

	// Add primary if not in cooldown
	if !f.isInCooldown(primary, now) {
		models = append(models, primary)
	}

	// Add fallbacks
	for _, model := range f.fallbackChain {
		if model == primary {
			continue
		}
		if !f.isInCooldown(model, now) {
			models = append(models, model)
		}
	}

	return models
}

func (f *ModelFailover) isInCooldown(model string, now time.Time) bool {
	if cooldownEnd, ok := f.cooldowns[model]; ok {
		return now.Before(cooldownEnd)
	}
	return false
}

func (f *ModelFailover) setCooldown(model string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cooldowns[model] = time.Now().Add(f.cooldownDur)
	f.logger.Info("Model entering cooldown",
		zap.String("model", model),
		zap.Duration("duration", f.cooldownDur),
	)
}

// ClearCooldown removes a model from cooldown (e.g. when user manually selects it)
func (f *ModelFailover) ClearCooldown(model string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.cooldowns, model)
}

// ClearAllCooldowns removes all cooldowns
func (f *ModelFailover) ClearAllCooldowns() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cooldowns = make(map[string]time.Time)
}

// GetCooldownStatus returns which models are currently in cooldown
func (f *ModelFailover) GetCooldownStatus() map[string]time.Duration {
	f.mu.RLock()
	defer f.mu.RUnlock()

	now := time.Now()
	status := make(map[string]time.Duration)
	for model, end := range f.cooldowns {
		if now.Before(end) {
			status[model] = end.Sub(now)
		}
	}
	return status
}

// isRetryableError determines if an error should trigger failover
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())

	retryablePatterns := []string{
		"rate limit",
		"rate_limit",
		"429",
		"too many requests",
		"quota exceeded",
		"authentication",
		"unauthorized",
		"401",
		"403",
		"forbidden",
		"timeout",
		"deadline exceeded",
		"connection refused",
		"unavailable",
		"503",
		"502",
		"bad gateway",
		"internal server error",
		"500",
		"overloaded",
		"capacity",
	}

	for _, pattern := range retryablePatterns {
		if strings.Contains(errStr, pattern) {
			return true
		}
	}

	return false
}
