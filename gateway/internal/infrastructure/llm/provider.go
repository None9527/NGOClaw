package llm

import (
	"context"

	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/service"
)

// Provider is the infrastructure-layer LLM provider interface.
// Each provider implements service.LLMClient (Generate + GenerateStream) to be usable by the AgentLoop.
type Provider interface {
	service.LLMClient

	// Name returns the provider identifier (e.g. "antigravity", "bailian")
	Name() string

	// Models returns the list of supported model identifiers
	Models() []string

	// SupportsModel checks if a specific model is supported
	SupportsModel(model string) bool

	// IsAvailable checks if the provider is reachable
	IsAvailable(ctx context.Context) bool
}

// ProviderConfig holds configuration for an LLM provider
type ProviderConfig struct {
	Name     string   `json:"name"`
	BaseURL  string   `json:"base_url"`
	APIKey   string   `json:"api_key"`
	Models   []string `json:"models"`
	Priority int      `json:"priority"` // Lower = higher priority
}
