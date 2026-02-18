package anthropic

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

// toolCallAccumulator tracks a tool_use block being streamed.
type toolCallAccumulator struct {
	ID          string
	Name        string
	ArgsBuilder strings.Builder
}

// ParseSSEStream reads Anthropic's event-based SSE format.
//
// Anthropic SSE events:
//   - message_start         → initial message metadata
//   - content_block_start   → new content block (text, tool_use, thinking)
//   - content_block_delta   → incremental update to current block
//   - content_block_stop    → current block finished
//   - message_delta         → stop_reason + final usage
//   - message_stop          → stream complete
func ParseSSEStream(ctx context.Context, reader io.Reader, deltaCh chan<- service.StreamChunk, logger *zap.Logger) (*service.LLMResponse, error) {
	idleTimeout := 60 * time.Second
	tReader := &timedReader{r: reader, timeout: idleTimeout}

	scanner := bufio.NewScanner(tReader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var contentBuilder strings.Builder
	var modelUsed string
	var tokensUsed int
	var finishReason string
	toolCalls := make(map[int]*toolCallAccumulator) // index → accumulator
	var currentEventType string

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		line := scanner.Text()

		// Anthropic SSE: "event: <type>" followed by "data: <json>"
		if strings.HasPrefix(line, "event: ") {
			currentEventType = strings.TrimPrefix(line, "event: ")
			continue
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		switch currentEventType {
		case "message_start":
			var evt StreamEvent
			if err := json.Unmarshal([]byte(data), &evt); err != nil {
				logger.Debug("Skip unparseable message_start", zap.Error(err))
				continue
			}
			if evt.Message != nil {
				modelUsed = evt.Message.Model
				if evt.Message.Usage.Total() > 0 {
					tokensUsed = evt.Message.Usage.Total()
				}
			}

		case "content_block_start":
			var evt StreamEvent
			if err := json.Unmarshal([]byte(data), &evt); err != nil {
				logger.Debug("Skip unparseable content_block_start", zap.Error(err))
				continue
			}
			if evt.ContentBlock != nil && evt.ContentBlock.Type == "tool_use" {
				toolCalls[evt.Index] = &toolCallAccumulator{
					ID:   evt.ContentBlock.ID,
					Name: evt.ContentBlock.Name,
				}
			}

		case "content_block_delta":
			var evt StreamEvent
			if err := json.Unmarshal([]byte(data), &evt); err != nil {
				logger.Debug("Skip unparseable content_block_delta", zap.Error(err))
				continue
			}
			if evt.Delta == nil {
				continue
			}

			switch evt.Delta.Type {
			case "text_delta":
				if evt.Delta.Text != "" {
					contentBuilder.WriteString(evt.Delta.Text)
					deltaCh <- service.StreamChunk{DeltaText: evt.Delta.Text}
				}
			case "input_json_delta":
				if acc, ok := toolCalls[evt.Index]; ok {
					acc.ArgsBuilder.WriteString(evt.Delta.PartialJSON)
				}
			case "thinking_delta":
				// Thinking content — skip, we strip reasoning tags
			}

		case "message_delta":
			var evt StreamEvent
			if err := json.Unmarshal([]byte(data), &evt); err != nil {
				logger.Debug("Skip unparseable message_delta", zap.Error(err))
				continue
			}
			if evt.Delta != nil && evt.Delta.StopReason != "" {
				finishReason = evt.Delta.StopReason
			}
			if evt.Usage != nil && evt.Usage.Total() > 0 {
				tokensUsed = evt.Usage.Total()
			}

		case "message_stop":
			// Stream complete
			break

		case "ping":
			// Heartbeat — ignore

		default:
			logger.Debug("Unknown Anthropic SSE event type", zap.String("type", currentEventType))
		}

		currentEventType = "" // reset after processing
	}

	if err := scanner.Err(); err != nil {
		if isIdleTimeoutErr(err) {
			logger.Warn("SSE stream idle timeout — Anthropic API stalled",
				zap.Duration("idle_timeout", idleTimeout))
			if contentBuilder.Len() == 0 && len(toolCalls) == 0 {
				return nil, fmt.Errorf("SSE stream stalled: no data for %v", idleTimeout)
			}
		} else {
			return nil, fmt.Errorf("SSE scan error: %w", err)
		}
	}

	// Send finish_reason delta
	if finishReason != "" {
		deltaCh <- service.StreamChunk{FinishReason: finishReason}
	}

	contentStr := contentBuilder.String()
	if tokensUsed == 0 && len(contentStr) > 0 {
		tokensUsed = len([]rune(contentStr))*3/2 + 50
	}

	resp := &service.LLMResponse{
		Content:    contentStr,
		ModelUsed:  modelUsed,
		TokensUsed: tokensUsed,
	}

	// Assemble tool calls
	for i := 0; i < len(toolCalls); i++ {
		acc, ok := toolCalls[i]
		if !ok {
			continue
		}
		var args map[string]interface{}
		if argsStr := acc.ArgsBuilder.String(); argsStr != "" {
			if err := json.Unmarshal([]byte(argsStr), &args); err != nil {
				logger.Warn("Failed to parse Anthropic tool call args",
					zap.String("tool", acc.Name),
					zap.Error(err))
				continue
			}
		}
		tc := entity.ToolCallInfo{
			ID:        acc.ID,
			Name:      acc.Name,
			Arguments: args,
		}
		resp.ToolCalls = append(resp.ToolCalls, tc)
		deltaCh <- service.StreamChunk{DeltaToolCall: &tc}
	}

	return resp, nil
}

// --- SSE idle timeout support (same pattern as OpenAI) ---

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
