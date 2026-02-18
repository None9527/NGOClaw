package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// MCPFileConfig represents the standalone ~/.ngoclaw/mcp.json configuration.
type MCPFileConfig struct {
	Servers []MCPServerEntry `json:"servers"`
}

// MCPServerEntry is one MCP server in mcp.json.
type MCPServerEntry struct {
	Name     string `json:"name"`
	Endpoint string `json:"endpoint"`
	Enabled  bool   `json:"enabled"`
}

// LoadMCPConfig loads MCP configuration from ~/.ngoclaw/mcp.json.
// If the file does not exist, it creates an empty config and returns it.
func LoadMCPConfig(homeDir string) (*MCPFileConfig, string, error) {
	configDir := filepath.Join(homeDir, ".ngoclaw")
	configPath := filepath.Join(configDir, "mcp.json")

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Create empty config
			cfg := &MCPFileConfig{Servers: []MCPServerEntry{}}
			if mkErr := os.MkdirAll(configDir, 0755); mkErr != nil {
				return cfg, configPath, nil // return empty, best effort
			}
			_ = SaveMCPConfig(configPath, cfg)
			return cfg, configPath, nil
		}
		return nil, configPath, err
	}

	var cfg MCPFileConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, configPath, err
	}

	return &cfg, configPath, nil
}

// SaveMCPConfig writes the MCP configuration to disk.
func SaveMCPConfig(path string, cfg *MCPFileConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
