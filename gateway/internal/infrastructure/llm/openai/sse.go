package openai

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/entity"
	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/service"
	"go.uber.org/zap"
)

// ToolCallAccumulator accumulates tool call fragments across SSE chunks.
type ToolCallAccumulator struct {
	ID          string
	Name        string
	ArgsBuilder strings.Builder
}

// ParseSSEStream reads a text/event-stream response, emitting deltas and accumulating the final response.
//
// Three-tier termination protection (industry best practice):
//
//	L1: Break on finish_reason (don't wait for [DONE] — some APIs never send it)
//	L2: 60s read idle timeout (detect stale connections)
//	L3: Per-call context timeout (set by callLLMWithRetry)
func ParseSSEStream(ctx context.Context, reader io.Reader, deltaCh chan<- service.StreamChunk, logger *zap.Logger) (*service.LLMResponse, error) {
	// L2: Wrap reader with idle timeout
	idleTimeout := 60 * time.Second
	tReader := &timedReader{r: reader, timeout: idleTimeout}

	scanner := bufio.NewScanner(tReader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // 1MB max line

	// Accumulators
	var contentBuilder strings.Builder
	toolCallMap := make(map[int]*ToolCallAccumulator)
	var modelUsed string
	var tokensUsed int
	var finishReason string

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk StreamChunkData
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			logger.Debug("Skip unparseable SSE chunk", zap.Error(err))
			continue
		}

		if chunk.Model != "" {
			modelUsed = chunk.Model
		}
		if chunk.Usage != nil {
			if t := chunk.Usage.Total(); t > 0 {
				tokensUsed = t
			}
		}

		if len(chunk.Choices) == 0 {
			continue
		}

		choice := chunk.Choices[0]
		delta := choice.Delta

		if choice.FinishReason != nil {
			finishReason = *choice.FinishReason
		}

		// Text delta
		if delta.Content != "" {
			contentBuilder.WriteString(delta.Content)
			deltaCh <- service.StreamChunk{
				DeltaText: delta.Content,
			}
		}

		// Tool call deltas
		for _, tc := range delta.ToolCalls {
			idx := tc.Index

			if _, ok := toolCallMap[idx]; !ok {
				toolCallMap[idx] = &ToolCallAccumulator{
					ID:   tc.ID,
					Name: tc.Function.Name,
				}
			}

			acc := toolCallMap[idx]
			if tc.ID != "" {
				acc.ID = tc.ID
			}
			if tc.Function.Name != "" {
				acc.Name = tc.Function.Name
			}
			acc.ArgsBuilder.WriteString(tc.Function.Arguments)
		}

		// L1: finish_reason received — break immediately
		if finishReason != "" {
			deltaCh <- service.StreamChunk{
				FinishReason: finishReason,
			}
			logger.Debug("SSE stream: finish_reason received, breaking",
				zap.String("finish_reason", finishReason))
			break
		}
	}

	// L2: Distinguish idle timeout from real scan errors
	if err := scanner.Err(); err != nil {
		if IsIdleTimeoutErr(err) {
			logger.Warn("SSE stream idle timeout — API stalled",
				zap.Duration("idle_timeout", idleTimeout),
				zap.String("content_so_far", TruncateForLog(contentBuilder.String(), 100)),
			)
			if contentBuilder.Len() == 0 && len(toolCallMap) == 0 {
				return nil, fmt.Errorf("SSE stream stalled: no data for %v", idleTimeout)
			}
			logger.Info("Returning partial SSE response after idle timeout")
		} else {
			return nil, fmt.Errorf("SSE scan error: %w", err)
		}
	}

	// Fallback: estimate tokens if API didn't return usage
	contentStr := contentBuilder.String()
	if tokensUsed == 0 && len(contentStr) > 0 {
		tokensUsed = len([]rune(contentStr))*3/2 + 50
	}

	resp := &service.LLMResponse{
		Content:    contentStr,
		ModelUsed:  modelUsed,
		TokensUsed: tokensUsed,
	}

	// Assemble accumulated tool calls
	for i := 0; i < len(toolCallMap); i++ {
		acc := toolCallMap[i]
		var args map[string]interface{}
		if argsStr := acc.ArgsBuilder.String(); argsStr != "" {
			if err := json.Unmarshal([]byte(argsStr), &args); err != nil {
				logger.Warn("Failed to parse streamed tool call args",
					zap.String("tool", acc.Name),
					zap.Error(err),
				)
				continue
			}
		}
		tc := entity.ToolCallInfo{
			ID:        acc.ID,
			Name:      acc.Name,
			Arguments: args,
		}
		resp.ToolCalls = append(resp.ToolCalls, tc)

		deltaCh <- service.StreamChunk{
			DeltaToolCall: &tc,
		}
	}

	return resp, nil
}

// --- SSE idle timeout support ---

var errIdleTimeout = fmt.Errorf("SSE read idle timeout")

// timedReader wraps an io.Reader and applies a per-Read deadline.
type timedReader struct {
	r       io.Reader
	timeout time.Duration
}

func (t *timedReader) Read(p []byte) (int, error) {
	type result struct {
		n   int
		err error
	}
	ch := make(chan result, 1)
	go func() {
		n, err := t.r.Read(p)
		ch <- result{n, err}
	}()

	select {
	case res := <-ch:
		return res.n, res.err
	case <-time.After(t.timeout):
		return 0, errIdleTimeout
	}
}

// IsIdleTimeoutErr checks if an error is our SSE idle timeout sentinel.
func IsIdleTimeoutErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "SSE read idle timeout")
}

// TruncateForLog truncates a string for safe logging.
func TruncateForLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
