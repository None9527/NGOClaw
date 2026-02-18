// Copyright 2026 NGOClaw. All rights reserved.

package service

import (
	"context"
	"strings"
	"sync"

	"go.uber.org/zap"

	"github.com/ngoclaw/ngoclaw/gateway/internal/infrastructure/config"
)

// ApprovalFunc is the callback to request user confirmation via Telegram.
// It blocks until the user responds or the context is cancelled.
// Returns true if approved, false if denied/timeout.
type ApprovalFunc func(ctx context.Context, toolName string, args map[string]interface{}) (bool, error)

// SecurityHook implements AgentLoopHook to enforce tool execution policies.
// It gates tool calls through BeforeToolCall based on SecurityConfig rules,
// optionally requesting Telegram inline-keyboard confirmation for dangerous tools.
type SecurityHook struct {
	cfg          config.SecurityConfig
	approvalFunc ApprovalFunc
	logger       *zap.Logger
	mu           sync.RWMutex
}

// NewSecurityHook creates a SecurityHook with the given config and approval callback.
func NewSecurityHook(cfg config.SecurityConfig, approvalFunc ApprovalFunc, logger *zap.Logger) *SecurityHook {
	return &SecurityHook{
		cfg:          cfg,
		approvalFunc: approvalFunc,
		logger:       logger,
	}
}

// ---- AgentLoopHook interface ----

func (h *SecurityHook) BeforeToolCall(ctx context.Context, toolName string, args map[string]interface{}) bool {
	h.mu.RLock()
	cfg := h.cfg
	h.mu.RUnlock()

	// 1. Auto mode — always allow
	if cfg.ApprovalMode == "auto" {
		return true
	}

	// 2. Trusted tools — always allow (highest priority)
	if h.isTrusted(toolName, args, cfg) {
		return true
	}

	// 3. ask_dangerous — only ask for tools in the dangerous list
	if cfg.ApprovalMode == "ask_dangerous" {
		if !h.isDangerous(toolName, cfg) {
			return true
		}
	}
	// ask_all falls through — every non-trusted tool needs approval

	// 4. Request approval via Telegram
	if h.approvalFunc == nil {
		h.logger.Warn("No approval function set, auto-approving",
			zap.String("tool", toolName),
		)
		return true
	}

	h.logger.Info("Requesting user approval for tool",
		zap.String("tool", toolName),
		zap.String("mode", cfg.ApprovalMode),
	)

	approved, err := h.approvalFunc(ctx, toolName, args)
	if err != nil {
		h.logger.Error("Approval request failed",
			zap.String("tool", toolName),
			zap.Error(err),
		)
		return false
	}

	if !approved {
		h.logger.Info("Tool call denied by user",
			zap.String("tool", toolName),
		)
	}

	return approved
}

func (h *SecurityHook) AfterToolCall(_ context.Context, _ string, _ string, _ bool) {}
func (h *SecurityHook) BeforeLLMCall(_ context.Context, _ *LLMRequest, _ int)       {}
func (h *SecurityHook) AfterLLMCall(_ context.Context, _ *LLMResponse, _ int)       {}
func (h *SecurityHook) OnStateChange(_ AgentState, _ AgentState, _ StateSnapshot)    {}
func (h *SecurityHook) OnError(_ context.Context, _ error, _ int)                    {}
func (h *SecurityHook) OnComplete(_ context.Context, _ *AgentResult)                 {}


// SetApprovalFunc sets the approval callback (deferred injection after TG adapter creation).
func (h *SecurityHook) SetApprovalFunc(fn ApprovalFunc) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.approvalFunc = fn
}

// ---- Policy helpers ----

// isTrusted checks if a tool/command is in the trust list.
func (h *SecurityHook) isTrusted(toolName string, args map[string]interface{}, cfg config.SecurityConfig) bool {
	for _, t := range cfg.TrustedTools {
		if t == toolName {
			return true
		}
	}

	// For shell_exec, check if the command matches a trusted command prefix
	if toolName == "shell_exec" {
		return h.isCommandTrusted(args, cfg)
	}

	return false
}

// isDangerous checks if a tool is in the dangerous list.
func (h *SecurityHook) isDangerous(toolName string, cfg config.SecurityConfig) bool {
	for _, d := range cfg.DangerousTools {
		if d == toolName {
			return true
		}
	}
	return false
}

// isCommandTrusted checks if a shell command matches a trusted command prefix.
func (h *SecurityHook) isCommandTrusted(args map[string]interface{}, cfg config.SecurityConfig) bool {
	cmd, ok := args["command"].(string)
	if !ok {
		return false
	}
	cmd = strings.TrimSpace(cmd)

	// Extract the first token (the actual command binary)
	firstToken := cmd
	if idx := strings.IndexAny(cmd, " \t|;&"); idx >= 0 {
		firstToken = cmd[:idx]
	}
	// Strip path prefix (e.g. /usr/bin/ls → ls)
	if idx := strings.LastIndex(firstToken, "/"); idx >= 0 {
		firstToken = firstToken[idx+1:]
	}

	for _, trusted := range cfg.TrustedCommands {
		if firstToken == trusted {
			return true
		}
	}
	return false
}

// ---- Runtime config updates (called by TG commands) ----

// UpdateConfig replaces the security config at runtime.
func (h *SecurityHook) UpdateConfig(cfg config.SecurityConfig) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cfg = cfg
}

// GetConfig returns the current security config.
func (h *SecurityHook) GetConfig() config.SecurityConfig {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.cfg
}

// SetApprovalMode changes the approval mode ("auto", "ask_dangerous", "ask_all").
func (h *SecurityHook) SetApprovalMode(mode string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cfg.ApprovalMode = mode
}

// TrustTool adds a tool to the trusted list (removes from dangerous if present).
func (h *SecurityHook) TrustTool(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Add to trusted if not already there
	for _, t := range h.cfg.TrustedTools {
		if t == name {
			goto removeDangerous
		}
	}
	h.cfg.TrustedTools = append(h.cfg.TrustedTools, name)

removeDangerous:
	// Remove from dangerous if present
	filtered := h.cfg.DangerousTools[:0]
	for _, d := range h.cfg.DangerousTools {
		if d != name {
			filtered = append(filtered, d)
		}
	}
	h.cfg.DangerousTools = filtered
}

// UntrustTool removes a tool from the trusted list.
func (h *SecurityHook) UntrustTool(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	filtered := h.cfg.TrustedTools[:0]
	for _, t := range h.cfg.TrustedTools {
		if t != name {
			filtered = append(filtered, t)
		}
	}
	h.cfg.TrustedTools = filtered
}

// TrustCommand adds a command prefix to the trusted commands list.
func (h *SecurityHook) TrustCommand(cmd string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for _, c := range h.cfg.TrustedCommands {
		if c == cmd {
			return
		}
	}
	h.cfg.TrustedCommands = append(h.cfg.TrustedCommands, cmd)
}
