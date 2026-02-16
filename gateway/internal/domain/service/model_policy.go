package service

import (
	"fmt"
	"strings"
	"time"
)

// ModelPolicy defines per-model runtime behavior for the agent loop.
// Resolved at agent loop start from model ID, with auto-detection by model
// family and optional YAML overrides in config.yaml (model_policies section).
//
// Resolution priority: defaults → auto-detect(modelID) → YAML override
// Inspired by OpenClaw's TranscriptPolicy pattern.
type ModelPolicy struct {
	// --- Message handling ---

	// RepairToolPairing fixes orphan tool_use/tool_result blocks before sending to LLM.
	RepairToolPairing bool

	// EnforceTurnOrdering ensures strict user→assistant message alternation.
	// Required by Gemini; harmless for others.
	EnforceTurnOrdering bool

	// ReasoningFormat controls the thinking tag style injected into the prompt.
	//   "native" — model has built-in reasoning (Claude)
	//   "xml"    — inject <think>...</think><final>...</final> tags (Qwen3)
	//   "none"   — no reasoning tags (MiniMax, weaker models)
	ReasoningFormat string

	// --- Agent loop behavior ---

	// ProgressInterval is the step interval at which progress reminders are
	// injected into the conversation. 0 = disabled (e.g. for Claude which
	// self-terminates correctly).
	ProgressInterval int

	// ProgressEscalation increases urgency of progress messages as step count grows.
	ProgressEscalation bool

	// RunTimeout overrides the default per-run timeout for this model family.
	RunTimeout time.Duration

	// --- Prompt adaptation ---

	// PromptStyle controls system prompt verbosity.
	//   "concise"  — short, direct instructions (better for weaker models)
	//   "detailed" — full instructions with examples (for capable models)
	PromptStyle string

	// SystemRoleSupport indicates whether the model supports system role messages.
	// If false, system content is prepended to the first user message.
	SystemRoleSupport bool

	// ThinkingTagHint tells the prompt builder to include
	// <think>...<final> format instructions in the system prompt.
	ThinkingTagHint bool
}

// DefaultModelPolicy returns a safe baseline that works with most models.
func DefaultModelPolicy() ModelPolicy {
	return ModelPolicy{
		RepairToolPairing:   true,
		EnforceTurnOrdering: true,
		ReasoningFormat:     "none",
		ProgressInterval:    10,
		ProgressEscalation:  true,
		RunTimeout:          10 * time.Minute,
		PromptStyle:         "concise",
		SystemRoleSupport:   true,
		ThinkingTagHint:     false,
	}
}

// ResolveModelPolicy auto-detects the best policy for a given model ID,
// then applies any YAML overrides from ModelPolicyOverrides.
//
// The detection uses substring matching on the model ID, similar to
// OpenClaw's resolveTranscriptPolicy which checks provider/modelApi/modelId.
func ResolveModelPolicy(modelID string, overrides map[string]*ModelPolicyOverride) ModelPolicy {
	policy := DefaultModelPolicy()

	// --- Auto-detect from model ID ---
	lower := strings.ToLower(modelID)

	switch {
	case containsAny(lower, "qwen"):
		policy.ReasoningFormat = "xml"
		policy.ThinkingTagHint = true
		policy.ProgressInterval = 15
		policy.PromptStyle = "detailed"

	case containsAny(lower, "minimax"):
		policy.ReasoningFormat = "none"
		policy.ProgressInterval = 8
		policy.PromptStyle = "concise"

	case containsAny(lower, "claude", "anthropic"):
		policy.ReasoningFormat = "native"
		policy.ProgressInterval = 0 // Claude self-terminates
		policy.PromptStyle = "detailed"
		policy.ThinkingTagHint = false

	case containsAny(lower, "gemini", "google"):
		policy.EnforceTurnOrdering = true
		policy.ReasoningFormat = "none"
		policy.ProgressInterval = 10
		policy.PromptStyle = "detailed"

	case containsAny(lower, "deepseek"):
		policy.ReasoningFormat = "xml"
		policy.ThinkingTagHint = true
		policy.ProgressInterval = 12

	case containsAny(lower, "gpt", "openai"):
		policy.ReasoningFormat = "none"
		policy.ProgressInterval = 10
		policy.PromptStyle = "detailed"
	}

	// --- Apply YAML overrides (highest priority) ---
	if overrides == nil {
		return policy
	}

	// Try exact model family match first, then prefix match
	matchedKey := ""
	for key := range overrides {
		if strings.Contains(lower, strings.ToLower(key)) {
			if len(key) > len(matchedKey) {
				matchedKey = key // Longest match wins
			}
		}
	}

	if matchedKey != "" {
		applyOverride(&policy, overrides[matchedKey])
	}

	return policy
}

// ModelPolicyOverride holds YAML-configurable per-model policy overrides.
// All fields are pointers so nil = "don't override, use auto-detected value".
type ModelPolicyOverride struct {
	RepairToolPairing   *bool          `mapstructure:"repair_tool_pairing"`
	EnforceTurnOrdering *bool          `mapstructure:"enforce_turn_ordering"`
	ReasoningFormat     *string        `mapstructure:"reasoning_format"`
	ProgressInterval    *int           `mapstructure:"progress_interval"`
	ProgressEscalation  *bool          `mapstructure:"progress_escalation"`
	RunTimeout          *time.Duration `mapstructure:"run_timeout"`
	PromptStyle         *string        `mapstructure:"prompt_style"`
	SystemRoleSupport   *bool          `mapstructure:"system_role_support"`
	ThinkingTagHint     *bool          `mapstructure:"thinking_tag_hint"`
}

// applyOverride merges non-nil override fields into the policy.
func applyOverride(p *ModelPolicy, o *ModelPolicyOverride) {
	if o == nil {
		return
	}
	if o.RepairToolPairing != nil {
		p.RepairToolPairing = *o.RepairToolPairing
	}
	if o.EnforceTurnOrdering != nil {
		p.EnforceTurnOrdering = *o.EnforceTurnOrdering
	}
	if o.ReasoningFormat != nil {
		p.ReasoningFormat = *o.ReasoningFormat
	}
	if o.ProgressInterval != nil {
		p.ProgressInterval = *o.ProgressInterval
	}
	if o.ProgressEscalation != nil {
		p.ProgressEscalation = *o.ProgressEscalation
	}
	if o.RunTimeout != nil {
		p.RunTimeout = *o.RunTimeout
	}
	if o.PromptStyle != nil {
		p.PromptStyle = *o.PromptStyle
	}
	if o.SystemRoleSupport != nil {
		p.SystemRoleSupport = *o.SystemRoleSupport
	}
	if o.ThinkingTagHint != nil {
		p.ThinkingTagHint = *o.ThinkingTagHint
	}
}

// BuildProgressMessage generates a step-appropriate progress reminder.
// The urgency escalates with step count when ProgressEscalation is enabled.
func (p *ModelPolicy) BuildProgressMessage(step int) string {
	if p.ProgressInterval <= 0 {
		return ""
	}

	if !p.ProgressEscalation {
		return fmt.Sprintf("[SYSTEM] 已执行 %d 步。请简要汇报当前进展和下一步计划。", step)
	}

	// Escalating urgency based on step count
	switch {
	case step <= 15:
		return fmt.Sprintf("[SYSTEM] 已执行 %d 步。请简要汇报当前进展。", step)
	case step <= 25:
		return fmt.Sprintf("[SYSTEM] ⚠️ 已执行 %d 步。请检查任务是否可以完成并回复用户。如果遇到无法解决的问题，请立即告知用户。", step)
	default:
		return fmt.Sprintf("[SYSTEM] 🚨 已执行 %d 步。你必须尽快完成当前任务并回复用户。如果无法完成，请告知用户当前进展和遇到的问题。", step)
	}
}

// containsAny returns true if s contains any of the given substrings.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
