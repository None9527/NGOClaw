package llm

import (
	"context"
	"fmt"
	"sync"

	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/service"
	"go.uber.org/zap"
)

// Provider is the infrastructure-layer LLM provider interface.
// Each provider implements service.LLMClient (Generate + GenerateStream) to be usable by the AgentLoop.
type Provider interface {
	service.LLMClient

	// Name returns the provider identifier (e.g. "bailian", "claude")
	Name() string

	// Models returns the list of supported model identifiers
	Models() []string

	// SupportsModel checks if a specific model is supported
	SupportsModel(model string) bool

	// IsAvailable checks if the provider is reachable
	IsAvailable(ctx context.Context) bool
}

// ProviderConfig holds configuration for an LLM provider.
type ProviderConfig struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`      // "openai" (default) | "anthropic" | "gemini"
	BaseURL  string   `json:"base_url"`
	APIKey   string   `json:"api_key"`
	Models   []string `json:"models"`
	Priority int      `json:"priority"` // Lower = higher priority
}

// --- Provider Factory Registry ---
// Providers register themselves via init() in their own package.
// Adding a new provider type = implement Provider + RegisterFactory("type", New).

// ProviderFactory creates a Provider from config.
type ProviderFactory func(cfg ProviderConfig, logger *zap.Logger) Provider

var (
	factoryMu sync.RWMutex
	factories = map[string]ProviderFactory{}
)

// RegisterFactory registers a provider factory for the given type name.
// Called from init() in each provider sub-package (e.g. llm/openai, llm/anthropic).
func RegisterFactory(typeName string, factory ProviderFactory) {
	factoryMu.Lock()
	defer factoryMu.Unlock()
	factories[typeName] = factory
}

// CreateProvider creates a Provider using the registered factory for cfg.Type.
// If Type is empty, defaults to "openai" for backward compatibility.
func CreateProvider(cfg ProviderConfig, logger *zap.Logger) (Provider, error) {
	t := cfg.Type
	if t == "" {
		t = "openai"
	}

	factoryMu.RLock()
	factory, ok := factories[t]
	factoryMu.RUnlock()

	if !ok {
		available := make([]string, 0, len(factories))
		factoryMu.RLock()
		for k := range factories {
			available = append(available, k)
		}
		factoryMu.RUnlock()
		return nil, fmt.Errorf("unknown provider type %q (available: %v)", t, available)
	}

	return factory(cfg, logger), nil
}
