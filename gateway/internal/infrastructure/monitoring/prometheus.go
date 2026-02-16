package monitoring

import (
	"fmt"
	"net/http"
	"runtime"
	"sync/atomic"
	"time"
)

// PrometheusHandler returns an http.Handler that serves Prometheus text format metrics.
// This avoids pulling in the full prometheus/client_golang dependency.
// Mount it at "/metrics" in your HTTP server.
func (m *Monitor) PrometheusHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

		var memStats runtime.MemStats
		runtime.ReadMemStats(&memStats)

		uptime := time.Since(m.metrics.StartTime).Seconds()

		// Write metrics in Prometheus exposition format
		lines := []struct {
			name string
			help string
			typ  string
			val  interface{}
		}{
			// Request counters
			{"ngoclaw_requests_total", "Total number of requests processed", "counter", atomic.LoadUint64(&m.metrics.RequestsTotal)},
			{"ngoclaw_requests_success_total", "Total successful requests", "counter", atomic.LoadUint64(&m.metrics.RequestsSuccess)},
			{"ngoclaw_requests_failed_total", "Total failed requests", "counter", atomic.LoadUint64(&m.metrics.RequestsFailed)},

			// Tool call counters
			{"ngoclaw_tool_calls_total", "Total tool calls executed", "counter", atomic.LoadUint64(&m.metrics.ToolCallsTotal)},
			{"ngoclaw_tool_calls_success_total", "Total successful tool calls", "counter", atomic.LoadUint64(&m.metrics.ToolCallsSuccess)},
			{"ngoclaw_tool_calls_failed_total", "Total failed tool calls", "counter", atomic.LoadUint64(&m.metrics.ToolCallsFailed)},

			// Model counters
			{"ngoclaw_model_calls_total", "Total LLM model calls", "counter", atomic.LoadUint64(&m.metrics.ModelCallsTotal)},
			{"ngoclaw_model_tokens_used_total", "Total tokens consumed", "counter", atomic.LoadUint64(&m.metrics.ModelTokensUsed)},

			// Errors
			{"ngoclaw_errors_total", "Total errors encountered", "counter", atomic.LoadUint64(&m.metrics.ErrorsTotal)},

			// Gauges
			{"ngoclaw_active_sessions", "Number of active sessions", "gauge", atomic.LoadInt64(&m.metrics.ActiveSessions)},
			{"ngoclaw_uptime_seconds", "Process uptime in seconds", "gauge", uptime},

			// Runtime metrics
			{"ngoclaw_memory_alloc_bytes", "Current memory allocation in bytes", "gauge", memStats.Alloc},
			{"ngoclaw_memory_sys_bytes", "Total memory obtained from OS", "gauge", memStats.Sys},
			{"ngoclaw_goroutines", "Number of goroutines", "gauge", runtime.NumGoroutine()},
			{"ngoclaw_gc_pause_total_ns", "Total GC pause time in nanoseconds", "counter", memStats.PauseTotalNs},
			{"ngoclaw_gc_cycles_total", "Total number of completed GC cycles", "counter", memStats.NumGC},
		}

		for _, l := range lines {
			fmt.Fprintf(w, "# HELP %s %s\n", l.name, l.help)
			fmt.Fprintf(w, "# TYPE %s %s\n", l.name, l.typ)
			switch v := l.val.(type) {
			case uint64:
				fmt.Fprintf(w, "%s %d\n", l.name, v)
			case int64:
				fmt.Fprintf(w, "%s %d\n", l.name, v)
			case int:
				fmt.Fprintf(w, "%s %d\n", l.name, v)
			case float64:
				fmt.Fprintf(w, "%s %f\n", l.name, v)
			case uint32:
				fmt.Fprintf(w, "%s %d\n", l.name, v)
			}
			fmt.Fprintln(w)
		}

		// Latency summaries
		reqCount := atomic.LoadUint64(&m.metrics.RequestLatencyCount)
		if reqCount > 0 {
			avgMs := float64(atomic.LoadUint64(&m.metrics.RequestLatencySum)) / float64(reqCount) / 1e6
			fmt.Fprintf(w, "# HELP ngoclaw_request_latency_avg_ms Average request latency in milliseconds\n")
			fmt.Fprintf(w, "# TYPE ngoclaw_request_latency_avg_ms gauge\n")
			fmt.Fprintf(w, "ngoclaw_request_latency_avg_ms %f\n\n", avgMs)
		}

		toolCount := atomic.LoadUint64(&m.metrics.ToolLatencyCount)
		if toolCount > 0 {
			avgMs := float64(atomic.LoadUint64(&m.metrics.ToolLatencySum)) / float64(toolCount) / 1e6
			fmt.Fprintf(w, "# HELP ngoclaw_tool_latency_avg_ms Average tool execution latency in milliseconds\n")
			fmt.Fprintf(w, "# TYPE ngoclaw_tool_latency_avg_ms gauge\n")
			fmt.Fprintf(w, "ngoclaw_tool_latency_avg_ms %f\n\n", avgMs)
		}
	})
}
