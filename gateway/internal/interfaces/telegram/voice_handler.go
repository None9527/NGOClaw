package telegram

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.uber.org/zap"
)

// VoiceHandler 语音闭环处理器
// 语音消息 → STT → AI → TTS → 语音回复
type VoiceHandler struct {
	sttProvider STTProvider
	ttsProvider TTSProvider
	logger      *zap.Logger
}

// STTProvider 语音转文字接口
type STTProvider interface {
	// Transcribe 将音频数据转为文本
	// audioData: OGG/WAV 数据, mimeType: 音频类型
	Transcribe(ctx context.Context, audioData []byte, mimeType string) (string, error)
}

// TTSProvider 文字转语音接口
type TTSProvider interface {
	// Synthesize 将文本转为音频
	// 返回 OGG Opus 数据 (Telegram voice 格式)
	Synthesize(ctx context.Context, text string) ([]byte, error)
}

// NewVoiceHandler 创建语音处理器
func NewVoiceHandler(stt STTProvider, tts TTSProvider, logger *zap.Logger) *VoiceHandler {
	return &VoiceHandler{
		sttProvider: stt,
		ttsProvider: tts,
		logger:      logger,
	}
}

// ProcessVoice 处理语音消息: OGG → 转文字 → 返回文本 (调用方继续走 AI 流程)
func (h *VoiceHandler) ProcessVoice(ctx context.Context, audioData []byte, mimeType string) (string, error) {
	if h.sttProvider == nil {
		return "", fmt.Errorf("STT provider not configured")
	}

	// Telegram 语音消息是 OGG Opus, 某些 STT API 需要 WAV
	// 先尝试直接传, 失败再转码
	text, err := h.sttProvider.Transcribe(ctx, audioData, mimeType)
	if err != nil {
		// 降级: 尝试 ffmpeg 转码为 WAV
		wavData, convErr := oggToWav(audioData)
		if convErr != nil {
			return "", fmt.Errorf("STT failed and ffmpeg conversion also failed: %w (original: %v)", convErr, err)
		}
		text, err = h.sttProvider.Transcribe(ctx, wavData, "audio/wav")
		if err != nil {
			return "", fmt.Errorf("STT failed even after WAV conversion: %w", err)
		}
	}

	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("STT returned empty text")
	}

	h.logger.Info("Voice transcribed",
		zap.Int("audio_bytes", len(audioData)),
		zap.Int("text_len", len(text)),
	)

	return text, nil
}

// SynthesizeReply 将 AI 回复转为语音消息并发送
func (h *VoiceHandler) SynthesizeReply(ctx context.Context, bot *tgbotapi.BotAPI, chatID int64, replyToID int, text string) error {
	if h.ttsProvider == nil {
		return fmt.Errorf("TTS provider not configured")
	}

	// 截断过长文本 (TTS 有长度限制)
	if len(text) > 3000 {
		text = text[:3000] + "..."
	}

	// 生成语音
	audioData, err := h.ttsProvider.Synthesize(ctx, text)
	if err != nil {
		return fmt.Errorf("TTS synthesis failed: %w", err)
	}

	h.logger.Info("Voice reply synthesized",
		zap.Int("text_len", len(text)),
		zap.Int("audio_bytes", len(audioData)),
	)

	// 发送语音消息
	voice := tgbotapi.NewVoice(chatID, tgbotapi.FileBytes{
		Name:  "reply.ogg",
		Bytes: audioData,
	})
	voice.ReplyToMessageID = replyToID

	_, err = bot.Send(voice)
	return err
}

// IsVoiceMessage 判断是否是语音消息
func IsVoiceMessage(media *MediaInfo) bool {
	if media == nil {
		return false
	}
	return media.Type == MediaTypeVoice || media.Type == MediaTypeAudio
}

// oggToWav 使用 ffmpeg 将 OGG 转为 WAV (16kHz mono, STT 最佳格式)
func oggToWav(oggData []byte) ([]byte, error) {
	cmd := exec.Command("ffmpeg",
		"-i", "pipe:0",          // 从 stdin 读取
		"-ar", "16000",          // 16kHz 采样率
		"-ac", "1",              // 单声道
		"-f", "wav",             // WAV 格式
		"-acodec", "pcm_s16le", // 16-bit PCM
		"pipe:1",                // 输出到 stdout
	)

	cmd.Stdin = bytes.NewReader(oggData)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg error: %w (stderr: %s)", err, stderr.String())
	}

	return stdout.Bytes(), nil
}
