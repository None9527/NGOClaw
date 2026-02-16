package sideload

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Manifest represents a module's manifest.yaml
type Manifest struct {
	Name         string            `yaml:"name"`
	Version      string            `yaml:"version"`
	Runtime      RuntimeType       `yaml:"runtime"`
	Entrypoint   string            `yaml:"entrypoint"`
	Transport    TransportType     `yaml:"transport"`
	Address      string            `yaml:"address,omitempty"`      // for tcp/unix transport
	Capabilities ManifestCaps      `yaml:"capabilities,omitempty"`
	Env          map[string]string `yaml:"env,omitempty"`          // extra environment variables
	WorkDir      string            `yaml:"work_dir,omitempty"`     // working directory for process
}

// RuntimeType represents the module's language runtime
type RuntimeType string

const (
	RuntimePython RuntimeType = "python"
	RuntimeNode   RuntimeType = "node"
	RuntimeBinary RuntimeType = "binary"
	RuntimeGo     RuntimeType = "go"
)

// TransportType represents the module's transport mechanism
type TransportType string

const (
	TransportStdio TransportType = "stdio"
	TransportTCP   TransportType = "tcp"
	TransportUnix  TransportType = "unix"
)

// ManifestCaps declares what the module provides
type ManifestCaps struct {
	Providers []ManifestProvider `yaml:"providers,omitempty"`
	Tools     []ManifestTool     `yaml:"tools,omitempty"`
	Hooks     []string           `yaml:"hooks,omitempty"`
}

// ManifestProvider declares a LLM provider in the manifest
type ManifestProvider struct {
	ID      string   `yaml:"id"`
	Models  []string `yaml:"models"`
	BaseURL string   `yaml:"base_url,omitempty"`
}

// ManifestTool declares a tool in the manifest
type ManifestTool struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// ParseManifest reads and parses a manifest.yaml file
func ParseManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest %s: %w", path, err)
	}

	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest %s: %w", path, err)
	}

	if err := m.validate(); err != nil {
		return nil, fmt.Errorf("invalid manifest %s: %w", path, err)
	}

	return &m, nil
}

func (m *Manifest) validate() error {
	if m.Name == "" {
		return fmt.Errorf("name is required")
	}
	if m.Version == "" {
		return fmt.Errorf("version is required")
	}
	if m.Runtime == "" {
		return fmt.Errorf("runtime is required")
	}
	if m.Entrypoint == "" {
		return fmt.Errorf("entrypoint is required")
	}
	if m.Transport == "" {
		m.Transport = TransportStdio // default
	}

	// Validate transport-specific fields
	switch m.Transport {
	case TransportStdio:
		// No extra fields needed
	case TransportTCP, TransportUnix:
		if m.Address == "" {
			return fmt.Errorf("address is required for transport %s", m.Transport)
		}
	default:
		return fmt.Errorf("unsupported transport: %s", m.Transport)
	}

	return nil
}

// DiscoverModules scans directories for manifest.yaml files
// Search order: global (~/.ngoclaw/modules/), then project-local (.ngoclaw/modules/)
func DiscoverModules(globalDir, projectDir string) ([]*DiscoveredModule, error) {
	var modules []*DiscoveredModule

	for _, dir := range []string{globalDir, projectDir} {
		if dir == "" {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read module dir %s: %w", dir, err)
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}

			manifestPath := filepath.Join(dir, entry.Name(), "manifest.yaml")
			if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
				continue
			}

			manifest, err := ParseManifest(manifestPath)
			if err != nil {
				// Log but don't fail — skip broken modules
				continue
			}

			modules = append(modules, &DiscoveredModule{
				Path:     filepath.Join(dir, entry.Name()),
				Manifest: manifest,
			})
		}
	}

	return modules, nil
}

// DiscoveredModule is a module found during discovery
type DiscoveredModule struct {
	Path     string
	Manifest *Manifest
}
