package handlers

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ngoclaw/ngoclaw/gateway/internal/application/usecase"
	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/entity"
	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/valueobject"
	"go.uber.org/zap"
)

type MessageHandler struct {
	processMessageUseCase *usecase.ProcessMessageUseCase
	logger                *zap.Logger
}

func NewMessageHandler(uc *usecase.ProcessMessageUseCase, logger *zap.Logger) *MessageHandler {
	return &MessageHandler{
		processMessageUseCase: uc,
		logger:                logger,
	}
}

type SendMessageRequest struct {
	Content        string `json:"content" binding:"required"`
	ConversationID string `json:"conversation_id" binding:"required"`
	UserID         string `json:"user_id" binding:"required"`
	UserName       string `json:"user_name"`
}

type SendMessageResponse struct {
	MessageID      string                 `json:"message_id"`
	Content        string                 `json:"content"`
	ConversationID string                 `json:"conversation_id"`
	Role           string                 `json:"role"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
}

func (h *MessageHandler) SendMessage(c *gin.Context) {
	var req SendMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Create User
	user := valueobject.NewUser(
		req.UserID,
		req.UserName,
		"user",
	)

	// Create Message Content
	content := valueobject.NewMessageContent(
		req.Content,
		valueobject.ContentTypeText,
	)

	// Create Domain Message
	msgID := fmt.Sprintf("msg_%d", time.Now().UnixNano())
	message, err := entity.NewMessage(
		msgID,
		req.ConversationID,
		content,
		user,
	)
	if err != nil {
		h.logger.Error("Failed to create message entity", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create message"})
		return
	}

	// Process Message
	responseMsg, err := h.processMessageUseCase.Execute(c.Request.Context(), message)
	if err != nil {
		h.logger.Error("Failed to process message", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process message"})
		return
	}

	// Construct Response
	resp := SendMessageResponse{
		MessageID:      responseMsg.ID(),
		Content:        responseMsg.Content().Text(),
		ConversationID: responseMsg.ConversationID(),
		Role:           "assistant", // Assuming bot response is assistant
		Metadata:       responseMsg.Metadata(),
	}

	c.JSON(http.StatusOK, resp)
}
