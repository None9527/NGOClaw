package sideload

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// ModuleState represents the lifecycle state of a module
type ModuleState int32

const (
	ModuleStateCreated     ModuleState = iota
	ModuleStateStarting
	ModuleStateReady
	ModuleStateStopping
	ModuleStateStopped
	ModuleStateError
)

func (s ModuleState) String() string {
	switch s {
	case ModuleStateCreated:
		return "created"
	case ModuleStateStarting:
		return "starting"
	case ModuleStateReady:
		return "ready"
	case ModuleStateStopping:
		return "stopping"
	case ModuleStateStopped:
		return "stopped"
	case ModuleStateError:
		return "error"
	default:
		return "unknown"
	}
}

// Module represents a running sideload module with its transport and capabilities
type Module struct {
	manifest   *Manifest
	path       string
	conn       Transport
	process    *os.Process
	caps       *ModuleCaps
	state      atomic.Int32
	lastError  error
	logger     *zap.Logger
	requestID  atomic.Int64
	mu         sync.RWMutex
}

// NewModule creates a new module instance from a discovered module
func NewModule(disc *DiscoveredModule, logger *zap.Logger) *Module {
	m := &Module{
		manifest: disc.Manifest,
		path:     disc.Path,
		logger:   logger.With(zap.String("module", disc.Manifest.Name)),
	}
	m.state.Store(int32(ModuleStateCreated))
	return m
}

// Name returns the module's name
func (m *Module) Name() string { return m.manifest.Name }

// State returns the current module state
func (m *Module) State() ModuleState { return ModuleState(m.state.Load()) }

// Capabilities returns the module's declared capabilities
func (m *Module) Capabilities() *ModuleCaps {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.caps
}

// Start spawns the module process and establishes transport
func (m *Module) Start(ctx context.Context) error {
	m.state.Store(int32(ModuleStateStarting))
	m.logger.Info("Starting module", zap.String("transport", string(m.manifest.Transport)))

	switch m.manifest.Transport {
	case TransportStdio:
		return m.startStdio(ctx)
	case TransportTCP:
		return m.startTCP(ctx)
	case TransportUnix:
		return m.startUnix(ctx)
	default:
		return fmt.Errorf("unsupported transport: %s", m.manifest.Transport)
	}
}

func (m *Module) startStdio(ctx context.Context) error {
	// Parse entrypoint into command + args
	parts := strings.Fields(m.manifest.Entrypoint)
	if len(parts) == 0 {
		return fmt.Errorf("empty entrypoint")
	}

	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	cmd.Dir = m.manifest.WorkDir
	if cmd.Dir == "" {
		cmd.Dir = m.path
	}

	// Set up environment
	cmd.Env = append(os.Environ(), "NGOCLAW_SIDELOAD=1")
	for k, v := range m.manifest.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	// Capture stderr for logging
	cmd.Stderr = &logWriter{logger: m.logger, level: "error"}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start process: %w", err)
	}

	m.process = cmd.Process
	m.conn = NewStdioTransport(stdin, stdout)

	// Monitor process exit
	go func() {
		if err := cmd.Wait(); err != nil {
			m.logger.Warn("Module process exited", zap.Error(err))
		}
		m.state.Store(int32(ModuleStateStopped))
	}()

	// Initialize the module
	return m.initialize(ctx)
}

func (m *Module) startTCP(ctx context.Context) error {
	t, err := DialTCP(ctx, m.manifest.Address)
	if err != nil {
		return err
	}
	m.conn = t
	return m.initialize(ctx)
}

func (m *Module) startUnix(ctx context.Context) error {
	t, err := DialUnix(ctx, m.manifest.Address)
	if err != nil {
		return err
	}
	m.conn = t
	return m.initialize(ctx)
}

// initialize sends the initialize request and reads capabilities
func (m *Module) initialize(ctx context.Context) error {
	initCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := NewRequest(m.nextID(), MethodInitialize, &InitializeParams{
		Capabilities: []string{"tool", "provider", "hook"},
	})
	if err != nil {
		return fmt.Errorf("create init request: %w", err)
	}

	resp, err := m.conn.Send(initCtx, req)
	if err != nil {
		m.state.Store(int32(ModuleStateError))
		m.lastError = err
		return fmt.Errorf("initialize failed: %w", err)
	}

	if resp.Error != nil {
		m.state.Store(int32(ModuleStateError))
		m.lastError = resp.Error
		return fmt.Errorf("initialize error: %v", resp.Error)
	}

	var initResult InitializeResult
	if err := resp.ParseResult(&initResult); err != nil {
		return fmt.Errorf("parse init result: %w", err)
	}

	m.mu.Lock()
	m.caps = &initResult.Capabilities
	m.mu.Unlock()

	m.state.Store(int32(ModuleStateReady))
	m.logger.Info("Module initialized",
		zap.String("version", initResult.Version),
		zap.Int("providers", len(initResult.Capabilities.Providers)),
		zap.Int("tools", len(initResult.Capabilities.Tools)),
		zap.Int("hooks", len(initResult.Capabilities.Hooks)),
	)

	return nil
}

// ExecuteTool calls a tool on this module
func (m *Module) ExecuteTool(ctx context.Context, params *ToolExecuteParams) (*ToolExecuteResult, error) {
	if m.State() != ModuleStateReady {
		return nil, fmt.Errorf("module not ready: state=%s", m.State())
	}

	req, err := NewRequest(m.nextID(), MethodToolExecute, params)
	if err != nil {
		return nil, err
	}

	resp, err := m.conn.Send(ctx, req)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, resp.Error
	}

	var result ToolExecuteResult
	if err := resp.ParseResult(&result); err != nil {
		return nil, fmt.Errorf("parse tool result: %w", err)
	}

	return &result, nil
}

// Generate calls a LLM provider on this module
func (m *Module) Generate(ctx context.Context, params *GenerateParams) (*GenerateResult, error) {
	if m.State() != ModuleStateReady {
		return nil, fmt.Errorf("module not ready: state=%s", m.State())
	}

	req, err := NewRequest(m.nextID(), MethodProviderGenerate, params)
	if err != nil {
		return nil, err
	}

	resp, err := m.conn.Send(ctx, req)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, resp.Error
	}

	var result GenerateResult
	if err := resp.ParseResult(&result); err != nil {
		return nil, fmt.Errorf("parse generate result: %w", err)
	}

	return &result, nil
}

// InvokeHook calls a hook on this module
func (m *Module) InvokeHook(ctx context.Context, params *HookInvokeParams) (*HookInvokeResult, error) {
	if m.State() != ModuleStateReady {
		return nil, fmt.Errorf("module not ready: state=%s", m.State())
	}

	req, err := NewRequest(m.nextID(), MethodHookInvoke, params)
	if err != nil {
		return nil, err
	}

	resp, err := m.conn.Send(ctx, req)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, resp.Error
	}

	var result HookInvokeResult
	if err := resp.ParseResult(&result); err != nil {
		return nil, fmt.Errorf("parse hook result: %w", err)
	}

	return &result, nil
}

// Stop gracefully shuts down the module
func (m *Module) Stop(ctx context.Context) error {
	if m.State() == ModuleStateStopped {
		return nil
	}

	m.state.Store(int32(ModuleStateStopping))
	m.logger.Info("Stopping module")

	// Send shutdown notification
	if m.conn != nil {
		shutdownReq, _ := NewNotification(MethodShutdown, nil)
		if err := m.conn.SendNotification(shutdownReq); err != nil {
			m.logger.Debug("Shutdown notification failed", zap.Error(err))
		}

		// Give the process time to exit gracefully
		time.Sleep(500 * time.Millisecond)
		if err := m.conn.Close(); err != nil {
			m.logger.Debug("Connection close failed", zap.Error(err))
		}
	}

	// Kill process if still running
	if m.process != nil {
		if err := m.process.Kill(); err != nil {
			m.logger.Debug("Process kill failed", zap.Error(err))
		}
	}

	m.state.Store(int32(ModuleStateStopped))
	return nil
}

func (m *Module) nextID() int {
	return int(m.requestID.Add(1))
}

// logWriter adapts module stderr to zap logger
type logWriter struct {
	logger *zap.Logger
	level  string
}

func (w *logWriter) Write(p []byte) (int, error) {
	msg := strings.TrimSpace(string(p))
	if msg != "" {
		w.logger.Warn("module stderr", zap.String("output", msg))
	}
	return len(p), nil
}
