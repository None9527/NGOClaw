package gemini

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

// ParseSSEStream reads Gemini's streaming response format.
// Gemini uses SSE-like "data: {...}" lines similar to OpenAI,
// where each chunk is a full GenerateContentResponse.
func ParseSSEStream(ctx context.Context, reader io.Reader, deltaCh chan<- service.StreamChunk, logger *zap.Logger) (*service.LLMResponse, error) {
	idleTimeout := 60 * time.Second
	tReader := &timedReader{r: reader, timeout: idleTimeout}

	scanner := bufio.NewScanner(tReader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var contentBuilder strings.Builder
	var modelUsed string
	var tokensUsed int
	var finishReason string
	var toolCalls []entity.ToolCallInfo

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

		var resp Response
		if err := json.Unmarshal([]byte(data), &resp); err != nil {
			logger.Debug("Skip unparseable Gemini SSE chunk", zap.Error(err))
			continue
		}

		if resp.ModelVersion != "" {
			modelUsed = resp.ModelVersion
		}
		if resp.UsageMetadata != nil && resp.UsageMetadata.Total() > 0 {
			tokensUsed = resp.UsageMetadata.Total()
		}

		if len(resp.Candidates) == 0 {
			continue
		}

		candidate := resp.Candidates[0]
		if candidate.FinishReason != "" {
			finishReason = candidate.FinishReason
		}

		for _, part := range candidate.Content.Parts {
			if part.Text != "" {
				contentBuilder.WriteString(part.Text)
				deltaCh <- service.StreamChunk{DeltaText: part.Text}
			}

			if part.FunctionCall != nil {
				tc := entity.ToolCallInfo{
					ID:        fmt.Sprintf("call_%s_%d", part.FunctionCall.Name, len(toolCalls)),
					Name:      part.FunctionCall.Name,
					Arguments: part.FunctionCall.Args,
				}
				toolCalls = append(toolCalls, tc)
				deltaCh <- service.StreamChunk{DeltaToolCall: &tc}
			}
		}

		if finishReason != "" {
			deltaCh <- service.StreamChunk{FinishReason: finishReason}
			break
		}
	}

	if err := scanner.Err(); err != nil {
		if isIdleTimeoutErr(err) {
			logger.Warn("SSE stream idle timeout — Gemini API stalled",
				zap.Duration("idle_timeout", idleTimeout))
			if contentBuilder.Len() == 0 && len(toolCalls) == 0 {
				return nil, fmt.Errorf("SSE stream stalled: no data for %v", idleTimeout)
			}
		} else {
			return nil, fmt.Errorf("SSE scan error: %w", err)
		}
	}

	contentStr := contentBuilder.String()
	if tokensUsed == 0 && len(contentStr) > 0 {
		tokensUsed = len([]rune(contentStr))*3/2 + 50
	}

	resp := &service.LLMResponse{
		Content:    contentStr,
		ModelUsed:  modelUsed,
		TokensUsed: tokensUsed,
		ToolCalls:  toolCalls,
	}

	return resp, nil
}

// --- SSE idle timeout support ---

var errIdleTimeout = fmt.Errorf("SSE read idle timeout")

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

func isIdleTimeoutErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "SSE read idle timeout")
}
