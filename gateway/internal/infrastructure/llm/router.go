package llm

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/service"
	"go.uber.org/zap"
)

// Router implements service.LLMClient by routing to the best available provider.
// Strategy: Sideload module first (primary) → Go builtin fallback.
// Features: per-provider latency tracking, circuit breaker, failover.
type Router struct {
	providers []Provider
	stats     map[string]*providerStats   // provider name → stats
	breakers  map[string]*CircuitBreaker // provider name → circuit breaker
	mu        sync.RWMutex
	logger    *zap.Logger
}

// providerStats tracks per-provider performance metrics.
type providerStats struct {
	TotalCalls   int64
	FailureCount int64
	LastLatency  time.Duration
}

// NewRouter creates a new LLM router
func NewRouter(logger *zap.Logger) *Router {
	return &Router{
		stats:    make(map[string]*providerStats),
		breakers: make(map[string]*CircuitBreaker),
		logger:   logger.With(zap.String("component", "llm-router")),
	}
}

// Compile-time interface check: Router implements service.LLMClient
var _ service.LLMClient = (*Router)(nil)

// AddProvider adds a provider to the router.
// Providers are tried in insertion order (add sideload first, then fallback).
func (r *Router) AddProvider(p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers = append(r.providers, p)
	r.stats[p.Name()] = &providerStats{}
	r.breakers[p.Name()] = NewCircuitBreaker(5, 30*time.Second)
	r.logger.Info("LLM provider added",
		zap.String("name", p.Name()),
		zap.Strings("models", p.Models()),
	)
}

// Generate implements service.LLMClient.
// It routes to the first available provider that supports the requested model.
func (r *Router) Generate(ctx context.Context, req *service.LLMRequest) (*service.LLMResponse, error) {
	r.mu.RLock()
	providers := make([]Provider, len(r.providers))
	copy(providers, r.providers)
	r.mu.RUnlock()

	var lastErr error

	for _, p := range providers {
		if !p.SupportsModel(req.Model) {
			continue
		}

		if !p.IsAvailable(ctx) {
			r.logger.Debug("Provider unavailable, skipping",
				zap.String("provider", p.Name()),
			)
			continue
		}

		// Circuit breaker check
		if cb, ok := r.breakers[p.Name()]; ok && !cb.Allow() {
			r.logger.Debug("Provider circuit open, skipping",
				zap.String("provider", p.Name()),
			)
			continue
		}

		r.logger.Debug("Routing to provider",
			zap.String("provider", p.Name()),
			zap.String("model", req.Model),
		)

		start := time.Now()
		resp, err := p.Generate(ctx, req)
		latency := time.Since(start)

		r.mu.Lock()
		if s, ok := r.stats[p.Name()]; ok {
			s.TotalCalls++
			s.LastLatency = latency
			if err != nil {
				s.FailureCount++
			}
		}
		r.mu.Unlock()

		if err != nil {
			if cb, ok := r.breakers[p.Name()]; ok {
				cb.RecordFailure()
			}
			lastErr = err
			r.logger.Warn("Provider failed, trying next",
				zap.String("provider", p.Name()),
				zap.Duration("latency", latency),
				zap.Error(err),
			)
			continue
		}

		if cb, ok := r.breakers[p.Name()]; ok {
			cb.RecordSuccess()
		}

		r.logger.Debug("Provider succeeded",
			zap.String("provider", p.Name()),
			zap.Duration("latency", latency),
			zap.Int("tokens", resp.TokensUsed),
		)

		return resp, nil
	}

	if lastErr != nil {
		return nil, fmt.Errorf("all providers failed, last error: %w", lastErr)
	}

	return nil, fmt.Errorf("no provider available for model '%s'", req.Model)
}

// GenerateStream implements service.LLMClient.
// Routes to the first available streaming-capable provider.
func (r *Router) GenerateStream(ctx context.Context, req *service.LLMRequest, deltaCh chan<- service.StreamChunk) (*service.LLMResponse, error) {
	r.mu.RLock()
	providers := make([]Provider, len(r.providers))
	copy(providers, r.providers)
	r.mu.RUnlock()

	var lastErr error

	for _, p := range providers {
		if !p.SupportsModel(req.Model) {
			continue
		}

		if !p.IsAvailable(ctx) {
			continue
		}

		// Circuit breaker check
		if cb, ok := r.breakers[p.Name()]; ok && !cb.Allow() {
			r.logger.Debug("Provider circuit open, skipping stream",
				zap.String("provider", p.Name()),
			)
			continue
		}

		r.logger.Debug("Streaming via provider",
			zap.String("provider", p.Name()),
			zap.String("model", req.Model),
		)

		start := time.Now()
		resp, err := p.GenerateStream(ctx, req, deltaCh)
		latency := time.Since(start)

		r.mu.Lock()
		if s, ok := r.stats[p.Name()]; ok {
			s.TotalCalls++
			s.LastLatency = latency
			if err != nil {
				s.FailureCount++
			}
		}
		r.mu.Unlock()

		if err != nil {
			if cb, ok := r.breakers[p.Name()]; ok {
				cb.RecordFailure()
			}
			lastErr = err
			r.logger.Warn("Streaming provider failed, trying next",
				zap.String("provider", p.Name()),
				zap.Duration("latency", latency),
				zap.Error(err),
			)
			continue
		}

		if cb, ok := r.breakers[p.Name()]; ok {
			cb.RecordSuccess()
		}

		return resp, nil
	}

	if lastErr != nil {
		return nil, fmt.Errorf("all streaming providers failed, last error: %w", lastErr)
	}

	return nil, fmt.Errorf("no streaming provider available for model '%s'", req.Model)
}

// ListProviders returns names, status, and performance stats of all registered providers
func (r *Router) ListProviders(ctx context.Context) []ProviderStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []ProviderStatus
	for _, p := range r.providers {
		ps := ProviderStatus{
			Name:      p.Name(),
			Models:    p.Models(),
			Available: p.IsAvailable(ctx),
		}
		if s, ok := r.stats[p.Name()]; ok {
			ps.TotalCalls = s.TotalCalls
			ps.FailureCount = s.FailureCount
			ps.LastLatencyMs = float64(s.LastLatency) / float64(time.Millisecond)
		}
		if cb, ok := r.breakers[p.Name()]; ok {
			ps.CircuitState = cb.State().String()
		}
		result = append(result, ps)
	}
	return result
}

// ProviderStatus describes a provider's current state and performance
type ProviderStatus struct {
	Name          string   `json:"name"`
	Models        []string `json:"models"`
	Available     bool     `json:"available"`
	TotalCalls    int64    `json:"total_calls"`
	FailureCount  int64    `json:"failure_count"`
	LastLatencyMs float64  `json:"last_latency_ms"`
	CircuitState  string   `json:"circuit_state"`
}
