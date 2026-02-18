package tool

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	domaintool "github.com/ngoclaw/ngoclaw/gateway/internal/domain/tool"
	"go.uber.org/zap"
)

// WebSearchTool 网络搜索工具 — 调用 research.py 脚本
type WebSearchTool struct {
	pythonBin  string // Python 可执行文件路径
	scriptPath string // research.py 完整路径
	timeout    time.Duration
	logger     *zap.Logger
}

// NewWebSearchTool 创建搜索工具
// pythonEnv: conda/venv 根目录 (如 /home/none/miniconda3/envs/claw)
// skillsDir: skills 目录根 (如 ~/.ngoclaw/skills)
func NewWebSearchTool(pythonEnv string, skillsDir string, logger *zap.Logger) *WebSearchTool {
	pythonBin := "python3" // fallback to PATH
	if pythonEnv != "" {
		pythonBin = filepath.Join(pythonEnv, "bin", "python3")
	}

	return &WebSearchTool{
		pythonBin:  pythonBin,
		scriptPath: filepath.Join(skillsDir, "web-research", "research.py"),
		timeout:    60 * time.Second,
		logger:     logger,
	}
}

func (t *WebSearchTool) Name() string {
	return "web_search"
}

func (t *WebSearchTool) Kind() domaintool.Kind { return domaintool.KindSearch }

func (t *WebSearchTool) Description() string {
	return "Search the web using SearXNG and extract full article content. " +
		"Returns JSON array of results with titles, URLs, snippets, and optionally full markdown content (deep mode). " +
		"Supports time filtering: day, week, month, year."
}

func (t *WebSearchTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "Search query string",
			},
			"deep": map[string]interface{}{
				"type":        "boolean",
				"description": "If true, fetch and extract full content from top results (recommended for complex questions)",
				"default":     false,
			},
			"time_range": map[string]interface{}{
				"type":        "string",
				"description": "Time filter: day, week, month, year (empty = no filter)",
				"enum":        []string{"", "day", "week", "month", "year"},
				"default":     "",
			},
		},
		"required": []string{"query"},
	}
}

func (t *WebSearchTool) Execute(ctx context.Context, args map[string]interface{}) (*domaintool.Result, error) {
	query, ok := args["query"].(string)
	if !ok || query == "" {
		return &domaintool.Result{
			Output:  "Error: 'query' parameter is required",
			Success: false,
		}, nil
	}

	// Build command args
	cmdArgs := []string{t.scriptPath, query}

	deep, _ := args["deep"].(bool)
	if deep {
		cmdArgs = append(cmdArgs, "--deep")
	}

	if timeRange, ok := args["time_range"].(string); ok && timeRange != "" {
		cmdArgs = append(cmdArgs, "--"+timeRange)
	}

	t.logger.Info("Executing web search",
		zap.String("query", query),
		zap.Bool("deep", deep),
		zap.String("python", t.pythonBin),
	)

	// Execute with timeout
	execCtx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, t.pythonBin, cmdArgs...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	if execCtx.Err() == context.DeadlineExceeded {
		return &domaintool.Result{
			Output:  fmt.Sprintf("Search timed out after %v", t.timeout),
			Success: false,
		}, nil
	}

	if err != nil {
		errMsg := stderr.String()
		if errMsg == "" {
			errMsg = err.Error()
		}
		t.logger.Warn("Web search script error",
			zap.Error(err),
			zap.String("stderr", errMsg),
		)
		return &domaintool.Result{
			Output:  fmt.Sprintf("Search error: %s", strings.TrimSpace(errMsg)),
			Success: false,
		}, nil
	}

	output := stdout.String()
	if output == "" || output == "[]" || output == "[]\n" {
		return &domaintool.Result{
			Output:  "No results found for query: " + query,
			Success: true,
		}, nil
	}

	return &domaintool.Result{
		Output:  output,
		Success: true,
	}, nil
}
