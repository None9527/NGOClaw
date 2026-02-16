package plugin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap"
)

// testPlugin implements Plugin for testing
type testPlugin struct {
	name    string
	version string
	inited  bool
	shut    bool
}

func (p *testPlugin) Name() string    { return p.name }
func (p *testPlugin) Version() string { return p.version }
func (p *testPlugin) Init(_ context.Context, _ map[string]interface{}) error {
	p.inited = true
	return nil
}
func (p *testPlugin) Execute(_ context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{"echo": input}, nil
}
func (p *testPlugin) Shutdown(_ context.Context) error {
	p.shut = true
	return nil
}

func setupTestLoader(t *testing.T) (*Loader, string) {
	t.Helper()

	dir := t.TempDir()
	loader, err := NewLoader(&LoaderConfig{
		PluginDir:     dir,
		EnableHotLoad: false,
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("failed to create loader: %v", err)
	}

	return loader, dir
}

func createPluginDir(t *testing.T, baseDir, name string, meta PluginMeta) string {
	t.Helper()

	pluginDir := filepath.Join(baseDir, name)
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("failed to create plugin dir: %v", err)
	}

	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("failed to marshal meta: %v", err)
	}

	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), data, 0644); err != nil {
		t.Fatalf("failed to write plugin.json: %v", err)
	}

	return pluginDir
}

func TestLoader_LoadAll_EmptyDir(t *testing.T) {
	loader, _ := setupTestLoader(t)

	err := loader.LoadAll(context.Background())
	if err != nil {
		t.Fatalf("LoadAll on empty dir should succeed: %v", err)
	}

	if len(loader.List()) != 0 {
		t.Errorf("expected 0 plugins, got %d", len(loader.List()))
	}
}

func TestLoader_Load_ValidPlugin(t *testing.T) {
	loader, dir := setupTestLoader(t)

	// Register factory
	loader.RegisterFactory("test_entry", func(meta PluginMeta) (Plugin, error) {
		return &testPlugin{name: meta.Name, version: meta.Version}, nil
	})

	// Create plugin directory with valid manifest
	createPluginDir(t, dir, "hello_plugin", PluginMeta{
		Name:       "hello_plugin",
		Version:    "1.0.0",
		EntryPoint: "test_entry",
		Enabled:    true,
	})

	err := loader.LoadAll(context.Background())
	if err != nil {
		t.Fatalf("LoadAll should succeed: %v", err)
	}

	plugins := loader.List()
	if len(plugins) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(plugins))
	}
	if plugins[0].Name != "hello_plugin" {
		t.Errorf("expected plugin name 'hello_plugin', got %q", plugins[0].Name)
	}
}

func TestLoader_Load_DisabledPlugin(t *testing.T) {
	loader, dir := setupTestLoader(t)

	loader.RegisterFactory("test_entry", func(meta PluginMeta) (Plugin, error) {
		return &testPlugin{name: meta.Name, version: meta.Version}, nil
	})

	createPluginDir(t, dir, "disabled_plugin", PluginMeta{
		Name:       "disabled_plugin",
		Version:    "1.0.0",
		EntryPoint: "test_entry",
		Enabled:    false,
	})

	loader.LoadAll(context.Background())

	if len(loader.List()) != 0 {
		t.Errorf("disabled plugin should not be loaded, got %d plugins", len(loader.List()))
	}
}

func TestLoader_Load_InvalidManifest(t *testing.T) {
	loader, dir := setupTestLoader(t)

	// Create directory with invalid JSON
	pluginDir := filepath.Join(dir, "bad_plugin")
	os.MkdirAll(pluginDir, 0755)
	os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte("{invalid"), 0644)

	// Should not panic, just log error
	err := loader.LoadAll(context.Background())
	if err != nil {
		t.Fatalf("LoadAll should not fail overall: %v", err)
	}
	if len(loader.List()) != 0 {
		t.Error("invalid plugin should not be loaded")
	}
}

func TestLoader_Execute(t *testing.T) {
	loader, dir := setupTestLoader(t)

	loader.RegisterFactory("test_entry", func(meta PluginMeta) (Plugin, error) {
		return &testPlugin{name: meta.Name, version: meta.Version}, nil
	})

	createPluginDir(t, dir, "exec_plugin", PluginMeta{
		Name:       "exec_plugin",
		Version:    "1.0.0",
		EntryPoint: "test_entry",
		Enabled:    true,
	})

	loader.LoadAll(context.Background())

	result, err := loader.Execute(context.Background(), "exec_plugin", map[string]interface{}{
		"key": "value",
	})
	if err != nil {
		t.Fatalf("Execute should succeed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestLoader_Unload(t *testing.T) {
	loader, dir := setupTestLoader(t)

	var pluginInstance *testPlugin
	loader.RegisterFactory("test_entry", func(meta PluginMeta) (Plugin, error) {
		pluginInstance = &testPlugin{name: meta.Name, version: meta.Version}
		return pluginInstance, nil
	})

	createPluginDir(t, dir, "unload_plugin", PluginMeta{
		Name:       "unload_plugin",
		Version:    "1.0.0",
		EntryPoint: "test_entry",
		Enabled:    true,
	})

	loader.LoadAll(context.Background())

	if len(loader.List()) != 1 {
		t.Fatal("expected 1 plugin loaded")
	}

	err := loader.Unload(context.Background(), "unload_plugin")
	if err != nil {
		t.Fatalf("Unload should succeed: %v", err)
	}

	if len(loader.List()) != 0 {
		t.Error("expected 0 plugins after unload")
	}

	if !pluginInstance.shut {
		t.Error("plugin Shutdown should have been called")
	}
}

func TestLoader_Callbacks(t *testing.T) {
	loader, dir := setupTestLoader(t)

	var loadedNames []string
	var unloadedNames []string

	loader.SetCallbacks(
		func(name string) { loadedNames = append(loadedNames, name) },
		func(name string) { unloadedNames = append(unloadedNames, name) },
		nil,
	)

	loader.RegisterFactory("test_entry", func(meta PluginMeta) (Plugin, error) {
		return &testPlugin{name: meta.Name, version: meta.Version}, nil
	})

	createPluginDir(t, dir, "callback_plugin", PluginMeta{
		Name:       "callback_plugin",
		Version:    "1.0.0",
		EntryPoint: "test_entry",
		Enabled:    true,
	})

	loader.LoadAll(context.Background())
	loader.Unload(context.Background(), "callback_plugin")

	if len(loadedNames) != 1 || loadedNames[0] != "callback_plugin" {
		t.Errorf("expected load callback for 'callback_plugin', got %v", loadedNames)
	}
	if len(unloadedNames) != 1 || unloadedNames[0] != "callback_plugin" {
		t.Errorf("expected unload callback for 'callback_plugin', got %v", unloadedNames)
	}
}

func TestExtensionRegistry_RegisterAndUnregister(t *testing.T) {
	registry := NewExtensionRegistry(zap.NewNop())
	mockRegistrar := &mockToolRegistrar{tools: make(map[string]bool)}

	// Register a tool from plugin
	err := registry.RegisterToolFromPlugin(
		"my_plugin", "my_tool",
		"A test tool",
		map[string]interface{}{"type": "object"},
		func(args map[string]interface{}) (string, error) { return "ok", nil },
		mockRegistrar,
	)
	if err != nil {
		t.Fatalf("RegisterToolFromPlugin failed: %v", err)
	}

	tools := registry.GetPluginTools("my_plugin")
	if len(tools) != 1 || tools[0] != "my_tool" {
		t.Errorf("expected [my_tool], got %v", tools)
	}

	if !mockRegistrar.tools["my_tool"] {
		t.Error("tool should be registered in registrar")
	}

	// Unregister
	registry.UnregisterPluginTools("my_plugin", mockRegistrar)
	tools = registry.GetPluginTools("my_plugin")
	if len(tools) != 0 {
		t.Errorf("expected empty tools after unregister, got %v", tools)
	}

	if mockRegistrar.tools["my_tool"] {
		t.Error("tool should be unregistered from registrar")
	}
}

// mockToolRegistrar implements ToolRegistrar for testing
type mockToolRegistrar struct {
	tools map[string]bool
}

func (m *mockToolRegistrar) RegisterDynamic(name, _ string, _ map[string]interface{}, _ func(args map[string]interface{}) (string, error)) error {
	m.tools[name] = true
	return nil
}

func (m *mockToolRegistrar) Unregister(name string) {
	delete(m.tools, name)
}
