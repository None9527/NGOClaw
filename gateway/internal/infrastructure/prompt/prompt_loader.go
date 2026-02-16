package prompt

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// PromptComponent represents a single hot-pluggable prompt module
// loaded from a .md file with YAML frontmatter.
type PromptComponent struct {
	Name     string       // unique component name
	Priority int          // sort weight (lower = earlier in prompt, default 50)
	Content  string       // the actual prompt text (markdown body)
	Requires *Requirements // conditions for loading (nil = always load)
	FilePath string       // source file path for debugging
}

// Requirements defines the conditions under which a component is loaded.
// All conditions must be satisfied (AND logic).
type Requirements struct {
	// Tools — component loads only if ALL listed tools are registered
	Tools []string `yaml:"tools"`

	// AnyTool — component loads if ANY listed tool is registered
	AnyTool []string `yaml:"any_tool"`

	// Intent — component loads only for these task intents
	Intent []string `yaml:"intent"`

	// Model — component loads only for models matching these prefixes
	Model []string `yaml:"model"`
}

// ParsePromptFile reads a .md file with YAML frontmatter and returns a PromptComponent.
//
// Expected format:
//
//	---
//	name: browser_rules
//	priority: 50
//	requires:
//	  tools: [browser_navigate, browser_screenshot]
//	  intent: [general, research]
//	---
//	Your prompt content here...
func ParsePromptFile(path string) (*PromptComponent, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read prompt file: %w", err)
	}

	content := string(data)

	// Check for YAML frontmatter
	if !strings.HasPrefix(content, "---") {
		// No frontmatter — treat entire file as content with defaults
		name := fileBaseName(path)
		return &PromptComponent{
			Name:     name,
			Priority: 50,
			Content:  strings.TrimSpace(content),
			FilePath: path,
		}, nil
	}

	// Parse frontmatter
	// Find closing ---
	lines := strings.SplitN(content, "\n", -1)
	closingIdx := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			closingIdx = i
			break
		}
	}

	if closingIdx == -1 {
		return nil, fmt.Errorf("unclosed YAML frontmatter in %s", path)
	}

	frontmatter := strings.Join(lines[1:closingIdx], "\n")
	body := strings.Join(lines[closingIdx+1:], "\n")

	comp := &PromptComponent{
		Name:     fileBaseName(path),
		Priority: 50,
		Content:  strings.TrimSpace(body),
		FilePath: path,
	}

	// Parse frontmatter (lightweight YAML — no external deps)
	parseFrontmatter(frontmatter, comp)

	return comp, nil
}

// parseFrontmatter does lightweight YAML parsing for our simple schema.
// We avoid pulling in a full YAML library for just frontmatter parsing.
func parseFrontmatter(fm string, comp *PromptComponent) {
	scanner := bufio.NewScanner(strings.NewReader(fm))
	var currentSection string

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// Top-level key: value
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			parts := strings.SplitN(trimmed, ":", 2)
			if len(parts) != 2 {
				continue
			}
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])

			switch key {
			case "name":
				comp.Name = val
			case "priority":
				if p, err := strconv.Atoi(val); err == nil {
					comp.Priority = p
				}
			case "requires":
				if comp.Requires == nil {
					comp.Requires = &Requirements{}
				}
				currentSection = "requires"
			default:
				currentSection = ""
			}
			continue
		}

		// Indented lines under "requires:"
		if currentSection == "requires" && comp.Requires != nil {
			parts := strings.SplitN(trimmed, ":", 2)
			if len(parts) != 2 {
				continue
			}
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			list := parseYAMLList(val)

			switch key {
			case "tools":
				comp.Requires.Tools = list
			case "any_tool":
				comp.Requires.AnyTool = list
			case "intent":
				comp.Requires.Intent = list
			case "model":
				comp.Requires.Model = list
			}
		}
	}
}

// parseYAMLList parses "[a, b, c]" or "a, b, c" into a string slice
func parseYAMLList(val string) []string {
	val = strings.TrimPrefix(val, "[")
	val = strings.TrimSuffix(val, "]")
	parts := strings.Split(val, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// fileBaseName extracts the file name without extension
func fileBaseName(path string) string {
	// Find last separator
	name := path
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			name = path[i+1:]
			break
		}
	}
	// Remove extension
	if idx := strings.LastIndex(name, "."); idx > 0 {
		name = name[:idx]
	}
	return name
}
