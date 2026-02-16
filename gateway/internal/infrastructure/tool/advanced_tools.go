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

// EditFileTool performs targeted edits on files using search-and-replace.
// Reference: OpenCode edit.ts (20KB) — supports single and multi-chunk edits.
type EditFileTool struct {
	sandbox *sandbox.ProcessSandbox
	logger  *zap.Logger
}

func NewEditFileTool(sandbox *sandbox.ProcessSandbox, logger *zap.Logger) *EditFileTool {
	return &EditFileTool{sandbox: sandbox, logger: logger}
}

func (t *EditFileTool) Name() string        { return "edit_file" }
func (t *EditFileTool) Kind() domaintool.Kind { return domaintool.KindEdit }
func (t *EditFileTool) Description() string {
	return `Make targeted edits to a file using search-and-replace. This is the preferred way to modify existing files because it:
1. Only changes the specific lines you target
2. Preserves the rest of the file
3. Shows a clear diff of changes

Provide the exact text to search for (old_text) and what to replace it with (new_text).
The old_text must match EXACTLY, including whitespace and indentation.`
}

func (t *EditFileTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path to the file to edit",
			},
			"old_text": map[string]interface{}{
				"type":        "string",
				"description": "The exact text to find and replace. Must match exactly.",
			},
			"new_text": map[string]interface{}{
				"type":        "string",
				"description": "The replacement text",
			},
		},
		"required": []string{"path", "old_text", "new_text"},
	}
}

func (t *EditFileTool) Execute(ctx context.Context, args map[string]interface{}) (*domaintool.Result, error) {
	path, _ := args["path"].(string)
	oldText, _ := args["old_text"].(string)
	newText, _ := args["new_text"].(string)

	if path == "" || oldText == "" {
		return &domaintool.Result{Success: false, Error: "path and old_text are required"}, nil
	}

	// Read original file
	readResult, err := t.sandbox.ExecuteShell(ctx, fmt.Sprintf("cat '%s'", path))
	if err != nil {
		return &domaintool.Result{Success: false, Error: readResult.Stderr}, nil
	}

	original := readResult.Stdout

	// Phase 1: Exact match
	if strings.Contains(original, oldText) {
		count := strings.Count(original, oldText)
		if count > 1 {
			return &domaintool.Result{
				Success: false,
				Error:   fmt.Sprintf("old_text found %d times in file. It must be unique. Provide more context to make it unique.", count),
			}, nil
		}

		modified := strings.Replace(original, oldText, newText, 1)
		return t.writeFile(ctx, path, modified, oldText, newText, "exact")
	}

	// Phase 2: Fuzzy self-repair — normalize whitespace and retry
	normalizedOld := normalizeWhitespace(oldText)
	lines := strings.Split(original, "\n")
	var matchStart, matchEnd int
	matchFound := false

	for i := 0; i < len(lines); i++ {
		// Try to find the start of a fuzzy match
		for windowEnd := i + 1; windowEnd <= len(lines) && windowEnd-i <= strings.Count(oldText, "\n")+2; windowEnd++ {
			candidate := strings.Join(lines[i:windowEnd], "\n")
			if normalizeWhitespace(candidate) == normalizedOld {
				if matchFound {
					// Multiple fuzzy matches — ambiguous, bail out
					return &domaintool.Result{
						Success: false,
						Error:   "old_text not found exactly, and fuzzy match found multiple candidates. Please provide exact text.",
					}, nil
				}
				matchStart = i
				matchEnd = windowEnd
				matchFound = true
			}
		}
	}

	if matchFound {
		// Replace the fuzzy-matched region
		result := strings.Join(lines[:matchStart], "\n") + "\n" + newText + "\n" + strings.Join(lines[matchEnd:], "\n")
		t.logger.Info("Edit self-repair: fuzzy match succeeded",
			zap.String("path", path),
			zap.Int("line_start", matchStart+1),
			zap.Int("line_end", matchEnd),
		)
		return t.writeFile(ctx, path, result, oldText, newText, "fuzzy")
	}

	// Phase 3: No match — provide context for LLM retry
	snippet := findClosestSnippet(original, oldText, 3)
	errMsg := "old_text not found in file. Make sure it matches exactly, including whitespace."
	if snippet != "" {
		errMsg += "\n\nClosest matching region in file:\n```\n" + snippet + "\n```"
	}

	return &domaintool.Result{
		Success: false,
		Error:   errMsg,
	}, nil
}

// writeFile writes modified content back to file
func (t *EditFileTool) writeFile(ctx context.Context, path, content, oldText, newText, matchType string) (*domaintool.Result, error) {
	writeCmd := fmt.Sprintf("cat > '%s' << 'NGOCLAW_EDIT_EOF'\n%s\nNGOCLAW_EDIT_EOF", path, content)
	writeResult, err := t.sandbox.ExecuteShell(ctx, writeCmd)
	if err != nil {
		return &domaintool.Result{Success: false, Error: writeResult.Stderr}, nil
	}

	msg := fmt.Sprintf("Successfully edited %s (replaced 1 occurrence, match: %s)", path, matchType)
	return &domaintool.Result{
		Output:  msg,
		Success: true,
		Metadata: map[string]interface{}{
			"path":        path,
			"match_type":  matchType,
			"chars_added": len(newText) - len(oldText),
		},
	}, nil
}

// normalizeWhitespace trims each line and collapses empty lines for fuzzy comparison
func normalizeWhitespace(s string) string {
	lines := strings.Split(s, "\n")
	var out []string
	for _, line := range lines {
		trimmed := strings.TrimRight(line, " \t\r")
		out = append(out, trimmed)
	}
	return strings.Join(out, "\n")
}

// findClosestSnippet finds the region in content most similar to target
func findClosestSnippet(content, target string, contextLines int) string {
	targetLines := strings.Split(target, "\n")
	if len(targetLines) == 0 {
		return ""
	}

	contentLines := strings.Split(content, "\n")
	firstTargetLine := strings.TrimSpace(targetLines[0])
	if firstTargetLine == "" && len(targetLines) > 1 {
		firstTargetLine = strings.TrimSpace(targetLines[1])
	}

	bestScore := 0
	bestIdx := -1

	for i, line := range contentLines {
		trimmed := strings.TrimSpace(line)
		score := longestCommonSubstring(trimmed, firstTargetLine)
		if score > bestScore && score > len(firstTargetLine)/3 {
			bestScore = score
			bestIdx = i
		}
	}

	if bestIdx == -1 {
		return ""
	}

	start := bestIdx - contextLines
	if start < 0 {
		start = 0
	}
	end := bestIdx + len(targetLines) + contextLines
	if end > len(contentLines) {
		end = len(contentLines)
	}

	return strings.Join(contentLines[start:end], "\n")
}

// longestCommonSubstring returns length of the longest common substring
func longestCommonSubstring(a, b string) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	maxLen := 0
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			if a[i-1] == b[j-1] {
				curr[j] = prev[j-1] + 1
				if curr[j] > maxLen {
					maxLen = curr[j]
				}
			} else {
				curr[j] = 0
			}
		}
		prev, curr = curr, prev
		for k := range curr {
			curr[k] = 0
		}
	}
	return maxLen
}

// GlobTool finds files using glob patterns.
// Reference: OpenCode glob.ts (2KB)
type GlobTool struct {
	sandbox *sandbox.ProcessSandbox
	logger  *zap.Logger
}

func NewGlobTool(sandbox *sandbox.ProcessSandbox, logger *zap.Logger) *GlobTool {
	return &GlobTool{sandbox: sandbox, logger: logger}
}

func (t *GlobTool) Name() string        { return "glob" }
func (t *GlobTool) Kind() domaintool.Kind { return domaintool.KindSearch }
func (t *GlobTool) Description() string {
	return `Find files matching a glob pattern. Use this to discover files by name or extension.
Examples: "*.go", "src/**/*.ts", "*.{py,js}", "test_*.py"`
}

func (t *GlobTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"pattern": map[string]interface{}{
				"type":        "string",
				"description": "Glob pattern to match files against",
			},
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Directory to search in (default: current directory)",
			},
		},
		"required": []string{"pattern"},
	}
}

func (t *GlobTool) Execute(ctx context.Context, args map[string]interface{}) (*domaintool.Result, error) {
	pattern, _ := args["pattern"].(string)
	path, _ := args["path"].(string)
	if path == "" {
		path = "."
	}

	if pattern == "" {
		return &domaintool.Result{Success: false, Error: "pattern is required"}, nil
	}

	// Use find with -name for simple patterns, or fd if available
	fullPattern := filepath.Join(path, pattern)
	cmd := fmt.Sprintf("find '%s' -path '%s' -type f 2>/dev/null | head -100 | sort", path, fullPattern)

	// Try fd first (faster, respects .gitignore)
	fdCmd := fmt.Sprintf("fd --type f --glob '%s' '%s' 2>/dev/null | head -100", pattern, path)
	result, err := t.sandbox.ExecuteShell(ctx, fdCmd)
	if err != nil || result.ExitCode != 0 || result.Stdout == "" {
		// Fallback to find
		result, err = t.sandbox.ExecuteShell(ctx, cmd)
		if err != nil {
			return &domaintool.Result{Success: false, Error: result.Stderr}, nil
		}
	}

	output := strings.TrimSpace(result.Stdout)
	if output == "" {
		output = "No files found matching pattern"
	}

	return &domaintool.Result{
		Output:  output,
		Success: true,
		Metadata: map[string]interface{}{
			"pattern": pattern,
			"path":    path,
		},
	}, nil
}

// ApplyPatchTool applies unified diff patches to files.
// Reference: OpenCode apply_patch.ts (9KB)
type ApplyPatchTool struct {
	sandbox *sandbox.ProcessSandbox
	logger  *zap.Logger
}

func NewApplyPatchTool(sandbox *sandbox.ProcessSandbox, logger *zap.Logger) *ApplyPatchTool {
	return &ApplyPatchTool{sandbox: sandbox, logger: logger}
}

func (t *ApplyPatchTool) Name() string        { return "apply_patch" }
func (t *ApplyPatchTool) Kind() domaintool.Kind { return domaintool.KindEdit }
func (t *ApplyPatchTool) Description() string {
	return `Apply a unified diff patch to one or more files. Use standard unified diff format:
--- a/path/to/file
+++ b/path/to/file
@@ -line,count +line,count @@
 context line
-removed line
+added line`
}

func (t *ApplyPatchTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"patch": map[string]interface{}{
				"type":        "string",
				"description": "The unified diff patch to apply",
			},
		},
		"required": []string{"patch"},
	}
}

func (t *ApplyPatchTool) Execute(ctx context.Context, args map[string]interface{}) (*domaintool.Result, error) {
	patch, _ := args["patch"].(string)
	if patch == "" {
		return &domaintool.Result{Success: false, Error: "patch is required"}, nil
	}

	// Write patch to temp file and apply
	cmd := fmt.Sprintf("echo '%s' | patch -p1 --no-backup-if-mismatch 2>&1",
		strings.ReplaceAll(patch, "'", "'\\''"))

	result, err := t.sandbox.ExecuteShell(ctx, cmd)
	if err != nil {
		return &domaintool.Result{
			Success: false,
			Error:   fmt.Sprintf("Patch failed: %s", result.Stderr),
		}, nil
	}

	return &domaintool.Result{
		Output:  result.Stdout,
		Success: result.ExitCode == 0,
	}, nil
}

// WebFetchTool fetches content from URLs and converts to readable text.
// Reference: OpenCode webfetch.ts (6KB)
type WebFetchTool struct {
	sandbox *sandbox.ProcessSandbox
	logger  *zap.Logger
}

func NewWebFetchTool(sandbox *sandbox.ProcessSandbox, logger *zap.Logger) *WebFetchTool {
	return &WebFetchTool{sandbox: sandbox, logger: logger}
}

func (t *WebFetchTool) Name() string        { return "web_fetch" }
func (t *WebFetchTool) Kind() domaintool.Kind { return domaintool.KindFetch }
func (t *WebFetchTool) Description() string {
	return "Fetch contents from a URL. Returns the text content of the page. Useful for reading documentation, APIs, or web resources."
}

func (t *WebFetchTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"url": map[string]interface{}{
				"type":        "string",
				"description": "The URL to fetch",
			},
		},
		"required": []string{"url"},
	}
}

func (t *WebFetchTool) Execute(ctx context.Context, args map[string]interface{}) (*domaintool.Result, error) {
	url, _ := args["url"].(string)
	if url == "" {
		return &domaintool.Result{Success: false, Error: "url is required"}, nil
	}

	// Use curl + html2text for content extraction
	cmd := fmt.Sprintf(
		"curl -sL --max-time 30 -A 'Mozilla/5.0' '%s' | "+
			"python3 -c 'import sys; "+
			"from html.parser import HTMLParser; "+
			"class S(HTMLParser):"+
			"\n  def __init__(s): super().__init__(); s.t=[]"+
			"\n  def handle_data(s,d): s.t.append(d)"+
			"\np=S(); p.feed(sys.stdin.read()); print(\" \".join(p.t)[:20000])'",
		strings.ReplaceAll(url, "'", "'\\''"),
	)

	result, err := t.sandbox.ExecuteShell(ctx, cmd)
	if err != nil {
		return &domaintool.Result{
			Success: false,
			Error:   fmt.Sprintf("Failed to fetch URL: %s", result.Stderr),
		}, nil
	}

	output := strings.TrimSpace(result.Stdout)
	if output == "" {
		output = "No content could be extracted from the URL"
	}

	return &domaintool.Result{
		Output:  output,
		Success: true,
		Metadata: map[string]interface{}{
			"url":   url,
			"chars": len(output),
		},
	}, nil
}
