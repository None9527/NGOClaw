package tool

import (
	"context"
	"fmt"
	"testing"

	"go.uber.org/zap"
)

// mockSkillExecutor implements SkillExecutor for testing
type mockSkillExecutor struct {
	output string
	err    error
	calls  []skillCall
}

type skillCall struct {
	skillID string
	input   string
}

func (m *mockSkillExecutor) ExecuteSkill(ctx context.Context, skillID string, input string, config map[string]string) (string, error) {
	m.calls = append(m.calls, skillCall{skillID: skillID, input: input})
	return m.output, m.err
}

func TestBrowserNavigateTool_Execute(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mock := &mockSkillExecutor{output: `{"title":"Example","url":"https://example.com"}`}
		tool := NewBrowserNavigateTool(mock, zap.NewNop())

		result, err := tool.Execute(context.Background(), map[string]interface{}{
			"url": "https://example.com",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.Success {
			t.Error("expected success")
		}
		if len(mock.calls) != 1 {
			t.Fatalf("expected 1 call, got %d", len(mock.calls))
		}
		if mock.calls[0].skillID != "browser_navigate" {
			t.Errorf("expected skill_id 'browser_navigate', got %q", mock.calls[0].skillID)
		}
	})

	t.Run("missing url", func(t *testing.T) {
		mock := &mockSkillExecutor{}
		tool := NewBrowserNavigateTool(mock, zap.NewNop())

		_, err := tool.Execute(context.Background(), map[string]interface{}{})
		if err == nil {
			t.Error("expected error for missing url")
		}
	})

	t.Run("grpc error graceful", func(t *testing.T) {
		mock := &mockSkillExecutor{err: fmt.Errorf("connection refused")}
		tool := NewBrowserNavigateTool(mock, zap.NewNop())

		result, err := tool.Execute(context.Background(), map[string]interface{}{
			"url": "https://example.com",
		})
		if err != nil {
			t.Fatalf("should not return error, but got: %v", err)
		}
		if result.Success {
			t.Error("expected failure on gRPC error")
		}
	})
}

func TestBrowserScreenshotTool_Execute(t *testing.T) {
	mock := &mockSkillExecutor{output: "[base64 image data]"}
	tool := NewBrowserScreenshotTool(mock, zap.NewNop())

	result, err := tool.Execute(context.Background(), map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
	if mock.calls[0].skillID != "browser_screenshot" {
		t.Errorf("expected skill_id 'browser_screenshot', got %q", mock.calls[0].skillID)
	}
}

func TestBrowserClickTool_Execute(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mock := &mockSkillExecutor{output: `{"clicked":true}`}
		tool := NewBrowserClickTool(mock, zap.NewNop())

		result, err := tool.Execute(context.Background(), map[string]interface{}{
			"selector": "#submit",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.Success {
			t.Error("expected success")
		}
	})

	t.Run("missing selector", func(t *testing.T) {
		mock := &mockSkillExecutor{}
		tool := NewBrowserClickTool(mock, zap.NewNop())

		_, err := tool.Execute(context.Background(), map[string]interface{}{})
		if err == nil {
			t.Error("expected error for missing selector")
		}
	})
}

func TestBrowserTypeTool_Execute(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mock := &mockSkillExecutor{output: `{"typed":true}`}
		tool := NewBrowserTypeTool(mock, zap.NewNop())

		result, err := tool.Execute(context.Background(), map[string]interface{}{
			"selector": "#input",
			"text":     "hello",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.Success {
			t.Error("expected success")
		}
	})

	t.Run("missing params", func(t *testing.T) {
		mock := &mockSkillExecutor{}
		tool := NewBrowserTypeTool(mock, zap.NewNop())

		_, err := tool.Execute(context.Background(), map[string]interface{}{
			"selector": "#input",
		})
		if err == nil {
			t.Error("expected error for missing text")
		}
	})
}

func TestBrowserTool_Schema(t *testing.T) {
	mock := &mockSkillExecutor{}

	tools := []struct {
		name       string
		tool       interface{ Schema() map[string]interface{} }
		hasRequired bool
	}{
		{"navigate", NewBrowserNavigateTool(mock, zap.NewNop()), true},
		{"screenshot", NewBrowserScreenshotTool(mock, zap.NewNop()), false},
		{"click", NewBrowserClickTool(mock, zap.NewNop()), true},
		{"type", NewBrowserTypeTool(mock, zap.NewNop()), true},
	}

	for _, tc := range tools {
		t.Run(tc.name, func(t *testing.T) {
			schema := tc.tool.Schema()
			if schema["type"] != "object" {
				t.Errorf("expected schema type 'object', got %v", schema["type"])
			}
			if tc.hasRequired {
				required, ok := schema["required"]
				if !ok || required == nil {
					t.Error("expected required fields in schema")
				}
			}
		})
	}
}
