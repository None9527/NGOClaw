package telegram

import (
	"context"
	"fmt"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.uber.org/zap"
)

// InlineHandler 处理 @bot 即时查询
type InlineHandler struct {
	aiClient       InlineAIClient
	logger         *zap.Logger
	defaultModel   string
	maxQueryLen    int
	maxResultLen   int
	cacheResults   bool
	cacheDuration  time.Duration
}

// InlineAIClient AI 客户端接口 (专为 inline 优化: 快速、低 token)
type InlineAIClient interface {
	QuickGenerate(ctx context.Context, prompt string, maxTokens int) (string, error)
}

// InlineConfig inline 模式配置
type InlineConfig struct {
	DefaultModel  string
	MaxQueryLen   int
	MaxResultLen  int
	CacheResults  bool
	CacheDuration time.Duration
}

// NewInlineHandler 创建 inline 处理器
func NewInlineHandler(aiClient InlineAIClient, logger *zap.Logger, cfg *InlineConfig) *InlineHandler {
	if cfg == nil {
		cfg = &InlineConfig{
			MaxQueryLen:   200,
			MaxResultLen:  4096,
			CacheResults:  true,
			CacheDuration: 5 * time.Minute,
		}
	}
	if cfg.MaxQueryLen == 0 {
		cfg.MaxQueryLen = 200
	}
	if cfg.MaxResultLen == 0 {
		cfg.MaxResultLen = 4096
	}

	return &InlineHandler{
		aiClient:      aiClient,
		logger:        logger,
		defaultModel:  cfg.DefaultModel,
		maxQueryLen:   cfg.MaxQueryLen,
		maxResultLen:  cfg.MaxResultLen,
		cacheResults:  cfg.CacheResults,
		cacheDuration: cfg.CacheDuration,
	}
}

// HandleInlineQuery 处理 inline 查询
func (h *InlineHandler) HandleInlineQuery(ctx context.Context, bot *tgbotapi.BotAPI, query *tgbotapi.InlineQuery) {
	queryText := strings.TrimSpace(query.Query)
	if queryText == "" {
		// 空查询: 返回使用说明
		h.answerWithHelp(bot, query)
		return
	}

	// 截断过长查询
	if len(queryText) > h.maxQueryLen {
		queryText = queryText[:h.maxQueryLen]
	}

	h.logger.Info("Inline query received",
		zap.String("query", queryText),
		zap.Int64("from_id", query.From.ID),
		zap.String("from_user", query.From.UserName),
	)

	// 并发生成: 简短回答 + 详细回答
	type result struct {
		text string
		err  error
	}

	shortCh := make(chan result, 1)
	detailCh := make(chan result, 1)

	// 简短回答 (50 token)
	go func() {
		prompt := fmt.Sprintf("用最简洁的方式回答 (不超过 2 句话):\n%s", queryText)
		text, err := h.aiClient.QuickGenerate(ctx, prompt, 100)
		shortCh <- result{text, err}
	}()

	// 详细回答 (500 token)
	go func() {
		prompt := fmt.Sprintf("详细回答以下问题:\n%s", queryText)
		text, err := h.aiClient.QuickGenerate(ctx, prompt, 500)
		detailCh <- result{text, err}
	}()

	// 等待结果 (最多 10 秒)
	timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var articles []tgbotapi.InlineQueryResultArticle

	select {
	case r := <-shortCh:
		if r.err == nil && r.text != "" {
			articles = append(articles, h.makeArticle(
				"quick",
				"⚡ 简要回答",
				r.text,
				queryText,
			))
		}
	case <-timeoutCtx.Done():
	}

	select {
	case r := <-detailCh:
		if r.err == nil && r.text != "" {
			articles = append(articles, h.makeArticle(
				"detail",
				"📖 详细回答",
				r.text,
				queryText,
			))
		}
	case <-timeoutCtx.Done():
	}

	// 始终添加 "在私聊中继续" 选项
	articles = append(articles, h.makeArticle(
		"continue",
		"💬 在私聊中继续",
		fmt.Sprintf("我想了解: %s\n\n请点击消息下方按钮，到私聊中获取完整回答。", queryText),
		queryText,
	))

	// 发送 inline 结果
	var results []interface{}
	for i := range articles {
		results = append(results, articles[i])
	}

	answer := tgbotapi.InlineConfig{
		InlineQueryID: query.ID,
		Results:       results,
		IsPersonal:    true,
	}
	if h.cacheResults {
		answer.CacheTime = int(h.cacheDuration.Seconds())
	}

	if _, err := bot.Request(answer); err != nil {
		h.logger.Error("Failed to answer inline query",
			zap.Error(err),
			zap.String("query", queryText),
		)
	}
}

func (h *InlineHandler) makeArticle(id, title, text, query string) tgbotapi.InlineQueryResultArticle {
	if len(text) > h.maxResultLen {
		text = text[:h.maxResultLen]
	}

	// 简短描述 (显示在选项列表中)
	desc := text
	if len(desc) > 100 {
		desc = desc[:100] + "..."
	}

	return tgbotapi.InlineQueryResultArticle{
		Type:  "article",
		ID:    fmt.Sprintf("%s_%d", id, time.Now().UnixMilli()),
		Title: title,
		InputMessageContent: tgbotapi.InputTextMessageContent{
			Text:      text,
			ParseMode: "Markdown",
		},
		Description: desc,
	}
}

func (h *InlineHandler) answerWithHelp(bot *tgbotapi.BotAPI, query *tgbotapi.InlineQuery) {
	helpArticle := tgbotapi.InlineQueryResultArticle{
		Type:  "article",
		ID:    "help",
		Title: "💡 输入问题即可获得 AI 回答",
		InputMessageContent: tgbotapi.InputTextMessageContent{
			Text:      "使用方式: 在任意聊天中输入 @NGOClawBot 你的问题\n\n示例: @NGOClawBot 什么是量子计算",
			ParseMode: "Markdown",
		},
		Description: "在任意聊天中 @NGOClawBot + 问题",
	}

	answer := tgbotapi.InlineConfig{
		InlineQueryID: query.ID,
		Results:       []interface{}{helpArticle},
		IsPersonal:    true,
		CacheTime:     300,
	}

	bot.Request(answer)
}
