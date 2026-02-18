package tool

import (
	"context"
	"fmt"
	"strings"

	domaintool "github.com/ngoclaw/ngoclaw/gateway/internal/domain/tool"
	"go.uber.org/zap"
)

// MediaSender abstracts Telegram media sending capabilities.
// Implemented by telegram.Adapter (SendPhoto, SendDocument, SendMediaGroup).
type MediaSender interface {
	SendPhoto(chatID int64, path string, caption string) error
	SendDocument(chatID int64, path string, caption string) error
	SendMediaGroup(chatID int64, photoPaths []string, caption string) error
}

// chatIDContextKey is a context key for passing chatID to media tools.
// Duplicated from application package to avoid circular imports.
type chatIDContextKey struct{}

// WithChatID stores chatID in the context (for use by media tools).
func WithChatID(ctx context.Context, chatID int64) context.Context {
	return context.WithValue(ctx, chatIDContextKey{}, chatID)
}

// chatIDFromContext extracts chatID from the context.
func chatIDFromContext(ctx context.Context) int64 {
	if v, ok := ctx.Value(chatIDContextKey{}).(int64); ok {
		return v
	}
	return 0
}

// ──────────────────────────────────────────────────────────────
// SendPhotoTool — send_photo
// ──────────────────────────────────────────────────────────────

// SendPhotoTool sends an image (local file or URL) to the current Telegram chat.
type SendPhotoTool struct {
	sender MediaSender
	logger *zap.Logger
}

func NewSendPhotoTool(sender MediaSender, logger *zap.Logger) *SendPhotoTool {
	return &SendPhotoTool{sender: sender, logger: logger}
}

func (t *SendPhotoTool) Name() string        { return "send_photo" }
func (t *SendPhotoTool) Kind() domaintool.Kind { return domaintool.KindCommunicate }
func (t *SendPhotoTool) Description() string {
	return `Send a photo to the current Telegram chat. Accepts local file path or HTTP(S) URL.
Use this when the user requests an image, chart, screenshot, or any visual content.
The photo will be sent directly to the chat as a Telegram photo message.`
}

func (t *SendPhotoTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Local file path or HTTP(S) URL of the photo to send",
			},
			"caption": map[string]interface{}{
				"type":        "string",
				"description": "Optional caption for the photo (supports Markdown)",
			},
		},
		"required": []string{"path"},
	}
}

func (t *SendPhotoTool) Execute(ctx context.Context, args map[string]interface{}) (*domaintool.Result, error) {
	path, _ := args["path"].(string)
	caption, _ := args["caption"].(string)

	if path == "" {
		return &domaintool.Result{Success: false, Error: "path is required"}, nil
	}

	chatID := chatIDFromContext(ctx)
	if chatID == 0 {
		return &domaintool.Result{
			Success: false,
			Error:   "send_photo is only available in Telegram mode (no chatID in context)",
		}, nil
	}

	t.logger.Info("Sending photo via TG",
		zap.Int64("chat_id", chatID),
		zap.String("path", path),
		zap.Bool("is_url", strings.HasPrefix(path, "http")),
	)

	if err := t.sender.SendPhoto(chatID, path, caption); err != nil {
		return &domaintool.Result{
			Success: false,
			Error:   fmt.Sprintf("Failed to send photo: %v", err),
		}, nil
	}

	return &domaintool.Result{
		Output:  fmt.Sprintf("Photo sent successfully to chat %d", chatID),
		Success: true,
		Metadata: map[string]interface{}{
			"chat_id": chatID,
			"path":    path,
		},
	}, nil
}

// ──────────────────────────────────────────────────────────────
// SendMediaGroupTool — send_media_group
// ──────────────────────────────────────────────────────────────

// SendMediaGroupTool sends 2-10 photos as a Telegram album (media group).
type SendMediaGroupTool struct {
	sender MediaSender
	logger *zap.Logger
}

func NewSendMediaGroupTool(sender MediaSender, logger *zap.Logger) *SendMediaGroupTool {
	return &SendMediaGroupTool{sender: sender, logger: logger}
}

func (t *SendMediaGroupTool) Name() string        { return "send_media_group" }
func (t *SendMediaGroupTool) Kind() domaintool.Kind { return domaintool.KindCommunicate }
func (t *SendMediaGroupTool) Description() string {
	return `Send multiple photos as a Telegram album (media group). Accepts 2-10 photos.
Use this when the user wants to see multiple images at once as a grouped album.
Each photo can be a local file path or HTTP(S) URL.
The photos will be displayed as a single album in Telegram.`
}

func (t *SendMediaGroupTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"photos": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "string",
				},
				"minItems":    2,
				"maxItems":    10,
				"description": "Array of 2-10 local file paths or HTTP(S) URLs of photos to send as an album",
			},
			"caption": map[string]interface{}{
				"type":        "string",
				"description": "Optional caption for the album (shown under the first photo, supports Markdown)",
			},
		},
		"required": []string{"photos"},
	}
}

func (t *SendMediaGroupTool) Execute(ctx context.Context, args map[string]interface{}) (*domaintool.Result, error) {
	caption, _ := args["caption"].(string)

	// Parse photos array
	rawPhotos, ok := args["photos"]
	if !ok {
		return &domaintool.Result{Success: false, Error: "photos is required"}, nil
	}

	var photos []string
	switch v := rawPhotos.(type) {
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				photos = append(photos, s)
			}
		}
	case []string:
		photos = v
	default:
		return &domaintool.Result{Success: false, Error: "photos must be an array of strings"}, nil
	}

	if len(photos) < 2 {
		return &domaintool.Result{Success: false, Error: "media group requires at least 2 photos"}, nil
	}
	if len(photos) > 10 {
		return &domaintool.Result{Success: false, Error: "media group supports at most 10 photos"}, nil
	}

	chatID := chatIDFromContext(ctx)
	if chatID == 0 {
		return &domaintool.Result{
			Success: false,
			Error:   "send_media_group is only available in Telegram mode (no chatID in context)",
		}, nil
	}

	t.logger.Info("Sending media group via TG",
		zap.Int64("chat_id", chatID),
		zap.Int("photo_count", len(photos)),
	)

	if err := t.sender.SendMediaGroup(chatID, photos, caption); err != nil {
		return &domaintool.Result{
			Success: false,
			Error:   fmt.Sprintf("Failed to send media group: %v", err),
		}, nil
	}

	return &domaintool.Result{
		Output:  fmt.Sprintf("Media group (%d photos) sent successfully to chat %d", len(photos), chatID),
		Success: true,
		Metadata: map[string]interface{}{
			"chat_id":     chatID,
			"photo_count": len(photos),
		},
	}, nil
}

// ──────────────────────────────────────────────────────────────
// SendDocumentTool — send_document
// ──────────────────────────────────────────────────────────────

// SendDocumentTool sends a file/document to the current Telegram chat.
type SendDocumentTool struct {
	sender MediaSender
	logger *zap.Logger
}

func NewSendDocumentTool(sender MediaSender, logger *zap.Logger) *SendDocumentTool {
	return &SendDocumentTool{sender: sender, logger: logger}
}

func (t *SendDocumentTool) Name() string        { return "send_document" }
func (t *SendDocumentTool) Kind() domaintool.Kind { return domaintool.KindCommunicate }
func (t *SendDocumentTool) Description() string {
	return `Send a document/file to the current Telegram chat. Accepts local file path.
Use this when the user requests a file download, report, log, or any non-image file.
Supports any file type: PDF, CSV, ZIP, text, code files, etc.`
}

func (t *SendDocumentTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Local file path of the document to send",
			},
			"caption": map[string]interface{}{
				"type":        "string",
				"description": "Optional caption for the document",
			},
		},
		"required": []string{"path"},
	}
}

func (t *SendDocumentTool) Execute(ctx context.Context, args map[string]interface{}) (*domaintool.Result, error) {
	path, _ := args["path"].(string)
	caption, _ := args["caption"].(string)

	if path == "" {
		return &domaintool.Result{Success: false, Error: "path is required"}, nil
	}

	chatID := chatIDFromContext(ctx)
	if chatID == 0 {
		return &domaintool.Result{
			Success: false,
			Error:   "send_document is only available in Telegram mode (no chatID in context)",
		}, nil
	}

	t.logger.Info("Sending document via TG",
		zap.Int64("chat_id", chatID),
		zap.String("path", path),
	)

	if err := t.sender.SendDocument(chatID, path, caption); err != nil {
		return &domaintool.Result{
			Success: false,
			Error:   fmt.Sprintf("Failed to send document: %v", err),
		}, nil
	}

	return &domaintool.Result{
		Output:  fmt.Sprintf("Document sent successfully to chat %d", chatID),
		Success: true,
		Metadata: map[string]interface{}{
			"chat_id": chatID,
			"path":    path,
		},
	}, nil
}
