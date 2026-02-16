package plugin

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// ScriptPlugin 脚本插件 (Python/Bash/Node)
type ScriptPlugin struct {
	meta       PluginMeta
	scriptPath string
	runtime    string // python, bash, node
}

// NewScriptPlugin 创建脚本插件
func NewScriptPlugin(meta PluginMeta) (Plugin, error) {
	config := meta.Config
	
	scriptPath, ok := config["script"].(string)
	if !ok {
		return nil, fmt.Errorf("script path not specified")
	}

	runtime, _ := config["runtime"].(string)
	if runtime == "" {
		// 根据扩展名推断
		if strings.HasSuffix(scriptPath, ".py") {
			runtime = "python3"
		} else if strings.HasSuffix(scriptPath, ".sh") {
			runtime = "bash"
		} else if strings.HasSuffix(scriptPath, ".js") {
			runtime = "node"
		} else {
			runtime = "bash"
		}
	}

	return &ScriptPlugin{
		meta:       meta,
		scriptPath: scriptPath,
		runtime:    runtime,
	}, nil
}

func (p *ScriptPlugin) Name() string    { return p.meta.Name }
func (p *ScriptPlugin) Version() string { return p.meta.Version }

func (p *ScriptPlugin) Init(ctx context.Context, config map[string]interface{}) error {
	return nil
}

func (p *ScriptPlugin) Execute(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	// 将输入作为参数传递
	args := []string{p.scriptPath}
	if inputStr, ok := input["input"].(string); ok {
		args = append(args, inputStr)
	}

	cmd := exec.CommandContext(ctx, p.runtime, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("script execution failed: %w, output: %s", err, string(output))
	}

	return map[string]interface{}{
		"output": strings.TrimSpace(string(output)),
	}, nil
}

func (p *ScriptPlugin) Shutdown(ctx context.Context) error {
	return nil
}

// ToolPlugin 工具插件 (扩展内置工具)
type ToolPlugin struct {
	meta     PluginMeta
	toolName string
	handler  func(ctx context.Context, args map[string]interface{}) (string, error)
}

// ToolPluginConfig 工具插件配置
type ToolPluginConfig struct {
	Name        string
	Description string
	Handler     func(ctx context.Context, args map[string]interface{}) (string, error)
}

// NewToolPlugin 创建工具插件
func NewToolPlugin(config ToolPluginConfig) Plugin {
	return &ToolPlugin{
		meta: PluginMeta{
			Name:        config.Name,
			Description: config.Description,
		},
		toolName: config.Name,
		handler:  config.Handler,
	}
}

func (p *ToolPlugin) Name() string    { return p.meta.Name }
func (p *ToolPlugin) Version() string { return "1.0.0" }

func (p *ToolPlugin) Init(ctx context.Context, config map[string]interface{}) error {
	return nil
}

func (p *ToolPlugin) Execute(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	output, err := p.handler(ctx, input)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"output": output}, nil
}

func (p *ToolPlugin) Shutdown(ctx context.Context) error {
	return nil
}

// BuiltinPlugins 注册内置插件工厂
func RegisterBuiltinPlugins(loader *Loader) {
	// 脚本插件工厂
	loader.RegisterFactory("script", func(meta PluginMeta) (Plugin, error) {
		return NewScriptPlugin(meta)
	})

	// HTTP 请求插件
	loader.RegisterFactory("http_request", func(meta PluginMeta) (Plugin, error) {
		return NewToolPlugin(ToolPluginConfig{
			Name:        "http_request",
			Description: "发送 HTTP 请求",
			Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
				// 简单实现，实际可扩展
				url, _ := args["url"].(string)
				return fmt.Sprintf("HTTP request to: %s", url), nil
			},
		}), nil
	})

	// JSON 处理插件
	loader.RegisterFactory("json_processor", func(meta PluginMeta) (Plugin, error) {
		return NewToolPlugin(ToolPluginConfig{
			Name:        "json_processor",
			Description: "处理 JSON 数据",
			Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
				return "JSON processed", nil
			},
		}), nil
	})
}
