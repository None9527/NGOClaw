package plugin

import (
	"fmt"
	"sync"

	"go.uber.org/zap"
)

// ExtensionRegistry connects the plugin Loader to tool/command registries,
// enabling plugins to export tools and commands into the global namespace.
type ExtensionRegistry struct {
	// pluginTools tracks which tools were registered by which plugin
	pluginTools map[string][]string // plugin_name -> []tool_name
	logger      *zap.Logger
	mu          sync.RWMutex
}

// NewExtensionRegistry creates a new extension registry
func NewExtensionRegistry(logger *zap.Logger) *ExtensionRegistry {
	return &ExtensionRegistry{
		pluginTools: make(map[string][]string),
		logger:      logger,
	}
}

// ToolRegistrar is the interface for registering/unregistering tools
type ToolRegistrar interface {
	RegisterDynamic(name, description string, schema map[string]interface{}, handler func(args map[string]interface{}) (string, error)) error
	Unregister(name string)
}

// RegisterToolFromPlugin registers a tool exported by a plugin
func (r *ExtensionRegistry) RegisterToolFromPlugin(
	pluginName string,
	toolName string,
	description string,
	schema map[string]interface{},
	handler func(args map[string]interface{}) (string, error),
	registrar ToolRegistrar,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Register with the global tool registry
	if err := registrar.RegisterDynamic(toolName, description, schema, handler); err != nil {
		return fmt.Errorf("failed to register tool %s from plugin %s: %w", toolName, pluginName, err)
	}

	// Track the association
	r.pluginTools[pluginName] = append(r.pluginTools[pluginName], toolName)

	r.logger.Info("Plugin tool registered",
		zap.String("plugin", pluginName),
		zap.String("tool", toolName),
	)

	return nil
}

// UnregisterPluginTools removes all tools registered by a specific plugin
func (r *ExtensionRegistry) UnregisterPluginTools(pluginName string, registrar ToolRegistrar) {
	r.mu.Lock()
	defer r.mu.Unlock()

	tools, exists := r.pluginTools[pluginName]
	if !exists {
		return
	}

	for _, toolName := range tools {
		registrar.Unregister(toolName)
		r.logger.Info("Plugin tool unregistered",
			zap.String("plugin", pluginName),
			zap.String("tool", toolName),
		)
	}

	delete(r.pluginTools, pluginName)
}

// GetPluginTools returns the tools registered by a specific plugin
func (r *ExtensionRegistry) GetPluginTools(pluginName string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tools := r.pluginTools[pluginName]
	result := make([]string, len(tools))
	copy(result, tools)
	return result
}

// PluginCount returns the number of plugins with registered tools
func (r *ExtensionRegistry) PluginCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.pluginTools)
}

// SetupLoaderCallbacks wires the extension registry into the Loader's lifecycle
// so that plugin load/unload events automatically trigger tool registration/cleanup
func (r *ExtensionRegistry) SetupLoaderCallbacks(loader *Loader, registrar ToolRegistrar) {
	loader.SetCallbacks(
		// onLoad
		func(name string) {
			r.logger.Info("Plugin loaded, ready for tool registration",
				zap.String("plugin", name),
			)
		},
		// onUnload - clean up all tools from this plugin
		func(name string) {
			r.UnregisterPluginTools(name, registrar)
		},
		// onReload
		func(name string) {
			r.logger.Info("Plugin reloaded",
				zap.String("plugin", name),
			)
		},
	)
}
