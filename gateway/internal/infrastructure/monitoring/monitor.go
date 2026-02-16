package monitoring

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// Metrics 指标收集器
type Metrics struct {
	// 请求计数
	RequestsTotal   uint64
	RequestsSuccess uint64
	RequestsFailed  uint64

	// 工具调用
	ToolCallsTotal   uint64
	ToolCallsSuccess uint64
	ToolCallsFailed  uint64

	// 会话
	ActiveSessions int64

	// 延迟 (纳秒)
	RequestLatencySum   uint64
	RequestLatencyCount uint64
	ToolLatencySum      uint64
	ToolLatencyCount    uint64

	// 模型调用
	ModelCallsTotal  uint64
	ModelTokensUsed  uint64

	// 错误
	ErrorsTotal uint64

	// 启动时间
	StartTime time.Time
}

// Monitor 性能监控器
type Monitor struct {
	metrics *Metrics
	logger  *zap.Logger
	mu      sync.RWMutex

	// 历史数据 (用于图表)
	history      []MetricsSnapshot
	historyLimit int
}

// MetricsSnapshot 指标快照
type MetricsSnapshot struct {
	Timestamp         time.Time
	RequestsPerSecond float64
	ToolCallsPerSec   float64
	AvgLatencyMs      float64
	ActiveSessions    int64
	MemoryMB          float64
	Goroutines        int
}

// NewMonitor 创建监控器
func NewMonitor(logger *zap.Logger) *Monitor {
	return &Monitor{
		metrics: &Metrics{
			StartTime: time.Now(),
		},
		logger:       logger,
		history:      make([]MetricsSnapshot, 0, 100),
		historyLimit: 100,
	}
}

// 计数方法
func (m *Monitor) IncRequestTotal()   { atomic.AddUint64(&m.metrics.RequestsTotal, 1) }
func (m *Monitor) IncRequestSuccess() { atomic.AddUint64(&m.metrics.RequestsSuccess, 1) }
func (m *Monitor) IncRequestFailed()  { atomic.AddUint64(&m.metrics.RequestsFailed, 1) }
func (m *Monitor) IncToolCallTotal()  { atomic.AddUint64(&m.metrics.ToolCallsTotal, 1) }
func (m *Monitor) IncToolCallSuccess() { atomic.AddUint64(&m.metrics.ToolCallsSuccess, 1) }
func (m *Monitor) IncToolCallFailed() { atomic.AddUint64(&m.metrics.ToolCallsFailed, 1) }
func (m *Monitor) IncModelCall()      { atomic.AddUint64(&m.metrics.ModelCallsTotal, 1) }
func (m *Monitor) IncError()          { atomic.AddUint64(&m.metrics.ErrorsTotal, 1) }

func (m *Monitor) AddTokensUsed(n int) {
	atomic.AddUint64(&m.metrics.ModelTokensUsed, uint64(n))
}

func (m *Monitor) SetActiveSessions(n int64) {
	atomic.StoreInt64(&m.metrics.ActiveSessions, n)
}

func (m *Monitor) RecordRequestLatency(d time.Duration) {
	atomic.AddUint64(&m.metrics.RequestLatencySum, uint64(d.Nanoseconds()))
	atomic.AddUint64(&m.metrics.RequestLatencyCount, 1)
}

func (m *Monitor) RecordToolLatency(d time.Duration) {
	atomic.AddUint64(&m.metrics.ToolLatencySum, uint64(d.Nanoseconds()))
	atomic.AddUint64(&m.metrics.ToolLatencyCount, 1)
}

// GetStats 获取当前统计
func (m *Monitor) GetStats() map[string]interface{} {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	uptime := time.Since(m.metrics.StartTime)
	reqTotal := atomic.LoadUint64(&m.metrics.RequestsTotal)
	
	avgLatency := float64(0)
	if count := atomic.LoadUint64(&m.metrics.RequestLatencyCount); count > 0 {
		avgLatency = float64(atomic.LoadUint64(&m.metrics.RequestLatencySum)) / float64(count) / 1e6 // ms
	}

	return map[string]interface{}{
		"uptime_seconds":     uptime.Seconds(),
		"requests_total":     reqTotal,
		"requests_success":   atomic.LoadUint64(&m.metrics.RequestsSuccess),
		"requests_failed":    atomic.LoadUint64(&m.metrics.RequestsFailed),
		"tool_calls_total":   atomic.LoadUint64(&m.metrics.ToolCallsTotal),
		"tool_calls_success": atomic.LoadUint64(&m.metrics.ToolCallsSuccess),
		"tool_calls_failed":  atomic.LoadUint64(&m.metrics.ToolCallsFailed),
		"model_calls_total":  atomic.LoadUint64(&m.metrics.ModelCallsTotal),
		"model_tokens_used":  atomic.LoadUint64(&m.metrics.ModelTokensUsed),
		"active_sessions":    atomic.LoadInt64(&m.metrics.ActiveSessions),
		"errors_total":       atomic.LoadUint64(&m.metrics.ErrorsTotal),
		"avg_latency_ms":     avgLatency,
		"memory_mb":          float64(memStats.Alloc) / 1024 / 1024,
		"goroutines":         runtime.NumGoroutine(),
		"rps":                float64(reqTotal) / uptime.Seconds(),
	}
}

// Snapshot 创建快照并保存
func (m *Monitor) Snapshot() MetricsSnapshot {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	uptime := time.Since(m.metrics.StartTime).Seconds()
	reqTotal := atomic.LoadUint64(&m.metrics.RequestsTotal)
	toolTotal := atomic.LoadUint64(&m.metrics.ToolCallsTotal)

	avgLatency := float64(0)
	if count := atomic.LoadUint64(&m.metrics.RequestLatencyCount); count > 0 {
		avgLatency = float64(atomic.LoadUint64(&m.metrics.RequestLatencySum)) / float64(count) / 1e6
	}

	snapshot := MetricsSnapshot{
		Timestamp:         time.Now(),
		RequestsPerSecond: float64(reqTotal) / uptime,
		ToolCallsPerSec:   float64(toolTotal) / uptime,
		AvgLatencyMs:      avgLatency,
		ActiveSessions:    atomic.LoadInt64(&m.metrics.ActiveSessions),
		MemoryMB:          float64(memStats.Alloc) / 1024 / 1024,
		Goroutines:        runtime.NumGoroutine(),
	}

	m.mu.Lock()
	m.history = append(m.history, snapshot)
	if len(m.history) > m.historyLimit {
		m.history = m.history[1:]
	}
	m.mu.Unlock()

	return snapshot
}

// GetHistory 获取历史快照
func (m *Monitor) GetHistory() []MetricsSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]MetricsSnapshot, len(m.history))
	copy(result, m.history)
	return result
}

// StartCollector 启动定期收集
func (m *Monitor) StartCollector(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.Snapshot()
		}
	}
}

// DashboardData 仪表盘数据
type DashboardData struct {
	Stats   map[string]interface{} `json:"stats"`
	History []MetricsSnapshot      `json:"history"`
}

// GetDashboardData 获取仪表盘数据
func (m *Monitor) GetDashboardData() *DashboardData {
	return &DashboardData{
		Stats:   m.GetStats(),
		History: m.GetHistory(),
	}
}
