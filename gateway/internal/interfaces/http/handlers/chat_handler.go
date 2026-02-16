package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// ChatHandler 对话 API 处理器
type ChatHandler struct {
	sessionManager SessionManager
	logger         *zap.Logger
}

// SessionManager 会话管理接口
type SessionManager interface {
	GetOrCreate(userID, channelType, channelID string) (Session, error)
	Get(channelType, channelID string) (Session, bool)
	Delete(channelType, channelID string)
	Clear(channelType, channelID string) error
	SetModel(channelType, channelID, model, provider string) error
}

// Session 会话接口
type Session interface {
	Run(ctx interface{}, message string) (string, error)
	GetID() string
	GetMessages() []Message
}

// Message 消息接口
type Message interface {
	GetRole() string
	GetContent() string
}

// NewChatHandler 创建对话处理器
func NewChatHandler(sessionManager SessionManager, logger *zap.Logger) *ChatHandler {
	return &ChatHandler{
		sessionManager: sessionManager,
		logger:         logger,
	}
}

// CreateChatRequest 创建对话请求
type CreateChatRequest struct {
	UserID   string `json:"user_id" binding:"required"`
	Model    string `json:"model"`
	Provider string `json:"provider"`
}

// ChatMessageRequest 发送消息请求
type ChatMessageRequest struct {
	Message string `json:"message" binding:"required"`
	Stream  bool   `json:"stream"`
}

// ChatResponse 对话响应
type ChatResponse struct {
	SessionID string              `json:"session_id"`
	Message   string              `json:"message"`
	Model     string              `json:"model,omitempty"`
	Timestamp int64               `json:"timestamp"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// MessageItem 消息项
type MessageItem struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	Timestamp int64  `json:"timestamp,omitempty"`
}

// CreateChat 创建对话会话
// POST /api/v1/chat
func (h *ChatHandler) CreateChat(c *gin.Context) {
	var req CreateChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	sessionID := "http:" + req.UserID + ":" + strconv.FormatInt(time.Now().UnixNano(), 10)

	session, err := h.sessionManager.GetOrCreate(req.UserID, "http", sessionID)
	if err != nil {
		h.logger.Error("Failed to create session", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create session"})
		return
	}

	// 设置模型 (如果指定)
	if req.Model != "" {
		h.sessionManager.SetModel("http", sessionID, req.Model, req.Provider)
	}

	c.JSON(http.StatusCreated, gin.H{
		"session_id": session.GetID(),
		"user_id":    req.UserID,
		"model":      req.Model,
		"created_at": time.Now().Unix(),
	})
}

// SendMessage 发送消息
// POST /api/v1/chat/:session_id/messages
func (h *ChatHandler) SendMessage(c *gin.Context) {
	sessionID := c.Param("session_id")

	var req ChatMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	session, exists := h.sessionManager.Get("http", sessionID)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}

	// 非流式响应
	response, err := session.Run(c.Request.Context(), req.Message)
	if err != nil {
		h.logger.Error("Failed to process message", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, ChatResponse{
		SessionID: session.GetID(),
		Message:   response,
		Timestamp: time.Now().Unix(),
	})
}

// GetMessages 获取会话消息历史
// GET /api/v1/chat/:session_id/messages
func (h *ChatHandler) GetMessages(c *gin.Context) {
	sessionID := c.Param("session_id")

	session, exists := h.sessionManager.Get("http", sessionID)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}

	messages := session.GetMessages()
	items := make([]MessageItem, 0, len(messages))
	for _, msg := range messages {
		items = append(items, MessageItem{
			Role:    msg.GetRole(),
			Content: msg.GetContent(),
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"session_id": session.GetID(),
		"messages":   items,
		"count":      len(items),
	})
}

// ClearChat 清除会话历史
// DELETE /api/v1/chat/:session_id/messages
func (h *ChatHandler) ClearChat(c *gin.Context) {
	sessionID := c.Param("session_id")

	if err := h.sessionManager.Clear("http", sessionID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "chat history cleared",
	})
}

// DeleteChat 删除会话
// DELETE /api/v1/chat/:session_id
func (h *ChatHandler) DeleteChat(c *gin.Context) {
	sessionID := c.Param("session_id")

	h.sessionManager.Delete("http", sessionID)

	c.JSON(http.StatusOK, gin.H{
		"message": "session deleted",
	})
}

// SetModel 设置会话模型
// PUT /api/v1/chat/:session_id/model
func (h *ChatHandler) SetModel(c *gin.Context) {
	sessionID := c.Param("session_id")

	var req struct {
		Model    string `json:"model" binding:"required"`
		Provider string `json:"provider"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.sessionManager.SetModel("http", sessionID, req.Model, req.Provider); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"model":    req.Model,
		"provider": req.Provider,
	})
}

// ListModels 列出可用模型
// GET /api/v1/models
func (h *ChatHandler) ListModels(c *gin.Context) {
	models := []gin.H{
		{"id": "gemini-2.0-flash", "provider": "antigravity", "name": "Gemini 2.0 Flash"},
		{"id": "claude-3-5-sonnet", "provider": "antigravity", "name": "Claude 3.5 Sonnet"},
		{"id": "gpt-4o", "provider": "antigravity", "name": "GPT-4o"},
		{"id": "minimax-abab7", "provider": "minimax", "name": "MiniMax ABAB7"},
	}

	c.JSON(http.StatusOK, gin.H{
		"models": models,
	})
}

// HealthCheck 健康检查
// GET /api/v1/health
func (h *ChatHandler) HealthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":    "ok",
		"timestamp": time.Now().Unix(),
		"version":   "1.0.0",
	})
}
