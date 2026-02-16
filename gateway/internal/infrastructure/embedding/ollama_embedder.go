package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// OllamaEmbedder generates embeddings via Ollama HTTP API.
// Implements memory.EmbeddingProvider interface.
type OllamaEmbedder struct {
	baseURL   string
	model     string
	dimension int
	client    *http.Client
	logger    *zap.Logger
}

// embedRequest matches Ollama /api/embed payload
type embedRequest struct {
	Model string      `json:"model"`
	Input interface{} `json:"input"` // string or []string
}

// embedResponse matches Ollama /api/embed response
type embedResponse struct {
	Model      string      `json:"model"`
	Embeddings [][]float32 `json:"embeddings"`
}

// NewOllamaEmbedder creates a new Ollama embedding provider.
// It probes the model to auto-detect the vector dimension.
func NewOllamaEmbedder(baseURL, model string, logger *zap.Logger) (*OllamaEmbedder, error) {
	if logger == nil {
		logger = zap.NewNop()
	}
	e := &OllamaEmbedder{
		baseURL: baseURL,
		model:   model,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: logger,
	}

	// Probe dimension with a short text
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	probe, err := e.Embed(ctx, "dimension probe")
	if err != nil {
		return nil, fmt.Errorf("failed to probe embedding dimension for model %s: %w", model, err)
	}
	e.dimension = len(probe)

	logger.Info("OllamaEmbedder initialized",
		zap.String("model", model),
		zap.String("url", baseURL),
		zap.Int("dimension", e.dimension),
	)

	return e, nil
}

// Embed generates an embedding vector for a single text.
func (e *OllamaEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	vectors, err := e.doEmbed(ctx, text)
	if err != nil {
		return nil, err
	}
	if len(vectors) == 0 {
		return nil, fmt.Errorf("empty embedding response from Ollama")
	}
	return vectors[0], nil
}

// EmbedBatch generates embedding vectors for multiple texts in one call.
// Ollama /api/embed natively supports []string input.
func (e *OllamaEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	if len(texts) == 1 {
		vec, err := e.Embed(ctx, texts[0])
		if err != nil {
			return nil, err
		}
		return [][]float32{vec}, nil
	}
	return e.doEmbed(ctx, texts)
}

// Dimension returns the vector dimension (auto-detected on init).
func (e *OllamaEmbedder) Dimension() int {
	return e.dimension
}

// doEmbed calls Ollama /api/embed with either string or []string input.
func (e *OllamaEmbedder) doEmbed(ctx context.Context, input interface{}) ([][]float32, error) {
	reqBody := embedRequest{
		Model: e.model,
		Input: input,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal embed request: %w", err)
	}

	url := e.baseURL + "/api/embed"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		// Retry once on network error
		e.logger.Warn("Ollama embed request failed, retrying",
			zap.Error(err),
		)
		resp, err = e.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("ollama embed request failed after retry: %w", err)
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama embed returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var embedResp embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&embedResp); err != nil {
		return nil, fmt.Errorf("failed to decode embed response: %w", err)
	}

	if len(embedResp.Embeddings) == 0 {
		return nil, fmt.Errorf("ollama returned empty embeddings array")
	}

	return embedResp.Embeddings, nil
}
