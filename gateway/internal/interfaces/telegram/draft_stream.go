package telegram

import (
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// DraftStream 流式消息输出器
// 参考 OpenClaw draft-stream.ts
type DraftStream struct {
	bot        *tgbotapi.BotAPI
	chatID     int64
	messageID  int
	lastText   string
	throttleMs int64
	lastUpdate int64
	parseMode  string
	mu         sync.Mutex
}

// NewDraftStream 创建流式输出器
func NewDraftStream(bot *tgbotapi.BotAPI, chatID int64) *DraftStream {
	return &DraftStream{
		bot:        bot,
		chatID:     chatID,
		throttleMs: 500, // 默认 500ms 节流
		parseMode:  "Markdown",
	}
}

// SetThrottle 设置节流间隔
func (d *DraftStream) SetThrottle(ms int64) {
	d.throttleMs = ms
}

// Update 更新流式消息 (节流)
func (d *DraftStream) Update(text string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now().UnixMilli()

	// 节流检查
	if now-d.lastUpdate < d.throttleMs {
		return nil
	}

	// 内容无变化
	if text == d.lastText {
		return nil
	}

	return d.doUpdate(text, now)
}

// ForceUpdate 强制更新 (忽略节流)
func (d *DraftStream) ForceUpdate(text string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.doUpdate(text, time.Now().UnixMilli())
}

// doUpdate 执行更新
func (d *DraftStream) doUpdate(text string, now int64) error {
	if d.messageID == 0 {
		// 首次发送
		msg := tgbotapi.NewMessage(d.chatID, text)
		if d.parseMode != "" {
			msg.ParseMode = d.parseMode
		}
		sent, err := d.bot.Send(msg)
		if err != nil {
			return err
		}
		d.messageID = sent.MessageID
	} else {
		// 编辑消息
		editMsg := tgbotapi.NewEditMessageText(d.chatID, d.messageID, text)
		if d.parseMode != "" {
			editMsg.ParseMode = d.parseMode
		}
		_, err := d.bot.Send(editMsg)
		if err != nil {
			// 忽略 "message is not modified" 错误
			if !isMessageNotModifiedError(err) {
				return err
			}
		}
	}

	d.lastText = text
	d.lastUpdate = now
	return nil
}

// Finalize 完成流式输出
func (d *DraftStream) Finalize(finalText string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.messageID == 0 {
		// 从未发送过，直接发送最终消息
		msg := tgbotapi.NewMessage(d.chatID, finalText)
		if d.parseMode != "" {
			msg.ParseMode = d.parseMode
		}
		sent, err := d.bot.Send(msg)
		if err != nil {
			return err
		}
		d.messageID = sent.MessageID
		d.lastText = finalText
		return nil
	}

	// 有消息，检查是否需要更新
	if finalText != d.lastText {
		editMsg := tgbotapi.NewEditMessageText(d.chatID, d.messageID, finalText)
		if d.parseMode != "" {
			editMsg.ParseMode = d.parseMode
		}
		_, err := d.bot.Send(editMsg)
		if err != nil && !isMessageNotModifiedError(err) {
			return err
		}
		d.lastText = finalText
	}

	return nil
}

// GetMessageID 获取消息 ID
func (d *DraftStream) GetMessageID() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.messageID
}

// isMessageNotModifiedError 检查是否是"消息未修改"错误
func isMessageNotModifiedError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return containsStr(errStr, "message is not modified") ||
		containsStr(errStr, "MESSAGE_NOT_MODIFIED")
}

// containsStr 检查字符串包含
func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
