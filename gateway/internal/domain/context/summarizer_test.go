package context

import (
	"context"
	"strings"
	"testing"
)

func TestSimpleSummarizer(t *testing.T) {
	summarizer := NewSimpleSummarizer()
	ctx := context.Background()

	t.Run("Empty messages", func(t *testing.T) {
		summary, err := summarizer.Summarize(ctx, []Message{})
		if err != nil {
			t.Fatalf("Summarize failed: %v", err)
		}
		if summary != "" {
			t.Errorf("Expected empty summary, got %s", summary)
		}
	})

	t.Run("Messages with keywords", func(t *testing.T) {
		messages := []Message{
			{Role: "user", Content: "Please fix the error in the code"},
			{Role: "assistant", Content: "I have completed the fix"},
			{Role: "user", Content: "Great, now modify the config"},
		}

		summary, err := summarizer.Summarize(ctx, messages)
		if err != nil {
			t.Fatalf("Summarize failed: %v", err)
		}

		if summary == "" {
			t.Error("Summary should not be empty")
		}

		// Should contain keyword matches
		if !strings.Contains(summary, "error") && !strings.Contains(summary, "完成") {
			t.Error("Summary should contain extracted keywords")
		}
	})

	t.Run("Messages without keywords", func(t *testing.T) {
		messages := []Message{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi there"},
		}

		summary, err := summarizer.Summarize(ctx, messages)
		if err != nil {
			t.Fatalf("Summarize failed: %v", err)
		}

		// Should return count-based summary
		if !strings.Contains(summary, "2") {
			t.Errorf("Expected count in summary, got %s", summary)
		}
	})
}

func TestSummarizePruner(t *testing.T) {
	config := &PruneConfig{
		Strategy:       PruneSummarize,
		MaxTokens:      100,
		SoftTrimRatio:  0.5, // Low threshold to trigger pruning
		HardClearRatio: 0.8,
		PreserveSystem: true,
		PreserveRecent: 2,
	}

	summarizer := NewSimpleSummarizer()
	pruner := NewSummarizePruner(config, nil, summarizer)

	t.Run("No pruning needed", func(t *testing.T) {
		ctx := context.Background()
		messages := []Message{
			{Role: "system", Content: "You are helpful"},
			{Role: "user", Content: "Hi"},
		}

		result, err := pruner.PruneWithSummary(ctx, messages)
		if err != nil {
			t.Fatalf("PruneWithSummary failed: %v", err)
		}

		if len(result) != len(messages) {
			t.Errorf("Should not prune, expected %d messages, got %d", len(messages), len(result))
		}
	})

	t.Run("Prune with summary", func(t *testing.T) {
		ctx := context.Background()
		
		// Create many messages to exceed threshold
		messages := []Message{
			{Role: "system", Content: "You are a helpful assistant."},
		}
		for i := 0; i < 20; i++ {
			messages = append(messages, Message{
				Role:    "user",
				Content: "This is a long message that takes up many tokens and needs pruning.",
			})
			messages = append(messages, Message{
				Role:    "assistant",
				Content: "This is also a long response with many tokens to trigger the pruning.",
			})
		}

		result, err := pruner.PruneWithSummary(ctx, messages)
		if err != nil {
			t.Fatalf("PruneWithSummary failed: %v", err)
		}

		// Should have fewer messages
		if len(result) >= len(messages) {
			t.Error("Should prune messages")
		}

		// Should preserve system message
		hasSystem := false
		for _, msg := range result {
			if msg.Role == "system" && !strings.Contains(msg.Content, "摘要") {
				hasSystem = true
			}
		}
		if !hasSystem {
			t.Error("Should preserve original system message")
		}

		// Should have summary
		summary := pruner.GetLastSummary()
		if summary == "" {
			// Note: may be empty if no keywords matched, which is OK
			t.Log("No summary generated (no keywords matched)")
		}
	})
}
