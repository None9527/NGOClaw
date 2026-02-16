package tool

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	domaintool "github.com/ngoclaw/ngoclaw/gateway/internal/domain/tool"
	"github.com/ngoclaw/ngoclaw/gateway/internal/infrastructure/sandbox"
	"go.uber.org/zap"
)

// LintFixTool runs linters, tests, and build checks, returning output for the agent to act on.
type LintFixTool struct {
	sandbox *sandbox.ProcessSandbox
	logger  *zap.Logger
}

func NewLintFixTool(sb *sandbox.ProcessSandbox, logger *zap.Logger) *LintFixTool {
	return &LintFixTool{sandbox: sb, logger: logger}
}

func (t *LintFixTool) Name() string      { return "lint_fix" }
func (t *LintFixTool) Kind() domaintool.Kind { return domaintool.KindEdit }

func (t *LintFixTool) Description() string {
	return "Run code quality checks: lint, test, or build. Returns errors and warnings for you to fix. " +
		"Automatically detects the project language from the directory contents. " +
		"Use this after editing code to verify correctness."
}

func (t *LintFixTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"lint", "test", "build"},
				"description": "Check to run: lint (static analysis), test (run tests), build (compile)",
			},
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Project directory path to check",
			},
			"target": map[string]interface{}{
				"type":        "string",
				"description": "Specific package or file to check (e.g. './internal/...' for Go, 'tests/' for Python)",
			},
		},
		"required": []string{"action", "path"},
	}
}

func (t *LintFixTool) Execute(ctx context.Context, args map[string]interface{}) (*Result, error) {
	action, ok := args["action"].(string)
	if !ok || action == "" {
		return &Result{Success: false, Error: "action is required (lint, test, build)"}, nil
	}

	projectPath, ok := args["path"].(string)
	if !ok || projectPath == "" {
		return &Result{Success: false, Error: "path is required"}, nil
	}

	target := "./..."
	if t2, ok := args["target"].(string); ok && t2 != "" {
		target = t2
	}

	// Detect language from project files
	lang := detectProjectLanguage(projectPath)

	var cmd string
	switch action {
	case "lint":
		cmd = buildLintCommand(lang, projectPath, target)
	case "test":
		cmd = buildTestCommand(lang, projectPath, target)
	case "build":
		cmd = buildBuildCommand(lang, projectPath, target)
	default:
		return &Result{
			Success: false,
			Error:   fmt.Sprintf("unsupported action '%s'. Use: lint, test, build", action),
		}, nil
	}

	t.logger.Info("Lint fix tool",
		zap.String("action", action),
		zap.String("path", projectPath),
		zap.String("lang", lang),
	)

	result, err := t.sandbox.ExecuteShell(ctx, cmd)
	if err != nil {
		return &Result{Success: false, Error: fmt.Sprintf("%s failed: %v", action, err)}, nil
	}
	if result == nil {
		return &Result{Success: false, Error: "no result from sandbox"}, nil
	}

	// Combine stdout + stderr for full picture
	var output strings.Builder
	if result.Stdout != "" {
		output.WriteString(result.Stdout)
	}
	if result.Stderr != "" {
		if output.Len() > 0 {
			output.WriteString("\n")
		}
		output.WriteString(result.Stderr)
	}

	finalOutput := output.String()
	if finalOutput == "" {
		if result.ExitCode == 0 {
			finalOutput = fmt.Sprintf("%s: all checks passed ✓", action)
		} else {
			finalOutput = fmt.Sprintf("%s: failed with exit code %d (no output)", action, result.ExitCode)
		}
	}

	if len(finalOutput) > 32000 {
		finalOutput = finalOutput[:32000] + "\n... (truncated)"
	}

	return &Result{
		Output:  finalOutput,
		Success: result.ExitCode == 0,
		Metadata: map[string]interface{}{
			"action":    action,
			"language":  lang,
			"exit_code": result.ExitCode,
		},
	}, nil
}

// detectProjectLanguage identifies the primary language from project files.
func detectProjectLanguage(path string) string {
	// Check for Go
	if matches, _ := filepath.Glob(filepath.Join(path, "go.mod")); len(matches) > 0 {
		return "go"
	}
	// Check for Python
	if matches, _ := filepath.Glob(filepath.Join(path, "pyproject.toml")); len(matches) > 0 {
		return "python"
	}
	if matches, _ := filepath.Glob(filepath.Join(path, "requirements.txt")); len(matches) > 0 {
		return "python"
	}
	if matches, _ := filepath.Glob(filepath.Join(path, "setup.py")); len(matches) > 0 {
		return "python"
	}
	// Check for Node.js
	if matches, _ := filepath.Glob(filepath.Join(path, "package.json")); len(matches) > 0 {
		return "javascript"
	}
	// Check for Rust
	if matches, _ := filepath.Glob(filepath.Join(path, "Cargo.toml")); len(matches) > 0 {
		return "rust"
	}
	return "unknown"
}

func buildLintCommand(lang, path, target string) string {
	escaped := shellQuote(path)
	switch lang {
	case "go":
		return fmt.Sprintf("cd %s && go vet %s 2>&1", escaped, target)
	case "python":
		ruffTarget := target
		if ruffTarget == "./..." {
			ruffTarget = "."
		}
		return fmt.Sprintf("cd %s && (ruff check %s 2>&1 || python -m flake8 %s 2>&1 || echo 'No Python linter found')", escaped, ruffTarget, ruffTarget)
	case "javascript":
		return fmt.Sprintf("cd %s && (npx eslint . 2>&1 || echo 'No JS linter found')", escaped)
	case "rust":
		return fmt.Sprintf("cd %s && cargo clippy 2>&1", escaped)
	default:
		return fmt.Sprintf("cd %s && echo 'Unknown language, cannot lint'", escaped)
	}
}

func buildTestCommand(lang, path, target string) string {
	escaped := shellQuote(path)
	switch lang {
	case "go":
		return fmt.Sprintf("cd %s && go test -count=1 -timeout 60s %s 2>&1", escaped, target)
	case "python":
		return fmt.Sprintf("cd %s && (python -m pytest -x --tb=short 2>&1 || python -m unittest discover 2>&1)", escaped)
	case "javascript":
		return fmt.Sprintf("cd %s && npm test 2>&1", escaped)
	case "rust":
		return fmt.Sprintf("cd %s && cargo test 2>&1", escaped)
	default:
		return fmt.Sprintf("cd %s && echo 'Unknown language, cannot test'", escaped)
	}
}

func buildBuildCommand(lang, path, target string) string {
	escaped := shellQuote(path)
	switch lang {
	case "go":
		return fmt.Sprintf("cd %s && go build %s 2>&1", escaped, target)
	case "python":
		return fmt.Sprintf("cd %s && python -m py_compile $(find . -name '*.py' -not -path './.venv/*' | head -20) 2>&1", escaped)
	case "javascript":
		return fmt.Sprintf("cd %s && npm run build 2>&1", escaped)
	case "rust":
		return fmt.Sprintf("cd %s && cargo build 2>&1", escaped)
	default:
		return fmt.Sprintf("cd %s && echo 'Unknown language, cannot build'", escaped)
	}
}

// shellQuote is like shellEscape but avoids name conflict.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
