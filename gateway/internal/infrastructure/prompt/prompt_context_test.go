package prompt

import (
	"strings"
	"testing"
)

// === FocusFile ===

func TestBuildFocusSection_Empty(t *testing.T) {
	ctx := &PromptContext{}
	if result := ctx.BuildFocusSection(); result != "" {
		t.Errorf("expected empty focus section for empty context, got: %q", result)
	}
}

func TestBuildFocusSection_FilesOnly(t *testing.T) {
	ctx := &PromptContext{
		FocusFiles: []FocusFile{
			{Path: "main.go", Language: "go", Line: 42},
			{Path: "config.yaml", Language: "yaml"},
		},
	}

	result := ctx.BuildFocusSection()

	if !strings.Contains(result, "## Current Focus") {
		t.Error("missing header")
	}
	if !strings.Contains(result, "`main.go`") {
		t.Error("missing main.go path")
	}
	if !strings.Contains(result, "(go)") {
		t.Error("missing language annotation")
	}
	if !strings.Contains(result, "at line 42") {
		t.Error("missing line number")
	}
	if !strings.Contains(result, "`config.yaml`") {
		t.Error("missing config.yaml")
	}
	if strings.Contains(result, "at line 0") {
		t.Error("should not show 'at line 0' for files without line")
	}
}

func TestBuildFocusSection_WithSnippet(t *testing.T) {
	ctx := &PromptContext{
		FocusFiles: []FocusFile{
			{
				Path:     "handler.go",
				Language: "go",
				Snippet:  "func Handle(w http.ResponseWriter) {",
			},
		},
	}

	result := ctx.BuildFocusSection()

	if !strings.Contains(result, "```go") {
		t.Error("missing code fence for snippet")
	}
	if !strings.Contains(result, "func Handle") {
		t.Error("missing snippet content")
	}
}

func TestBuildFocusSection_ContextOnly(t *testing.T) {
	ctx := &PromptContext{
		FocusContext: "recent git diff output",
	}

	result := ctx.BuildFocusSection()

	if !strings.Contains(result, "## Current Focus") {
		t.Error("missing header")
	}
	if !strings.Contains(result, "### Additional Context") {
		t.Error("missing context sub-header")
	}
	if !strings.Contains(result, "recent git diff output") {
		t.Error("missing context content")
	}
}

func TestBuildFocusSection_FilesAndContext(t *testing.T) {
	ctx := &PromptContext{
		FocusFiles: []FocusFile{
			{Path: "test.py", Language: "python"},
		},
		FocusContext: "error: undefined variable",
	}

	result := ctx.BuildFocusSection()

	if !strings.Contains(result, "`test.py`") {
		t.Error("missing file")
	}
	if !strings.Contains(result, "error: undefined variable") {
		t.Error("missing context")
	}
}

// === formatInt helper ===

func TestFormatInt(t *testing.T) {
	tests := []struct {
		input    int
		expected string
	}{
		{0, "0"},
		{1, "1"},
		{42, "42"},
		{100, "100"},
		{-5, "-5"},
		{999, "999"},
	}

	for _, tt := range tests {
		result := formatInt(tt.input)
		if result != tt.expected {
			t.Errorf("formatInt(%d) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

// === PromptContext helpers ===

func TestHasTool(t *testing.T) {
	ctx := &PromptContext{
		RegisteredTools: []string{"shell_exec", "file_read", "web_search"},
	}

	if !ctx.HasTool("shell_exec") {
		t.Error("should find shell_exec")
	}
	if ctx.HasTool("nonexistent") {
		t.Error("should not find nonexistent tool")
	}
}

func TestHasAnyTool(t *testing.T) {
	ctx := &PromptContext{
		RegisteredTools: []string{"shell_exec", "file_read"},
	}

	if !ctx.HasAnyTool([]string{"nonexistent", "file_read"}) {
		t.Error("should find file_read in the list")
	}
	if ctx.HasAnyTool([]string{"a", "b", "c"}) {
		t.Error("should not find any")
	}
}

func TestModelPrefix(t *testing.T) {
	tests := []struct {
		model    string
		expected string
	}{
		{"bailian/qwen3-max", "bailian"},
		{"openai/gpt-4o", "openai"},
		{"gpt-4o", "gpt-4o"},    // No slash = full name
		{"a/b/c", "a"},           // Only first slash
	}

	for _, tt := range tests {
		ctx := &PromptContext{ModelName: tt.model}
		if got := ctx.ModelPrefix(); got != tt.expected {
			t.Errorf("ModelPrefix(%q) = %q, want %q", tt.model, got, tt.expected)
		}
	}
}

func TestModelShortName(t *testing.T) {
	tests := []struct {
		model    string
		expected string
	}{
		{"bailian/qwen3-max", "qwen3-max"},
		{"openai/gpt-4o", "gpt-4o"},
		{"gpt-4o", "gpt-4o"},
	}

	for _, tt := range tests {
		ctx := &PromptContext{ModelName: tt.model}
		if got := ctx.ModelShortName(); got != tt.expected {
			t.Errorf("ModelShortName(%q) = %q, want %q", tt.model, got, tt.expected)
		}
	}
}

// === Intent ===

func TestTaskIntent_String(t *testing.T) {
	tests := []struct {
		intent   TaskIntent
		expected string
	}{
		{IntentGeneral, "general"},
		{IntentCoding, "coding"},
		{IntentResearch, "research"},
		{IntentFinance, "finance"},
		{IntentSystem, "system"},
		{IntentCreative, "creative"},
		{TaskIntent(99), "general"},
	}

	for _, tt := range tests {
		if got := tt.intent.String(); got != tt.expected {
			t.Errorf("TaskIntent(%d).String() = %q, want %q", tt.intent, got, tt.expected)
		}
	}
}
