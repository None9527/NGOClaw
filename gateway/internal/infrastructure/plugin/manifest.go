package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Manifest represents a plugin's manifest.json / plugin.json
type Manifest struct {
	// Identity
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Author      string `json:"author"`

	// Entry points
	Main string `json:"main"` // Main executable/script path

	// Capabilities
	Tools    []ManifestTool    `json:"tools,omitempty"`
	Commands []ManifestCommand `json:"commands,omitempty"`
	Hooks    []ManifestHook    `json:"hooks,omitempty"`

	// Requirements
	MinGatewayVersion string   `json:"min_gateway_version,omitempty"`
	Dependencies      []string `json:"dependencies,omitempty"`

	// Runtime
	Config map[string]ManifestConfigField `json:"config,omitempty"`
}

// ManifestTool defines a tool provided by the plugin
type ManifestTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Schema      map[string]interface{} `json:"schema,omitempty"`
}

// ManifestCommand defines a chat command provided by the plugin
type ManifestCommand struct {
	Name        string   `json:"name"`
	Aliases     []string `json:"aliases,omitempty"`
	Description string   `json:"description"`
	Usage       string   `json:"usage,omitempty"`
}

// ManifestHook defines a lifecycle hook
type ManifestHook struct {
	Event   string `json:"event"` // on_load, on_unload, on_message, on_command
	Handler string `json:"handler"`
}

// ManifestConfigField defines a configurable field
type ManifestConfigField struct {
	Type        string      `json:"type"` // string, int, bool
	Default     interface{} `json:"default,omitempty"`
	Description string      `json:"description"`
	Required    bool        `json:"required"`
}

// LoadManifest loads and validates a plugin manifest from a directory
func LoadManifest(pluginDir string) (*Manifest, error) {
	// Try multiple manifest file names
	names := []string{"plugin.json", "manifest.json"}
	var data []byte
	var err error

	for _, name := range names {
		path := filepath.Join(pluginDir, name)
		data, err = os.ReadFile(path)
		if err == nil {
			break
		}
	}

	if data == nil {
		return nil, fmt.Errorf("no manifest found in %s (tried: %v)", pluginDir, names)
	}

	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}

	if err := m.Validate(); err != nil {
		return nil, fmt.Errorf("invalid manifest: %w", err)
	}

	return &m, nil
}

// Validate checks that required fields are present
func (m *Manifest) Validate() error {
	if m.Name == "" {
		return fmt.Errorf("missing required field: name")
	}
	if m.Version == "" {
		return fmt.Errorf("missing required field: version")
	}
	return nil
}

// HasTools returns true if the plugin provides tools
func (m *Manifest) HasTools() bool {
	return len(m.Tools) > 0
}

// HasCommands returns true if the plugin provides commands
func (m *Manifest) HasCommands() bool {
	return len(m.Commands) > 0
}

// HasHooks returns true if the plugin has lifecycle hooks
func (m *Manifest) HasHooks() bool {
	return len(m.Hooks) > 0
}
