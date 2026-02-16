package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
)

// traceIDKey is the private context key for trace IDs.
type traceIDKey struct{}

// WithTraceID injects a trace ID into the context.
// If traceID is empty, a random one is generated.
func WithTraceID(ctx context.Context, traceID string) context.Context {
	if traceID == "" {
		traceID = generateTraceID()
	}
	return context.WithValue(ctx, traceIDKey{}, traceID)
}

// TraceIDFromContext extracts the trace ID from the context.
// Returns empty string if no trace ID is set.
func TraceIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(traceIDKey{}).(string); ok {
		return id
	}
	return ""
}

// generateTraceID creates a random 16-character hex trace ID.
func generateTraceID() string {
	b := make([]byte, 8) // 8 bytes = 16 hex chars
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
