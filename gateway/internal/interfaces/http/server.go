package http

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ngoclaw/ngoclaw/gateway/internal/application/usecase"
	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/service"
	"github.com/ngoclaw/ngoclaw/gateway/internal/infrastructure/prompt"
	"github.com/ngoclaw/ngoclaw/gateway/internal/interfaces/http/handlers"
	"go.uber.org/zap"
)

// Server HTTP服务器
type Server struct {
	server *http.Server
	logger *zap.Logger
}

// Config HTTP服务器配置
type Config struct {
	Host string
	Port int
	Mode string // debug, release
}

// NewServer 创建HTTP服务器
func NewServer(cfg Config, uc *usecase.ProcessMessageUseCase, agentLoop *service.AgentLoop, toolExec service.ToolExecutor, promptEngine *prompt.PromptEngine, logger *zap.Logger) *Server {
	// 设置Gin模式
	if cfg.Mode == "production" {
		gin.SetMode(gin.ReleaseMode)
	} else {
		gin.SetMode(gin.DebugMode)
	}

	// 创建路由
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(ginLogger(logger))

	// 初始化处理器
	messageHandler := handlers.NewMessageHandler(uc, logger)
	openaiHandler := handlers.NewOpenAIHandler(uc, logger, nil)
	var agentHandler *handlers.AgentHandler
	if agentLoop != nil {
		agentHandler = handlers.NewAgentHandler(agentLoop, toolExec, promptEngine, logger)
	}

	// 注册路由
	setupRoutes(router, messageHandler, openaiHandler, agentHandler)

	// 创建HTTP服务器
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	server := &http.Server{
		Addr:    addr,
		Handler: router,
	}

	return &Server{
		server: server,
		logger: logger,
	}
}

// Start 启动服务器
func (s *Server) Start(ctx context.Context) error {
	s.logger.Info("Starting HTTP server", zap.String("address", s.server.Addr))

	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("HTTP server error", zap.Error(err))
		}
	}()

	return nil
}

// Stop 停止服务器
func (s *Server) Stop(ctx context.Context) error {
	s.logger.Info("Stopping HTTP server")
	return s.server.Shutdown(ctx)
}

// setupRoutes 设置路由
func setupRoutes(router *gin.Engine, messageHandler *handlers.MessageHandler, openaiHandler *handlers.OpenAIHandler, agentHandler *handlers.AgentHandler) {
	// 健康检查
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
			"time":   time.Now().Unix(),
		})
	})

	// API版本1
	v1 := router.Group("/api/v1")
	{
		v1.GET("/ping", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"message": "pong",
			})
		})

		v1.POST("/messages", messageHandler.SendMessage)

		// Agent Loop endpoints (SSE streaming)
		if agentHandler != nil {
			v1.POST("/agent", agentHandler.RunAgent)
			v1.GET("/agent/tools", agentHandler.GetTools)
		}
	}

	// OpenAI-compatible API
	oai := router.Group("/v1")
	{
		oai.POST("/chat/completions", openaiHandler.ChatCompletions)
		oai.GET("/models", openaiHandler.ListModels)
	}
}

// ginLogger Gin日志中间件
func ginLogger(logger *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		latency := time.Since(start)
		statusCode := c.Writer.Status()

		logger.Info("HTTP request",
			zap.String("method", c.Request.Method),
			zap.String("path", path),
			zap.String("query", query),
			zap.Int("status", statusCode),
			zap.Duration("latency", latency),
			zap.String("ip", c.ClientIP()),
		)
	}
}
