// Copyright 2026 NGOClaw Authors. All rights reserved.
package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	domaintool "github.com/ngoclaw/ngoclaw/gateway/internal/domain/tool"
	"go.uber.org/zap"
)

// MemoryStore is the top-level JSON structure for structured memory.
// Source: Deer-Flow memory.json pattern — facts with confidence/category/source.
type MemoryStore struct {
	Context struct {
		WorkContext     string `json:"workContext"`
		PersonalContext string `json:"personalContext"`
	} `json:"context"`
	Facts []MemoryFact `json:"facts"`
}

// MemoryFact represents a single remembered fact with metadata.
type MemoryFact struct {
	ID         string  `json:"id"`
	Content    string  `json:"content"`
	Category   string  `json:"category"`   // preference|knowledge|context|behavior|goal
	Confidence float64 `json:"confidence"` // 0.0-1.0
	Source     string  `json:"source,omitempty"` // "user"|"compaction"|"agent"
	CreatedAt  string  `json:"createdAt"`
}

// ValidCategories defines the allowed fact categories.
var ValidCategories = map[string]bool{
	"preference": true,
	"knowledge":  true,
	"context":    true,
	"behavior":   true,
	"goal":       true,
}

// SaveMemoryTool allows the agent to persist important facts to ~/.ngoclaw/memory.json
// Upgraded from Markdown to structured JSON with category, confidence, and deduplication.
type SaveMemoryTool struct {
	mu     sync.Mutex
	logger *zap.Logger
}

const (
	memoryDirName  = ".ngoclaw"
	memoryFileJSON = "memory.json"
	memoryFileMD   = "memory.md" // legacy — auto-migrated on first load
)

// NewSaveMemoryTool creates the save_memory tool
func NewSaveMemoryTool(logger *zap.Logger) *SaveMemoryTool {
	return &SaveMemoryTool{logger: logger}
}

func (t *SaveMemoryTool) Name() string         { return "save_memory" }
func (t *SaveMemoryTool) Kind() domaintool.Kind { return domaintool.KindThink }
func (t *SaveMemoryTool) Description() string {
	return "Save an important fact to long-term memory. Use this when you discover user preferences, " +
		"environment details, project decisions, or corrections that should be remembered across sessions. " +
		"Facts are stored as structured JSON with category and confidence."
}

func (t *SaveMemoryTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"fact": map[string]interface{}{
				"type":        "string",
				"description": "The fact to remember. Should be a concise, self-contained statement.",
			},
			"category": map[string]interface{}{
				"type":        "string",
				"description": "Category of the fact: preference, knowledge, context, behavior, goal. Default: knowledge.",
				"enum":        []string{"preference", "knowledge", "context", "behavior", "goal"},
			},
			"confidence": map[string]interface{}{
				"type":        "number",
				"description": "Confidence score 0.0-1.0 (how certain is this fact). Default: 0.8.",
			},
		},
		"required": []string{"fact"},
	}
}

func (t *SaveMemoryTool) Execute(ctx context.Context, args map[string]interface{}) (*Result, error) {
	fact, ok := args["fact"].(string)
	if !ok || strings.TrimSpace(fact) == "" {
		return &Result{Output: "Error: 'fact' parameter is required", Success: false}, nil
	}

	sanitized := strings.Join(strings.Fields(fact), " ")
	sanitized = strings.TrimLeft(sanitized, "- ")

	category := "knowledge"
	if cat, ok := args["category"].(string); ok && ValidCategories[cat] {
		category = cat
	}

	confidence := 0.8
	if conf, ok := args["confidence"].(float64); ok && conf >= 0.0 && conf <= 1.0 {
		confidence = conf
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	store, err := LoadMemoryStore()
	if err != nil {
		return &Result{Output: fmt.Sprintf("Failed to load memory: %v", err), Success: false}, nil
	}

	// Deduplication: LCS similarity > 80% within same category → update instead of append
	for i, existing := range store.Facts {
		if existing.Category == category && lcsSimilarity(existing.Content, sanitized) > 0.8 {
			store.Facts[i].Content = sanitized
			store.Facts[i].Confidence = confidence
			store.Facts[i].CreatedAt = time.Now().Format(time.RFC3339)
			if err := SaveMemoryStore(store); err != nil {
				return &Result{Output: fmt.Sprintf("Failed to save memory: %v", err), Success: false}, nil
			}
			t.logger.Info("Memory updated (deduplicated)",
				zap.String("fact", sanitized),
				zap.String("category", category),
			)
			return &Result{
				Output:  fmt.Sprintf("Updated existing memory: \"%s\"", sanitized),
				Display: fmt.Sprintf("💾 Updated: [%s] %s", category, sanitized),
				Success: true,
			}, nil
		}
	}

	// New fact — append
	newFact := MemoryFact{
		ID:         uuid.New().String()[:8],
		Content:    sanitized,
		Category:   category,
		Confidence: confidence,
		Source:     "agent",
		CreatedAt:  time.Now().Format(time.RFC3339),
	}
	store.Facts = append(store.Facts, newFact)

	if err := SaveMemoryStore(store); err != nil {
		return &Result{Output: fmt.Sprintf("Failed to save memory: %v", err), Success: false}, nil
	}

	t.logger.Info("Memory saved", zap.String("fact", sanitized), zap.String("category", category))
	return &Result{
		Output:  fmt.Sprintf("Remembered: \"%s\" [%s, %.1f]", sanitized, category, confidence),
		Display: fmt.Sprintf("💾 Saved: [%s] %s (%.0f%%)", category, sanitized, confidence*100),
		Success: true,
	}, nil
}

// --- Memory Store I/O ---

// getMemoryJSONPath returns ~/.ngoclaw/memory.json
func getMemoryJSONPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/tmp"
	}
	return filepath.Join(home, memoryDirName, memoryFileJSON)
}

// getGlobalMemoryPath returns ~/.ngoclaw/memory.md (legacy)
func getGlobalMemoryPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/tmp"
	}
	return filepath.Join(home, memoryDirName, memoryFileMD)
}

// LoadMemoryStore loads memory.json, auto-migrating from memory.md if needed.
func LoadMemoryStore() (*MemoryStore, error) {
	jsonPath := getMemoryJSONPath()

	data, err := os.ReadFile(jsonPath)
	if err == nil && len(data) > 0 {
		var store MemoryStore
		if err := json.Unmarshal(data, &store); err != nil {
			return nil, fmt.Errorf("corrupt memory.json: %w", err)
		}
		return &store, nil
	}

	// Try legacy migration from memory.md
	store := &MemoryStore{}
	mdPath := getGlobalMemoryPath()
	mdData, mdErr := os.ReadFile(mdPath)
	if mdErr == nil && len(mdData) > 0 {
		store.Facts = migrateMarkdownToFacts(string(mdData))
	}

	return store, nil
}

// SaveMemoryStore writes the store to memory.json.
func SaveMemoryStore(store *MemoryStore) error {
	jsonPath := getMemoryJSONPath()
	if err := os.MkdirAll(filepath.Dir(jsonPath), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(jsonPath, data, 0644)
}

// GetTopFacts returns facts sorted by confidence, limited to n.
func GetTopFacts(store *MemoryStore, n int) []MemoryFact {
	if len(store.Facts) == 0 {
		return nil
	}
	sorted := make([]MemoryFact, len(store.Facts))
	copy(sorted, store.Facts)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Confidence > sorted[j].Confidence
	})
	if n > 0 && len(sorted) > n {
		sorted = sorted[:n]
	}
	return sorted
}

// ReadMemoryFile reads the global memory file content (backward compat for prompt engine).
func ReadMemoryFile() (string, error) {
	store, err := LoadMemoryStore()
	if err != nil {
		return "", err
	}
	if len(store.Facts) == 0 {
		return "", nil
	}
	// Format as readable text for prompt injection
	var sb strings.Builder
	facts := GetTopFacts(store, 20)
	for _, f := range facts {
		sb.WriteString(fmt.Sprintf("- [%s] %s\n", f.Category, f.Content))
	}
	return sb.String(), nil
}

// ReadWorkspaceMemoryFile reads workspace-level memory file
func ReadWorkspaceMemoryFile(workspaceDir string) (string, error) {
	if workspaceDir == "" {
		return "", nil
	}
	// Workspace memory stays as .md for simplicity
	path := filepath.Join(workspaceDir, memoryDirName, memoryFileMD)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

// --- Daily Log I/O (OpenClaw-style memory/YYYY-MM-DD.md) ---

const dailyLogDir = "memory"

// getDailyLogDir returns ~/.ngoclaw/memory/
func getDailyLogDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/tmp"
	}
	return filepath.Join(home, memoryDirName, dailyLogDir)
}

// AppendDailyLog writes a timestamped entry to ~/.ngoclaw/memory/YYYY-MM-DD.md
func AppendDailyLog(entry string) error {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return nil
	}

	dir := getDailyLogDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create daily log dir: %w", err)
	}

	filename := time.Now().Format("2006-01-02") + ".md"
	path := filepath.Join(dir, filename)

	timestamp := time.Now().Format("15:04")
	line := fmt.Sprintf("- [%s] %s\n", timestamp, entry)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open daily log: %w", err)
	}
	defer f.Close()

	_, err = f.WriteString(line)
	return err
}

// ReadDailyLogs reads today + yesterday daily logs and returns combined content.
// Returns empty string if no logs exist.
func ReadDailyLogs() string {
	dir := getDailyLogDir()
	now := time.Now()

	var parts []string
	for _, offset := range []int{-1, 0} { // yesterday first, today second
		day := now.AddDate(0, 0, offset)
		filename := day.Format("2006-01-02") + ".md"
		path := filepath.Join(dir, filename)

		data, err := os.ReadFile(path)
		if err != nil || len(data) == 0 {
			continue
		}

		content := strings.TrimSpace(string(data))
		// Truncate if too large (keep last 2000 chars for prompt budget)
		if len(content) > 2000 {
			content = "...\n" + content[len(content)-2000:]
		}

		label := day.Format("2006-01-02")
		if offset == 0 {
			label += " (today)"
		} else {
			label += " (yesterday)"
		}
		parts = append(parts, fmt.Sprintf("### %s\n%s", label, content))
	}

	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n\n")
}

// --- Migration & Deduplication helpers ---

// migrateMarkdownToFacts parses legacy memory.md bullet points into MemoryFacts.
func migrateMarkdownToFacts(content string) []MemoryFact {
	var facts []MemoryFact
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		text := strings.TrimPrefix(line, "- ")
		if text == "" {
			continue
		}
		facts = append(facts, MemoryFact{
			ID:         uuid.New().String()[:8],
			Content:    text,
			Category:   "knowledge",
			Confidence: 0.7,
			Source:     "compaction", // migrated from legacy
			CreatedAt:  time.Now().Format(time.RFC3339),
		})
	}
	return facts
}

// lcsSimilarity returns the ratio of longest common substring length to the
// average of the two string lengths. Range: 0.0 to 1.0.
func lcsSimilarity(a, b string) float64 {
	if a == "" || b == "" {
		return 0.0
	}
	a = strings.ToLower(a)
	b = strings.ToLower(b)
	lcsLen := longestCommonSubstringLen(a, b)
	avg := float64(len(a)+len(b)) / 2.0
	return float64(lcsLen) / avg
}

// longestCommonSubstringLen finds the length of the longest common substring.
func longestCommonSubstringLen(a, b string) int {
	m, n := len(a), len(b)
	if m == 0 || n == 0 {
		return 0
	}
	// Rolling row DP to save memory
	prev := make([]int, n+1)
	curr := make([]int, n+1)
	maxLen := 0
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if a[i-1] == b[j-1] {
				curr[j] = prev[j-1] + 1
				if curr[j] > maxLen {
					maxLen = curr[j]
				}
			} else {
				curr[j] = 0
			}
		}
		prev, curr = curr, prev
		// Reset curr for next iteration
		for j := range curr {
			curr[j] = 0
		}
	}
	return maxLen
}
