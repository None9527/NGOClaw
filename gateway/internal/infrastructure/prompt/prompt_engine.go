package prompt

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	toolpkg "github.com/ngoclaw/ngoclaw/gateway/internal/infrastructure/tool"
	"go.uber.org/zap"
)

// PromptEngine is the hot-pluggable system prompt assembly engine.
// It discovers prompt components from the filesystem and assembles
// a context-aware system prompt at runtime.
//
// Three-layer architecture:
//
//	System layer:    ~/.ngoclaw/          — global defaults
//	Workspace layer: <project>/.ngoclaw/  — project-specific overrides
//	Channel layer:   ~/.ngoclaw/<channel>/ — channel-specific (cli, telegram)
//
// Within each layer:
//   - SOUL:       soul.md — always loaded, defines agent persona
//   - Components: prompts/*.md — loaded by requires conditions
//   - Variants:   prompts/variants/*.md — loaded by model name
//
// Merge rules:
//   - Workspace overrides system (same-name component replaces)
//   - Channel overrides shared (same-name component replaces)
type PromptEngine struct {
	soul       string                      // core soul.md content (always prepended)
	components []*PromptComponent          // all shared components (merged)
	variants   map[string]*PromptComponent // model prefix → variant

	// Channel-specific overlays
	channelSouls map[string]string                // "cli" → cli/soul.md content
	channelComps map[string][]*PromptComponent     // "cli" → cli/prompts/*.md

	systemDir string  // ~/.ngoclaw (system-level)
	wsDir     string  // <workspace>/.ngoclaw (workspace-level, may be empty)
	logger    *zap.Logger
	mu        sync.RWMutex

	// Assembly cache: avoids re-assembling identical prompts within the same session.
	// Key: "channel|model|intent|focusLen|userRulesLen"
	// Invalidated on Reload() and Discover().
	cache map[string]string
}

// NewPromptEngine creates a new prompt engine.
// workspaceDir is the project root (can be empty for no workspace layer).
// Call Discover() to load files from the filesystem.
func NewPromptEngine(workspaceDir string, logger *zap.Logger) *PromptEngine {
	homeDir, _ := os.UserHomeDir()

	var wsDir string
	if workspaceDir != "" {
		wsDir = filepath.Join(workspaceDir, ".ngoclaw")
	}

	return &PromptEngine{
		components:   make([]*PromptComponent, 0),
		variants:     make(map[string]*PromptComponent),
		channelSouls: make(map[string]string),
		channelComps: make(map[string][]*PromptComponent),
		cache:        make(map[string]string),
		systemDir:    filepath.Join(homeDir, ".ngoclaw"),
		wsDir:        wsDir,
		logger:       logger,
	}
}

// Discover scans System, Workspace, and Channel layers for prompt files.
// Workspace items override System items with the same name.
// Channel items override shared items with the same name.
// Called at startup and can be called again for hot-reload.
func (e *PromptEngine) Discover() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Reset
	e.soul = ""
	e.components = e.components[:0]
	e.variants = make(map[string]*PromptComponent)
	e.channelSouls = make(map[string]string)
	e.channelComps = make(map[string][]*PromptComponent)
	e.cache = make(map[string]string) // Invalidate assembly cache

	// 1. Load SOUL — workspace overrides system
	soulPaths := []string{filepath.Join(e.systemDir, "soul.md")}
	if e.wsDir != "" {
		soulPaths = append(soulPaths, filepath.Join(e.wsDir, "soul.md"))
	}
	for _, sp := range soulPaths {
		if data, err := os.ReadFile(sp); err == nil {
			e.soul = strings.TrimSpace(string(data))
			e.logger.Info("Loaded soul", zap.String("path", sp), zap.Int("chars", len(e.soul)))
		}
	}

	// 2. Load shared components from both layers — workspace overrides system by name
	compMap := make(map[string]*PromptComponent) // name → component (last wins)

	promptDirs := []string{filepath.Join(e.systemDir, "prompts")}
	if e.wsDir != "" {
		promptDirs = append(promptDirs, filepath.Join(e.wsDir, "prompts"))
	}

	for _, dir := range promptDirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			e.logger.Warn("Failed to create prompts dir", zap.String("dir", dir), zap.Error(err))
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			comp, err := ParsePromptFile(path)
			if err != nil {
				e.logger.Warn("Failed to parse prompt", zap.String("file", path), zap.Error(err))
				continue
			}
			compMap[comp.Name] = comp // workspace same-name replaces system
			e.logger.Info("Loaded prompt component",
				zap.String("name", comp.Name),
				zap.String("from", dir),
				zap.Int("priority", comp.Priority),
				zap.Bool("conditional", comp.Requires != nil),
			)
		}
	}

	for _, comp := range compMap {
		e.components = append(e.components, comp)
	}

	// 3. Load variants from both layers — workspace overrides system
	variantDirs := []string{filepath.Join(e.systemDir, "prompts", "variants")}
	if e.wsDir != "" {
		variantDirs = append(variantDirs, filepath.Join(e.wsDir, "prompts", "variants"))
	}

	for _, dir := range variantDirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			continue
		}
		entries, _ := os.ReadDir(dir)
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			comp, err := ParsePromptFile(path)
			if err != nil {
				e.logger.Warn("Failed to parse variant", zap.String("file", path), zap.Error(err))
				continue
			}
			key := strings.TrimSuffix(entry.Name(), ".md")
			e.variants[key] = comp
			e.logger.Info("Loaded prompt variant", zap.String("key", key), zap.String("from", dir))
		}
	}

	// 4. Load channel-specific overlays (cli, telegram, etc.)
	for _, channel := range []string{"cli", "telegram"} {
		channelDir := filepath.Join(e.systemDir, channel)

		// Channel soul.md
		channelSoulPath := filepath.Join(channelDir, "soul.md")
		if data, err := os.ReadFile(channelSoulPath); err == nil {
			content := strings.TrimSpace(string(data))
			if content != "" {
				e.channelSouls[channel] = content
				e.logger.Info("Loaded channel soul",
					zap.String("channel", channel),
					zap.Int("chars", len(content)),
				)
			}
		}

		// Channel prompts/*.md
		channelPromptsDir := filepath.Join(channelDir, "prompts")
		if err := os.MkdirAll(channelPromptsDir, 0755); err != nil {
			continue
		}
		entries, err := os.ReadDir(channelPromptsDir)
		if err != nil {
			continue
		}
		var channelComps []*PromptComponent
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			path := filepath.Join(channelPromptsDir, entry.Name())
			comp, err := ParsePromptFile(path)
			if err != nil {
				e.logger.Warn("Failed to parse channel prompt",
					zap.String("channel", channel),
					zap.String("file", path),
					zap.Error(err),
				)
				continue
			}
			channelComps = append(channelComps, comp)
			e.logger.Info("Loaded channel prompt",
				zap.String("channel", channel),
				zap.String("name", comp.Name),
				zap.Int("priority", comp.Priority),
			)
		}
		if len(channelComps) > 0 {
			e.channelComps[channel] = channelComps
		}
	}

	layers := 1
	if e.wsDir != "" {
		if _, err := os.Stat(e.wsDir); err == nil {
			layers = 2
		}
	}
	channelCount := len(e.channelSouls) + len(e.channelComps)

	e.logger.Info("Prompt engine initialized",
		zap.Bool("has_soul", e.soul != ""),
		zap.Int("components", len(e.components)),
		zap.Int("variants", len(e.variants)),
		zap.Int("channel_overlays", channelCount),
		zap.Int("layers", layers),
	)

	return nil
}

// Assemble builds the final system prompt from discovered components,
// filtered by the runtime context. This is the core intelligence.
//
// Assembly order:
//  1. Core SOUL (always first — highest attention)
//  2. Channel SOUL (if exists)
//  3. Runtime environment block (OS, time, model, workspace)
//  4. Matched variant (model-specific rules)
//  5. Shared components + channel components (merged, sorted by priority)
//  6. Long-term memory
//  7. Focus chain
//  8. User rules (from config)
//  9. Token budget truncation if needed
func (e *PromptEngine) Assemble(ctx PromptContext) string {
	// Auto-detect intent from user message
	if ctx.DetectedIntent == IntentGeneral && ctx.UserMessage != "" {
		ctx.DetectedIntent = AnalyzeIntent(ctx.UserMessage)
	}

	// NOTE: Prompt cache is intentionally DISABLED.
	// loadMemoryFiles() reads dynamic data (memory.json, daily logs) that changes
	// between requests. Serving from cache would freeze stale memory into the prompt,
	// making /new unable to clear pollution. If caching is re-enabled in the future,
	// memory sections must be assembled outside the cached path.

	e.mu.Lock()
	defer e.mu.Unlock()

	var sections []string

	// 1. Core SOUL — always first
	if e.soul != "" {
		sections = append(sections, e.soul)
	}

	// 2. Channel SOUL — appends to core soul
	if ctx.Channel != "" {
		if channelSoul, ok := e.channelSouls[ctx.Channel]; ok {
			sections = append(sections, channelSoul)
		}
	}

	// 3. Runtime environment block
	runtimeBlock := BuildRuntimeBlock(RuntimeBlockOptions{
		Channel:   ctx.Channel,
		ModelName: ctx.ModelName,
		Workspace: ctx.Workspace,
	})
	sections = append(sections, runtimeBlock)

	// 3b. Tooling section — tool summaries + call style (OpenClaw-aligned)
	if toolSection := buildToolingSection(ctx); toolSection != "" {
		sections = append(sections, toolSection)
	}

	// 4. Model variant
	variant := e.matchVariant(ctx.ModelName)
	if variant != nil {
		sections = append(sections, variant.Content)
	}

	// 5. Merge shared components + channel components
	//    Channel components with the same name override shared ones.
	eligible := e.filterComponents(ctx)

	// Build a set of channel component names for override detection
	channelCompNames := make(map[string]bool)
	var channelComps []*PromptComponent
	if ctx.Channel != "" {
		if comps, ok := e.channelComps[ctx.Channel]; ok {
			for _, comp := range comps {
				// Also filter channel components by requirements
				if e.meetsRequirements(comp, ctx) {
					channelComps = append(channelComps, comp)
					channelCompNames[comp.Name] = true
				}
			}
		}
	}

	// Filter out shared components that are overridden by channel components
	var merged []*PromptComponent
	for _, comp := range eligible {
		if !channelCompNames[comp.Name] {
			merged = append(merged, comp)
		}
	}
	merged = append(merged, channelComps...)

	// Sort by priority
	sort.Slice(merged, func(i, j int) bool {
		return merged[i].Priority < merged[j].Priority
	})

	for _, comp := range merged {
		sections = append(sections, comp.Content)
	}

	// 6. Long-term Memory
	if memContent := e.loadMemoryFiles(ctx); memContent != "" {
		sections = append(sections, memContent)
	}

	// 7. Focus Chain
	if focusSection := ctx.BuildFocusSection(); focusSection != "" {
		sections = append(sections, focusSection)
	}

	// 8. User rules (from config)
	if ctx.UserRules != "" {
		sections = append(sections, "## User Custom Rules\n"+ctx.UserRules)
	}

	// 9. Assemble with separators
	result := strings.Join(sections, "\n\n---\n\n")

	// 10. Token budget truncation (rough: 1 token ≈ 3 chars for CJK, 4 for EN)
	if ctx.MaxTokenBudget > 0 {
		maxChars := ctx.MaxTokenBudget * 3 // conservative CJK estimate
		if len(result) > maxChars {
			result = result[:maxChars]
			result += "\n\n[System prompt truncated due to token budget]"
			e.logger.Warn("System prompt truncated",
				zap.Int("budget_tokens", ctx.MaxTokenBudget),
				zap.Int("original_chars", len(result)),
			)
		}
	}

	return result
}

// buildToolingSection generates the "## Tooling" and "## Tool Call Style" sections.
// Aligned with OpenClaw's coreToolSummaries pattern: a quick-reference table of available
// tools embedded in the system prompt, plus efficiency guidelines for tool usage.
func buildToolingSection(ctx PromptContext) string {
	if len(ctx.RegisteredTools) == 0 {
		return ""
	}

	var sb strings.Builder

	// Section 1: Tool availability table
	sb.WriteString("## Tooling\n\n")
	sb.WriteString("Tool availability (filtered by policy). Names are case-sensitive.\n\n")

	for _, name := range ctx.RegisteredTools {
		if summary, ok := ctx.ToolSummaries[name]; ok && summary != "" {
			// Truncate to first sentence for brevity
			brief := firstSentence(summary)
			sb.WriteString("- " + name + ": " + brief + "\n")
		} else {
			sb.WriteString("- " + name + "\n")
		}
	}

	// Section 2: Tool Call Style (efficiency guidelines)
	sb.WriteString("\n## Tool Call Style\n\n")
	sb.WriteString("Default: do not narrate routine, low-risk tool calls (just call the tool).\n")
	sb.WriteString("Narrate only when it helps: multi-step work, complex/challenging problems, sensitive actions (e.g. deletions), or when the user explicitly asks.\n")
	sb.WriteString("Keep narration brief and value-dense; avoid repeating obvious steps.\n")
	sb.WriteString("\nBest practices:\n")
	sb.WriteString("- curl downloads: prefer `-L` (follow redirects) as a safe default. After downloading, verify content type with `file <path>` before further use.\n")
	sb.WriteString("- One-shot preference: combine related commands where possible (e.g. `curl -L ... -o file && file file`).\n")
	sb.WriteString("- After a successful send_photo/send_document, stop — do not re-send unless the user asks.\n")

	return sb.String()
}

// firstSentence extracts the first sentence from a description string.
// Truncates at first period, newline, or 80 chars, whichever comes first.
func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		s = s[:idx]
	}
	if idx := strings.Index(s, ". "); idx >= 0 && idx < 80 {
		return s[:idx+1]
	}
	if len(s) > 80 {
		return s[:80] + "…"
	}
	return s
}

// filterComponents returns components whose requirements are satisfied
func (e *PromptEngine) filterComponents(ctx PromptContext) []*PromptComponent {
	result := make([]*PromptComponent, 0, len(e.components))

	for _, comp := range e.components {
		if e.meetsRequirements(comp, ctx) {
			result = append(result, comp)
		}
	}

	return result
}

// meetsRequirements checks if a component's conditions are met (AND logic)
func (e *PromptEngine) meetsRequirements(comp *PromptComponent, ctx PromptContext) bool {
	req := comp.Requires
	if req == nil {
		return true // no requirements = always load
	}

	// Check: ALL required tools must be registered
	if len(req.Tools) > 0 {
		for _, t := range req.Tools {
			if !ctx.HasTool(t) {
				return false
			}
		}
	}

	// Check: ANY of these tools must be registered
	if len(req.AnyTool) > 0 {
		if !ctx.HasAnyTool(req.AnyTool) {
			return false
		}
	}

	// Check: intent must match
	if len(req.Intent) > 0 {
		intentStr := ctx.DetectedIntent.String()
		matched := false
		for _, i := range req.Intent {
			if i == intentStr {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// Check: model must match (prefix match)
	if len(req.Model) > 0 {
		modelLower := strings.ToLower(ctx.ModelName)
		matched := false
		for _, m := range req.Model {
			if strings.Contains(modelLower, strings.ToLower(m)) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	return true
}

// matchVariant finds the best matching variant for the model
func (e *PromptEngine) matchVariant(modelName string) *PromptComponent {
	if modelName == "" {
		return e.variants["default"]
	}

	lower := strings.ToLower(modelName)

	// Try exact model short name match first (e.g. "qwen3-max")
	for key, v := range e.variants {
		if strings.Contains(lower, strings.ToLower(key)) {
			return v
		}
	}

	// Fallback to default
	return e.variants["default"]
}

// AnalyzeIntent detects the task type from the user's message.
// This is a key differentiator over Cline — we don't just check
// registered tools, we understand what the user actually wants.
func AnalyzeIntent(message string) TaskIntent {
	msg := strings.ToLower(message)

	// Finance signals
	financeKeywords := []string{
		"股票", "走势", "行情", "k线", "涨", "跌", "买入", "卖出",
		"stock", "price", "trade", "kline", "market",
		"预测", "分析", "基金", "期货", "指数",
	}
	for _, kw := range financeKeywords {
		if strings.Contains(msg, kw) {
			return IntentFinance
		}
	}

	// Coding signals
	codingKeywords := []string{
		"代码", "函数", "bug", "报错", "编译", "debug", "重构",
		"code", "function", "error", "compile", "refactor", "implement",
		"golang", "python", "javascript", "typescript", "rust",
		"接口", "api", "class", "struct", "模块",
	}
	for _, kw := range codingKeywords {
		if strings.Contains(msg, kw) {
			return IntentCoding
		}
	}

	// Research signals
	researchKeywords := []string{
		"搜索", "查找", "研究", "新闻", "最新",
		"search", "find", "research", "news", "latest",
		"总结", "汇总", "对比", "分析报告",
	}
	for _, kw := range researchKeywords {
		if strings.Contains(msg, kw) {
			return IntentResearch
		}
	}

	// System signals
	systemKeywords := []string{
		"文件", "目录", "进程", "服务", "部署", "配置",
		"file", "directory", "process", "service", "deploy", "config",
		"docker", "nginx", "ssh", "systemctl",
	}
	for _, kw := range systemKeywords {
		if strings.Contains(msg, kw) {
			return IntentSystem
		}
	}

	// Creative signals
	creativeKeywords := []string{
		"写一篇", "故事", "文章", "翻译", "润色", "创意",
		"write", "story", "article", "translate", "creative",
	}
	for _, kw := range creativeKeywords {
		if strings.Contains(msg, kw) {
			return IntentCreative
		}
	}

	return IntentGeneral
}

// ComponentCount returns the number of loaded components (for diagnostics)
func (e *PromptEngine) ComponentCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.components)
}

// VariantCount returns the number of loaded variants
func (e *PromptEngine) VariantCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.variants)
}

// HasSoul returns true if a soul.md was loaded
func (e *PromptEngine) HasSoul() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.soul != ""
}

// Reload reloads all prompt files from disk (hot-reload support)
func (e *PromptEngine) Reload() error {
	e.logger.Info("Reloading prompt engine")
	return e.Discover()
}

// loadMemoryFiles reads daily logs + workspace MEMORY.md and returns assembled section.
// Aligned with OpenClaw: plain Markdown files are the source of truth.
//
// Sources (in order):
//   - Daily logs:  ~/.ngoclaw/memory/YYYY-MM-DD.md (today + yesterday)
//   - MEMORY.md:   <workspace>/MEMORY.md (curated long-term memory, if exists)
//
// NOTE: memory.json (structured facts) is intentionally NOT loaded.
// The old MemoryMiddleware produced low-quality, unfiltered facts that polluted
// the system prompt and caused the bot to ignore user prompts after /new.
// Future: agent writes memory via file tools (OpenClaw pattern).
func (e *PromptEngine) loadMemoryFiles(ctx PromptContext) string {
	var parts []string

	// Daily logs — today + yesterday (OpenClaw-style memory/YYYY-MM-DD.md)
	if dailyContent := toolpkg.ReadDailyLogs(); dailyContent != "" {
		parts = append(parts, fmt.Sprintf("<MEMORY[daily_log]>\n%s\n</MEMORY[daily_log]>", dailyContent))
	}

	// Workspace MEMORY.md — curated long-term memory (OpenClaw pattern)
	// Check both <workspace>/MEMORY.md and <workspace>/.ngoclaw/memory.md
	if e.wsDir != "" {
		memoryPaths := []string{
			filepath.Join(filepath.Dir(e.wsDir), "MEMORY.md"), // <workspace>/MEMORY.md (OpenClaw standard)
			filepath.Join(e.wsDir, "memory.md"),                // <workspace>/.ngoclaw/memory.md (legacy)
		}
		for _, mp := range memoryPaths {
			if data, err := os.ReadFile(mp); err == nil && len(data) > 0 {
				parts = append(parts, fmt.Sprintf("<MEMORY[workspace]>\n%s\n</MEMORY[workspace]>", strings.TrimSpace(string(data))))
				break // first found wins
			}
		}
	}

	if len(parts) == 0 {
		return ""
	}

	return "## Long-term Memory\n\n" + strings.Join(parts, "\n\n")
}
