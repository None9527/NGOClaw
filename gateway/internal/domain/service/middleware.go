// Copyright 2026 NGOClaw Authors. All rights reserved.
package service

import (
	"context"

	"go.uber.org/zap"
)

// Middleware defines a data-transformation hook around LLM calls.
// Unlike AgentHook (which is observational / side-effect only),
// Middleware can MODIFY messages before a call and responses after.
//
// Design: Deer-Flow 2.0 middleware chain pattern.
//
//	Hook  = side-channel (metrics, logging, security veto)
//	MW    = main-line    (inject context, trim response, summarize)
type Middleware interface {
	// Name returns a human-readable identifier for logging/debugging.
	Name() string

	// BeforeModel is called before each LLM request.
	// It receives the current messages slice and MUST return a (possibly modified) copy.
	// Implementations SHOULD NOT mutate the input slice in place.
	BeforeModel(ctx context.Context, messages []LLMMessage, step int) []LLMMessage

	// AfterModel is called after each successful LLM response.
	// It receives the response and MUST return a (possibly modified) copy.
	AfterModel(ctx context.Context, resp *LLMResponse, step int) *LLMResponse
}

// MiddlewarePipeline chains multiple Middleware in order.
// BeforeModel runs in registration order (first added → first executed).
// AfterModel runs in reverse order (last added → first executed) — like HTTP
// middleware unwinding.
type MiddlewarePipeline struct {
	middlewares []Middleware
	logger      *zap.Logger
}

// NewMiddlewarePipeline creates an empty pipeline.
func NewMiddlewarePipeline(logger *zap.Logger) *MiddlewarePipeline {
	return &MiddlewarePipeline{
		middlewares: make([]Middleware, 0, 4),
		logger:      logger,
	}
}

// Use appends one or more middlewares to the pipeline.
func (p *MiddlewarePipeline) Use(mws ...Middleware) {
	p.middlewares = append(p.middlewares, mws...)
}

// Len returns the number of registered middlewares.
func (p *MiddlewarePipeline) Len() int {
	return len(p.middlewares)
}

// RunBeforeModel executes all BeforeModel hooks in order.
func (p *MiddlewarePipeline) RunBeforeModel(ctx context.Context, messages []LLMMessage, step int) []LLMMessage {
	for _, mw := range p.middlewares {
		messages = mw.BeforeModel(ctx, messages, step)
	}
	return messages
}

// RunAfterModel executes all AfterModel hooks in REVERSE order.
func (p *MiddlewarePipeline) RunAfterModel(ctx context.Context, resp *LLMResponse, step int) *LLMResponse {
	for i := len(p.middlewares) - 1; i >= 0; i-- {
		resp = p.middlewares[i].AfterModel(ctx, resp, step)
	}
	return resp
}

// --- NoOpMiddleware for embedding ---

// NoOpMiddleware provides pass-through defaults. Embed in custom middleware
// to only override the methods you need.
type NoOpMiddleware struct{}

func (NoOpMiddleware) BeforeModel(_ context.Context, msgs []LLMMessage, _ int) []LLMMessage {
	return msgs
}

func (NoOpMiddleware) AfterModel(_ context.Context, resp *LLMResponse, _ int) *LLMResponse {
	return resp
}
