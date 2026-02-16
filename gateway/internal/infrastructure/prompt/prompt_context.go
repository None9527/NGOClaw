package prompt

import "strings"

// PromptContext carries runtime information used to decide
// which prompt components to load. This goes beyond Cline's
// simple "which tools exist" — it includes intent detection,
// token budgeting, and project context.
type PromptContext struct {
	// RegisteredTools lists all currently registered tool names
	RegisteredTools []string

	// ModelName is the current LLM model identifier (e.g. "bailian/qwen3-max")
	ModelName string

	// UserMessage is the raw user input — used for intent detection
	UserMessage string

	// Workspace is the current working directory
	Workspace string

	// UserRules is optional user-defined rules from config.yaml
	UserRules string

	// MaxTokenBudget is the maximum tokens to allocate for system prompt.
	// Components are loaded by priority until budget is exhausted.
	// 0 means unlimited.
	MaxTokenBudget int

	// DetectedIntent is auto-populated by AnalyzeIntent()
	DetectedIntent TaskIntent

	// --- Focus Chain ---

	// FocusFiles lists files the user is currently working on (e.g. open editor tabs).
	// These are injected as high-priority context for the LLM.
	FocusFiles []FocusFile

	// FocusContext is free-form contextual information (e.g. recent git diff, error output).
	// Injected after focus files in the system prompt.
	FocusContext string
}

// TaskIntent represents the detected type of user task.
// Used for intelligent component selection beyond simple tool matching.
type TaskIntent int

const (
	IntentGeneral    TaskIntent = iota // default: conversational
	IntentCoding                       // code generation, debugging, refactoring
	IntentResearch                     // web search, analysis, summarization
	IntentFinance                      // stock analysis, financial data
	IntentSystem                       // system admin, file management
	IntentCreative                     // writing, brainstorming
)

// String returns a human-readable name for the intent
func (i TaskIntent) String() string {
	switch i {
	case IntentCoding:
		return "coding"
	case IntentResearch:
		return "research"
	case IntentFinance:
		return "finance"
	case IntentSystem:
		return "system"
	case IntentCreative:
		return "creative"
	default:
		return "general"
	}
}

// HasTool checks if a specific tool is registered
func (c *PromptContext) HasTool(name string) bool {
	for _, t := range c.RegisteredTools {
		if t == name {
			return true
		}
	}
	return false
}

// HasAnyTool checks if any of the specified tools are registered
func (c *PromptContext) HasAnyTool(names []string) bool {
	for _, name := range names {
		if c.HasTool(name) {
			return true
		}
	}
	return false
}

// ModelPrefix extracts the provider prefix from ModelName (e.g. "bailian" from "bailian/qwen3-max")
func (c *PromptContext) ModelPrefix() string {
	for i, ch := range c.ModelName {
		if ch == '/' {
			return c.ModelName[:i]
		}
	}
	return c.ModelName
}

// ModelShortName extracts the model name without provider (e.g. "qwen3-max" from "bailian/qwen3-max")
func (c *PromptContext) ModelShortName() string {
	for i, ch := range c.ModelName {
		if ch == '/' {
			return c.ModelName[i+1:]
		}
	}
	return c.ModelName
}

// FocusFile represents a file in the user's attention focus.
type FocusFile struct {
	Path     string `json:"path"`              // Absolute or relative path
	Language string `json:"language,omitempty"` // Language identifier (e.g. "go", "python")
	Snippet  string `json:"snippet,omitempty"`  // Optional content snippet (e.g. visible lines)
	Line     int    `json:"line,omitempty"`     // Cursor line position
}

// BuildFocusSection assembles the Focus Chain into a formatted prompt section.
func (c *PromptContext) BuildFocusSection() string {
	if len(c.FocusFiles) == 0 && c.FocusContext == "" {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Current Focus\n\n")

	if len(c.FocusFiles) > 0 {
		sb.WriteString("The user is currently working on these files:\n\n")
		for _, f := range c.FocusFiles {
			sb.WriteString("- `" + f.Path + "`")
			if f.Language != "" {
				sb.WriteString(" (" + f.Language + ")")
			}
			if f.Line > 0 {
				sb.WriteString(" at line " + formatInt(f.Line))
			}
			sb.WriteString("\n")
			if f.Snippet != "" {
				sb.WriteString("  ```" + f.Language + "\n")
				sb.WriteString("  " + f.Snippet + "\n")
				sb.WriteString("  ```\n")
			}
		}
		sb.WriteString("\n")
	}

	if c.FocusContext != "" {
		sb.WriteString("### Additional Context\n\n")
		sb.WriteString(c.FocusContext)
		sb.WriteString("\n")
	}

	return sb.String()
}

// formatInt converts an int to string without importing strconv in this file
func formatInt(n int) string {
	if n == 0 {
		return "0"
	}
	result := ""
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	for n > 0 {
		result = string(rune('0'+n%10)) + result
		n /= 10
	}
	if neg {
		result = "-" + result
	}
	return result
}
