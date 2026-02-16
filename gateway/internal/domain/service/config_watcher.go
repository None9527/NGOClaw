package service

import (
	"encoding/json"
	"os"
	"sync"
	"time"

	"go.uber.org/zap"
)

// ConfigWatcher monitors a JSON config file and hot-reloads AgentLoopConfig
// when the file changes. Safe for concurrent reads from the AgentLoop.
//
// Usage:
//
//	watcher := NewConfigWatcher("/etc/ngoclaw/agent.json", logger)
//	go watcher.Start()
//	defer watcher.Stop()
//	config := watcher.Config() // Always returns latest
type ConfigWatcher struct {
	path     string
	mu       sync.RWMutex
	config   AgentLoopConfig
	lastMod  time.Time
	interval time.Duration
	stopCh   chan struct{}
	logger   *zap.Logger
}

// NewConfigWatcher creates a config file watcher with polling.
// If the file doesn't exist or can't be parsed, defaults are used.
func NewConfigWatcher(path string, logger *zap.Logger) *ConfigWatcher {
	w := &ConfigWatcher{
		path:     path,
		config:   DefaultAgentLoopConfig(),
		interval: 5 * time.Second,
		stopCh:   make(chan struct{}),
		logger:   logger.With(zap.String("component", "config-watcher")),
	}

	// Try initial load
	if err := w.reload(); err != nil {
		w.logger.Warn("Initial config load failed, using defaults",
			zap.String("path", path),
			zap.Error(err),
		)
	}

	return w
}

// Config returns the current config (thread-safe).
func (w *ConfigWatcher) Config() AgentLoopConfig {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.config
}

// Start begins polling the config file for changes.
// Blocks until Stop() is called.
func (w *ConfigWatcher) Start() {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	w.logger.Info("Config watcher started",
		zap.String("path", w.path),
		zap.Duration("interval", w.interval),
	)

	for {
		select {
		case <-w.stopCh:
			w.logger.Info("Config watcher stopped")
			return
		case <-ticker.C:
			info, err := os.Stat(w.path)
			if err != nil {
				continue // File might not exist yet
			}

			w.mu.RLock()
			lastMod := w.lastMod
			w.mu.RUnlock()

			if info.ModTime().After(lastMod) {
				if err := w.reload(); err != nil {
					w.logger.Warn("Config reload failed",
						zap.Error(err),
					)
				}
			}
		}
	}
}

// Stop signals the watcher to stop polling.
func (w *ConfigWatcher) Stop() {
	close(w.stopCh)
}

// reload reads and applies the config file.
func (w *ConfigWatcher) reload() error {
	data, err := os.ReadFile(w.path)
	if err != nil {
		return err
	}

	// Start from defaults, then overlay file values
	newConfig := DefaultAgentLoopConfig()
	if err := json.Unmarshal(data, &newConfig); err != nil {
		return err
	}

	info, _ := os.Stat(w.path)

	w.mu.Lock()
	w.config = newConfig
	if info != nil {
		w.lastMod = info.ModTime()
	}
	w.mu.Unlock()

	w.logger.Info("Config reloaded",
		zap.String("path", w.path),
		zap.String("model", newConfig.Model),
	)

	return nil
}

// SetInterval changes the polling interval (for testing).
func (w *ConfigWatcher) SetInterval(d time.Duration) {
	w.interval = d
}
