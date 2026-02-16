package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"go.uber.org/zap"
)

// Plugin 插件接口
type Plugin interface {
	// Name 插件名称
	Name() string
	// Version 插件版本
	Version() string
	// Init 初始化插件
	Init(ctx context.Context, config map[string]interface{}) error
	// Execute 执行插件
	Execute(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error)
	// Shutdown 关闭插件
	Shutdown(ctx context.Context) error
}

// PluginMeta 插件元数据
type PluginMeta struct {
	Name        string                 `json:"name"`
	Version     string                 `json:"version"`
	Description string                 `json:"description"`
	Author      string                 `json:"author"`
	EntryPoint  string                 `json:"entry_point"`
	Config      map[string]interface{} `json:"config"`
	Enabled     bool                   `json:"enabled"`
}

// LoadedPlugin 已加载插件
type LoadedPlugin struct {
	Meta      PluginMeta
	Instance  Plugin
	LoadedAt  time.Time
	Path      string
	IsRunning bool
}

// Loader 插件加载器
type Loader struct {
	pluginDir   string
	plugins     map[string]*LoadedPlugin
	factories   map[string]PluginFactory
	watcher     *fsnotify.Watcher
	logger      *zap.Logger
	mu          sync.RWMutex
	onLoad      func(name string)
	onUnload    func(name string)
	onReload    func(name string)
}

// PluginFactory 插件工厂函数
type PluginFactory func(meta PluginMeta) (Plugin, error)

// LoaderConfig 加载器配置
type LoaderConfig struct {
	PluginDir      string
	WatchInterval  time.Duration
	EnableHotLoad  bool
}

// NewLoader 创建插件加载器
func NewLoader(config *LoaderConfig, logger *zap.Logger) (*Loader, error) {
	if config.PluginDir == "" {
		config.PluginDir = "./plugins"
	}

	// 确保目录存在
	if err := os.MkdirAll(config.PluginDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create plugin dir: %w", err)
	}

	loader := &Loader{
		pluginDir: config.PluginDir,
		plugins:   make(map[string]*LoadedPlugin),
		factories: make(map[string]PluginFactory),
		logger:    logger,
	}

	// 创建文件监视器 (热加载)
	if config.EnableHotLoad {
		watcher, err := fsnotify.NewWatcher()
		if err != nil {
			return nil, fmt.Errorf("failed to create watcher: %w", err)
		}
		loader.watcher = watcher
	}

	return loader, nil
}

// RegisterFactory 注册插件工厂
func (l *Loader) RegisterFactory(name string, factory PluginFactory) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.factories[name] = factory
}

// LoadAll 加载所有插件
func (l *Loader) LoadAll(ctx context.Context) error {
	entries, err := os.ReadDir(l.pluginDir)
	if err != nil {
		return fmt.Errorf("failed to read plugin dir: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		pluginPath := filepath.Join(l.pluginDir, entry.Name())
		if err := l.Load(ctx, pluginPath); err != nil {
			l.logger.Error("Failed to load plugin",
				zap.String("path", pluginPath),
				zap.Error(err),
			)
		}
	}

	return nil
}

// Load 加载单个插件
func (l *Loader) Load(ctx context.Context, pluginPath string) error {
	// 读取插件配置
	metaPath := filepath.Join(pluginPath, "plugin.json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return fmt.Errorf("failed to read plugin.json: %w", err)
	}

	var meta PluginMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return fmt.Errorf("failed to parse plugin.json: %w", err)
	}

	if !meta.Enabled {
		l.logger.Info("Plugin disabled, skipping",
			zap.String("name", meta.Name),
		)
		return nil
	}

	// 获取工厂
	l.mu.RLock()
	factory, exists := l.factories[meta.EntryPoint]
	l.mu.RUnlock()

	if !exists {
		return fmt.Errorf("no factory registered for entry point: %s", meta.EntryPoint)
	}

	// 创建插件实例
	instance, err := factory(meta)
	if err != nil {
		return fmt.Errorf("failed to create plugin instance: %w", err)
	}

	// 初始化插件
	if err := instance.Init(ctx, meta.Config); err != nil {
		return fmt.Errorf("failed to init plugin: %w", err)
	}

	// 注册插件
	l.mu.Lock()
	l.plugins[meta.Name] = &LoadedPlugin{
		Meta:      meta,
		Instance:  instance,
		LoadedAt:  time.Now(),
		Path:      pluginPath,
		IsRunning: true,
	}
	l.mu.Unlock()

	l.logger.Info("Plugin loaded",
		zap.String("name", meta.Name),
		zap.String("version", meta.Version),
	)

	if l.onLoad != nil {
		l.onLoad(meta.Name)
	}

	return nil
}

// Unload 卸载插件
func (l *Loader) Unload(ctx context.Context, name string) error {
	l.mu.Lock()
	plugin, exists := l.plugins[name]
	if !exists {
		l.mu.Unlock()
		return fmt.Errorf("plugin not found: %s", name)
	}
	delete(l.plugins, name)
	l.mu.Unlock()

	// 关闭插件
	if err := plugin.Instance.Shutdown(ctx); err != nil {
		l.logger.Error("Failed to shutdown plugin",
			zap.String("name", name),
			zap.Error(err),
		)
	}

	l.logger.Info("Plugin unloaded", zap.String("name", name))

	if l.onUnload != nil {
		l.onUnload(name)
	}

	return nil
}

// Reload 重新加载插件
func (l *Loader) Reload(ctx context.Context, name string) error {
	l.mu.RLock()
	plugin, exists := l.plugins[name]
	l.mu.RUnlock()

	if !exists {
		return fmt.Errorf("plugin not found: %s", name)
	}

	path := plugin.Path

	// 卸载
	if err := l.Unload(ctx, name); err != nil {
		return err
	}

	// 重新加载
	if err := l.Load(ctx, path); err != nil {
		return err
	}

	if l.onReload != nil {
		l.onReload(name)
	}

	return nil
}

// Get 获取插件
func (l *Loader) Get(name string) (Plugin, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	plugin, exists := l.plugins[name]
	if !exists {
		return nil, false
	}
	return plugin.Instance, true
}

// List 列出所有插件
func (l *Loader) List() []PluginMeta {
	l.mu.RLock()
	defer l.mu.RUnlock()

	result := make([]PluginMeta, 0, len(l.plugins))
	for _, p := range l.plugins {
		result = append(result, p.Meta)
	}
	return result
}

// StartWatching 启动热加载监视
func (l *Loader) StartWatching(ctx context.Context) error {
	if l.watcher == nil {
		return nil
	}

	if err := l.watcher.Add(l.pluginDir); err != nil {
		return fmt.Errorf("failed to watch plugin dir: %w", err)
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-l.watcher.Events:
				if !ok {
					return
				}
				l.handleWatchEvent(ctx, event)
			case err, ok := <-l.watcher.Errors:
				if !ok {
					return
				}
				l.logger.Error("Watcher error", zap.Error(err))
			}
		}
	}()

	l.logger.Info("Plugin hot-reload watching started",
		zap.String("dir", l.pluginDir),
	)

	return nil
}

// handleWatchEvent 处理文件变更事件
func (l *Loader) handleWatchEvent(ctx context.Context, event fsnotify.Event) {
	// 仅处理 plugin.json 变更
	if filepath.Base(event.Name) != "plugin.json" {
		return
	}

	pluginDir := filepath.Dir(event.Name)
	pluginName := filepath.Base(pluginDir)

	switch {
	case event.Op&fsnotify.Write == fsnotify.Write:
		l.logger.Info("Plugin config changed, reloading",
			zap.String("plugin", pluginName),
		)
		l.Reload(ctx, pluginName)

	case event.Op&fsnotify.Create == fsnotify.Create:
		l.logger.Info("New plugin detected, loading",
			zap.String("plugin", pluginName),
		)
		l.Load(ctx, pluginDir)

	case event.Op&fsnotify.Remove == fsnotify.Remove:
		l.logger.Info("Plugin removed, unloading",
			zap.String("plugin", pluginName),
		)
		l.Unload(ctx, pluginName)
	}
}

// SetCallbacks 设置回调
func (l *Loader) SetCallbacks(onLoad, onUnload, onReload func(string)) {
	l.onLoad = onLoad
	l.onUnload = onUnload
	l.onReload = onReload
}

// Close 关闭加载器
func (l *Loader) Close() error {
	if l.watcher != nil {
		return l.watcher.Close()
	}
	return nil
}

// Execute 执行插件
func (l *Loader) Execute(ctx context.Context, name string, input map[string]interface{}) (map[string]interface{}, error) {
	plugin, exists := l.Get(name)
	if !exists {
		return nil, fmt.Errorf("plugin not found: %s", name)
	}

	return plugin.Execute(ctx, input)
}
