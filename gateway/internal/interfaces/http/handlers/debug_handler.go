package handlers

import (
	"net/http"
	"runtime"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// DebugHandler 调试 API 处理器
type DebugHandler struct {
	monitor      Monitor
	pluginLoader PluginLoader
	sessionMgr   SessionStats
	logger       *zap.Logger
}

// Monitor 监控接口
type Monitor interface {
	GetStats() map[string]interface{}
	GetHistory() []interface{}
	GetDashboardData() interface{}
}

// PluginLoader 插件加载器接口
type PluginLoader interface {
	List() []interface{}
	Get(name string) (interface{}, bool)
}

// SessionStats 会话统计接口
type SessionStats interface {
	Stats() map[string]interface{}
}

// NewDebugHandler 创建调试处理器
func NewDebugHandler(monitor Monitor, pluginLoader PluginLoader, sessionMgr SessionStats, logger *zap.Logger) *DebugHandler {
	return &DebugHandler{
		monitor:      monitor,
		pluginLoader: pluginLoader,
		sessionMgr:   sessionMgr,
		logger:       logger,
	}
}

// GetMetrics 获取性能指标
// GET /api/v1/debug/metrics
func (h *DebugHandler) GetMetrics(c *gin.Context) {
	stats := h.monitor.GetStats()
	c.JSON(http.StatusOK, stats)
}

// GetDashboard 获取仪表盘数据
// GET /api/v1/debug/dashboard
func (h *DebugHandler) GetDashboard(c *gin.Context) {
	data := h.monitor.GetDashboardData()
	c.JSON(http.StatusOK, data)
}

// GetSessions 获取会话统计
// GET /api/v1/debug/sessions
func (h *DebugHandler) GetSessions(c *gin.Context) {
	if h.sessionMgr == nil {
		c.JSON(http.StatusOK, gin.H{"sessions": []interface{}{}, "count": 0})
		return
	}
	stats := h.sessionMgr.Stats()
	c.JSON(http.StatusOK, stats)
}

// GetPlugins 获取插件列表
// GET /api/v1/debug/plugins
func (h *DebugHandler) GetPlugins(c *gin.Context) {
	if h.pluginLoader == nil {
		c.JSON(http.StatusOK, gin.H{"plugins": []interface{}{}, "count": 0})
		return
	}
	plugins := h.pluginLoader.List()
	c.JSON(http.StatusOK, gin.H{
		"plugins": plugins,
		"count":   len(plugins),
	})
}

// GetRuntime 获取运行时信息
// GET /api/v1/debug/runtime
func (h *DebugHandler) GetRuntime(c *gin.Context) {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	c.JSON(http.StatusOK, gin.H{
		"go_version":     runtime.Version(),
		"num_cpu":        runtime.NumCPU(),
		"num_goroutine":  runtime.NumGoroutine(),
		"memory": gin.H{
			"alloc_mb":       float64(memStats.Alloc) / 1024 / 1024,
			"total_alloc_mb": float64(memStats.TotalAlloc) / 1024 / 1024,
			"sys_mb":         float64(memStats.Sys) / 1024 / 1024,
			"num_gc":         memStats.NumGC,
		},
		"timestamp": time.Now().Unix(),
	})
}

// TriggerGC 手动触发 GC
// POST /api/v1/debug/gc
func (h *DebugHandler) TriggerGC(c *gin.Context) {
	before := runtime.NumGoroutine()
	runtime.GC()
	after := runtime.NumGoroutine()

	c.JSON(http.StatusOK, gin.H{
		"message":           "GC triggered",
		"goroutines_before": before,
		"goroutines_after":  after,
	})
}

// GetLogs 获取最近日志 (简化实现)
// GET /api/v1/debug/logs
func (h *DebugHandler) GetLogs(c *gin.Context) {
	// 实际实现需要日志收集器
	c.JSON(http.StatusOK, gin.H{
		"message": "Log streaming available via WebSocket at /ws/logs",
		"logs":    []interface{}{},
	})
}

// GetAgentState 获取 Agent 状态
// GET /api/v1/debug/agents/:id/state
func (h *DebugHandler) GetAgentState(c *gin.Context) {
	agentID := c.Param("id")
	
	// 实际实现需要从 SessionManager 获取
	c.JSON(http.StatusOK, gin.H{
		"agent_id": agentID,
		"state":    "idle",
		"history":  []interface{}{},
	})
}

// GetToolHistory 获取工具调用历史
// GET /api/v1/debug/tools/history
func (h *DebugHandler) GetToolHistory(c *gin.Context) {
	// 实际实现需要工具执行历史收集器
	c.JSON(http.StatusOK, gin.H{
		"history": []interface{}{},
		"count":   0,
	})
}

// RegisterDebugRoutes 注册调试路由
func RegisterDebugRoutes(router *gin.RouterGroup, handler *DebugHandler) {
	debug := router.Group("/debug")
	{
		debug.GET("/metrics", handler.GetMetrics)
		debug.GET("/dashboard", handler.GetDashboard)
		debug.GET("/sessions", handler.GetSessions)
		debug.GET("/plugins", handler.GetPlugins)
		debug.GET("/runtime", handler.GetRuntime)
		debug.POST("/gc", handler.TriggerGC)
		debug.GET("/logs", handler.GetLogs)
		debug.GET("/agents/:id/state", handler.GetAgentState)
		debug.GET("/tools/history", handler.GetToolHistory)
	}
}
