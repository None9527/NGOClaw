package service

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// HeartbeatConfig heartbeat configuration
type HeartbeatConfig struct {
	FilePath string        // Path to HEARTBEAT.md
	Interval time.Duration // Check interval (default: 1h)
	ChatID   int64         // Target Telegram ChatID for output
	Enabled  bool
}

// HeartbeatExecutor callback to execute heartbeat commands and send results
type HeartbeatExecutor func(ctx context.Context, chatID int64, command string) (string, error)

// HeartbeatService periodically reads HEARTBEAT.md and executes its instructions
type HeartbeatService struct {
	config   HeartbeatConfig
	executor HeartbeatExecutor
	logger   *zap.Logger
	ctx      context.Context
	cancel   context.CancelFunc
	running  bool
	mu       sync.Mutex
}

// NewHeartbeatService creates a new heartbeat service
func NewHeartbeatService(cfg HeartbeatConfig, logger *zap.Logger) *HeartbeatService {
	ctx, cancel := context.WithCancel(context.Background())

	if cfg.Interval == 0 {
		cfg.Interval = time.Hour
	}
	if cfg.FilePath == "" {
		cfg.FilePath = "HEARTBEAT.md"
	}

	return &HeartbeatService{
		config: cfg,
		logger: logger,
		ctx:    ctx,
		cancel: cancel,
	}
}

// SetExecutor sets the command executor callback
func (h *HeartbeatService) SetExecutor(executor HeartbeatExecutor) {
	h.executor = executor
}

// Start begins the heartbeat loop
func (h *HeartbeatService) Start() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.config.Enabled {
		h.logger.Info("Heartbeat service disabled")
		return nil
	}

	if h.running {
		return nil
	}

	h.running = true
	h.logger.Info("Starting heartbeat service",
		zap.String("file", h.config.FilePath),
		zap.Duration("interval", h.config.Interval),
		zap.Int64("chat_id", h.config.ChatID),
	)

	go h.loop()
	return nil
}

// Stop halts the heartbeat loop
func (h *HeartbeatService) Stop() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.running {
		h.cancel()
		h.running = false
		h.logger.Info("Heartbeat service stopped")
	}
}

// loop runs the periodic heartbeat check
func (h *HeartbeatService) loop() {
	// Run once immediately on start
	h.execute()

	ticker := time.NewTicker(h.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-h.ctx.Done():
			return
		case <-ticker.C:
			h.execute()
		}
	}
}

// execute reads HEARTBEAT.md and processes its commands
func (h *HeartbeatService) execute() {
	if h.executor == nil {
		h.logger.Warn("Heartbeat executor not set, skipping")
		return
	}

	commands, err := h.readHeartbeatFile()
	if err != nil {
		h.logger.Debug("Heartbeat file not available",
			zap.String("path", h.config.FilePath),
			zap.Error(err),
		)
		return
	}

	if len(commands) == 0 {
		return
	}

	h.logger.Info("Executing heartbeat",
		zap.Int("commands", len(commands)),
	)

	for _, cmd := range commands {
		result, err := h.executor(h.ctx, h.config.ChatID, cmd)
		if err != nil {
			h.logger.Error("Heartbeat command failed",
				zap.String("command", cmd),
				zap.Error(err),
			)
			continue
		}

		h.logger.Debug("Heartbeat command executed",
			zap.String("command", cmd),
			zap.Int("result_len", len(result)),
		)
	}
}

// readHeartbeatFile reads and parses HEARTBEAT.md
// Format: each non-empty, non-comment line is a command to execute
func (h *HeartbeatService) readHeartbeatFile() ([]string, error) {
	data, err := os.ReadFile(h.config.FilePath)
	if err != nil {
		return nil, fmt.Errorf("read heartbeat file: %w", err)
	}

	var commands []string
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Skip empty lines, comments, and markdown headers
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		commands = append(commands, line)
	}

	return commands, nil
}
