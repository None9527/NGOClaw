package prompt

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"
)

// RuntimeBlockOptions holds runtime options for the environment block.
type RuntimeBlockOptions struct {
	Channel   string // "cli", "telegram", "api", "grpc"
	ModelName string // Current model identifier
	Workspace string // Working directory
}

// BuildRuntimeBlock generates the runtime environment section of the system prompt.
// This is purely factual (OS, time, model, workspace) — no behavioral directives.
// Behavioral directives belong in soul.md and prompts/*.md (user-editable).
func BuildRuntimeBlock(opts RuntimeBlockOptions) string {
	hostname, _ := os.Hostname()
	user := os.Getenv("USER")
	if user == "" {
		user = "unknown"
	}
	homeDir, _ := os.UserHomeDir()
	now := time.Now().Format("2006-01-02 15:04:05 MST")

	channelInfo := "API"
	if opts.Channel != "" {
		channelInfo = opts.Channel
	}

	modelInfo := "unknown"
	if opts.ModelName != "" {
		modelInfo = opts.ModelName
	}

	workspace := homeDir
	if opts.Workspace != "" {
		workspace = opts.Workspace
	}

	// Detect Python (configured env > system python3 > not available)
	pythonInfo := "not available"
	if p := os.Getenv("NGOCLAW_PYTHON"); p != "" {
		pythonInfo = p
	} else if _, err := exec.LookPath("python3"); err == nil {
		pythonInfo = "python3"
	}

	return fmt.Sprintf(`## 系统环境

- 系统: %s/%s | 主机: %s
- 用户: %s | HOME: %s
- 时间: %s
- 通道: %s
- 模型: %s
- Shell: bash | Python: %s

## Workspace

工作目录: %s
命令在用户真实环境中执行，~/.ssh、~/.config 等路径均可正常访问。
所有文件操作默认在此目录下进行，除非用户指定其他路径。`,
		runtime.GOOS, runtime.GOARCH, hostname,
		user, homeDir, now,
		channelInfo, modelInfo,
		pythonInfo,
		workspace)
}

// BasePromptOptions is kept for backward compatibility during migration.
// Deprecated: Use RuntimeBlockOptions instead.
type BasePromptOptions = RuntimeBlockOptions

// BasePrompt is kept for backward compatibility during migration.
// Deprecated: Use BuildRuntimeBlock instead.
func BasePrompt(opts BasePromptOptions) string {
	return BuildRuntimeBlock(opts)
}
