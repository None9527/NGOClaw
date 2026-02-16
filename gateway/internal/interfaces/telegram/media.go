package telegram

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.uber.org/zap"
)

// MediaType represents the type of media attached to a message
type MediaType string

const (
	MediaTypePhoto    MediaType = "photo"
	MediaTypeAudio    MediaType = "audio"
	MediaTypeVoice    MediaType = "voice"
	MediaTypeVideo    MediaType = "video"
	MediaTypeDocument MediaType = "document"
)

// MediaInfo holds information about a media attachment
type MediaInfo struct {
	Type     MediaType
	FileID   string
	MimeType string
	FileName string
	FileSize int
	Caption  string
}

// ExtractMedia extracts media information from a Telegram update
func ExtractMedia(msg *tgbotapi.Message) *MediaInfo {
	if msg == nil {
		return nil
	}

	// Photo (array of PhotoSize, pick largest)
	if msg.Photo != nil && len(msg.Photo) > 0 {
		largest := msg.Photo[len(msg.Photo)-1]
		return &MediaInfo{
			Type:     MediaTypePhoto,
			FileID:   largest.FileID,
			MimeType: "image/jpeg",
			FileSize: largest.FileSize,
			Caption:  msg.Caption,
		}
	}

	// Voice message
	if msg.Voice != nil {
		return &MediaInfo{
			Type:     MediaTypeVoice,
			FileID:   msg.Voice.FileID,
			MimeType: msg.Voice.MimeType,
			FileSize: msg.Voice.FileSize,
			Caption:  msg.Caption,
		}
	}

	// Audio file
	if msg.Audio != nil {
		mime := msg.Audio.MimeType
		if mime == "" {
			mime = "audio/mpeg"
		}
		return &MediaInfo{
			Type:     MediaTypeAudio,
			FileID:   msg.Audio.FileID,
			MimeType: mime,
			FileName: msg.Audio.Title,
			FileSize: msg.Audio.FileSize,
			Caption:  msg.Caption,
		}
	}

	// Video
	if msg.Video != nil {
		mime := msg.Video.MimeType
		if mime == "" {
			mime = "video/mp4"
		}
		return &MediaInfo{
			Type:     MediaTypeVideo,
			FileID:   msg.Video.FileID,
			MimeType: mime,
			FileSize: msg.Video.FileSize,
			Caption:  msg.Caption,
		}
	}

	// Document
	if msg.Document != nil {
		return &MediaInfo{
			Type:     MediaTypeDocument,
			FileID:   msg.Document.FileID,
			MimeType: msg.Document.MimeType,
			FileName: msg.Document.FileName,
			FileSize: msg.Document.FileSize,
			Caption:  msg.Caption,
		}
	}

	return nil
}

// DownloadFile downloads a file from Telegram by file ID
func DownloadFile(bot *tgbotapi.BotAPI, fileID string, logger *zap.Logger) ([]byte, error) {
	file, err := bot.GetFile(tgbotapi.FileConfig{FileID: fileID})
	if err != nil {
		return nil, fmt.Errorf("get file: %w", err)
	}

	fileURL := file.Link(bot.Token)
	logger.Debug("Downloading Telegram file",
		zap.String("file_id", fileID),
		zap.String("url_path", file.FilePath),
	)

	resp, err := http.Get(fileURL)
	if err != nil {
		return nil, fmt.Errorf("download file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	logger.Info("Downloaded Telegram file",
		zap.String("file_id", fileID),
		zap.Int("size_bytes", len(data)),
	)

	return data, nil
}

// IsImageMIME checks if a MIME type represents an image
func IsImageMIME(mimeType string) bool {
	return strings.HasPrefix(mimeType, "image/")
}

// IsAudioMIME checks if a MIME type represents audio
func IsAudioMIME(mimeType string) bool {
	return strings.HasPrefix(mimeType, "audio/")
}

// IsVideoMIME checks if a MIME type represents video
func IsVideoMIME(mimeType string) bool {
	return strings.HasPrefix(mimeType, "video/")
}

// MediaProcessor processes media attachments into text descriptions for LLM prompts
type MediaProcessor struct {
	logger *zap.Logger
}

// NewMediaProcessor creates a new MediaProcessor
func NewMediaProcessor(logger *zap.Logger) *MediaProcessor {
	return &MediaProcessor{logger: logger}
}

// DescribeMedia converts a media attachment into a text description or data URI
// that can be included in the AI prompt for multimodal understanding
func (p *MediaProcessor) DescribeMedia(media *MediaInfo, data []byte) string {
	if media == nil || len(data) == 0 {
		return ""
	}

	switch media.Type {
	case MediaTypePhoto:
		return p.describeImage(media, data)
	case MediaTypeVoice, MediaTypeAudio:
		return p.describeAudio(media, data)
	case MediaTypeVideo:
		return p.describeVideo(media, data)
	case MediaTypeDocument:
		return p.describeDocument(media, data)
	default:
		return fmt.Sprintf("[Attachment: %s, type=%s, size=%d bytes]",
			media.FileName, media.MimeType, len(data))
	}
}

// describeImage returns a base64 data URI for multimodal model consumption
func (p *MediaProcessor) describeImage(media *MediaInfo, data []byte) string {
	encoded := base64.StdEncoding.EncodeToString(data)
	mime := media.MimeType
	if mime == "" {
		mime = "image/jpeg"
	}

	p.logger.Debug("Encoding image for multimodal",
		zap.String("mime", mime),
		zap.Int("original_bytes", len(data)),
	)

	// Return as inline data URI - multimodal models can interpret this
	return fmt.Sprintf("[Image attached: data:%s;base64,%s]", mime, encoded)
}

// describeAudio returns a description with base64 data for transcription
func (p *MediaProcessor) describeAudio(media *MediaInfo, data []byte) string {
	encoded := base64.StdEncoding.EncodeToString(data)
	mime := media.MimeType
	if mime == "" {
		mime = "audio/ogg"
	}

	label := "Audio"
	if media.Type == MediaTypeVoice {
		label = "Voice message"
	}

	p.logger.Debug("Encoding audio for transcription",
		zap.String("mime", mime),
		zap.Int("original_bytes", len(data)),
	)

	return fmt.Sprintf("[%s attached: data:%s;base64,%s]", label, mime, encoded)
}

// describeVideo returns metadata about the video attachment
func (p *MediaProcessor) describeVideo(media *MediaInfo, data []byte) string {
	// Video files are typically too large for inline base64
	// Provide metadata and request the model to acknowledge the attachment
	return fmt.Sprintf("[Video attached: type=%s, size=%d bytes. Please acknowledge this video attachment.]",
		media.MimeType, len(data))
}

// describeDocument returns text content for text-based documents, metadata for others
func (p *MediaProcessor) describeDocument(media *MediaInfo, data []byte) string {
	// For text-based documents, include the content directly
	if isTextMIME(media.MimeType) {
		content := string(data)
		if len(content) > 8000 {
			content = content[:8000] + "\n... [truncated]"
		}
		return fmt.Sprintf("[Document: %s]\n```\n%s\n```", media.FileName, content)
	}

	return fmt.Sprintf("[Document attached: name=%s, type=%s, size=%d bytes]",
		media.FileName, media.MimeType, len(data))
}

// isTextMIME checks if a MIME type represents text content
func isTextMIME(mimeType string) bool {
	textTypes := []string{
		"text/", "application/json", "application/xml",
		"application/javascript", "application/yaml",
		"application/x-yaml", "application/toml",
	}
	for _, prefix := range textTypes {
		if strings.HasPrefix(mimeType, prefix) {
			return true
		}
	}
	return false
}
