package telegram

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Skill 技能定义
type Skill struct {
	ID          string
	Name        string
	Description string
	Path        string   // 技能目录路径
	Commands    []string // 提供的命令列表
	Enabled     bool
	InstalledAt time.Time
}

// SkillManager 技能管理器
type SkillManager struct {
	skills    map[string]*Skill
	skillDir  string // 技能安装目录
	mu        sync.RWMutex
}

// NewSkillManager 创建技能管理器
func NewSkillManager(skillDir string) *SkillManager {
	m := &SkillManager{
		skills:   make(map[string]*Skill),
		skillDir: skillDir,
	}

	// 扫描已安装技能
	m.scanInstalledSkills()

	return m
}

// scanInstalledSkills 扫描已安装技能
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

// loadSkillFromPath 从路径加载技能
func (m *SkillManager) loadSkillFromPath(path string) *Skill {
	// 检查 SKILL.md 是否存在
	skillFile := filepath.Join(path, "SKILL.md")
	if _, err := os.Stat(skillFile); os.IsNotExist(err) {
		return nil
	}

	// 读取技能信息
	content, err := os.ReadFile(skillFile)
	if err != nil {
		return nil
	}

	// 解析技能元数据 (简化实现)
	name := filepath.Base(path)
	description := ""

	// 尝试从内容提取描述
	lines := splitLines(string(content))
	if len(lines) > 0 {
		// 第一行通常是标题
		if len(lines[0]) > 2 && lines[0][0] == '#' {
			name = trimSpace(lines[0][1:])
		}
	}
	if len(lines) > 2 {
		description = trimSpace(lines[2])
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

// Install 安装技能
func (m *SkillManager) Install(source, name string) (*Skill, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 确定目标路径
	targetPath := filepath.Join(m.skillDir, name)

	// 检查是否已存在
	if _, exists := m.skills[name]; exists {
		return nil, fmt.Errorf("技能已存在: %s", name)
	}

	// Install from local path (symlink).
	// Future: support git clone (source starts with "http"/"git@") or npm pack.
	if _, err := os.Stat(source); err != nil {
		return nil, fmt.Errorf("源路径不存在: %s", source)
	}

	// 复制/链接技能目录
	// 简化：创建符号链接
	if err := os.Symlink(source, targetPath); err != nil {
		return nil, fmt.Errorf("安装失败: %w", err)
	}

	skill := m.loadSkillFromPath(targetPath)
	if skill == nil {
		os.Remove(targetPath)
		return nil, fmt.Errorf("无效的技能目录 (缺少 SKILL.md)")
	}

	m.skills[skill.ID] = skill
	return skill, nil
}

// Uninstall 卸载技能
func (m *SkillManager) Uninstall(skillID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	skill, exists := m.skills[skillID]
	if !exists {
		return fmt.Errorf("技能不存在: %s", skillID)
	}

	// 删除目录
	if err := os.RemoveAll(skill.Path); err != nil {
		return fmt.Errorf("卸载失败: %w", err)
	}

	delete(m.skills, skillID)
	return nil
}

// Get 获取技能
func (m *SkillManager) Get(skillID string) *Skill {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.skills[skillID]
}

// List 列出所有技能
func (m *SkillManager) List() []*Skill {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*Skill, 0, len(m.skills))
	for _, skill := range m.skills {
		result = append(result, skill)
	}
	return result
}

// Enable 启用技能
func (m *SkillManager) Enable(skillID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	skill, exists := m.skills[skillID]
	if !exists {
		return fmt.Errorf("技能不存在: %s", skillID)
	}

	skill.Enabled = true
	return nil
}

// Disable 禁用技能
func (m *SkillManager) Disable(skillID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	skill, exists := m.skills[skillID]
	if !exists {
		return fmt.Errorf("技能不存在: %s", skillID)
	}

	skill.Enabled = false
	return nil
}

// GetEnabledSkills 获取启用的技能列表
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

// ToolPromoter 工具注册接口 (依赖倒转, 避免循环引用)
type ToolPromoter interface {
	RegisterCommand(name, description, command string, aliases map[string][]string) error
}

// PromoteToTool 将 Skill 升级为持久化 Tool
// 条件: Skill 目录下存在 scripts/*.py 或 scripts/*.sh 可执行脚本
// 行为: 注册为 command 后端 Tool, 自动生成别名
func (m *SkillManager) PromoteToTool(skillID string, promoter ToolPromoter) error {
	m.mu.RLock()
	skill, exists := m.skills[skillID]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("技能不存在: %s", skillID)
	}

	// 查找 scripts/ 目录下的可执行脚本
	scriptsDir := filepath.Join(skill.Path, "scripts")
	entries, err := os.ReadDir(scriptsDir)
	if err != nil {
		return fmt.Errorf("技能无 scripts/ 目录, 无法升级: %w", err)
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

		// 工具名: skill_id + 脚本名 (去扩展名)
		toolName := skillID + "_" + name[:len(name)-len(ext)]
		scriptPath := filepath.Join(scriptsDir, name)

		// 生成执行命令
		var command string
		switch ext {
		case ".py":
			command = "python3 " + scriptPath
		case ".sh":
			command = "bash " + scriptPath
		}

		// 自动生成 per-model 别名 (基础: 原名 + PascalCase)
		aliases := map[string][]string{
			"claude": {toolName, toPascalCase(toolName)},
			"gemini": {toolName},
			"openai": {toolName},
		}

		description := fmt.Sprintf("Promoted from skill '%s': %s", skill.Name, skill.Description)
		if err := promoter.RegisterCommand(toolName, description, command, aliases); err != nil {
			return fmt.Errorf("注册工具失败 %s: %w", toolName, err)
		}
		promoted++
	}

	if promoted == 0 {
		return fmt.Errorf("技能 %s 的 scripts/ 下无可升级的脚本 (.py 或 .sh)", skillID)
	}

	return nil
}

// toPascalCase 将 snake_case 转 PascalCase
func toPascalCase(s string) string {
	parts := splitByUnderscore(s)
	var result string
	for _, p := range parts {
		if len(p) > 0 {
			result += string(p[0]-32) + p[1:] // ASCII uppercase
		}
	}
	return result
}

func splitByUnderscore(s string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '_' {
			if i > start {
				parts = append(parts, s[start:i])
			}
			start = i + 1
		}
	}
	if start < len(s) {
		parts = append(parts, s[start:])
	}
	return parts
}

// 辅助函数
func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func trimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}
