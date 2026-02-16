package embedding

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOllamaEmbedder_Embed(t *testing.T) {
	dim := 8
	mockVec := make([]float32, dim)
	for i := range mockVec {
		mockVec[i] = float32(i) * 0.1
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embed" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		var req embedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}

		if req.Model != "test-model" {
			t.Fatalf("unexpected model: %s", req.Model)
		}

		resp := embedResponse{
			Model:      "test-model",
			Embeddings: [][]float32{mockVec},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// NewOllamaEmbedder probes dimension on init
	embedder, err := NewOllamaEmbedder(server.URL, "test-model", nil)
	if err != nil {
		t.Fatalf("failed to create embedder: %v", err)
	}

	if embedder.Dimension() != dim {
		t.Fatalf("expected dimension %d, got %d", dim, embedder.Dimension())
	}

	vec, err := embedder.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	if len(vec) != dim {
		t.Fatalf("expected %d dims, got %d", dim, len(vec))
	}
}

func TestOllamaEmbedder_EmbedBatch(t *testing.T) {
	dim := 4
	callCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++

		var req embedRequest
		json.NewDecoder(r.Body).Decode(&req)

		// Determine how many embeddings to return
		n := 1
		switch v := req.Input.(type) {
		case []interface{}:
			n = len(v)
		}

		embeddings := make([][]float32, n)
		for i := range embeddings {
			vec := make([]float32, dim)
			for j := range vec {
				vec[j] = float32(i+1) * 0.1
			}
			embeddings[i] = vec
		}

		resp := embedResponse{
			Model:      "test-model",
			Embeddings: embeddings,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	embedder, err := NewOllamaEmbedder(server.URL, "test-model", nil)
	if err != nil {
		t.Fatalf("failed to create embedder: %v", err)
	}
	// Reset call count after probe
	callCount = 0

	texts := []string{"hello", "world", "test"}
	vecs, err := embedder.EmbedBatch(context.Background(), texts)
	if err != nil {
		t.Fatalf("EmbedBatch failed: %v", err)
	}

	if len(vecs) != 3 {
		t.Fatalf("expected 3 vectors, got %d", len(vecs))
	}

	// Should be a single call (batch)
	if callCount != 1 {
		t.Fatalf("expected 1 API call for batch, got %d", callCount)
	}
}

func TestOllamaEmbedder_EmptyBatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := embedResponse{
			Model:      "test-model",
			Embeddings: [][]float32{{0.1, 0.2}},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	embedder, err := NewOllamaEmbedder(server.URL, "test-model", nil)
	if err != nil {
		t.Fatalf("failed to create embedder: %v", err)
	}

	vecs, err := embedder.EmbedBatch(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error for empty batch: %v", err)
	}
	if vecs != nil {
		t.Fatalf("expected nil for empty batch, got %v", vecs)
	}
}

func TestOllamaEmbedder_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("model not found"))
	}))
	defer server.Close()

	_, err := NewOllamaEmbedder(server.URL, "bad-model", nil)
	if err == nil {
		t.Fatal("expected error for bad model, got nil")
	}
}
