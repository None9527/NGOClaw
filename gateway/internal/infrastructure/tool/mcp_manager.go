package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/ngoclaw/ngoclaw/gateway/internal/infrastructure/config"
	domaintool "github.com/ngoclaw/ngoclaw/gateway/internal/domain/tool"
	"go.uber.org/zap"
)

// MCPServerInfo is a read-only view of a managed MCP server.
type MCPServerInfo struct {
	Name      string `json:"name"`
	Endpoint  string `json:"endpoint"`
	Enabled   bool   `json:"enabled"`
	ToolCount int    `json:"tool_count"`
}

// MCPManager manages MCP server lifecycle: add/remove/refresh with
// automatic tool registration and config persistence to ~/.ngoclaw/mcp.json.
type MCPManager struct {
	configPath string
	adapters   map[string]*MCPAdapter
	registry   domaintool.Registry
	logger     *zap.Logger
	mu         sync.RWMutex
}

// NewMCPManager creates a manager and loads existing servers from mcp.json.
func NewMCPManager(configPath string, registry domaintool.Registry, logger *zap.Logger) *MCPManager {
	return &MCPManager{
		configPath: configPath,
		adapters:   make(map[string]*MCPAdapter),
		registry:   registry,
		logger:     logger,
	}
}

// InitFromConfig loads mcp.json and discovers tools for all enabled servers.
func (m *MCPManager) InitFromConfig() {
	cfg, err := m.loadConfig()
	if err != nil {
		m.logger.Warn("Failed to load mcp.json, starting with empty MCP config",
			zap.Error(err),
		)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, srv := range cfg.Servers {
		if !srv.Enabled {
			m.logger.Info("MCP server disabled, skipping", zap.String("name", srv.Name))
			continue
		}

		if err := m.addAndDiscover(ctx, srv.Name, srv.Endpoint); err != nil {
			m.logger.Error("MCP server init failed",
				zap.String("name", srv.Name),
				zap.String("endpoint", srv.Endpoint),
				zap.Error(err),
			)
		}
	}
}

// AddServer adds a new MCP server, discovers its tools, registers them,
// and persists the configuration to mcp.json. Hot-pluggable, no restart needed.
func (m *MCPManager) AddServer(name, endpoint string) error {
	m.mu.Lock()
	if _, exists := m.adapters[name]; exists {
		m.mu.Unlock()
		return fmt.Errorf("MCP server '%s' already exists", name)
	}
	m.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := m.addAndDiscover(ctx, name, endpoint); err != nil {
		return err
	}

	// Persist
	return m.persistAdd(name, endpoint)
}

// RemoveServer unregisters all tools from a server and removes it from config.
func (m *MCPManager) RemoveServer(name string) error {
	m.mu.Lock()
	adapter, exists := m.adapters[name]
	if !exists {
		m.mu.Unlock()
		return fmt.Errorf("MCP server '%s' not found", name)
	}

	// Unregister all tools from this server
	for _, def := range adapter.GetTools() {
		toolName := fmt.Sprintf("%s_%s", name, def.Name)
		if err := m.registry.Unregister(toolName); err != nil {
			m.logger.Warn("Failed to unregister MCP tool",
				zap.String("tool", toolName),
				zap.Error(err),
			)
		}
	}
	delete(m.adapters, name)
	m.mu.Unlock()

	m.logger.Info("MCP server removed", zap.String("name", name))

	return m.persistRemove(name)
}

// ListServers returns info about all managed MCP servers.
func (m *MCPManager) ListServers() []MCPServerInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Read from config to include disabled servers too
	cfg, err := m.loadConfig()
	if err != nil {
		// Fallback to in-memory adapters
		var infos []MCPServerInfo
		for name, adapter := range m.adapters {
			infos = append(infos, MCPServerInfo{
				Name:      name,
				Endpoint:  adapter.endpoint,
				Enabled:   true,
				ToolCount: len(adapter.GetTools()),
			})
		}
		return infos
	}

	var infos []MCPServerInfo
	for _, srv := range cfg.Servers {
		info := MCPServerInfo{
			Name:     srv.Name,
			Endpoint: srv.Endpoint,
			Enabled:  srv.Enabled,
		}
		if adapter, ok := m.adapters[srv.Name]; ok {
			info.ToolCount = len(adapter.GetTools())
		}
		infos = append(infos, info)
	}
	return infos
}

// RefreshServer re-discovers tools for an existing server.
func (m *MCPManager) RefreshServer(name string) error {
	m.mu.RLock()
	adapter, exists := m.adapters[name]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("MCP server '%s' not found", name)
	}

	// Unregister old tools
	for _, def := range adapter.GetTools() {
		toolName := fmt.Sprintf("%s_%s", name, def.Name)
		_ = m.registry.Unregister(toolName)
	}

	// Re-discover
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	count, err := RegisterMCPTools(ctx, adapter, m.registry, m.logger)
	if err != nil {
		return err
	}

	m.logger.Info("MCP server refreshed",
		zap.String("name", name),
		zap.Int("tools", count),
	)
	return nil
}

// ── internal ──

func (m *MCPManager) addAndDiscover(ctx context.Context, name, endpoint string) error {
	adapter := NewMCPAdapter(name, endpoint, m.logger)
	count, err := RegisterMCPTools(ctx, adapter, m.registry, m.logger)
	if err != nil {
		return fmt.Errorf("MCP discovery failed for %s: %w", name, err)
	}

	m.mu.Lock()
	m.adapters[name] = adapter
	m.mu.Unlock()

	m.logger.Info("MCP server added",
		zap.String("name", name),
		zap.String("endpoint", endpoint),
		zap.Int("tools", count),
	)
	return nil
}

func (m *MCPManager) loadConfig() (*config.MCPFileConfig, error) {
	data, err := os.ReadFile(m.configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &config.MCPFileConfig{Servers: []config.MCPServerEntry{}}, nil
		}
		return nil, err
	}
	var cfg config.MCPFileConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (m *MCPManager) persistAdd(name, endpoint string) error {
	cfg := m.readOrCreateConfig()
	cfg.Servers = append(cfg.Servers, config.MCPServerEntry{
		Name:     name,
		Endpoint: endpoint,
		Enabled:  true,
	})
	return config.SaveMCPConfig(m.configPath, cfg)
}

func (m *MCPManager) persistRemove(name string) error {
	cfg := m.readOrCreateConfig()
	var filtered []config.MCPServerEntry
	for _, s := range cfg.Servers {
		if s.Name != name {
			filtered = append(filtered, s)
		}
	}
	cfg.Servers = filtered
	return config.SaveMCPConfig(m.configPath, cfg)
}

func (m *MCPManager) readOrCreateConfig() *config.MCPFileConfig {
	data, err := os.ReadFile(m.configPath)
	if err != nil {
		return &config.MCPFileConfig{Servers: []config.MCPServerEntry{}}
	}
	var cfg config.MCPFileConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return &config.MCPFileConfig{Servers: []config.MCPServerEntry{}}
	}
	return &cfg
}
