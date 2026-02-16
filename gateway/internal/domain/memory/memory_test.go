package memory

import (
	"context"
	"testing"
	"time"
)

func TestInMemoryVectorStore(t *testing.T) {
	store := NewInMemoryVectorStore()
	ctx := context.Background()

	t.Run("Insert and Search", func(t *testing.T) {
		entry := &MemoryEntry{
			ID:        "test-1",
			Content:   "Hello world",
			Embedding: []float32{1.0, 0.0, 0.0},
			UserID:    "user-1",
			SessionID: "session-1",
			CreatedAt: time.Now(),
		}

		err := store.Insert(ctx, entry)
		if err != nil {
			t.Fatalf("Insert failed: %v", err)
		}

		// Search with similar vector
		query := []float32{0.9, 0.1, 0.0}
		results, err := store.Search(ctx, query, 10, nil)
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}

		if len(results) != 1 {
			t.Errorf("Expected 1 result, got %d", len(results))
		}

		if results[0].ID != "test-1" {
			t.Errorf("Expected ID test-1, got %s", results[0].ID)
		}

		if results[0].Score <= 0 {
			t.Error("Score should be positive")
		}
	})

	t.Run("Filter by UserID", func(t *testing.T) {
		// Insert entries for different users
		store.Insert(ctx, &MemoryEntry{
			ID:        "user1-entry",
			Content:   "User 1 memory",
			Embedding: []float32{1.0, 0.0, 0.0},
			UserID:    "user-1",
		})
		store.Insert(ctx, &MemoryEntry{
			ID:        "user2-entry",
			Content:   "User 2 memory",
			Embedding: []float32{1.0, 0.0, 0.0},
			UserID:    "user-2",
		})

		filter := &SearchFilter{UserID: "user-2"}
		results, _ := store.Search(ctx, []float32{1.0, 0.0, 0.0}, 10, filter)

		found := false
		for _, r := range results {
			if r.UserID != "user-2" {
				t.Errorf("Got entry from wrong user: %s", r.UserID)
			}
			if r.ID == "user2-entry" {
				found = true
			}
		}
		if !found {
			t.Error("Should find user-2 entry")
		}
	})

	t.Run("Delete", func(t *testing.T) {
		store.Insert(ctx, &MemoryEntry{
			ID:        "to-delete",
			Content:   "Will be deleted",
			Embedding: []float32{0.0, 1.0, 0.0},
		})

		err := store.Delete(ctx, "to-delete")
		if err != nil {
			t.Fatalf("Delete failed: %v", err)
		}

		results, _ := store.Search(ctx, []float32{0.0, 1.0, 0.0}, 10, nil)
		for _, r := range results {
			if r.ID == "to-delete" {
				t.Error("Deleted entry should not appear in search")
			}
		}
	})

	t.Run("GetBySession", func(t *testing.T) {
		store.Insert(ctx, &MemoryEntry{
			ID:        "session-entry",
			Content:   "Session memory",
			Embedding: []float32{0.5, 0.5, 0.0},
			SessionID: "session-test",
		})

		results, err := store.GetBySession(ctx, "session-test")
		if err != nil {
			t.Fatalf("GetBySession failed: %v", err)
		}

		found := false
		for _, r := range results {
			if r.ID == "session-entry" {
				found = true
			}
		}
		if !found {
			t.Error("Should find session entry")
		}
	})
}

func TestSimpleEmbedder(t *testing.T) {
	embedder := NewSimpleEmbedder(128)

	t.Run("Dimension", func(t *testing.T) {
		if embedder.Dimension() != 128 {
			t.Errorf("Dimension = %d, want 128", embedder.Dimension())
		}
	})

	t.Run("Embed", func(t *testing.T) {
		ctx := context.Background()
		embedding, err := embedder.Embed(ctx, "Hello world")
		if err != nil {
			t.Fatalf("Embed failed: %v", err)
		}

		if len(embedding) != 128 {
			t.Errorf("Embedding length = %d, want 128", len(embedding))
		}

		// Check normalization
		var norm float32
		for _, v := range embedding {
			norm += v * v
		}
		// Should be close to 1.0
		if norm < 0.99 || norm > 1.01 {
			t.Errorf("Embedding norm = %f, want ~1.0", norm)
		}
	})

	t.Run("Similar texts produce similar embeddings", func(t *testing.T) {
		ctx := context.Background()
		emb1, _ := embedder.Embed(ctx, "Hello world")
		emb2, _ := embedder.Embed(ctx, "Hello there")
		emb3, _ := embedder.Embed(ctx, "Goodbye universe")

		sim12 := cosineSimilarity(emb1, emb2)
		sim13 := cosineSimilarity(emb1, emb3)

		// "Hello world" should be more similar to "Hello there" than "Goodbye universe"
		if sim12 <= sim13 {
			t.Errorf("Expected sim(hello world, hello there) > sim(hello world, goodbye universe), got %f <= %f", sim12, sim13)
		}
	})
}

func TestMemoryManager(t *testing.T) {
	store := NewInMemoryVectorStore()
	embedder := NewSimpleEmbedder(64)
	manager := NewMemoryManager(store, embedder)
	ctx := context.Background()

	t.Run("Remember and Recall", func(t *testing.T) {
		// Remember something
		entry, err := manager.Remember(ctx, "User prefers dark mode", map[string]interface{}{
			"user_id": "user-1",
			"type":    "preference",
		})
		if err != nil {
			t.Fatalf("Remember failed: %v", err)
		}

		if entry.ID == "" {
			t.Error("Entry should have ID")
		}

		// Recall with related query
		results, err := manager.Recall(ctx, "What theme does user want?", 5, nil)
		if err != nil {
			t.Fatalf("Recall failed: %v", err)
		}

		if len(results) == 0 {
			t.Error("Should recall at least one memory")
		}
	})

	t.Run("Forget", func(t *testing.T) {
		entry, _ := manager.Remember(ctx, "Temporary memory", nil)
		
		err := manager.Forget(ctx, entry.ID)
		if err != nil {
			t.Fatalf("Forget failed: %v", err)
		}
	})
}

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name string
		a    []float32
		b    []float32
		want float32
	}{
		{"Identical vectors", []float32{1, 0, 0}, []float32{1, 0, 0}, 1.0},
		{"Orthogonal vectors", []float32{1, 0, 0}, []float32{0, 1, 0}, 0.0},
		{"Opposite vectors", []float32{1, 0, 0}, []float32{-1, 0, 0}, -1.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cosineSimilarity(tt.a, tt.b)
			// Allow small floating point error
			if got < tt.want-0.01 || got > tt.want+0.01 {
				t.Errorf("cosineSimilarity() = %v, want %v", got, tt.want)
			}
		})
	}
}
