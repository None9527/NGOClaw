package service

import (
	"testing"
)

// === ParseToolCallsFromText ===

func TestParseToolCallsFromText_Pattern1(t *testing.T) {
	input := `Here is my response.
[TOOL_CALL] shell_exec({"command":"ls -la"}) [/TOOL_CALL]
Some more text.`

	cleaned, calls := ParseToolCallsFromText(input)

	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].Name != "shell_exec" {
		t.Errorf("tool name: got %q, want %q", calls[0].Name, "shell_exec")
	}
	cmd, ok := calls[0].Arguments["command"]
	if !ok || cmd != "ls -la" {
		t.Errorf("command arg: got %v", calls[0].Arguments)
	}
	if calls[0].ID != "tc_0" {
		t.Errorf("ID: got %q, want %q", calls[0].ID, "tc_0")
	}
	// Cleaned text should not contain tool call markers
	if contains(cleaned, "[TOOL_CALL]") {
		t.Errorf("cleaned text still contains [TOOL_CALL]: %q", cleaned)
	}
}

func TestParseToolCallsFromText_Pattern2(t *testing.T) {
	input := "Let me search.\n```tool_call\n{\"name\":\"web_search\",\"arguments\":{\"query\":\"golang testing\"}}\n```\nDone."

	cleaned, calls := ParseToolCallsFromText(input)

	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].Name != "web_search" {
		t.Errorf("tool name: got %q, want %q", calls[0].Name, "web_search")
	}
	q, ok := calls[0].Arguments["query"]
	if !ok || q != "golang testing" {
		t.Errorf("query arg: got %v", calls[0].Arguments)
	}
	if contains(cleaned, "```tool_call") {
		t.Errorf("cleaned text still contains code fence: %q", cleaned)
	}
}

func TestParseToolCallsFromText_Multiple(t *testing.T) {
	input := `[TOOL_CALL] file_read({"path":"a.go"}) [/TOOL_CALL]
[TOOL_CALL] file_read({"path":"b.go"}) [/TOOL_CALL]`

	_, calls := ParseToolCallsFromText(input)
	if len(calls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(calls))
	}
	if calls[0].ID != "tc_0" || calls[1].ID != "tc_1" {
		t.Errorf("IDs wrong: %q, %q", calls[0].ID, calls[1].ID)
	}
}

func TestParseToolCallsFromText_NoToolCalls(t *testing.T) {
	input := "This is just a normal response with no tool calls."
	cleaned, calls := ParseToolCallsFromText(input)

	if len(calls) != 0 {
		t.Errorf("expected 0 tool calls, got %d", len(calls))
	}
	if cleaned != input {
		t.Errorf("cleaned text should be unchanged")
	}
}

func TestParseToolCallsFromText_MalformedJSON(t *testing.T) {
	input := `[TOOL_CALL] bad_tool(not json) [/TOOL_CALL]`
	_, calls := ParseToolCallsFromText(input)
	if len(calls) != 0 {
		t.Errorf("malformed JSON should produce 0 tool calls, got %d", len(calls))
	}
}

func TestParseToolCallsFromText_MixedPatterns(t *testing.T) {
	input := `[TOOL_CALL] tool_a({"x":1}) [/TOOL_CALL]
some text
` + "```tool_call\n{\"name\":\"tool_b\",\"arguments\":{\"y\":2}}\n```"

	_, calls := ParseToolCallsFromText(input)
	if len(calls) != 2 {
		t.Fatalf("expected 2 tool calls from mixed patterns, got %d", len(calls))
	}
	if calls[0].Name != "tool_a" || calls[1].Name != "tool_b" {
		t.Errorf("names: %q, %q", calls[0].Name, calls[1].Name)
	}
}

// helper
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && findSubstring(s, substr))
}

func findSubstring(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
