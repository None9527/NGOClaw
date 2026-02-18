package application

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ngoclaw/ngoclaw/gateway/internal/application/usecase"
	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/entity"
	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/repository"
	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/service"
	domaintool "github.com/ngoclaw/ngoclaw/gateway/internal/domain/tool"
	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/valueobject"
	"github.com/ngoclaw/ngoclaw/gateway/internal/infrastructure/config"
	"github.com/ngoclaw/ngoclaw/gateway/internal/infrastructure/llm"
	_ "github.com/ngoclaw/ngoclaw/gateway/internal/infrastructure/llm/anthropic" // register anthropic provider factory
	_ "github.com/ngoclaw/ngoclaw/gateway/internal/infrastructure/llm/gemini"    // register gemini provider factory
	_ "github.com/ngoclaw/ngoclaw/gateway/internal/infrastructure/llm/openai"    // register openai provider factory
	"github.com/ngoclaw/ngoclaw/gateway/internal/infrastructure/persistence"
	"github.com/ngoclaw/ngoclaw/gateway/internal/infrastructure/prompt"
	"github.com/ngoclaw/ngoclaw/gateway/internal/infrastructure/sandbox"
	toolpkg "github.com/ngoclaw/ngoclaw/gateway/internal/infrastructure/tool"
	"github.com/ngoclaw/ngoclaw/gateway/internal/interfaces/agentgrpc"
	httpServer "github.com/ngoclaw/ngoclaw/gateway/internal/interfaces/http"
	"github.com/ngoclaw/ngoclaw/gateway/internal/interfaces/telegram"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// App 应用程序
type App struct {
	// 配置
	config *config.Config
	logger *zap.Logger
	db     *gorm.DB

	// 仓储层
	agentRepo   repository.AgentRepository
	messageRepo repository.MessageRepository

	// 领域服务
	agentSelector service.AgentSelector
	messageRouter service.MessageRouter

	// 应用服务
	processMessageUseCase *usecase.ProcessMessageUseCase

	// 基础设施
	toolRegistry    domaintool.Registry
	toolExecutor    *toolpkg.Executor
	llmRouter       *llm.Router
	mcpManager      *toolpkg.MCPManager
	agentLoop       *service.AgentLoop
	securityHook    *service.SecurityHook
	grpcAgentSrv    *agentgrpc.Server
	telegramAdapter *telegram.Adapter
	httpServer      *httpServer.Server

	// 记忆系统


	// Prompt 引擎
	promptEngine   *prompt.PromptEngine
}

// NewApp 创建应用程序（依赖注入容器）
func NewApp(cfg *config.Config, logger *zap.Logger) (*App, error) {
	// Bootstrap: ensure ~/.ngoclaw/ exists with default files on first run
	if err := config.Bootstrap(logger); err != nil {
		logger.Warn("Bootstrap failed (non-fatal)", zap.Error(err))
	}

	app := &App{
		config: cfg,
		logger: logger,
	}

	// 初始化各层组件
	if err := app.initRepositories(); err != nil {
		return nil, fmt.Errorf("failed to init repositories: %w", err)
	}

	if err := app.initDomainServices(); err != nil {
		return nil, fmt.Errorf("failed to init domain services: %w", err)
	}

	if err := app.initInfrastructure(); err != nil {
		return nil, fmt.Errorf("failed to init infrastructure: %w", err)
	}

	if err := app.initApplicationServices(); err != nil {
		return nil, fmt.Errorf("failed to init application services: %w", err)
	}

	if err := app.initInterfaces(); err != nil {
		return nil, fmt.Errorf("failed to init interfaces: %w", err)
	}

	// 初始化默认数据
	if err := app.seedData(); err != nil {
		return nil, fmt.Errorf("failed to seed data: %w", err)
	}

	return app, nil
}

// NewAppCLI creates a lightweight app for CLI mode.
// Only initializes: DB (silent), Tools, LLM Router, AgentLoop, PromptEngine.
// Skips: HTTP server, Telegram, gRPC, seed data.
func NewAppCLI(cfg *config.Config, logger *zap.Logger) (*App, error) {
	if err := config.Bootstrap(logger); err != nil {
		logger.Warn("Bootstrap failed (non-fatal)", zap.Error(err))
	}

	app := &App{
		config: cfg,
		logger: logger,
	}

	// DB with silent logging (no SQL spam)
	if err := app.initRepositoriesSilent(); err != nil {
		return nil, fmt.Errorf("failed to init repositories: %w", err)
	}

	if err := app.initDomainServices(); err != nil {
		return nil, fmt.Errorf("failed to init domain services: %w", err)
	}

	if err := app.initInfrastructure(); err != nil {
		return nil, fmt.Errorf("failed to init infrastructure: %w", err)
	}

	if err := app.initApplicationServices(); err != nil {
		return nil, fmt.Errorf("failed to init application services: %w", err)
	}

	// No initInterfaces (HTTP/TG/gRPC) — CLI doesn't need servers
	// No seedData — avoid noisy DB writes on every CLI launch
	return app, nil
}

// initRepositories 初始化仓储层
func (app *App) initRepositories() error {
	app.logger.Info("Initializing repositories")

	// 连接数据库
	db, err := persistence.NewDBConnection(&app.config.Database)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	app.db = db

	// 初始化 GORM 仓储
	app.agentRepo = persistence.NewGormAgentRepository(db)
	app.messageRepo = persistence.NewGormMessageRepository(db)

	return nil
}

// initRepositoriesSilent initializes repos with silent DB logging (for CLI mode)
func (app *App) initRepositoriesSilent() error {
	db, err := persistence.NewDBConnectionSilent(&app.config.Database)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	app.db = db
	app.agentRepo = persistence.NewGormAgentRepository(db)
	app.messageRepo = persistence.NewGormMessageRepository(db)
	return nil
}

// initDomainServices 初始化领域服务
func (app *App) initDomainServices() error {
	app.logger.Info("Initializing domain services")

	// 代理选择器
	app.agentSelector = service.NewDefaultAgentSelector(app.agentRepo)

	// 消息路由器
	app.messageRouter = service.NewDefaultMessageRouter(app.agentSelector)

	return nil
}

// initInfrastructure 初始化基础设施
func (app *App) initInfrastructure() error {
	app.logger.Info("Initializing infrastructure")

	// Tool Registry + Executor
	app.toolRegistry = domaintool.NewInMemoryRegistry()
	homeDir, _ := os.UserHomeDir()
	systemSkillsDir := filepath.Join(homeDir, ".ngoclaw", "skills")

	// Workspace-level skills (project-specific overrides)
	workspaceDir := app.config.Agent.Workspace
	skillsDirs := []string{systemSkillsDir}
	if workspaceDir != "" {
		wsSkillsDir := filepath.Join(workspaceDir, ".ngoclaw", "skills")
		skillsDirs = append(skillsDirs, wsSkillsDir)
	}

	sbxCfg := sandbox.DefaultConfig()
	sbxCfg.PythonEnv = app.config.PythonEnv
	if app.config.Agent.Runtime.ToolTimeout > 0 {
		sbxCfg.Timeout = app.config.Agent.Runtime.ToolTimeout
	}
	sbx, sbxErr := sandbox.NewProcessSandbox(sbxCfg, app.logger)
	if sbxErr != nil {
		app.logger.Warn("Sandbox init failed, tools will run unsandboxed", zap.Error(sbxErr))
	}

	// Executor (只负责执行，不再负责注册)
	app.toolExecutor = toolpkg.NewExecutor(
		app.toolRegistry,
		&domaintool.Policy{Profile: "full"},
		sbx, nil, app.logger,
	)

	// LLM Router (modular provider factory with failover)
	// NOTE: must be initialized BEFORE RegisterAllTools because sub_agent depends on it.
	app.llmRouter = llm.NewRouter(app.logger)
	for _, p := range app.config.Agent.Providers {
		provider, err := llm.CreateProvider(llm.ProviderConfig{
			Name:     p.Name,
			Type:     p.Type,
			BaseURL:  p.BaseURL,
			APIKey:   p.APIKey,
			Models:   p.Models,
			Priority: p.Priority,
		}, app.logger)
		if err != nil {
			app.logger.Error("Failed to create LLM provider",
				zap.String("name", p.Name),
				zap.String("type", p.Type),
				zap.Error(err),
			)
			continue
		}
		app.llmRouter.AddProvider(provider)
	}
	app.logger.Info("LLM Router initialized",
		zap.Int("providers", len(app.config.Agent.Providers)),
	)

	// MCP Manager (hot-pluggable, reads ~/.ngoclaw/mcp.json)
	homeDir, _ = os.UserHomeDir()
	mcpConfigPath := filepath.Join(homeDir, ".ngoclaw", "mcp.json")
	app.mcpManager = toolpkg.NewMCPManager(mcpConfigPath, app.toolRegistry, app.logger)

	// ── Unified Tool Registration (single entry point) ──
	subMaxSteps := app.config.Agent.Runtime.SubAgentMaxSteps
	if subMaxSteps <= 0 {
		subMaxSteps = 25
	}
	// Pick first available provider for research LLM summarization
	var researchURL, researchKey, researchModel string
	if len(app.config.Agent.Providers) > 0 {
		p := app.config.Agent.Providers[0]
		researchURL = p.BaseURL
		researchKey = p.APIKey
		if len(p.Models) > 0 {
			// Strip provider prefix (e.g. "bailian/qwen3-coder-plus" -> "qwen3-coder-plus")
			model := p.Models[0]
			if idx := strings.Index(model, "/"); idx >= 0 {
				model = model[idx+1:]
			}
			researchModel = model
		}
	}

	toolpkg.RegisterAllTools(toolpkg.ToolLayerDeps{
		Registry:         app.toolRegistry,
		Sandbox:          sbx,
		SkillExec:        nil,
		PythonEnv:        app.config.PythonEnv,
		SkillsDir:        systemSkillsDir,
		ResearchLLMURL:   researchURL,
		ResearchLLMKey:   researchKey,
		ResearchLLMModel: researchModel,
		Workspace:        app.config.Agent.Workspace,
		MCPManager:       app.mcpManager,
		SubAgent: &toolpkg.SubAgentDeps{
			LLMClient:    app.llmRouter,
			ToolExecutor: &toolBridge{registry: app.toolRegistry},
			DefaultModel: app.config.Agent.DefaultModel,
			MaxSteps:     subMaxSteps,
			Timeout:      app.config.Agent.Runtime.SubAgentTimeout,
		},
		Logger: app.logger,
	})


	// Prompt Engine (hot-pluggable system prompt assembly — System + Workspace layers)
	app.promptEngine = prompt.NewPromptEngine(app.config.Agent.Workspace, app.logger)
	if err := app.promptEngine.Discover(); err != nil {
		app.logger.Warn("Prompt engine discovery failed, will use empty system prompt",
			zap.Error(err),
		)
	}

	return nil
}

// initApplicationServices 初始化应用服务
func (app *App) initApplicationServices() error {
	app.logger.Info("Initializing application services")

	// ProcessMessageUseCase (legacy HTTP/REPL path — uses llmRouter directly)
	app.processMessageUseCase = usecase.NewProcessMessageUseCase(
		app.messageRepo,
		app.messageRouter,
		app.llmRouter,
		app.logger,
	)

	// Agent Loop (ReAct Engine) — uses LLM Router + Tool Bridge
	loopTools := &toolBridge{registry: app.toolRegistry}


	loopCfg := service.DefaultAgentLoopConfig()
	loopCfg.Model = app.config.Agent.DefaultModel

	// Bridge per-model policy overrides from config.yaml
	if len(app.config.Agent.ModelPolicies) > 0 {
		loopCfg.ModelPolicies = make(map[string]*service.ModelPolicyOverride)
		for key, cfgPolicy := range app.config.Agent.ModelPolicies {
			override := &service.ModelPolicyOverride{
				RepairToolPairing:   cfgPolicy.RepairToolPairing,
				EnforceTurnOrdering: cfgPolicy.EnforceTurnOrdering,
				ReasoningFormat:     cfgPolicy.ReasoningFormat,
				ProgressInterval:    cfgPolicy.ProgressInterval,
				ProgressEscalation:  cfgPolicy.ProgressEscalation,
				PromptStyle:         cfgPolicy.PromptStyle,
				SystemRoleSupport:   cfgPolicy.SystemRoleSupport,
				ThinkingTagHint:     cfgPolicy.ThinkingTagHint,
			}
			loopCfg.ModelPolicies[key] = override
		}
	}
	if app.config.Agent.Guardrails.LoopDetectThreshold > 0 {
		loopCfg.DoomLoopThreshold = app.config.Agent.Guardrails.LoopDetectThreshold
	}
	if app.config.Agent.Guardrails.LoopNameThreshold > 0 {
		loopCfg.LoopNameThreshold = app.config.Agent.Guardrails.LoopNameThreshold
	}

	// Retry config from config.yaml
	if app.config.Agent.Runtime.MaxRetries > 0 {
		loopCfg.MaxRetries = app.config.Agent.Runtime.MaxRetries
	}
	if app.config.Agent.Runtime.RetryBaseWait > 0 {
		loopCfg.RetryBaseWait = app.config.Agent.Runtime.RetryBaseWait
	}

	// Compaction config from config.yaml
	if app.config.Agent.Compaction.MessageThreshold > 0 {
		loopCfg.CompactThreshold = app.config.Agent.Compaction.MessageThreshold
	}
	if app.config.Agent.Compaction.KeepRecent > 0 {
		loopCfg.CompactKeepLast = app.config.Agent.Compaction.KeepRecent
	}


	app.agentLoop = service.NewAgentLoop(
		app.llmRouter,
		loopTools,
		loopCfg,
		app.logger,
	)
	app.logger.Info("Agent Loop initialized",
		zap.String("model", loopCfg.Model),
	)

	// Create SecurityHook and attach to agent loop
	app.securityHook = service.NewSecurityHook(
		app.config.Agent.Security,
		nil, // approvalFunc is set later in initInterfaces after TG adapter creation
		app.logger,
	)
	app.agentLoop.SetHooks(app.securityHook)

	// Middleware pipeline (data-transformation hooks around LLM calls)
	mwPipeline := service.NewMiddlewarePipeline(app.logger)
	mwPipeline.Use(
		service.NewDanglingToolCallMiddleware(app.logger),
		// NOTE: MemoryMiddleware intentionally removed.
		// It produced low-quality, unfiltered facts (201 entries in memory.json)
		// that polluted the system prompt and caused context poisoning.
		// Future: agent writes memory via file tools (OpenClaw pattern).
	)
	app.agentLoop.SetMiddleware(mwPipeline)
	app.logger.Info("Middleware pipeline configured",
		zap.Int("middlewares", mwPipeline.Len()),
	)

	return nil
}

// chatIDKey is a context key for passing chatID to SecurityHook.
type chatIDKey struct{}

// WithChatID stores chatID in the context.
func WithChatID(ctx context.Context, chatID int64) context.Context {
	return context.WithValue(ctx, chatIDKey{}, chatID)
}

// ChatIDFromContext extracts chatID from the context.
func ChatIDFromContext(ctx context.Context) int64 {
	if v, ok := ctx.Value(chatIDKey{}).(int64); ok {
		return v
	}
	return 0
}

// initInterfaces 初始化接口层
func (app *App) initInterfaces() error {
	app.logger.Info("Initializing interfaces")

	// HTTP服务器
	loopToolsBridge := &toolBridge{registry: app.toolRegistry}
	app.httpServer = httpServer.NewServer(
		httpServer.Config{
			Host: app.config.Gateway.Host,
			Port: app.config.Gateway.Port,
			Mode: app.config.Gateway.Mode,
		},
		app.processMessageUseCase,
		app.agentLoop,
		loopToolsBridge,
		app.promptEngine,
		app.logger,
	)

	// Telegram适配器
	if app.config.Telegram.BotToken != "" {
		var err error
		app.telegramAdapter, err = telegram.NewAdapter(
			&telegram.Config{
				BotToken:       app.config.Telegram.BotToken,
				AllowedUserIDs: app.config.Telegram.AllowIDs,
				DMPolicy:       app.config.Telegram.DMPolicy,
				GroupPolicy:    app.config.Telegram.GroupPolicy,
				GroupAllowFrom: app.config.Telegram.GroupAllowFrom,
			},
			app.logger,
		)
		if err != nil {
			return fmt.Errorf("failed to create telegram adapter: %w", err)
		}

		// Register media tools (TG-only, delayed because adapter created here)
		app.toolRegistry.Register(toolpkg.NewSendPhotoTool(app.telegramAdapter, app.logger))
		app.toolRegistry.Register(toolpkg.NewSendDocumentTool(app.telegramAdapter, app.logger))
		app.logger.Info("Registered TG media tools (send_photo, send_document)")

		// 创建会话管理器
		sessionManager := telegram.NewDefaultSessionManager(app.config.Agent.DefaultModel)

		// 从配置加载模型列表
		if len(app.config.Agent.Models) > 0 {
			models := make([]telegram.ModelInfo, len(app.config.Agent.Models))
			for i, m := range app.config.Agent.Models {
				models[i] = telegram.ModelInfo{
					ID:          m.ID,
					Alias:       m.Alias,
					Provider:    m.Provider,
					Description: m.Description,
				}
			}
			sessionManager.SetAvailableModels(models)
		}

		// 创建命令注册表
		cmdRegistry := telegram.NewCommandRegistry()

		// 设置会话管理器
		cmdRegistry.SetSessionManager(sessionManager)

		// 创建技能管理器
		skillHome, _ := os.UserHomeDir()
		skillDir := filepath.Join(skillHome, ".ngoclaw", "skills")
		skillManager := toolpkg.NewSkillManager(skillDir)
		cmdRegistry.SetSkillManager(skillManager)
		app.logger.Info("Skill manager initialized", zap.String("dir", skillDir), zap.Int("count", len(skillManager.List())))

		// 注册内置命令
		app.telegramAdapter.RegisterBuiltinCommands(cmdRegistry, app.securityHook)

		// 设置命令注册表
		app.telegramAdapter.SetCommandRegistry(cmdRegistry)

		// 设置消息处理器 (agent loop + DraftStream 流式输出)
		msgHandler := &telegramMessageHandler{
			agentLoop:      app.agentLoop,
			toolExec:       loopToolsBridge,
			promptEngine:   app.promptEngine,
			tgAdapter:      app.telegramAdapter,
			logger:         app.logger,
			sessionManager: sessionManager,
			workspaceDir:   app.config.Agent.Workspace,
		}
		app.telegramAdapter.SetMessageHandler(msgHandler)

		// Wire SecurityHook approval function now that TG adapter exists
		if app.securityHook != nil {
			adapter := app.telegramAdapter
			app.securityHook.SetApprovalFunc(func(ctx context.Context, toolName string, args map[string]interface{}) (bool, error) {
				chatID := ChatIDFromContext(ctx)
				if chatID == 0 {
					return true, nil // No chatID in context — auto-approve (e.g. HTTP API)
				}
				argsJSON, _ := json.Marshal(args)
				return adapter.RequestApproval(ctx, chatID, toolName, string(argsJSON))
			})
		}

		// 允许 /new /clear /reset 命令清除对话历史
		cmdRegistry.SetHistoryClearer(msgHandler)

		// 允许 /stop 命令和对话打断
		cmdRegistry.SetRunController(msgHandler)
		app.telegramAdapter.SetRunController(msgHandler)

		app.logger.Info("Telegram adapter initialized with command registry and session manager")
	} else {
		app.logger.Warn("Telegram bot token not configured, skipping telegram adapter")
	}

	// gRPC Agent Server (for VS Code Extension / SDK)
	grpcPort := app.config.Agent.GRPCPort
	if grpcPort == 0 {
		grpcPort = 50052
	}
	loopTools := &toolBridge{registry: app.toolRegistry}
	app.grpcAgentSrv = agentgrpc.NewServer(app.agentLoop, loopTools, grpcPort, app.logger)
	app.logger.Info("gRPC agent server created", zap.Int("port", grpcPort))

	return nil

}



// seedData 初始化默认数据
func (app *App) seedData() error {
	app.logger.Info("Seeding default data")

	ctx := context.Background()

	// 创建默认代理
	defaultAgent, err := entity.NewAgent(
		"default",
		"默认助手",
		valueobject.DefaultModelConfig(),
	)
	if err != nil {
		return fmt.Errorf("failed to create default agent: %w", err)
	}

	// 保存默认代理
	if err := app.agentRepo.Save(ctx, defaultAgent); err != nil {
		return fmt.Errorf("failed to save default agent: %w", err)
	}

	app.logger.Info("Default agent created",
		zap.String("id", defaultAgent.ID()),
		zap.String("name", defaultAgent.Name()),
	)

	return nil
}

// Start 启动应用程序
func (app *App) Start(ctx context.Context) error {
	app.logger.Info("Starting application")


	// 启动HTTP服务器
	if err := app.httpServer.Start(ctx); err != nil {
		return fmt.Errorf("failed to start HTTP server: %w", err)
	}

	// 启动Telegram适配器
	if app.telegramAdapter != nil {
		if err := app.telegramAdapter.Start(ctx); err != nil {
			return fmt.Errorf("failed to start telegram adapter: %w", err)
		}
	}

	// 启动 gRPC Agent Server
	if app.grpcAgentSrv != nil {
		if err := app.grpcAgentSrv.Start(); err != nil {
			app.logger.Warn("gRPC agent server failed to start", zap.Error(err))
		}
	}

	app.logger.Info("Application started successfully")
	return nil
}

// Stop 停止应用程序
func (app *App) Stop(ctx context.Context) error {
	app.logger.Info("Stopping application")

	// 停止 gRPC Agent Server
	if app.grpcAgentSrv != nil {
		app.grpcAgentSrv.Stop()
	}

	// 停止Telegram适配器
	if app.telegramAdapter != nil {
		app.telegramAdapter.Stop()
	}

	// 停止HTTP服务器
	if err := app.httpServer.Stop(ctx); err != nil {
		app.logger.Error("Failed to stop HTTP server", zap.Error(err))
	}





	// 关闭数据库连接
	if app.db != nil {
		sqlDB, err := app.db.DB()
		if err == nil {
			if err := sqlDB.Close(); err != nil {
				app.logger.Error("Failed to close database connection", zap.Error(err))
			}
		}
	}

	app.logger.Info("Application stopped successfully")
	return nil
}

// ProcessMessageUseCase returns the message processing usecase (used by REPL)
func (app *App) ProcessMessageUseCase() *usecase.ProcessMessageUseCase {
	return app.processMessageUseCase
}

// Logger returns the application logger
func (app *App) Logger() *zap.Logger {
	return app.logger
}

// Config returns the application config
func (app *App) AppConfig() *config.Config {
	return app.config
}

// AgentLoop returns the agent loop instance (used by CLI/TUI)
func (app *App) AgentLoop() *service.AgentLoop {
	return app.agentLoop
}

// PromptEngine returns the prompt engine (used by CLI/TUI)
func (app *App) PromptEngine() *prompt.PromptEngine {
	return app.promptEngine
}

// ToolRegistry returns the tool registry (used by CLI/TUI)
func (app *App) ToolRegistry() domaintool.Registry {
	return app.toolRegistry
}

// telegramMessageHandler 实现 telegram.MessageHandler + telegram.RunController 接口
// 通过 agentLoop.Run() + DraftStream 实现流式 TG 消息输出
// 支持对话打断: 新消息自动取消旧的运行中 agent loop
type telegramMessageHandler struct {
	agentLoop      *service.AgentLoop
	toolExec       service.ToolExecutor
	promptEngine   *prompt.PromptEngine
	tgAdapter      *telegram.Adapter
	logger         *zap.Logger
	sessionManager telegram.SessionManager
	workspaceDir   string
	// 每个 chatID 的对话历史
	histories sync.Map // map[int64][]service.LLMMessage
	// 每个 chatID 的活跃运行 (用于打断)
	activeRuns sync.Map // map[int64]context.CancelFunc
}

// maxHistoryPairs 最多保留的对话对数 (user+assistant = 1 pair)
const maxHistoryPairs = 30

func (h *telegramMessageHandler) HandleMessage(ctx context.Context, msg *telegram.IncomingMessage) (*telegram.OutgoingMessage, error) {
	// ===== 打断机制: 取消此 chatID 之前的运行 =====
	if oldCancel, ok := h.activeRuns.Load(msg.ChatID); ok {
		oldCancel.(context.CancelFunc)()
		h.logger.Info("Interrupted previous run",
			zap.Int64("chat_id", msg.ChatID),
		)
	}

	// 创建可取消的上下文, 注册到 activeRuns
	runCtx, runCancel := context.WithCancel(ctx)
	runCtx = WithChatID(runCtx, msg.ChatID)     // for SecurityHook
	runCtx = toolpkg.WithChatID(runCtx, msg.ChatID) // for media tools (send_photo, send_document)
	h.activeRuns.Store(msg.ChatID, runCancel)
	defer func() {
		runCancel()
		h.activeRuns.Delete(msg.ChatID)
	}()

	// 发送 typing 状态
	h.tgAdapter.SendTyping(msg.ChatID)

	// 组装 system prompt (两层架构)
	toolNames := make([]string, 0)
	toolSummaries := make(map[string]string)
	for _, d := range h.toolExec.GetDefinitions() {
		toolNames = append(toolNames, d.Name)
		if d.Description != "" {
			toolSummaries[d.Name] = d.Description
		}
	}

	// 获取当前模型名称
	modelName := ""
	if h.sessionManager != nil {
		modelName = h.sessionManager.GetCurrentModel(msg.ChatID)
	}

	// Build unified system prompt (channel-aware assembly)
	systemPrompt := ""
	if h.promptEngine != nil {
		systemPrompt = h.promptEngine.Assemble(prompt.PromptContext{
			Channel:         "telegram",
			RegisteredTools: toolNames,
			ToolSummaries:   toolSummaries,
			ModelName:       modelName,
			UserMessage:     msg.Text,
			Workspace:       h.workspaceDir,
		})
	}


	// 加载对话历史
	history := h.getHistory(msg.ChatID)

	// 运行 agent loop (异步, 通过 eventCh 流式输出)
	result, eventCh := h.agentLoop.Run(runCtx, systemPrompt, msg.Text, history, modelName)

	// 创建 StagedReply: Antigravity 风格的阶段性回复
	// Phase 1: 状态消息 (思考 → 工具执行 → 步骤进度)
	// Phase 2: 删除状态消息 → 发送完整回复
	staged := h.tgAdapter.CreateStagedReply(msg.ChatID)
	_ = staged.StatusThinking()

	var lastSegment strings.Builder // Accumulated text from final segment (after last tool result)
	interrupted := false

	for event := range eventCh {
		// 检查是否被打断
		if runCtx.Err() != nil {
			interrupted = true
			break
		}

		switch event.Type {
		case entity.EventTextDelta:
			lastSegment.WriteString(event.Content)

		case entity.EventToolCall:
			// Reset lastSegment on each tool call so the fallback only contains text
			// from the FINAL LLM segment (after the last tool result).
			// Without this, intermediate narration ("先检查…", "服务正在运行…") from
			// every LLM step accumulates and contaminates the output.
			lastSegment.Reset()
			if event.ToolCall != nil {
				_ = staged.StatusToolStart(event.ToolCall.Name, event.ToolCall.Arguments)
			}

		case entity.EventToolResult:
			if event.ToolCall != nil {
				_ = staged.StatusToolDone(event.ToolCall.Name, event.ToolCall.Arguments, event.ToolCall.Success)
			}

		case entity.EventError:
			_ = staged.StatusCustom("❌ " + event.Error)

		case entity.EventStepDone:
			if event.StepInfo != nil {
				_ = staged.StatusStep(event.StepInfo.Step, 0)
			}
			h.tgAdapter.SendTyping(msg.ChatID)
		}
	}

	// 处理被打断的情况
	if interrupted {
		partial := lastSegment.String()
		if partial == "" {
			partial = "(被用户打断)"
		}
		h.appendHistory(msg.ChatID, msg.Text, partial+" [已打断]")
		_ = staged.DeliverWithSuffix(h.tgAdapter, partial, "⏹ <i>已打断</i>")
		return nil, nil
	}

	// 正常完成 → 选择最佳输出
	// Priority: result.FinalContent > lastSegment > "(无输出)"
	// NOTE: reasoning tags stripped by agent_loop (StripReasoningTags).
	// lastSegment fallback also stripped as safety net (OpenClaw pattern).
	finalText := strings.TrimSpace(result.FinalContent)
	if finalText == "" {
		finalText = strings.TrimSpace(service.StripReasoningTags(lastSegment.String()))
	}

	isEmpty := strings.TrimSpace(finalText) == ""
	if isEmpty {
		finalText = "(无输出)"
	}

	h.logger.Info("[DIAG] Delivering final response to TG",
		zap.Int64("chat_id", msg.ChatID),
		zap.Int("content_len", len(finalText)),
		zap.Int("steps", result.TotalSteps),
		zap.Bool("empty_fallback", isEmpty),
	)

	// Only append valid responses to history — empty/failed responses pollute context
	// and cause the model to ignore subsequent user prompts.
	if !isEmpty {
		h.appendHistory(msg.ChatID, msg.Text, finalText)
	} else {
		h.logger.Warn("[DIAG] Skipping history append for empty response",
			zap.Int64("chat_id", msg.ChatID),
			zap.String("raw_final", result.FinalContent),
			zap.String("raw_segment", lastSegment.String()),
		)
	}

	if err := staged.DeliverWithSuffix(h.tgAdapter, finalText, "<i>— NGOClaw</i>"); err != nil {
		h.logger.Error("[DIAG] TG delivery FAILED", zap.Error(err), zap.Int64("chat_id", msg.ChatID))
	} else {
		h.logger.Info("[DIAG] TG delivery succeeded", zap.Int64("chat_id", msg.ChatID))
	}
	return nil, nil
}


// ===== RunController 接口实现 =====

// AbortRun 中止指定 chatID 的当前运行 (供 /stop 命令调用)
func (h *telegramMessageHandler) AbortRun(chatID int64) bool {
	if cancel, ok := h.activeRuns.Load(chatID); ok {
		cancel.(context.CancelFunc)()
		return true
	}
	return false
}

// IsRunActive 检查指定 chatID 是否有活跃运行
func (h *telegramMessageHandler) IsRunActive(chatID int64) bool {
	_, ok := h.activeRuns.Load(chatID)
	return ok
}

// GetRunState 获取指定 chatID 的运行状态
func (h *telegramMessageHandler) GetRunState(chatID int64) string {
	if h.IsRunActive(chatID) {
		return "running"
	}
	return "idle"
}

// ===== HistoryClearer 接口实现 =====

// ClearHistory 清除指定 chatID 的对话历史
func (h *telegramMessageHandler) ClearHistory(chatID int64) {
	h.histories.Delete(chatID)
}

// GetHistory returns conversation history as simplified messages for session-memory saving.
func (h *telegramMessageHandler) GetHistory(chatID int64) []telegram.HistoryMessage {
	msgs := h.getHistory(chatID)
	if len(msgs) == 0 {
		return nil
	}
	result := make([]telegram.HistoryMessage, 0, len(msgs))
	for _, m := range msgs {
		if m.Role == "user" || m.Role == "assistant" {
			content := m.Content
			if len(content) > 500 {
				content = content[:500] + "..."
			}
			result = append(result, telegram.HistoryMessage{
				Role:    m.Role,
				Content: content,
			})
		}
	}
	return result
}

// ===== 内部方法 =====

func (h *telegramMessageHandler) getHistory(chatID int64) []service.LLMMessage {
	if val, ok := h.histories.Load(chatID); ok {
		return val.([]service.LLMMessage)
	}
	return nil
}

func (h *telegramMessageHandler) appendHistory(chatID int64, userText, assistantText string) {
	history := h.getHistory(chatID)
	history = append(history,
		service.LLMMessage{Role: "user", Content: userText},
		service.LLMMessage{Role: "assistant", Content: assistantText},
	)
	maxMessages := maxHistoryPairs * 2
	if len(history) > maxMessages {
		history = history[len(history)-maxMessages:]
	}
	h.histories.Store(chatID, history)
}

