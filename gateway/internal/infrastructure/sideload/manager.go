package sideload

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	domaintool "github.com/ngoclaw/ngoclaw/gateway/internal/domain/tool"
	"go.uber.org/zap"
)

// Manager discovers, starts, and manages sideload modules.
// It integrates modules into the tool registry so that
// sideloaded tools appear alongside Go-native builtins.
type Manager struct {
	modules    map[string]*Module
	registry   domaintool.Registry
	globalDir  string
	projectDir string
	logger     *zap.Logger
	mu         sync.RWMutex
}

// NewManager creates a new module manager
func NewManager(registry domaintool.Registry, logger *zap.Logger) *Manager {
	homeDir, _ := os.UserHomeDir()
	return &Manager{
		modules:    make(map[string]*Module),
		registry:   registry,
		globalDir:  filepath.Join(homeDir, ".ngoclaw", "modules"),
		projectDir: filepath.Join(".", ".ngoclaw", "modules"),
		logger:     logger,
	}
}

// SetProjectDir sets the project-level module directory
func (mgr *Manager) SetProjectDir(dir string) {
	mgr.projectDir = filepath.Join(dir, ".ngoclaw", "modules")
}

// DiscoverAndStart finds all modules and starts them
func (mgr *Manager) DiscoverAndStart(ctx context.Context) error {
	discovered, err := DiscoverModules(mgr.globalDir, mgr.projectDir)
	if err != nil {
		return fmt.Errorf("discover modules: %w", err)
	}

	mgr.logger.Info("Discovered modules",
		zap.Int("count", len(discovered)),
		zap.String("global_dir", mgr.globalDir),
		zap.String("project_dir", mgr.projectDir),
	)

	var startErrors []error

	for _, disc := range discovered {
		module := NewModule(disc, mgr.logger)

		// Start with timeout
		startCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		if err := module.Start(startCtx); err != nil {
			cancel()
			mgr.logger.Error("Failed to start module",
				zap.String("module", disc.Manifest.Name),
				zap.Error(err),
			)
			startErrors = append(startErrors, fmt.Errorf("module %s: %w", disc.Manifest.Name, err))
			continue
		}
		cancel()

		// Register module
		mgr.mu.Lock()
		mgr.modules[module.Name()] = module
		mgr.mu.Unlock()

		// Register module's tools into the global registry
		if err := mgr.registerModuleTools(module); err != nil {
			mgr.logger.Error("Failed to register module tools",
				zap.String("module", module.Name()),
				zap.Error(err),
			)
		}

		mgr.logger.Info("Module started successfully",
			zap.String("module", module.Name()),
			zap.String("state", module.State().String()),
		)
	}

	if len(startErrors) > 0 {
		mgr.logger.Warn("Some modules failed to start",
			zap.Int("failed", len(startErrors)),
			zap.Int("succeeded", len(mgr.modules)),
		)
	}

	return nil
}

// registerModuleTools creates wrapped tools for each tool the module exposes
func (mgr *Manager) registerModuleTools(module *Module) error {
	caps := module.Capabilities()
	if caps == nil {
		return nil
	}

	for _, tc := range caps.Tools {
		toolWrapper := &sideloadTool{
			module:      module,
			name:        tc.Name,
			description: tc.Description,
			schema:      tc.InputSchema,
		}

		if err := mgr.registry.Register(toolWrapper); err != nil {
			mgr.logger.Warn("Failed to register sideload tool",
				zap.String("module", module.Name()),
				zap.String("tool", tc.Name),
				zap.Error(err),
			)
		} else {
			mgr.logger.Info("Registered sideload tool",
				zap.String("module", module.Name()),
				zap.String("tool", tc.Name),
			)
		}
	}

	return nil
}

// GetModule returns a specific module by name
func (mgr *Manager) GetModule(name string) (*Module, bool) {
	mgr.mu.RLock()
	defer mgr.mu.RUnlock()
	m, ok := mgr.modules[name]
	return m, ok
}

// GetProviderModule finds the module that provides a specific LLM provider
func (mgr *Manager) GetProviderModule(providerID string) (*Module, bool) {
	mgr.mu.RLock()
	defer mgr.mu.RUnlock()

	for _, module := range mgr.modules {
		caps := module.Capabilities()
		if caps == nil {
			continue
		}
		for _, p := range caps.Providers {
			if p.ID == providerID {
				return module, true
			}
		}
	}
	return nil, false
}

// ListModules returns all loaded module names and states
func (mgr *Manager) ListModules() map[string]string {
	mgr.mu.RLock()
	defer mgr.mu.RUnlock()

	result := make(map[string]string)
	for name, module := range mgr.modules {
		result[name] = module.State().String()
	}
	return result
}

// StopAll gracefully stops all modules
func (mgr *Manager) StopAll(ctx context.Context) {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	for name, module := range mgr.modules {
		if err := module.Stop(ctx); err != nil {
			mgr.logger.Error("Failed to stop module",
				zap.String("module", name),
				zap.Error(err),
			)
		}

		// Unregister tools
		caps := module.Capabilities()
		if caps != nil {
			for _, tc := range caps.Tools {
			if err := mgr.registry.Unregister(tc.Name); err != nil {
					mgr.logger.Debug("Failed to unregister tool during shutdown",
						zap.String("tool", tc.Name),
						zap.Error(err),
					)
				}
			}
		}
	}

	mgr.modules = make(map[string]*Module)
	mgr.logger.Info("All modules stopped")
}

// sideloadTool wraps a remote module's tool as a domaintool.Tool
type sideloadTool struct {
	module      *Module
	name        string
	description string
	schema      map[string]interface{}
}

func (t *sideloadTool) Name() string             { return t.name }
func (t *sideloadTool) Kind() domaintool.Kind      { return domaintool.KindExecute }
func (t *sideloadTool) Description() string        { return t.description }

func (t *sideloadTool) Schema() map[string]interface{} {
	if t.schema != nil {
		return t.schema
	}
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}

func (t *sideloadTool) Execute(ctx context.Context, args map[string]interface{}) (*domaintool.Result, error) {
	result, err := t.module.ExecuteTool(ctx, &ToolExecuteParams{
		Name:      t.name,
		Arguments: args,
	})
	if err != nil {
		return &domaintool.Result{
			Output:  fmt.Sprintf("Sideload tool '%s' error: %v", t.name, err),
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	return &domaintool.Result{
		Output:   result.Output,
		Success:  result.Success,
		Metadata: result.Metadata,
		Error:    result.Error,
	}, nil
}
