package tool

import (
	"context"
	"fmt"
	"strings"

	domaintool "github.com/ngoclaw/ngoclaw/gateway/internal/domain/tool"
	"github.com/ngoclaw/ngoclaw/gateway/internal/infrastructure/sandbox"
	"go.uber.org/zap"
)

// Result 类型别名
type Result = domaintool.Result

// Kind 类型别名
type Kind = domaintool.Kind

// BashTool Bash 命令执行工具
type BashTool struct {
	sandbox *sandbox.ProcessSandbox
	logger  *zap.Logger
}

// NewBashTool 创建 Bash 工具
func NewBashTool(sandbox *sandbox.ProcessSandbox, logger *zap.Logger) *BashTool {
	return &BashTool{
		sandbox: sandbox,
		logger:  logger,
	}
}

// Name 返回工具名称
func (t *BashTool) Name() string {
	return "bash"
}

func (t *BashTool) Kind() domaintool.Kind { return domaintool.KindExecute }

// Description 返回工具描述
func (t *BashTool) Description() string {
	return `Execute bash commands in a sandboxed environment.
IMPORTANT constraints:
- Commands have a 60-second timeout. Exit code 124 means TIMEOUT (command killed).
- For SSH/network commands: ALWAYS use 'timeout 10' and '-o ConnectTimeout=5'.
- If a command fails twice with the same error, STOP retrying and report the issue to the user.
- Avoid interactive or long-running commands (e.g. top, watch, tail -f).
- Working directory defaults to /tmp/ngoclaw-sandbox unless work_dir is specified.
- Prefer simple, targeted commands over complex pipelines.`
}

// Schema 返回参数 JSON Schema
func (t *BashTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"command": map[string]interface{}{
				"type":        "string",
				"description": "The bash command to execute",
			},
			"work_dir": map[string]interface{}{
				"type":        "string",
				"description": "Optional working directory for the command",
			},
		},
		"required": []string{"command"},
	}
}

// Execute 执行 Bash 命令
func (t *BashTool) Execute(ctx context.Context, args map[string]interface{}) (*Result, error) {
	// 解析命令参数
	command, ok := args["command"].(string)
	if !ok || command == "" {
		return &Result{
			Success: false,
			Error:   "command is required",
		}, fmt.Errorf("command is required")
	}

	// 可选的工作目录
	if workDir, ok := args["work_dir"].(string); ok && workDir != "" {
		if err := t.sandbox.SetWorkDir(workDir); err != nil {
			return &Result{
				Success: false,
				Error:   err.Error(),
			}, err
		}
	}

	t.logger.Info("Executing bash command",
		zap.String("command", command),
	)

	// 执行命令
	result, err := t.sandbox.ExecuteShell(ctx, command)
	if err != nil {
		res := &Result{Success: false, Error: err.Error()}
		if result != nil {
			res.Output = result.Stderr
			res.Metadata = map[string]interface{}{
				"exit_code": result.ExitCode,
				"duration":  result.Duration.String(),
				"killed":    result.Killed,
			}
		}
		return res, nil
	}

	// 组合输出
	output := result.Stdout
	if result.Stderr != "" {
		output += "\n[stderr]\n" + result.Stderr
	}

	// P2.11: Generate concise Display for long output
	var display string
	if len(output) > 2000 {
		lines := strings.Split(output, "\n")
		lineCount := len(lines)
		charCount := len(output)

		// Show head + tail
		headLines := 5
		tailLines := 5
		if headLines+tailLines >= lineCount {
			headLines = lineCount / 2
			tailLines = lineCount - headLines
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("📋 `%s`\n", truncateCmd(command, 60)))
		if result.ExitCode == 0 {
			sb.WriteString(fmt.Sprintf("✅ exit=0 | %d lines | %d chars | %s\n", lineCount, charCount, result.Duration))
		} else {
			sb.WriteString(fmt.Sprintf("❌ exit=%d | %d lines | %s\n", result.ExitCode, lineCount, result.Duration))
		}
		sb.WriteString("```\n")
		for i := 0; i < headLines && i < lineCount; i++ {
			sb.WriteString(truncateLine(lines[i], 120) + "\n")
		}
		if headLines+tailLines < lineCount {
			sb.WriteString(fmt.Sprintf("... (%d lines omitted) ...\n", lineCount-headLines-tailLines))
		}
		for i := lineCount - tailLines; i < lineCount; i++ {
			if i >= headLines {
				sb.WriteString(truncateLine(lines[i], 120) + "\n")
			}
		}
		sb.WriteString("```")
		display = sb.String()
	}

	return &Result{
		Output:  output,
		Display: display,
		Success: result.ExitCode == 0,
		Metadata: map[string]interface{}{
			"exit_code": result.ExitCode,
			"duration":  result.Duration.String(),
		},
	}, nil
}

// truncateCmd shortens a command string for display
func truncateCmd(cmd string, maxLen int) string {
	cmd = strings.TrimSpace(cmd)
	if len(cmd) <= maxLen {
		return cmd
	}
	return cmd[:maxLen-3] + "..."
}

// truncateLine shortens a single line for display
func truncateLine(line string, maxLen int) string {
	if len(line) <= maxLen {
		return line
	}
	return line[:maxLen-3] + "..."
}

// ReadFileTool 读取文件工具
type ReadFileTool struct {
	sandbox *sandbox.ProcessSandbox
	logger  *zap.Logger
}

// NewReadFileTool 创建读取文件工具
func NewReadFileTool(sandbox *sandbox.ProcessSandbox, logger *zap.Logger) *ReadFileTool {
	return &ReadFileTool{
		sandbox: sandbox,
		logger:  logger,
	}
}

// Name 返回工具名称
func (t *ReadFileTool) Name() string {
	return "read_file"
}

func (t *ReadFileTool) Kind() domaintool.Kind { return domaintool.KindRead }

// Description 返回工具描述
func (t *ReadFileTool) Description() string {
	return "Read the contents of a file. Supports text files. Use this to examine source code, configuration files, and other text content."
}

// Schema 返回参数 JSON Schema
func (t *ReadFileTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "The path to the file to read",
			},
			"start_line": map[string]interface{}{
				"type":        "integer",
				"description": "Optional starting line number (1-indexed)",
			},
			"end_line": map[string]interface{}{
				"type":        "integer",
				"description": "Optional ending line number (1-indexed)",
			},
		},
		"required": []string{"path"},
	}
}

// Execute 读取文件
func (t *ReadFileTool) Execute(ctx context.Context, args map[string]interface{}) (*Result, error) {
	path, ok := args["path"].(string)
	if !ok || path == "" {
		return &Result{
			Success: false,
			Error:   "path is required",
		}, fmt.Errorf("path is required")
	}

	// 构建命令
	var cmd string
	startLine, hasStart := args["start_line"].(float64)
	endLine, hasEnd := args["end_line"].(float64)

	if hasStart && hasEnd {
		// 使用 sed 提取指定行范围
		cmd = fmt.Sprintf("sed -n '%d,%dp' '%s'", int(startLine), int(endLine), path)
	} else if hasStart {
		// 从指定行开始读取
		cmd = fmt.Sprintf("tail -n +%d '%s'", int(startLine), path)
	} else {
		// 读取整个文件
		cmd = fmt.Sprintf("cat '%s'", path)
	}

	result, err := t.sandbox.ExecuteShell(ctx, cmd)
	if err != nil {
		errMsg := err.Error()
		if result != nil {
			errMsg = result.Stderr
		}
		return &Result{Success: false, Error: errMsg}, nil
	}

	return &Result{
		Output:  result.Stdout,
		Success: true,
		Metadata: map[string]interface{}{
			"path": path,
		},
	}, nil
}

// WriteFileTool 写入文件工具
type WriteFileTool struct {
	sandbox *sandbox.ProcessSandbox
	logger  *zap.Logger
}

// NewWriteFileTool 创建写入文件工具
func NewWriteFileTool(sandbox *sandbox.ProcessSandbox, logger *zap.Logger) *WriteFileTool {
	return &WriteFileTool{
		sandbox: sandbox,
		logger:  logger,
	}
}

// Name 返回工具名称
func (t *WriteFileTool) Name() string {
	return "write_file"
}

func (t *WriteFileTool) Kind() domaintool.Kind { return domaintool.KindEdit }

// Description 返回工具描述
func (t *WriteFileTool) Description() string {
	return "Write content to a file. Creates the file if it doesn't exist, or overwrites it if it does."
}

// Schema 返回参数 JSON Schema
func (t *WriteFileTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "The path to the file to write",
			},
			"content": map[string]interface{}{
				"type":        "string",
				"description": "The content to write to the file",
			},
		},
		"required": []string{"path", "content"},
	}
}

// Execute 写入文件
func (t *WriteFileTool) Execute(ctx context.Context, args map[string]interface{}) (*Result, error) {
	path, ok := args["path"].(string)
	if !ok || path == "" {
		return &Result{
			Success: false,
			Error:   "path is required",
		}, fmt.Errorf("path is required")
	}

	content, ok := args["content"].(string)
	if !ok {
		return &Result{
			Success: false,
			Error:   "content is required",
		}, fmt.Errorf("content is required")
	}

	// 使用 cat 配合 heredoc 写入文件
	cmd := fmt.Sprintf("cat > '%s' << 'NGOCLAW_EOF'\n%s\nNGOCLAW_EOF", path, content)

	result, err := t.sandbox.ExecuteShell(ctx, cmd)
	if err != nil {
		errMsg := err.Error()
		if result != nil {
			errMsg = result.Stderr
		}
		return &Result{Success: false, Error: errMsg}, nil
	}

	return &Result{
		Output:  fmt.Sprintf("Successfully wrote to %s", path),
		Success: true,
		Metadata: map[string]interface{}{
			"path":          path,
			"bytes_written": len(content),
		},
	}, nil
}

// ListDirTool 列出目录工具
type ListDirTool struct {
	sandbox *sandbox.ProcessSandbox
	logger  *zap.Logger
}

// NewListDirTool 创建目录列表工具
func NewListDirTool(sandbox *sandbox.ProcessSandbox, logger *zap.Logger) *ListDirTool {
	return &ListDirTool{
		sandbox: sandbox,
		logger:  logger,
	}
}

// Name 返回工具名称
func (t *ListDirTool) Name() string {
	return "list_dir"
}

func (t *ListDirTool) Kind() domaintool.Kind { return domaintool.KindRead }

// Description 返回工具描述
func (t *ListDirTool) Description() string {
	return "List contents of a directory. Shows files and subdirectories with their sizes and types."
}

// Schema 返回参数 JSON Schema
func (t *ListDirTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "The directory path to list",
			},
			"recursive": map[string]interface{}{
				"type":        "boolean",
				"description": "Whether to list recursively",
			},
		},
		"required": []string{"path"},
	}
}

// Execute 列出目录
func (t *ListDirTool) Execute(ctx context.Context, args map[string]interface{}) (*Result, error) {
	path, ok := args["path"].(string)
	if !ok || path == "" {
		path = "."
	}

	recursive, _ := args["recursive"].(bool)

	var cmd string
	if recursive {
		cmd = fmt.Sprintf("find '%s' -maxdepth 3 -type f -o -type d | head -100", path)
	} else {
		cmd = fmt.Sprintf("ls -la '%s'", path)
	}

	result, err := t.sandbox.ExecuteShell(ctx, cmd)
	if err != nil {
		errMsg := err.Error()
		if result != nil {
			errMsg = result.Stderr
		}
		return &Result{
			Success: false,
			Error:   errMsg,
		}, nil
	}

	return &Result{
		Output:  result.Stdout,
		Success: true,
		Metadata: map[string]interface{}{
			"path": path,
		},
	}, nil
}

// SearchTool 搜索工具
type SearchTool struct {
	sandbox *sandbox.ProcessSandbox
	logger  *zap.Logger
}

// NewSearchTool 创建搜索工具
func NewSearchTool(sandbox *sandbox.ProcessSandbox, logger *zap.Logger) *SearchTool {
	return &SearchTool{
		sandbox: sandbox,
		logger:  logger,
	}
}

// Name 返回工具名称
func (t *SearchTool) Name() string {
	return "grep_search"
}

func (t *SearchTool) Kind() domaintool.Kind { return domaintool.KindSearch }

// Description 返回工具描述
func (t *SearchTool) Description() string {
	return "Search for patterns in files using grep. Supports regular expressions."
}

// Schema 返回参数 JSON Schema
func (t *SearchTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"pattern": map[string]interface{}{
				"type":        "string",
				"description": "The pattern to search for",
			},
			"path": map[string]interface{}{
				"type":        "string",
				"description": "The file or directory to search in",
			},
			"recursive": map[string]interface{}{
				"type":        "boolean",
				"description": "Search recursively in directories",
			},
		},
		"required": []string{"pattern", "path"},
	}
}

// Execute 搜索
func (t *SearchTool) Execute(ctx context.Context, args map[string]interface{}) (*Result, error) {
	pattern, ok := args["pattern"].(string)
	if !ok || pattern == "" {
		return &Result{
			Success: false,
			Error:   "pattern is required",
		}, fmt.Errorf("pattern is required")
	}

	path, ok := args["path"].(string)
	if !ok || path == "" {
		path = "."
	}

	recursive, _ := args["recursive"].(bool)

	var cmd string
	if recursive {
		cmd = fmt.Sprintf("grep -rn '%s' '%s' | head -50", pattern, path)
	} else {
		cmd = fmt.Sprintf("grep -n '%s' '%s' | head -50", pattern, path)
	}

	result, err := t.sandbox.ExecuteShell(ctx, cmd)
	if err != nil && (result == nil || result.ExitCode != 1) {
		errMsg := err.Error()
		if result != nil {
			errMsg = result.Stderr
		}
		return &Result{Success: false, Error: errMsg}, nil
	}
	if result == nil {
		return &Result{Success: false, Error: "no result from sandbox"}, nil
	}

	output := result.Stdout
	if output == "" {
		output = "No matches found"
	}

	return &Result{
		Output:  output,
		Success: true,
		Metadata: map[string]interface{}{
			"pattern": pattern,
			"path":    path,
		},
	}, nil
}
