package telegram

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.uber.org/zap"
)

// StartWebhook 启动 Webhook 模式
func (a *Adapter) StartWebhook(ctx context.Context, listenAddr string) error {
	if a.config.WebhookURL == "" {
		return fmt.Errorf("webhook URL not configured")
	}

	// 设置 Webhook
	wh, err := tgbotapi.NewWebhook(a.config.WebhookURL)
	if err != nil {
		return fmt.Errorf("failed to create webhook: %w", err)
	}

	_, err = a.bot.Request(wh)
	if err != nil {
		return fmt.Errorf("failed to set webhook: %w", err)
	}

	a.logger.Info("Webhook set",
		zap.String("url", a.config.WebhookURL),
	)

	// 获取更新通道
	updates := a.bot.ListenForWebhook("/" + a.bot.Token)

	// 启动 HTTP 服务器
	go func() {
		a.logger.Info("Starting webhook server",
			zap.String("addr", listenAddr),
		)
		if err := http.ListenAndServe(listenAddr, nil); err != nil {
			a.logger.Error("Webhook server error", zap.Error(err))
		}
	}()

	// 处理更新
	for {
		select {
		case <-ctx.Done():
			// 删除 Webhook
			a.bot.Request(tgbotapi.DeleteWebhookConfig{})
			return ctx.Err()
		case update := <-updates:
			go a.handleUpdate(ctx, update)
		}
	}
}

// SendPhoto 发送图片
func (a *Adapter) SendPhoto(chatID int64, photoPath string, caption string) error {
	// 检查是 URL 还是本地文件
	if strings.HasPrefix(photoPath, "http://") || strings.HasPrefix(photoPath, "https://") {
		photo := tgbotapi.NewPhoto(chatID, tgbotapi.FileURL(photoPath))
		photo.Caption = caption
		photo.ParseMode = "Markdown"
		_, err := a.bot.Send(photo)
		return err
	}

	// 本地文件
	file, err := os.Open(photoPath)
	if err != nil {
		return fmt.Errorf("failed to open photo: %w", err)
	}
	defer file.Close()

	photo := tgbotapi.NewPhoto(chatID, tgbotapi.FileReader{
		Name:   filepath.Base(photoPath),
		Reader: file,
	})
	photo.Caption = caption
	photo.ParseMode = "Markdown"
	_, err = a.bot.Send(photo)
	return err
}

// SendDocument 发送文档
func (a *Adapter) SendDocument(chatID int64, docPath string, caption string) error {
	file, err := os.Open(docPath)
	if err != nil {
		return fmt.Errorf("failed to open document: %w", err)
	}
	defer file.Close()

	doc := tgbotapi.NewDocument(chatID, tgbotapi.FileReader{
		Name:   filepath.Base(docPath),
		Reader: file,
	})
	doc.Caption = caption
	_, err = a.bot.Send(doc)
	return err
}

// SendVoice 发送语音
func (a *Adapter) SendVoice(chatID int64, voicePath string) error {
	file, err := os.Open(voicePath)
	if err != nil {
		return fmt.Errorf("failed to open voice: %w", err)
	}
	defer file.Close()

	voice := tgbotapi.NewVoice(chatID, tgbotapi.FileReader{
		Name:   filepath.Base(voicePath),
		Reader: file,
	})
	_, err = a.bot.Send(voice)
	return err
}

// DownloadFile 下载 Telegram 文件
func (a *Adapter) DownloadFile(fileID string, destPath string) error {
	// 获取文件信息
	file, err := a.bot.GetFile(tgbotapi.FileConfig{FileID: fileID})
	if err != nil {
		return fmt.Errorf("failed to get file: %w", err)
	}

	// 下载文件
	url := file.Link(a.config.BotToken)
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	// 保存到本地
	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

// EditMessage 编辑消息
func (a *Adapter) EditMessage(chatID int64, messageID int, text string, parseMode ...string) error {
	edit := tgbotapi.NewEditMessageText(chatID, messageID, text)
	if len(parseMode) > 0 && parseMode[0] != "" {
		edit.ParseMode = parseMode[0]
	} else {
		edit.ParseMode = "Markdown"
	}
	_, err := a.bot.Send(edit)
	if err != nil && isMessageNotModifiedError(err) {
		return nil
	}
	return err
}

// ReactMessage sends an emoji reaction to a message
func (a *Adapter) ReactMessage(chatID int64, messageID int, emoji string) error {
	params := tgbotapi.Params{}
	params.AddFirstValid("chat_id", chatID)
	params.AddNonZero("message_id", messageID)
	reactionJSON := fmt.Sprintf(`[{"type":"emoji","emoji":"%s"}]`, emoji)
	params["reaction"] = reactionJSON
	_, err := a.bot.MakeRequest("setMessageReaction", params)
	return err
}

// DeleteMessage 删除消息
func (a *Adapter) DeleteMessage(chatID int64, messageID int) error {
	del := tgbotapi.NewDeleteMessage(chatID, messageID)
	_, err := a.bot.Request(del)
	return err
}

// SendLongMessage 发送长消息 (自动分割)
func (a *Adapter) SendLongMessage(chatID int64, text string, parseMode string) error {
	const maxLen = 4000 // Telegram 限制 4096

	if len(text) <= maxLen {
		msg := tgbotapi.NewMessage(chatID, text)
		msg.ParseMode = parseMode
		_, err := a.bot.Send(msg)
		return err
	}

	// 分割消息
	parts := splitMessage(text, maxLen)
	for i, part := range parts {
		msg := tgbotapi.NewMessage(chatID, part)
		if i == 0 && parseMode != "" {
			msg.ParseMode = parseMode
		}
		if _, err := a.bot.Send(msg); err != nil {
			return err
		}
	}
	return nil
}

// splitMessage 分割长消息
func splitMessage(text string, maxLen int) []string {
	var parts []string
	
	for len(text) > 0 {
		if len(text) <= maxLen {
			parts = append(parts, text)
			break
		}

		// 找合适的分割点 (换行符)
		cutPoint := maxLen
		for i := maxLen - 1; i > maxLen/2; i-- {
			if text[i] == '\n' {
				cutPoint = i + 1
				break
			}
		}

		parts = append(parts, text[:cutPoint])
		text = text[cutPoint:]
	}

	return parts
}

// SendProgress 发送进度消息 (可更新)
func (a *Adapter) SendProgress(chatID int64, text string) (int, error) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	sent, err := a.bot.Send(msg)
	if err != nil {
		return 0, err
	}
	return sent.MessageID, nil
}

// UpdateProgress 更新进度消息
func (a *Adapter) UpdateProgress(chatID int64, messageID int, text string) error {
	return a.EditMessage(chatID, messageID, text)
}

// StreamWriter 流式输出写入器
type StreamWriter struct {
	adapter   *Adapter
	chatID    int64
	messageID int
	buffer    strings.Builder
	lastFlush int
}

// NewStreamWriter 创建流式写入器
func (a *Adapter) NewStreamWriter(chatID int64) (*StreamWriter, error) {
	// 发送初始消息
	msgID, err := a.SendProgress(chatID, "⏳ 处理中...")
	if err != nil {
		return nil, err
	}

	return &StreamWriter{
		adapter:   a,
		chatID:    chatID,
		messageID: msgID,
		lastFlush: 0,
	}, nil
}

// Write 实现 io.Writer
func (w *StreamWriter) Write(p []byte) (n int, err error) {
	w.buffer.Write(p)

	// 每 100 字符刷新一次
	if w.buffer.Len()-w.lastFlush >= 100 {
		w.Flush()
	}

	return len(p), nil
}

// WriteString 写入字符串
func (w *StreamWriter) WriteString(s string) (n int, err error) {
	return w.Write([]byte(s))
}

// Flush 刷新缓冲区到 Telegram
func (w *StreamWriter) Flush() error {
	text := w.buffer.String()
	if len(text) == 0 {
		return nil
	}

	// 截断过长的内容
	if len(text) > 4000 {
		text = text[len(text)-4000:]
		text = "...\n" + text
	}

	w.lastFlush = w.buffer.Len()
	return w.adapter.EditMessage(w.chatID, w.messageID, text)
}

// Close 关闭并发送最终消息
func (w *StreamWriter) Close() error {
	return w.Flush()
}

// GetMessageID 获取消息 ID
func (w *StreamWriter) GetMessageID() int {
	return w.messageID
}
