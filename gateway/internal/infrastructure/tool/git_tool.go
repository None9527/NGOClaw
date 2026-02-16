package tool

import (
	"context"
	"fmt"
	"strings"

	domaintool "github.com/ngoclaw/ngoclaw/gateway/internal/domain/tool"
	"github.com/ngoclaw/ngoclaw/gateway/internal/infrastructure/sandbox"
	"go.uber.org/zap"
)

// GitTool provides safe git operations for the agent.
// Only read operations + commit are allowed. No push/reset/rebase.
type GitTool struct {
	sandbox *sandbox.ProcessSandbox
	logger  *zap.Logger
}

func NewGitTool(sb *sandbox.ProcessSandbox, logger *zap.Logger) *GitTool {
	return &GitTool{sandbox: sb, logger: logger}
}

func (t *GitTool) Name() string { return "git" }
func (t *GitTool) Kind() domaintool.Kind { return domaintool.KindExecute }

func (t *GitTool) Description() string {
	return "Execute safe git operations. Supported actions: status, diff, log, commit, show. " +
		"Use this to check file changes, view history, and commit work. " +
		"For safety, push/reset/rebase are not available."
}

func (t *GitTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"status", "diff", "log", "commit", "show"},
				"description": "Git action to perform",
			},
			"repo_path": map[string]interface{}{
				"type":        "string",
				"description": "Path to the git repository (default: current directory)",
			},
			"message": map[string]interface{}{
				"type":        "string",
				"description": "Commit message (required for 'commit' action)",
			},
			"file": map[string]interface{}{
				"type":        "string",
				"description": "Optional file path for 'diff' or 'show' (e.g. 'src/main.go')",
			},
			"staged": map[string]interface{}{
				"type":        "boolean",
				"description": "If true, show staged changes only (for 'diff' action)",
			},
			"count": map[string]interface{}{
				"type":        "integer",
				"description": "Number of log entries to show (default: 10, for 'log' action)",
			},
		},
		"required": []string{"action"},
	}
}

func (t *GitTool) Execute(ctx context.Context, args map[string]interface{}) (*Result, error) {
	action, ok := args["action"].(string)
	if !ok || action == "" {
		return &Result{Success: false, Error: "action is required (status, diff, log, commit, show)"}, nil
	}

	repoPath := "."
	if rp, ok := args["repo_path"].(string); ok && rp != "" {
		repoPath = rp
	}

	var cmd string

	switch action {
	case "status":
		cmd = fmt.Sprintf("cd %s && git status --short --branch", shellEscape(repoPath))

	case "diff":
		cmd = fmt.Sprintf("cd %s && git diff", shellEscape(repoPath))
		if staged, ok := args["staged"].(bool); ok && staged {
			cmd += " --staged"
		}
		if file, ok := args["file"].(string); ok && file != "" {
			cmd += " -- " + shellEscape(file)
		}

	case "log":
		count := 10
		if c, ok := args["count"].(float64); ok && c > 0 {
			count = int(c)
			if count > 50 {
				count = 50
			}
		}
		cmd = fmt.Sprintf("cd %s && git log --oneline --no-decorate -n %d", shellEscape(repoPath), count)

	case "commit":
		message, ok := args["message"].(string)
		if !ok || message == "" {
			return &Result{Success: false, Error: "message is required for commit action"}, nil
		}
		// Escape single quotes in message
		escapedMsg := strings.ReplaceAll(message, "'", "'\\''")
		cmd = fmt.Sprintf("cd %s && git add -A && git commit -m '%s'", shellEscape(repoPath), escapedMsg)

	case "show":
		cmd = fmt.Sprintf("cd %s && git show --stat HEAD", shellEscape(repoPath))
		if file, ok := args["file"].(string); ok && file != "" {
			cmd = fmt.Sprintf("cd %s && git show HEAD:%s", shellEscape(repoPath), shellEscape(file))
		}

	default:
		return &Result{
			Success: false,
			Error:   fmt.Sprintf("unsupported action '%s'. Use: status, diff, log, commit, show", action),
		}, nil
	}

	t.logger.Info("Git tool", zap.String("action", action), zap.String("repo", repoPath))

	result, err := t.sandbox.ExecuteShell(ctx, cmd)
	if err != nil {
		return &Result{Success: false, Error: fmt.Sprintf("git %s failed: %v", action, err)}, nil
	}
	if result == nil {
		return &Result{Success: false, Error: "no result from sandbox"}, nil
	}

	output := result.Stdout
	if output == "" && result.Stderr != "" {
		output = result.Stderr
	}
	if output == "" {
		output = "(no output)"
	}

	// Truncate large output
	if len(output) > 16000 {
		output = output[:16000] + "\n... (truncated)"
	}

	return &Result{
		Output:  output,
		Success: result.ExitCode == 0,
		Metadata: map[string]interface{}{
			"action":    action,
			"exit_code": result.ExitCode,
		},
	}, nil
}

// shellEscape wraps a string for safe shell usage.
func shellEscape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
