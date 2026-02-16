package monitoring

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Span represents a single traced operation, compatible with OpenTelemetry concepts.
// This is a zero-dependency abstraction that can be bridged to real OTel SDKs later.
type Span struct {
	TraceID    string            `json:"trace_id"`
	SpanID     string            `json:"span_id"`
	ParentID   string            `json:"parent_id,omitempty"`
	Name       string            `json:"name"`
	Service    string            `json:"service"`
	Kind       SpanKind          `json:"kind"`
	Status     SpanStatus        `json:"status"`
	StartTime  time.Time         `json:"start_time"`
	EndTime    time.Time         `json:"end_time,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
	Events     []SpanEvent       `json:"events,omitempty"`
	mu         sync.Mutex
}

// SpanKind mirrors OpenTelemetry SpanKind.
type SpanKind int

const (
	SpanKindInternal SpanKind = iota
	SpanKindServer
	SpanKindClient
)

// SpanStatus mirrors OpenTelemetry status codes.
type SpanStatus int

const (
	SpanStatusUnset SpanStatus = iota
	SpanStatusOK
	SpanStatusError
)

// SpanEvent is a timestamped annotation within a span.
type SpanEvent struct {
	Name       string            `json:"name"`
	Timestamp  time.Time         `json:"timestamp"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// Tracer creates and manages spans for distributed tracing.
type Tracer struct {
	service string
	logger  *zap.Logger

	mu      sync.RWMutex
	spans   []*Span // completed spans (ring buffer)
	maxSize int
}

// NewTracer creates a tracer for the given service name.
func NewTracer(service string, logger *zap.Logger) *Tracer {
	return &Tracer{
		service: service,
		logger:  logger.With(zap.String("component", "tracer")),
		spans:   make([]*Span, 0, 1024),
		maxSize: 10000,
	}
}

// traceCtxKey is the context key for the current span.
type traceCtxKey struct{}

// StartSpan creates a new span as a child of any span in the context.
func (t *Tracer) StartSpan(ctx context.Context, name string) (context.Context, *Span) {
	span := &Span{
		SpanID:     generateID(),
		Name:       name,
		Service:    t.service,
		Kind:       SpanKindInternal,
		Status:     SpanStatusUnset,
		StartTime:  time.Now(),
		Attributes: make(map[string]string),
	}

	// Inherit parent span if present
	if parent, ok := ctx.Value(traceCtxKey{}).(*Span); ok {
		span.ParentID = parent.SpanID
		span.TraceID = parent.TraceID
	} else {
		span.TraceID = generateID()
	}

	return context.WithValue(ctx, traceCtxKey{}, span), span
}

// EndSpan completes a span, recording its duration and status.
func (t *Tracer) EndSpan(span *Span, err error) {
	span.mu.Lock()
	span.EndTime = time.Now()
	if err != nil {
		span.Status = SpanStatusError
		span.Attributes["error"] = err.Error()
	} else {
		span.Status = SpanStatusOK
	}
	span.mu.Unlock()

	t.mu.Lock()
	if len(t.spans) >= t.maxSize {
		// Evict oldest 10%
		cut := t.maxSize / 10
		t.spans = t.spans[cut:]
	}
	t.spans = append(t.spans, span)
	t.mu.Unlock()

	t.logger.Debug("Span completed",
		zap.String("name", span.Name),
		zap.String("trace_id", span.TraceID),
		zap.Duration("duration", span.EndTime.Sub(span.StartTime)),
	)
}

// SetAttribute adds a key-value attribute to a span.
func SetAttribute(span *Span, key, value string) {
	if span == nil {
		return
	}
	span.mu.Lock()
	span.Attributes[key] = value
	span.mu.Unlock()
}

// AddEvent adds a timestamped event to a span.
func AddEvent(span *Span, name string, attrs map[string]string) {
	if span == nil {
		return
	}
	span.mu.Lock()
	span.Events = append(span.Events, SpanEvent{
		Name:       name,
		Timestamp:  time.Now(),
		Attributes: attrs,
	})
	span.mu.Unlock()
}

// RecentSpans returns the most recent N spans for inspection.
func (t *Tracer) RecentSpans(n int) []*Span {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if n > len(t.spans) {
		n = len(t.spans)
	}
	result := make([]*Span, n)
	copy(result, t.spans[len(t.spans)-n:])
	return result
}

// SpansByTraceID returns all spans for a given trace.
func (t *Tracer) SpansByTraceID(traceID string) []*Span {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var result []*Span
	for _, s := range t.spans {
		if s.TraceID == traceID {
			result = append(result, s)
		}
	}
	return result
}

// generateID produces an 8-byte hex string.
func generateID() string {
	return fmt.Sprintf("%016x", time.Now().UnixNano())
}
