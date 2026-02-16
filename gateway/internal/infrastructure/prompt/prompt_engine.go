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
// Two-layer architecture (like Claude Code / Cursor):
//
//	System layer:    ~/.ngoclaw/          — global defaults
//	Workspace layer: <project>/.ngoclaw/  — project-specific overrides
//
// Within each layer:
//   - SOUL:       soul.md — always loaded, defines agent persona
//   - Components: prompts/*.md — loaded by requires conditions
//   - Variants:   prompts/variants/*.md — loaded by model name
//
// Merge rule: workspace overrides system (same-name component replaces).
type PromptEngine struct {
	soul       string                      // soul.md content (always prepended)
	components []*PromptComponent          // all discovered components (merged)
	variants   map[string]*PromptComponent // model prefix → variant
	systemDir  string                      // ~/.ngoclaw (system-level)
	wsDir      string                      // <workspace>/.ngoclaw (workspace-level, may be empty)
	logger     *zap.Logger
	mu         sync.RWMutex

	// Assembly cache: avoids re-assembling identical prompts within the same session.
	// Key: "model|intent|userRules_hash" → assembled prompt string.
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
		components: make([]*PromptComponent, 0),
		variants:   make(map[string]*PromptComponent),
		cache:      make(map[string]string),
		systemDir:  filepath.Join(homeDir, ".ngoclaw"),
		wsDir:      wsDir,
		logger:     logger,
	}
}

// Discover scans System and Workspace layers for prompt files and merges them.
// Workspace items override System items with the same name.
// Called at startup and can be called again for hot-reload.
func (e *PromptEngine) Discover() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Reset
	e.soul = ""
	e.components = e.components[:0]
	e.variants = make(map[string]*PromptComponent)
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

	// 2. Load components from both layers — workspace overrides system by name
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

	layers := 1
	if e.wsDir != "" {
		if _, err := os.Stat(e.wsDir); err == nil {
			layers = 2
		}
	}

	e.logger.Info("Prompt engine initialized",
		zap.Bool("has_soul", e.soul != ""),
		zap.Int("components", len(e.components)),
		zap.Int("variants", len(e.variants)),
		zap.Int("layers", layers),
	)

	return nil
}

// Assemble builds the final system prompt from discovered components,
// filtered by the runtime context. This is the core intelligence.
//
// Assembly order:
//  1. SOUL (always first — highest attention)
//  2. Matched variant (model-specific rules)
//  3. Filtered components sorted by priority
//  4. User rules (from config)
//  5. Token budget truncation if needed
func (e *PromptEngine) Assemble(ctx PromptContext) string {
	// Auto-detect intent from user message
	if ctx.DetectedIntent == IntentGeneral && ctx.UserMessage != "" {
		ctx.DetectedIntent = AnalyzeIntent(ctx.UserMessage)
	}

	// Cache key: model + intent + focus chain length + user rules length
	// This covers the primary factors that influence prompt assembly.
	cacheKey := fmt.Sprintf("%s|%d|%d|%d", ctx.ModelName, ctx.DetectedIntent, len(ctx.FocusFiles), len(ctx.UserRules))

	e.mu.RLock()
	if cached, ok := e.cache[cacheKey]; ok {
		e.mu.RUnlock()
		return cached
	}
	e.mu.RUnlock()

	e.mu.Lock()
	defer e.mu.Unlock()

	// Double-check after acquiring write lock
	if cached, ok := e.cache[cacheKey]; ok {
		return cached
	}

	var sections []string

	// 1. SOUL — always first
	if e.soul != "" {
		sections = append(sections, e.soul)
	}

	// 2. Model variant
	variant := e.matchVariant(ctx.ModelName)
	if variant != nil {
		sections = append(sections, variant.Content)
	}

	// 3. Filter and sort components
	eligible := e.filterComponents(ctx)
	sort.Slice(eligible, func(i, j int) bool {
		return eligible[i].Priority < eligible[j].Priority
	})

	for _, comp := range eligible {
		sections = append(sections, comp.Content)
	}

	// 3.4 Long-term Memory — inject from ~/.ngoclaw/memory.md + <workspace>/.ngoclaw/memory.md
	if memContent := e.loadMemoryFiles(ctx); memContent != "" {
		sections = append(sections, memContent)
	}

	// 3.5 Focus Chain — inject current working context
	if focusSection := ctx.BuildFocusSection(); focusSection != "" {
		sections = append(sections, focusSection)
	}

	// 4. User rules (from config)
	if ctx.UserRules != "" {
		sections = append(sections, "## User Custom Rules\n"+ctx.UserRules)
	}

	// 5. Assemble with separators
	result := strings.Join(sections, "\n\n---\n\n")

	// 6. Token budget truncation (rough: 1 token ≈ 3 chars for CJK, 4 for EN)
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

	// Store in cache
	e.cache[cacheKey] = result

	return result
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

// loadMemoryFiles reads global memory.json + workspace memory.md and returns assembled section.
// Global: ~/.ngoclaw/memory.json (structured, with confidence/category)
// Workspace: <workspace>/.ngoclaw/memory.md (plain markdown, unchanged)
func (e *PromptEngine) loadMemoryFiles(ctx PromptContext) string {
	var parts []string

	// Global memory — structured JSON (top 20 facts by confidence)
	store, err := toolpkg.LoadMemoryStore()
	if err == nil && store != nil {
		var memBuf strings.Builder
		// Context summaries
		if store.Context.WorkContext != "" {
			memBuf.WriteString(fmt.Sprintf("Work context: %s\n", store.Context.WorkContext))
		}
		if store.Context.PersonalContext != "" {
			memBuf.WriteString(fmt.Sprintf("Personal context: %s\n", store.Context.PersonalContext))
		}
		// Top facts
		facts := toolpkg.GetTopFacts(store, 20)
		if len(facts) > 0 {
			if memBuf.Len() > 0 {
				memBuf.WriteString("\n")
			}
			for _, f := range facts {
				memBuf.WriteString(fmt.Sprintf("- [%s] %s\n", f.Category, f.Content))
			}
		}
		if memBuf.Len() > 0 {
			parts = append(parts, fmt.Sprintf("<MEMORY[global]>\n%s</MEMORY[global]>", memBuf.String()))
		}
	}

	// Daily logs — today + yesterday (OpenClaw-style memory/YYYY-MM-DD.md)
	if dailyContent := toolpkg.ReadDailyLogs(); dailyContent != "" {
		parts = append(parts, fmt.Sprintf("<MEMORY[daily_log]>\n%s\n</MEMORY[daily_log]>", dailyContent))
	}

	// Workspace memory (plain .md — unchanged)
	if e.wsDir != "" {
		wsPath := filepath.Join(e.wsDir, ".ngoclaw", "memory.md")
		if data, err := os.ReadFile(wsPath); err == nil && len(data) > 0 {
			parts = append(parts, fmt.Sprintf("<MEMORY[workspace]>\n%s\n</MEMORY[workspace]>", strings.TrimSpace(string(data))))
		}
	}

	if len(parts) == 0 {
		return ""
	}

	return "## Long-term Memory\n\n" + strings.Join(parts, "\n\n")
}
