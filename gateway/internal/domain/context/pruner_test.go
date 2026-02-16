package context

import (
	"testing"
)

func TestSimpleTokenizer(t *testing.T) {
	tokenizer := NewSimpleTokenizer()

	tests := []struct {
		name     string
		text     string
		minTokens int
		maxTokens int
	}{
		{"Empty", "", 1, 2},
		{"Short English", "Hello world", 2, 5},
		{"Long English", "This is a longer sentence with more words in it.", 10, 20},
		{"Chinese", "你好世界", 2, 5},
		{"Mixed", "Hello 你好 world 世界", 4, 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count := tokenizer.Count(tt.text)
			if count < tt.minTokens || count > tt.maxTokens {
				t.Errorf("Count(%q) = %d, want between %d and %d", tt.text, count, tt.minTokens, tt.maxTokens)
			}
		})
	}
}

func TestPruner(t *testing.T) {
	config := &PruneConfig{
		Strategy:       PruneAdaptive,
		MaxTokens:      100,
		SoftTrimRatio:  0.7,
		HardClearRatio: 0.85,
		PreserveSystem: true,
		PreserveRecent: 2,
		ImportanceThreshold: 0.3,
	}

	pruner := NewPruner(config, nil)

	t.Run("No pruning needed", func(t *testing.T) {
		messages := []Message{
			{Role: "system", Content: "You are helpful."},
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi there!"},
		}

		result := pruner.Prune(messages)
		if len(result) != len(messages) {
			t.Errorf("Expected %d messages, got %d", len(messages), len(result))
		}
	})

	t.Run("Prune when over threshold", func(t *testing.T) {
		messages := make([]Message, 0)
		messages = append(messages, Message{Role: "system", Content: "You are helpful."})
		
		// Add many messages to exceed threshold
		for i := 0; i < 20; i++ {
			messages = append(messages, Message{
				Role:    "user",
				Content: "This is a somewhat long message that contains quite a few tokens.",
			})
			messages = append(messages, Message{
				Role:    "assistant",
				Content: "This is a response that also contains several tokens for testing.",
			})
		}

		result := pruner.Prune(messages)
		
		// Should have fewer messages than original
		if len(result) >= len(messages) {
			t.Error("Pruning should reduce message count")
		}

		// System message should be preserved
		hasSystem := false
		for _, msg := range result {
			if msg.Role == "system" {
				hasSystem = true
				break
			}
		}
		if !hasSystem {
			t.Error("System message should be preserved")
		}
	})

	t.Run("NeedsPruning detection", func(t *testing.T) {
		smallMessages := []Message{
			{Role: "user", Content: "Hi"},
		}
		
		if pruner.NeedsPruning(smallMessages) {
			t.Error("Small messages should not need pruning")
		}
	})
}

func TestPruningStrategy(t *testing.T) {
	tests := []struct {
		strategy PruningStrategy
		want     string
	}{
		{PruneNone, "none"},
		{PruneAdaptive, "adaptive"},
		{PruneHardClear, "hard_clear"},
		{PruneSummarize, "summarize"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.strategy.String(); got != tt.want {
				t.Errorf("String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEvaluateImportance(t *testing.T) {
	config := DefaultPruneConfig()
	pruner := NewPruner(config, nil)

	tests := []struct {
		name        string
		msg         Message
		minImportance float64
	}{
		{
			"Tool message",
			Message{Role: "tool", Content: "Output"},
			0.6,
		},
		{
			"Code block",
			Message{Role: "assistant", Content: "Here is the code:\n```go\nfunc main() {}\n```"},
			0.6,
		},
		{
			"Error message",
			Message{Role: "assistant", Content: "An error occurred: file not found"},
			0.5,
		},
		{
			"Plain message",
			Message{Role: "user", Content: "Hello"},
			0.4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			importance := pruner.evaluateImportance(tt.msg)
			if importance < tt.minImportance {
				t.Errorf("evaluateImportance() = %v, want >= %v", importance, tt.minImportance)
			}
		})
	}
}
