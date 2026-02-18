package service

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// Reasoning tag stripping — ported from OpenClaw's shared/text/reasoning-tags.ts.
//
// Design: model-agnostic. All models (Qwen3, MiniMax, DeepSeek, Claude, etc.)
// go through the same stripping pipeline. This avoids per-model branching and
// ensures no thinking content ever reaches the user.

// StripMode controls how unclosed <think> tags are handled.
type StripMode int

const (
	// StripStrict truncates everything after an unclosed <think> (safe default).
	StripStrict StripMode = iota
	// StripPreserve keeps content after an unclosed <think> tag.
	StripPreserve
)

// TrimMode controls whitespace trimming of the result.
type TrimMode int

const (
	TrimBoth  TrimMode = iota // default
	TrimStart                 // only leading whitespace
	TrimNone                  // no trimming
)

// StripOption configures StripReasoningTags behavior.
type StripOption func(*stripConfig)

type stripConfig struct {
	mode StripMode
	trim TrimMode
}

// WithStripMode sets strict or preserve mode.
func WithStripMode(m StripMode) StripOption {
	return func(c *stripConfig) { c.mode = m }
}

// WithTrimMode sets trimming behavior.
func WithTrimMode(t TrimMode) StripOption {
	return func(c *stripConfig) { c.trim = t }
}

// --- Compiled patterns (matching OpenClaw's reasoning-tags.ts) ---

// quickTagRe is the fast-path check: if no match, skip all processing.
var quickTagRe = regexp.MustCompile(`(?i)<\s*/?\s*(?:think(?:ing)?|thought|antthinking|final)\b`)

// finalTagRe matches <final> and </final> tags.
var finalTagRe = regexp.MustCompile(`(?i)<\s*/?\s*final\b[^<>]*>`)

// thinkingTagRe matches opening/closing think/thinking/thought/antthinking tags.
// Capture group 1 = "/" if closing tag, empty if opening.
var thinkingTagRe = regexp.MustCompile(`(?i)<\s*(/?)\s*(?:think(?:ing)?|thought|antthinking)\b[^<>]*>`)

// --- Code region detection (protects tags inside code blocks) ---

type codeRegion struct {
	start, end int
}

// findCodeRegions finds fenced code blocks (``` / ~~~) and inline code spans.
// Tags inside these regions are preserved (not stripped).
func findCodeRegions(text string) []codeRegion {
	var regions []codeRegion

	// Fenced code blocks: ```...``` or ~~~...~~~
	// Go's RE2 engine does not support backreferences, so we scan manually.
	regions = append(regions, findFencedBlocks(text, "```")...)
	regions = append(regions, findFencedBlocks(text, "~~~")...)

	// Inline code: `...` (but not inside fenced blocks)
	inlineRe := regexp.MustCompile("`+[^`]+`+")
	for _, match := range inlineRe.FindAllStringIndex(text, -1) {
		insideFenced := false
		for _, r := range regions {
			if match[0] >= r.start && match[1] <= r.end {
				insideFenced = true
				break
			}
		}
		if !insideFenced {
			regions = append(regions, codeRegion{match[0], match[1]})
		}
	}

	return regions
}

// findFencedBlocks scans text for fenced code blocks delimited by fence (``` or ~~~).
func findFencedBlocks(text, fence string) []codeRegion {
	var regions []codeRegion
	offset := 0
	for offset < len(text) {
		// Find opening fence at start of line
		idx := strings.Index(text[offset:], fence)
		if idx < 0 {
			break
		}
		start := offset + idx
		// Opening fence must be at start of text or preceded by newline
		if start > 0 && text[start-1] != '\n' {
			offset = start + len(fence)
			continue
		}
		// Skip to end of opening fence line
		lineEnd := strings.Index(text[start:], "\n")
		if lineEnd < 0 {
			break // no newline after fence = unclosed, treat rest as code
		}
		searchFrom := start + lineEnd + 1
		// Find closing fence (same delimiter, at start of line)
		closeIdx := -1
		pos := searchFrom
		for pos < len(text) {
			ci := strings.Index(text[pos:], fence)
			if ci < 0 {
				break
			}
			cand := pos + ci
			if cand == 0 || text[cand-1] == '\n' {
				closeIdx = cand
				break
			}
			pos = cand + len(fence)
		}
		if closeIdx >= 0 {
			// End after the closing fence line
			end := closeIdx + len(fence)
			if nlAfter := strings.Index(text[end:], "\n"); nlAfter >= 0 {
				end += nlAfter + 1
			} else {
				end = len(text)
			}
			regions = append(regions, codeRegion{start, end})
			offset = end
		} else {
			// Unclosed fence — treat rest of text as code
			regions = append(regions, codeRegion{start, len(text)})
			break
		}
	}
	return regions
}

func isInsideCode(pos int, regions []codeRegion) bool {
	for _, r := range regions {
		if pos >= r.start && pos < r.end {
			return true
		}
	}
	return false
}

// StripReasoningTags removes reasoning/thinking tags from model output.
//
// Supported tags (case-insensitive): <think>, <thinking>, <thought>, <antthinking>, <final>
// Tags inside code blocks (fenced ``` / ~~~ or inline `) are preserved.
//
// Default: strict mode + trim both sides.
func StripReasoningTags(text string, opts ...StripOption) string {
	if text == "" {
		return text
	}

	// Fast path: no tags detected → return immediately
	if !quickTagRe.MatchString(text) {
		return text
	}

	cfg := &stripConfig{mode: StripStrict, trim: TrimBoth}
	for _, o := range opts {
		o(cfg)
	}

	cleaned := text

	// 1. Strip <final> tags (remove markup, preserve content)
	if finalTagRe.MatchString(cleaned) {
		preCodeRegions := findCodeRegions(cleaned)
		matches := finalTagRe.FindAllStringIndex(cleaned, -1)

		// Process in reverse to preserve indices
		for i := len(matches) - 1; i >= 0; i-- {
			m := matches[i]
			if !isInsideCode(m[0], preCodeRegions) {
				cleaned = cleaned[:m[0]] + cleaned[m[1]:]
			}
		}
	}

	// 2. Strip thinking tags using state machine (like OpenClaw)
	codeRegions := findCodeRegions(cleaned)

	allMatches := thinkingTagRe.FindAllStringSubmatchIndex(cleaned, -1)

	var result strings.Builder
	result.Grow(len(cleaned))

	lastIndex := 0
	inThinking := false

	for _, match := range allMatches {
		// match[0..1] = full match, match[2..3] = group 1 (/ or empty)
		idx := match[0]
		matchEnd := match[1]

		// Check if "/" is captured (closing tag)
		isClose := match[2] != match[3] // group 1 is non-empty → closing tag

		if isInsideCode(idx, codeRegions) {
			continue
		}

		if !inThinking {
			result.WriteString(cleaned[lastIndex:idx])
			if !isClose {
				inThinking = true
			}
		} else if isClose {
			inThinking = false
		}

		lastIndex = matchEnd
	}

	// Handle tail
	if !inThinking || cfg.mode == StripPreserve {
		result.WriteString(cleaned[lastIndex:])
	}

	return applyTrim(result.String(), cfg.trim)
}

func applyTrim(s string, mode TrimMode) string {
	switch mode {
	case TrimNone:
		return s
	case TrimStart:
		return trimLeftUTF8(s)
	default: // TrimBoth
		return strings.TrimSpace(s)
	}
}

func trimLeftUTF8(s string) string {
	i := 0
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r != ' ' && r != '\t' && r != '\n' && r != '\r' {
			break
		}
		i += size
	}
	return s[i:]
}
