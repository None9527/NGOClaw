package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ngoclaw/ngoclaw/gateway/internal/application/usecase"
	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/entity"
	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/valueobject"
	"go.uber.org/zap"
)

// OpenAIHandler implements OpenAI Chat Completions compatible API
type OpenAIHandler struct {
	usecase *usecase.ProcessMessageUseCase
	logger  *zap.Logger
	models  []OpenAIModel
}

// OpenAI API types

// ChatCompletionRequest mirrors OpenAI's request format
type ChatCompletionRequest struct {
	Model       string             `json:"model" binding:"required"`
	Messages    []ChatMessage      `json:"messages" binding:"required"`
	Temperature *float64           `json:"temperature,omitempty"`
	MaxTokens   *int               `json:"max_tokens,omitempty"`
	Stream      bool               `json:"stream,omitempty"`
	User        string             `json:"user,omitempty"`
}

// ChatMessage represents a message in the conversation
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatCompletionResponse mirrors OpenAI's response format
type ChatCompletionResponse struct {
	ID                string              `json:"id"`
	Object            string              `json:"object"`
	Created           int64               `json:"created"`
	Model             string              `json:"model"`
	Choices           []ChatChoice        `json:"choices"`
	Usage             *ChatUsage          `json:"usage,omitempty"`
	SystemFingerprint string              `json:"system_fingerprint,omitempty"`
}

// ChatChoice represents a completion choice
type ChatChoice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

// ChatUsage represents token usage
type ChatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ChatStreamChunk represents a streaming chunk
type ChatStreamChunk struct {
	ID                string              `json:"id"`
	Object            string              `json:"object"`
	Created           int64               `json:"created"`
	Model             string              `json:"model"`
	Choices           []ChatStreamChoice  `json:"choices"`
	SystemFingerprint string              `json:"system_fingerprint,omitempty"`
}

// ChatStreamChoice represents a streaming choice delta
type ChatStreamChoice struct {
	Index        int              `json:"index"`
	Delta        ChatStreamDelta  `json:"delta"`
	FinishReason *string          `json:"finish_reason"`
}

// ChatStreamDelta represents the delta in a streaming choice
type ChatStreamDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

// OpenAIModel represents a model in the /v1/models response
type OpenAIModel struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// ModelsResponse mirrors OpenAI's models list response
type ModelsResponse struct {
	Object string        `json:"object"`
	Data   []OpenAIModel `json:"data"`
}

// NewOpenAIHandler creates a new OpenAI-compatible handler
func NewOpenAIHandler(uc *usecase.ProcessMessageUseCase, logger *zap.Logger, models []OpenAIModel) *OpenAIHandler {
	if len(models) == 0 {
		// Default model list
		models = []OpenAIModel{
			{ID: "ngoclaw", Object: "model", Created: time.Now().Unix(), OwnedBy: "ngoclaw"},
		}
	}
	return &OpenAIHandler{
		usecase: uc,
		logger:  logger,
		models:  models,
	}
}

// ChatCompletions handles POST /v1/chat/completions
func (h *OpenAIHandler) ChatCompletions(c *gin.Context) {
	var req ChatCompletionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"message": err.Error(),
				"type":    "invalid_request_error",
			},
		})
		return
	}

	if len(req.Messages) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"message": "messages array must not be empty",
				"type":    "invalid_request_error",
			},
		})
		return
	}

	// Extract the last user message as the prompt
	lastMsg := req.Messages[len(req.Messages)-1]

	if req.Stream {
		h.handleStream(c, &req, lastMsg)
		return
	}

	h.handleNonStream(c, &req, lastMsg)
}

// handleNonStream processes non-streaming chat completions
func (h *OpenAIHandler) handleNonStream(c *gin.Context, req *ChatCompletionRequest, lastMsg ChatMessage) {
	userID := req.User
	if userID == "" {
		userID = "openai_api"
	}

	user := valueobject.NewUser(userID, userID, "openai_api")
	content := valueobject.NewMessageContent(lastMsg.Content, valueobject.ContentTypeText)

	msgID := fmt.Sprintf("oai_%d", time.Now().UnixNano())
	convID := fmt.Sprintf("oai_conv_%s_%d", userID, time.Now().UnixNano())

	msg, err := entity.NewMessage(msgID, convID, content, user)
	if err != nil {
		h.logger.Error("Failed to create message", zap.Error(err))
		c.JSON(http.StatusInternalServerError, h.errorResponse("Failed to create message", "server_error"))
		return
	}

	response, err := h.usecase.Execute(c.Request.Context(), msg)
	if err != nil {
		h.logger.Error("Failed to process message", zap.Error(err))
		c.JSON(http.StatusInternalServerError, h.errorResponse(err.Error(), "server_error"))
		return
	}

	responseText := ""
	if response != nil {
		responseText = response.Content().Text()
	}

	c.JSON(http.StatusOK, ChatCompletionResponse{
		ID:      fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   req.Model,
		Choices: []ChatChoice{
			{
				Index: 0,
				Message: ChatMessage{
					Role:    "assistant",
					Content: responseText,
				},
				FinishReason: "stop",
			},
		},
		Usage: &ChatUsage{
			PromptTokens:     len(lastMsg.Content) / 4, // rough estimate
			CompletionTokens: len(responseText) / 4,
			TotalTokens:      (len(lastMsg.Content) + len(responseText)) / 4,
		},
	})
}

// handleStream processes streaming chat completions (SSE)
func (h *OpenAIHandler) handleStream(c *gin.Context, req *ChatCompletionRequest, lastMsg ChatMessage) {
	userID := req.User
	if userID == "" {
		userID = "openai_api"
	}

	user := valueobject.NewUser(userID, userID, "openai_api")
	content := valueobject.NewMessageContent(lastMsg.Content, valueobject.ContentTypeText)

	msgID := fmt.Sprintf("oai_%d", time.Now().UnixNano())
	convID := fmt.Sprintf("oai_conv_%s_%d", userID, time.Now().UnixNano())

	msg, err := entity.NewMessage(msgID, convID, content, user)
	if err != nil {
		h.logger.Error("Failed to create message", zap.Error(err))
		c.JSON(http.StatusInternalServerError, h.errorResponse("Failed to create message", "server_error"))
		return
	}

	// Set SSE headers
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	completionID := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	created := time.Now().Unix()

	// Send role delta first
	h.writeSSEChunk(c.Writer, ChatStreamChunk{
		ID:      completionID,
		Object:  "chat.completion.chunk",
		Created: created,
		Model:   req.Model,
		Choices: []ChatStreamChoice{
			{
				Index: 0,
				Delta: ChatStreamDelta{Role: "assistant"},
			},
		},
	})
	c.Writer.Flush()

	// Execute and stream
	response, err := h.usecase.Execute(c.Request.Context(), msg)
	if err != nil {
		h.logger.Error("Failed to process message", zap.Error(err))
		return
	}

	responseText := ""
	if response != nil {
		responseText = response.Content().Text()
	}

	// Send content chunks (split by sentences for realistic streaming)
	chunks := splitIntoChunks(responseText, 50)
	for _, chunk := range chunks {
		h.writeSSEChunk(c.Writer, ChatStreamChunk{
			ID:      completionID,
			Object:  "chat.completion.chunk",
			Created: created,
			Model:   req.Model,
			Choices: []ChatStreamChoice{
				{
					Index: 0,
					Delta: ChatStreamDelta{Content: chunk},
				},
			},
		})
		c.Writer.Flush()
	}

	// Send finish chunk
	finishReason := "stop"
	h.writeSSEChunk(c.Writer, ChatStreamChunk{
		ID:      completionID,
		Object:  "chat.completion.chunk",
		Created: created,
		Model:   req.Model,
		Choices: []ChatStreamChoice{
			{
				Index:        0,
				Delta:        ChatStreamDelta{},
				FinishReason: &finishReason,
			},
		},
	})
	c.Writer.Flush()

	// Send [DONE]
	io.WriteString(c.Writer, "data: [DONE]\n\n")
	c.Writer.Flush()
}

// ListModels handles GET /v1/models
func (h *OpenAIHandler) ListModels(c *gin.Context) {
	c.JSON(http.StatusOK, ModelsResponse{
		Object: "list",
		Data:   h.models,
	})
}

// writeSSEChunk writes a single SSE event
func (h *OpenAIHandler) writeSSEChunk(w io.Writer, chunk ChatStreamChunk) {
	data, err := json.Marshal(chunk)
	if err != nil {
		h.logger.Error("Failed to marshal SSE chunk", zap.Error(err))
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
}

// errorResponse constructs an OpenAI-compatible error
func (h *OpenAIHandler) errorResponse(message, errType string) gin.H {
	return gin.H{
		"error": gin.H{
			"message": message,
			"type":    errType,
		},
	}
}

// splitIntoChunks splits text into chunks of approximately maxLen characters,
// preferring to split at word boundaries
func splitIntoChunks(text string, maxLen int) []string {
	if len(text) <= maxLen {
		return []string{text}
	}

	var chunks []string
	words := strings.Fields(text)
	var current strings.Builder

	for _, word := range words {
		if current.Len() > 0 && current.Len()+1+len(word) > maxLen {
			chunks = append(chunks, current.String()+" ")
			current.Reset()
		}
		if current.Len() > 0 {
			current.WriteString(" ")
		}
		current.WriteString(word)
	}
	if current.Len() > 0 {
		chunks = append(chunks, current.String())
	}
	return chunks
}
