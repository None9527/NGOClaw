package service

import (
	"errors"
	"fmt"
	"strings"
)

// LLMErrorKind classifies LLM errors for retry and reporting decisions.
type LLMErrorKind int

const (
	// ErrKindTransient means the error is temporary and retrying may succeed.
	// Examples: timeout, network reset, 502/503/504, rate limit.
	ErrKindTransient LLMErrorKind = iota

	// ErrKindAuth means authentication or authorization failed.
	// Examples: invalid API key, 401/403.
	ErrKindAuth

	// ErrKindBadRequest means the request itself is malformed.
	// Examples: invalid argument, model not found, 400.
	ErrKindBadRequest

	// ErrKindContentFilter means the request was blocked by content policy.
	// Examples: safety filter triggered, content policy violation.
	ErrKindContentFilter

	// ErrKindBudget means the request exceeded a cost or resource limit.
	// Examples: token budget exhausted, run timeout.
	ErrKindBudget

	// ErrKindCancelled means the request was explicitly cancelled.
	// Examples: context.Canceled, context.DeadlineExceeded.
	ErrKindCancelled
)

// String returns a human-readable label for the error kind.
func (k LLMErrorKind) String() string {
	switch k {
	case ErrKindTransient:
		return "transient"
	case ErrKindAuth:
		return "auth"
	case ErrKindBadRequest:
		return "bad_request"
	case ErrKindContentFilter:
		return "content_filter"
	case ErrKindBudget:
		return "budget"
	case ErrKindCancelled:
		return "cancelled"
	default:
		return "unknown"
	}
}

// IsRetryable returns true if this error kind should be retried.
func (k LLMErrorKind) IsRetryable() bool {
	return k == ErrKindTransient
}

// LLMError is a structured error from an LLM operation.
// It wraps the original error with classification metadata
// for smarter retry, logging, and metrics.
type LLMError struct {
	Kind       LLMErrorKind // Classification of the error
	Message    string       // Human-readable description
	StatusCode int          // HTTP status code if applicable (0 if unknown)
	Provider   string       // Provider name that generated the error
	Model      string       // Model that was being used
	Cause      error        // Original underlying error
}

// Error implements the error interface.
func (e *LLMError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Kind, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s] %s", e.Kind, e.Message)
}

// Unwrap enables errors.Is/errors.As on the cause chain.
func (e *LLMError) Unwrap() error {
	return e.Cause
}

// IsRetryable returns true if this error should be retried.
func (e *LLMError) IsRetryable() bool {
	return e.Kind.IsRetryable()
}

// ClassifyError examines an error and returns a classified LLMError.
// If the error is already an *LLMError, it is returned as-is.
// Otherwise, the error string is pattern-matched against known categories.
func ClassifyError(err error, provider, model string) *LLMError {
	if err == nil {
		return nil
	}

	// Check if already classified
	var llmErr *LLMError
	if errors.As(err, &llmErr) {
		return llmErr
	}

	errStr := strings.ToLower(err.Error())

	// Cancellation
	if errors.Is(err, errors.New("context canceled")) ||
		strings.Contains(errStr, "context canceled") ||
		strings.Contains(errStr, "context deadline exceeded") {
		return &LLMError{
			Kind:     ErrKindCancelled,
			Message:  "request cancelled",
			Provider: provider,
			Model:    model,
			Cause:    err,
		}
	}

	// Auth errors
	authPatterns := []string{"unauthorized", "invalid api key", "403", "authentication", "permission denied"}
	for _, p := range authPatterns {
		if strings.Contains(errStr, p) {
			return &LLMError{
				Kind:       ErrKindAuth,
				Message:    "authentication failed",
				StatusCode: extractStatusCode(errStr),
				Provider:   provider,
				Model:      model,
				Cause:      err,
			}
		}
	}

	// Content filter
	filterPatterns := []string{"content filter", "content policy", "safety", "blocked", "harmful"}
	for _, p := range filterPatterns {
		if strings.Contains(errStr, p) {
			return &LLMError{
				Kind:     ErrKindContentFilter,
				Message:  "content filtered",
				Provider: provider,
				Model:    model,
				Cause:    err,
			}
		}
	}

	// Bad request
	badReqPatterns := []string{"bad request", "invalid argument", "model not found", "400", "invalid_request"}
	for _, p := range badReqPatterns {
		if strings.Contains(errStr, p) {
			return &LLMError{
				Kind:       ErrKindBadRequest,
				Message:    "invalid request",
				StatusCode: extractStatusCode(errStr),
				Provider:   provider,
				Model:      model,
				Cause:      err,
			}
		}
	}

	// Budget
	budgetPatterns := []string{"budget", "quota", "insufficient", "billing"}
	for _, p := range budgetPatterns {
		if strings.Contains(errStr, p) {
			return &LLMError{
				Kind:     ErrKindBudget,
				Message:  "budget or quota exceeded",
				Provider: provider,
				Model:    model,
				Cause:    err,
			}
		}
	}

	// Default: transient (retryable)
	return &LLMError{
		Kind:       ErrKindTransient,
		Message:    "transient error",
		StatusCode: extractStatusCode(errStr),
		Provider:   provider,
		Model:      model,
		Cause:      err,
	}
}

// extractStatusCode tries to find HTTP status codes in an error string.
func extractStatusCode(errStr string) int {
	codes := map[string]int{
		"400": 400, "401": 401, "403": 403, "404": 404,
		"429": 429, "500": 500, "502": 502, "503": 503,
		"504": 504, "529": 529,
	}
	for code, num := range codes {
		if strings.Contains(errStr, code) {
			return num
		}
	}
	return 0
}
