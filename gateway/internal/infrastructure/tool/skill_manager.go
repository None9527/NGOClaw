package tool

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Skill represents an installed skill with metadata parsed from SKILL.md.
type Skill struct {
	ID          string
	Name        string
	Description string
	Path        string   // Skill directory path
	Commands    []string // Provided commands
	Enabled     bool
	InstalledAt time.Time
}

// SkillManager discovers, installs, and manages skills from a directory.
// Skills are identified by a SKILL.md file in their root.
type SkillManager struct {
	skills   map[string]*Skill
	skillDir string
	mu       sync.RWMutex
}

// NewSkillManager creates a skill manager and scans the given directory.
func NewSkillManager(skillDir string) *SkillManager {
	m := &SkillManager{
		skills:   make(map[string]*Skill),
		skillDir: skillDir,
	}
	m.scanInstalledSkills()
	return m
}

// scanInstalledSkills scans the skill directory for installed skills.
func (m *SkillManager) scanInstalledSkills() {
	if m.skillDir == "" {
		return
	}

	entries, err := os.ReadDir(m.skillDir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		skillPath := filepath.Join(m.skillDir, entry.Name())
		// Follow symlinks: os.Stat resolves symlinks, entry.IsDir() does not
		info, err := os.Stat(skillPath)
		if err != nil || !info.IsDir() {
			continue
		}

		skill := m.loadSkillFromPath(skillPath)
		if skill != nil {
			m.skills[skill.ID] = skill
		}
	}
}

// loadSkillFromPath loads a skill definition from a directory path.
func (m *SkillManager) loadSkillFromPath(path string) *Skill {
	skillFile := filepath.Join(path, "SKILL.md")
	if _, err := os.Stat(skillFile); os.IsNotExist(err) {
		return nil
	}

	content, err := os.ReadFile(skillFile)
	if err != nil {
		return nil
	}

	name := filepath.Base(path)
	description := ""

	lines := strings.Split(string(content), "\n")
	if len(lines) > 0 {
		if len(lines[0]) > 2 && lines[0][0] == '#' {
			name = strings.TrimSpace(lines[0][1:])
		}
	}
	if len(lines) > 2 {
		description = strings.TrimSpace(lines[2])
	}

	return &Skill{
		ID:          filepath.Base(path),
		Name:        name,
		Description: description,
		Path:        path,
		Enabled:     true,
		InstalledAt: time.Now(),
	}
}

// Install installs a skill from a local source path via symlink.
func (m *SkillManager) Install(source, name string) (*Skill, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	targetPath := filepath.Join(m.skillDir, name)

	if _, exists := m.skills[name]; exists {
		return nil, fmt.Errorf("skill already exists: %s", name)
	}

	if _, err := os.Stat(source); err != nil {
		return nil, fmt.Errorf("source path does not exist: %s", source)
	}

	if err := os.MkdirAll(m.skillDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create skill dir: %w", err)
	}

	if err := os.Symlink(source, targetPath); err != nil {
		return nil, fmt.Errorf("install failed: %w", err)
	}

	skill := m.loadSkillFromPath(targetPath)
	if skill == nil {
		os.Remove(targetPath)
		return nil, fmt.Errorf("invalid skill directory (missing SKILL.md)")
	}

	m.skills[skill.ID] = skill
	return skill, nil
}

// Uninstall removes a skill by ID.
func (m *SkillManager) Uninstall(skillID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	skill, exists := m.skills[skillID]
	if !exists {
		return fmt.Errorf("skill not found: %s", skillID)
	}

	if err := os.RemoveAll(skill.Path); err != nil {
		return fmt.Errorf("uninstall failed: %w", err)
	}

	delete(m.skills, skillID)
	return nil
}

// Get returns a skill by ID.
func (m *SkillManager) Get(skillID string) *Skill {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.skills[skillID]
}

// List returns all installed skills.
func (m *SkillManager) List() []*Skill {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*Skill, 0, len(m.skills))
	for _, skill := range m.skills {
		result = append(result, skill)
	}
	return result
}

// Enable enables a skill by ID.
func (m *SkillManager) Enable(skillID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	skill, exists := m.skills[skillID]
	if !exists {
		return fmt.Errorf("skill not found: %s", skillID)
	}
	skill.Enabled = true
	return nil
}

// Disable disables a skill by ID.
func (m *SkillManager) Disable(skillID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	skill, exists := m.skills[skillID]
	if !exists {
		return fmt.Errorf("skill not found: %s", skillID)
	}
	skill.Enabled = false
	return nil
}

// GetEnabledSkills returns all enabled skills.
func (m *SkillManager) GetEnabledSkills() []*Skill {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*Skill
	for _, skill := range m.skills {
		if skill.Enabled {
			result = append(result, skill)
		}
	}
	return result
}

// ToolPromoter is a dependency-inverted interface for registering
// skill scripts as tool backends.
type ToolPromoter interface {
	RegisterCommand(name, description, command string, aliases map[string][]string) error
}

// PromoteToTool upgrades a skill to a registered tool.
// Condition: scripts/*.py or scripts/*.sh exist in the skill directory.
func (m *SkillManager) PromoteToTool(skillID string, promoter ToolPromoter) error {
	m.mu.RLock()
	skill, exists := m.skills[skillID]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("skill not found: %s", skillID)
	}

	scriptsDir := filepath.Join(skill.Path, "scripts")
	entries, err := os.ReadDir(scriptsDir)
	if err != nil {
		return fmt.Errorf("skill has no scripts/ directory: %w", err)
	}

	promoted := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := filepath.Ext(name)
		if ext != ".py" && ext != ".sh" {
			continue
		}

		toolName := skillID + "_" + name[:len(name)-len(ext)]
		scriptPath := filepath.Join(scriptsDir, name)

		var command string
		switch ext {
		case ".py":
			command = "python3 " + scriptPath
		case ".sh":
			command = "bash " + scriptPath
		}

		aliases := map[string][]string{
			"claude": {toolName, toPascalCase(toolName)},
			"gemini": {toolName},
			"openai": {toolName},
		}

		description := fmt.Sprintf("Promoted from skill '%s': %s", skill.Name, skill.Description)
		if err := promoter.RegisterCommand(toolName, description, command, aliases); err != nil {
			return fmt.Errorf("failed to register tool %s: %w", toolName, err)
		}
		promoted++
	}

	if promoted == 0 {
		return fmt.Errorf("skill %s has no promotable scripts (.py or .sh) in scripts/", skillID)
	}

	return nil
}

// toPascalCase converts snake_case to PascalCase.
func toPascalCase(s string) string {
	parts := strings.Split(s, "_")
	var result string
	for _, p := range parts {
		if len(p) > 0 {
			result += strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return result
}
