package usecase

import (
	"context"
	"fmt"
	"strings"

	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/entity"
	"go.uber.org/zap"
)

// Compaction thresholds
const (
	// CompactMessageThreshold triggers compaction when history exceeds this count
	CompactMessageThreshold = 30
	// CompactTokenEstimate rough chars-per-token estimate
	CompactTokenEstimate = 4
	// CompactTokenThreshold triggers compaction when estimated tokens exceed this
	CompactTokenThreshold = 30000
	// CompactKeepRecent number of recent messages to keep verbatim
	CompactKeepRecent = 10
	// CompactSummaryMaxTokens max tokens for summary generation
	CompactSummaryMaxTokens = 1000
)

// Compactor compresses long conversation history by summarizing older messages
// and keeping only recent messages verbatim. Matches OpenClaw compaction.ts logic.
type Compactor struct {
	aiClient      AIServiceClient
	memoryFlusher MemoryFlusher
	logger        *zap.Logger
}

// MemoryFlusher 记忆刷盘接口 (压缩前将关键事实存入向量库)
type MemoryFlusher interface {
	FlushToMemory(ctx context.Context, content string, metadata map[string]interface{}) error
}

// NewCompactor creates a new compactor
func NewCompactor(aiClient AIServiceClient, logger *zap.Logger) *Compactor {
	return &Compactor{
		aiClient: aiClient,
		logger:   logger,
	}
}

// SetMemoryFlusher 可选注入记忆刷盘器
func (c *Compactor) SetMemoryFlusher(flusher MemoryFlusher) {
	c.memoryFlusher = flusher
}

// CompactResult holds the result of a compaction
type CompactResult struct {
	// Summary is the AI-generated summary of older messages
	Summary string
	// RecentMessages are the verbatim recent messages kept
	RecentMessages []*entity.Message
	// WasCompacted indicates if compaction actually happened
	WasCompacted bool
	// CompactedCount is the number of messages that were summarized
	CompactedCount int
}

// CompactIfNeeded checks if history needs compaction and performs it
func (c *Compactor) CompactIfNeeded(ctx context.Context, history []*entity.Message, model string) (*CompactResult, error) {
	result := &CompactResult{
		RecentMessages: history,
		WasCompacted:   false,
	}

	// Check thresholds
	if len(history) <= CompactMessageThreshold && c.estimateTokens(history) <= CompactTokenThreshold {
		return result, nil
	}

	c.logger.Info("Compaction triggered",
		zap.Int("message_count", len(history)),
		zap.Int("estimated_tokens", c.estimateTokens(history)),
	)

	return c.doCompact(ctx, history, model)
}

// ForceCompact performs compaction regardless of thresholds
func (c *Compactor) ForceCompact(ctx context.Context, history []*entity.Message, model string) (*CompactResult, error) {
	if len(history) <= CompactKeepRecent {
		return &CompactResult{
			RecentMessages: history,
			WasCompacted:   false,
		}, nil
	}
	return c.doCompact(ctx, history, model)
}

func (c *Compactor) doCompact(ctx context.Context, history []*entity.Message, model string) (*CompactResult, error) {
	// Split into old (to summarize) and recent (to keep)
	splitIndex := len(history) - CompactKeepRecent
	if splitIndex < 1 {
		splitIndex = 1
	}

	oldMessages := history[:splitIndex]
	recentMessages := history[splitIndex:]

	// Pre-flush: 压缩前将 assistant 关键内容存入向量记忆
	if c.memoryFlusher != nil {
		c.preFlushToMemory(ctx, oldMessages)
	}

	// Build summary prompt
	summaryPrompt := c.buildSummaryPrompt(oldMessages)

	// Generate summary via AI
	summaryReq := &AIRequest{
		Prompt:    summaryPrompt,
		Model:     model,
		MaxTokens: CompactSummaryMaxTokens,
	}

	summaryResp, err := c.aiClient.GenerateResponse(ctx, summaryReq)
	if err != nil {
		c.logger.Error("Failed to generate compaction summary", zap.Error(err))
		// Fallback: truncate without summary
		return &CompactResult{
			RecentMessages: recentMessages,
			WasCompacted:   true,
			CompactedCount: len(oldMessages),
		}, nil
	}

	c.logger.Info("Compaction complete",
		zap.Int("compacted", len(oldMessages)),
		zap.Int("kept", len(recentMessages)),
		zap.Int("summary_len", len(summaryResp.Content)),
	)

	return &CompactResult{
		Summary:        summaryResp.Content,
		RecentMessages: recentMessages,
		WasCompacted:   true,
		CompactedCount: len(oldMessages),
	}, nil
}

func (c *Compactor) buildSummaryPrompt(messages []*entity.Message) string {
	var sb strings.Builder
	sb.WriteString("Please provide a concise summary of the following conversation. ")
	sb.WriteString("Focus on key topics, decisions, and context that would be important ")
	sb.WriteString("for continuing the conversation. Keep the summary under 500 words.\n\n")
	sb.WriteString("=== Conversation History ===\n\n")

	for _, msg := range messages {
		role := "User"
		if msg.Sender().Type() == "bot" {
			role = "Assistant"
		}
		text := msg.Content().Text()
		if len(text) > 500 {
			text = text[:500] + "..."
		}
		sb.WriteString(fmt.Sprintf("[%s]: %s\n\n", role, text))
	}

	sb.WriteString("=== End of Conversation ===\n\n")
	sb.WriteString("Summary:")
	return sb.String()
}

func (c *Compactor) estimateTokens(messages []*entity.Message) int {
	total := 0
	for _, msg := range messages {
		total += len(msg.Content().Text()) / CompactTokenEstimate
	}
	return total
}

// preFlushToMemory 压缩前将 assistant 关键事实存入向量记忆
func (c *Compactor) preFlushToMemory(ctx context.Context, messages []*entity.Message) {
	flushed := 0
	for _, msg := range messages {
		if msg.Sender().Type() != "bot" {
			continue
		}
		text := msg.Content().Text()
		if len(text) < 50 {
			continue // 过短的消息不值得存储
		}
		// 截断过长消息 (向量库只需要关键信息)
		if len(text) > 2000 {
			text = text[:2000]
		}

		metadata := map[string]interface{}{
			"source":    "compaction_flush",
			"timestamp": msg.Timestamp().Unix(),
		}

		if err := c.memoryFlusher.FlushToMemory(ctx, text, metadata); err != nil {
			c.logger.Warn("Failed to flush message to memory",
				zap.String("msg_id", msg.ID()),
				zap.Error(err),
			)
			continue
		}
		flushed++
	}

	if flushed > 0 {
		c.logger.Info("Pre-compaction memory flush complete",
			zap.Int("flushed_count", flushed),
		)
	}
}
