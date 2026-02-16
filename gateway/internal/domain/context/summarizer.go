package context

import (
	"context"
	"fmt"
	"strings"
)

// Summarizer 消息摘要生成器接口
type Summarizer interface {
	// Summarize 生成对话摘要
	Summarize(ctx context.Context, messages []Message) (string, error)
}

// ModelClient 模型客户端接口 (用于摘要生成)
type ModelClient interface {
	Generate(ctx context.Context, prompt string) (string, error)
}

// LLMSummarizer 基于 LLM 的摘要生成器
type LLMSummarizer struct {
	client           ModelClient
	maxInputTokens   int
	maxOutputTokens  int
	summaryPrompt    string
}

// SummarizerConfig 摘要器配置
type SummarizerConfig struct {
	MaxInputTokens  int    // 输入消息最大 token
	MaxOutputTokens int    // 摘要最大 token
	CustomPrompt    string // 自定义摘要提示词
}

// DefaultSummarizerConfig 默认配置
func DefaultSummarizerConfig() *SummarizerConfig {
	return &SummarizerConfig{
		MaxInputTokens:  8000,
		MaxOutputTokens: 500,
		CustomPrompt:    "",
	}
}

// NewLLMSummarizer 创建 LLM 摘要器
func NewLLMSummarizer(client ModelClient, config *SummarizerConfig) *LLMSummarizer {
	if config == nil {
		config = DefaultSummarizerConfig()
	}

	prompt := config.CustomPrompt
	if prompt == "" {
		prompt = defaultSummaryPrompt
	}

	return &LLMSummarizer{
		client:          client,
		maxInputTokens:  config.MaxInputTokens,
		maxOutputTokens: config.MaxOutputTokens,
		summaryPrompt:   prompt,
	}
}

const defaultSummaryPrompt = `请将以下对话历史压缩成简洁的摘要，保留关键信息：
1. 用户的核心需求和目标
2. 已完成的重要操作和决策
3. 关键的代码修改或配置变更
4. 未解决的问题或待办事项

保持摘要简洁，不超过 300 字。使用要点列表格式。

对话历史：
%s

摘要：`

// Summarize 生成对话摘要
func (s *LLMSummarizer) Summarize(ctx context.Context, messages []Message) (string, error) {
	if len(messages) == 0 {
		return "", nil
	}

	// 格式化消息为文本
	var sb strings.Builder
	tokenizer := NewSimpleTokenizer()
	totalTokens := 0

	for _, msg := range messages {
		line := fmt.Sprintf("[%s]: %s\n", msg.Role, msg.Content)
		lineTokens := tokenizer.Count(line)

		if totalTokens+lineTokens > s.maxInputTokens {
			sb.WriteString("... (更早的消息已省略)\n")
			break
		}

		sb.WriteString(line)
		totalTokens += lineTokens
	}

	// 构建摘要请求
	prompt := fmt.Sprintf(s.summaryPrompt, sb.String())

	// 调用模型生成摘要
	summary, err := s.client.Generate(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("failed to generate summary: %w", err)
	}

	return summary, nil
}

// SummarizePruner 带摘要功能的修剪器
type SummarizePruner struct {
	*Pruner
	summarizer Summarizer
	summaryMsg *Message // 缓存的摘要消息
}

// NewSummarizePruner 创建带摘要的修剪器
func NewSummarizePruner(config *PruneConfig, tokenizer Tokenizer, summarizer Summarizer) *SummarizePruner {
	config.Strategy = PruneSummarize
	return &SummarizePruner{
		Pruner:     NewPruner(config, tokenizer),
		summarizer: summarizer,
	}
}

// PruneWithSummary 使用摘要进行修剪
func (p *SummarizePruner) PruneWithSummary(ctx context.Context, messages []Message) ([]Message, error) {
	if !p.NeedsPruning(messages) {
		return messages, nil
	}

	// 分离系统消息和对话消息
	var systemMsgs, dialogMsgs []Message
	for _, msg := range messages {
		if msg.Role == "system" {
			systemMsgs = append(systemMsgs, msg)
		} else {
			dialogMsgs = append(dialogMsgs, msg)
		}
	}

	// 保留最近的消息
	recentCount := p.config.PreserveRecent
	if recentCount > len(dialogMsgs) {
		recentCount = len(dialogMsgs)
	}
	
	recentMsgs := dialogMsgs[len(dialogMsgs)-recentCount:]
	oldMsgs := dialogMsgs[:len(dialogMsgs)-recentCount]

	// 对旧消息生成摘要
	if len(oldMsgs) > 0 && p.summarizer != nil {
		summary, err := p.summarizer.Summarize(ctx, oldMsgs)
		if err != nil {
			// 摘要失败，回退到普通修剪
			return p.Prune(messages), nil
		}

		// 创建摘要消息
		p.summaryMsg = &Message{
			Role:    "system",
			Content: fmt.Sprintf("[对话历史摘要]\n%s", summary),
		}
	}

	// 组合结果
	result := make([]Message, 0, len(systemMsgs)+1+len(recentMsgs))
	result = append(result, systemMsgs...)
	if p.summaryMsg != nil {
		result = append(result, *p.summaryMsg)
	}
	result = append(result, recentMsgs...)

	return result, nil
}

// GetLastSummary 获取最近生成的摘要
func (p *SummarizePruner) GetLastSummary() string {
	if p.summaryMsg != nil {
		return p.summaryMsg.Content
	}
	return ""
}

// SimpleSummarizer 简单摘要器 (不依赖 LLM，用于测试)
type SimpleSummarizer struct{}

// NewSimpleSummarizer 创建简单摘要器
func NewSimpleSummarizer() *SimpleSummarizer {
	return &SimpleSummarizer{}
}

// Summarize 简单提取关键信息
func (s *SimpleSummarizer) Summarize(ctx context.Context, messages []Message) (string, error) {
	if len(messages) == 0 {
		return "", nil
	}

	var points []string
	
	for _, msg := range messages {
		// 提取包含关键词的消息
		content := strings.ToLower(msg.Content)
		if strings.Contains(content, "error") ||
			strings.Contains(content, "完成") ||
			strings.Contains(content, "created") ||
			strings.Contains(content, "修改") {
			// 截取前 100 字符
			summary := msg.Content
			if len(summary) > 100 {
				summary = summary[:100] + "..."
			}
			points = append(points, fmt.Sprintf("- [%s] %s", msg.Role, summary))
		}
	}

	if len(points) == 0 {
		return fmt.Sprintf("共 %d 条历史消息", len(messages)), nil
	}

	// 限制最多 10 条
	if len(points) > 10 {
		points = points[len(points)-10:]
	}

	return strings.Join(points, "\n"), nil
}
