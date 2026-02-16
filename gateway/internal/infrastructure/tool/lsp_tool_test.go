package tool

import (
	"bufio"
	"bytes"
	"encoding/json"
	"testing"
)

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"main.go", "go"},
		{"app.ts", "typescript"},
		{"app.tsx", "typescript"},
		{"index.js", "javascript"},
		{"index.jsx", "javascript"},
		{"script.py", "python"},
		{"lib.rs", "rust"},
		{"readme.md", ""},
		{"data.json", ""},
	}

	for _, tt := range tests {
		got := detectLanguage(tt.path)
		if got != tt.want {
			t.Errorf("detectLanguage(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestLanguageServerCommand(t *testing.T) {
	tests := []struct {
		lang    string
		wantCmd string
	}{
		{"go", "gopls"},
		{"typescript", "typescript-language-server"},
		{"javascript", "typescript-language-server"},
		{"python", "pylsp"},
		{"rust", "rust-analyzer"},
		{"unknown", ""},
	}

	for _, tt := range tests {
		cmd, _ := languageServerCommand(tt.lang)
		if cmd != tt.wantCmd {
			t.Errorf("languageServerCommand(%q) cmd = %q, want %q", tt.lang, cmd, tt.wantCmd)
		}
	}
}

func TestPathToURIAndBack(t *testing.T) {
	path := "/home/user/project/main.go"
	uri := pathToURI(path)
	if uri != "file:///home/user/project/main.go" {
		t.Errorf("pathToURI(%q) = %q", path, uri)
	}

	back := uriToPath(uri)
	if back != path {
		t.Errorf("uriToPath(%q) = %q, want %q", uri, back, path)
	}
}

func TestIntArg(t *testing.T) {
	args := map[string]interface{}{
		"line":    float64(42),
		"column":  10,
		"missing": "not a number",
	}

	if got := intArg(args, "line", 1); got != 42 {
		t.Errorf("intArg(line) = %d, want 42", got)
	}
	if got := intArg(args, "column", 1); got != 10 {
		t.Errorf("intArg(column) = %d, want 10", got)
	}
	if got := intArg(args, "undefined", 99); got != 99 {
		t.Errorf("intArg(undefined) = %d, want 99", got)
	}
}

func TestWriteAndReadJSONRPC(t *testing.T) {
	var buf bytes.Buffer

	// Write a request
	err := writeJSONRPC(&buf, 1, "textDocument/definition", map[string]interface{}{
		"textDocument": map[string]string{"uri": "file:///test.go"},
		"position":     map[string]int{"line": 10, "character": 5},
	})
	if err != nil {
		t.Fatalf("writeJSONRPC failed: %v", err)
	}

	// Read it back
	reader := bufio.NewReader(&buf)
	resp, err := readJSONRPC(reader)
	if err != nil {
		t.Fatalf("readJSONRPC failed: %v", err)
	}

	if resp.ID != 1 {
		t.Errorf("resp.ID = %d, want 1", resp.ID)
	}

	// The result should be nil (it was a request, not a response, but the struct will parse)
	// What we really verify is that the round-trip doesn't error
}

func TestExtractHoverText(t *testing.T) {
	// String
	if got := extractHoverText("hello"); got != "hello" {
		t.Errorf("extractHoverText(string) = %q", got)
	}

	// MarkupContent
	mc := map[string]interface{}{"kind": "markdown", "value": "func Foo() int"}
	if got := extractHoverText(mc); got != "func Foo() int" {
		t.Errorf("extractHoverText(MarkupContent) = %q", got)
	}
}

func TestLSPToolSchema(t *testing.T) {
	tool := NewLSPTool("/tmp", nil)
	schema := tool.Schema()

	if schema["type"] != "object" {
		t.Error("schema type should be object")
	}

	props, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("schema properties missing")
	}

	for _, key := range []string{"action", "file", "line", "column"} {
		if _, ok := props[key]; !ok {
			t.Errorf("missing property: %s", key)
		}
	}
}

func TestLSPToolExecute_UnsupportedFile(t *testing.T) {
	tool := NewLSPTool("/tmp", nil)
	result, err := tool.Execute(nil, map[string]interface{}{
		"action": "definition",
		"file":   "/tmp/readme.md",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected failure for unsupported file type")
	}
}

func TestFormatLocations_Empty(t *testing.T) {
	tool := NewLSPTool("/tmp", nil)
	result, _ := tool.formatLocations("Definition", json.RawMessage("null"))
	if result.Output != "Definition: no results found" {
		t.Errorf("unexpected output: %s", result.Output)
	}
}

func TestFormatLocations_Array(t *testing.T) {
	tool := NewLSPTool("/tmp", nil)
	raw := json.RawMessage(`[{"uri":"file:///test.go","range":{"start":{"line":10,"character":5}}}]`)
	result, _ := tool.formatLocations("References", raw)
	if !result.Success {
		t.Error("expected success")
	}
	if result.Output == "" {
		t.Error("output should not be empty")
	}
}
