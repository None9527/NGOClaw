package context

import (
	"strings"
	"unicode/utf8"
)

// PruningStrategy 修剪策略
type PruningStrategy int

const (
	PruneNone       PruningStrategy = iota // 不修剪
	PruneAdaptive                          // 自适应修剪
	PruneHardClear                         // 硬性清除
	PruneSummarize                         // 摘要压缩 (需要模型支持)
)

// String 返回策略的字符串表示
func (s PruningStrategy) String() string {
	switch s {
	case PruneNone:
		return "none"
	case PruneAdaptive:
		return "adaptive"
	case PruneHardClear:
		return "hard_clear"
	case PruneSummarize:
		return "summarize"
	default:
		return "unknown"
	}
}

// Message 用于上下文管理的消息结构
type Message struct {
	Role       string
	Content    string
	ToolCallID string
	Importance float64 // 重要性评分 (0-1)
	Tokens     int     // 预估 token 数
}

// PruneConfig 修剪配置
type PruneConfig struct {
	Strategy        PruningStrategy
	MaxTokens       int     // 最大 token 数
	SoftTrimRatio   float64 // 软修剪阈值 (如 0.7 表示 70% 时开始)
	HardClearRatio  float64 // 硬清除阈值 (如 0.85 表示 85% 时强制)
	PreserveSystem  bool    // 是否保留系统提示
	PreserveRecent  int     // 始终保留最近的 N 条消息
	ImportanceThreshold float64 // 重要性阈值
}

// DefaultPruneConfig 返回默认配置
func DefaultPruneConfig() *PruneConfig {
	return &PruneConfig{
		Strategy:        PruneAdaptive,
		MaxTokens:       100000,
		SoftTrimRatio:   0.7,
		HardClearRatio:  0.85,
		PreserveSystem:  true,
		PreserveRecent:  4,
		ImportanceThreshold: 0.3,
	}
}

// Pruner 上下文修剪器
type Pruner struct {
	config    *PruneConfig
	tokenizer Tokenizer
}

// Tokenizer token 计数接口
type Tokenizer interface {
	Count(text string) int
}

// SimpleTokenizer 简单 token 计数器 (基于字符估算)
type SimpleTokenizer struct {
	charsPerToken float64
}

// NewSimpleTokenizer 创建简单计数器
func NewSimpleTokenizer() *SimpleTokenizer {
	return &SimpleTokenizer{
		charsPerToken: 4.0, // 英文平均 4 字符一个 token，中文约 2 字符
	}
}

// Count 估算 token 数
func (t *SimpleTokenizer) Count(text string) int {
	// 统计中文字符
	chineseCount := 0
	for _, r := range text {
		if r >= 0x4E00 && r <= 0x9FFF {
			chineseCount++
		}
	}
	
	totalChars := utf8.RuneCountInString(text)
	englishChars := totalChars - chineseCount
	
	// 中文约 2 字符一个 token，英文约 4 字符一个 token
	tokens := float64(chineseCount)/2.0 + float64(englishChars)/t.charsPerToken
	
	return int(tokens) + 1
}

// NewPruner 创建修剪器
func NewPruner(config *PruneConfig, tokenizer Tokenizer) *Pruner {
	if tokenizer == nil {
		tokenizer = NewSimpleTokenizer()
	}
	return &Pruner{
		config:    config,
		tokenizer: tokenizer,
	}
}

// Prune 执行上下文修剪
func (p *Pruner) Prune(messages []Message) []Message {
	if p.config.Strategy == PruneNone {
		return messages
	}

	// 计算当前总 token 数
	totalTokens := p.calculateTotalTokens(messages)
	
	// 检查是否需要修剪
	softThreshold := int(float64(p.config.MaxTokens) * p.config.SoftTrimRatio)
	hardThreshold := int(float64(p.config.MaxTokens) * p.config.HardClearRatio)

	if totalTokens < softThreshold {
		return messages
	}

	switch p.config.Strategy {
	case PruneAdaptive:
		return p.adaptivePrune(messages, totalTokens, softThreshold, hardThreshold)
	case PruneHardClear:
		return p.hardClearPrune(messages, hardThreshold)
	case PruneSummarize:
		// 摘要需要调用模型，暂时回退到 adaptive
		return p.adaptivePrune(messages, totalTokens, softThreshold, hardThreshold)
	default:
		return messages
	}
}

// calculateTotalTokens 计算总 token 数
func (p *Pruner) calculateTotalTokens(messages []Message) int {
	total := 0
	for i := range messages {
		if messages[i].Tokens == 0 {
			messages[i].Tokens = p.tokenizer.Count(messages[i].Content)
		}
		total += messages[i].Tokens
	}
	return total
}

// adaptivePrune 自适应修剪
func (p *Pruner) adaptivePrune(messages []Message, totalTokens, softThreshold, hardThreshold int) []Message {
	if len(messages) == 0 {
		return messages
	}

	result := make([]Message, 0, len(messages))
	
	// 1. 始终保留系统消息
	systemMessages := make([]Message, 0)
	if p.config.PreserveSystem {
		for _, msg := range messages {
			if msg.Role == "system" {
				systemMessages = append(systemMessages, msg)
			}
		}
	}
	
	// 2. 始终保留最近的消息
	recentStart := len(messages) - p.config.PreserveRecent
	if recentStart < 0 {
		recentStart = 0
	}
	recentMessages := messages[recentStart:]
	
	// 3. 中间消息按重要性筛选
	middleMessages := make([]Message, 0)
	for i, msg := range messages {
		if msg.Role == "system" {
			continue
		}
		if i >= recentStart {
			continue
		}
		
		// 根据重要性和内容筛选
		importance := p.evaluateImportance(msg)
		if importance >= p.config.ImportanceThreshold {
			middleMessages = append(middleMessages, msg)
		}
	}
	
	// 组合结果
	result = append(result, systemMessages...)
	result = append(result, middleMessages...)
	result = append(result, recentMessages...)
	
	// 如果仍然超过硬阈值，进一步裁剪中间消息
	currentTokens := p.calculateTotalTokens(result)
	if currentTokens > hardThreshold && len(middleMessages) > 0 {
		// 移除一半的中间消息
		halfMiddle := len(middleMessages) / 2
		result = make([]Message, 0)
		result = append(result, systemMessages...)
		result = append(result, middleMessages[halfMiddle:]...)
		result = append(result, recentMessages...)
	}
	
	return result
}

// hardClearPrune 硬性清除修剪
func (p *Pruner) hardClearPrune(messages []Message, hardThreshold int) []Message {
	if len(messages) == 0 {
		return messages
	}

	result := make([]Message, 0)
	currentTokens := 0
	
	// 保留系统消息
	if p.config.PreserveSystem {
		for _, msg := range messages {
			if msg.Role == "system" {
				result = append(result, msg)
				currentTokens += msg.Tokens
			}
		}
	}
	
	// 从后往前添加消息，直到达到阈值
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Role == "system" {
			continue
		}
		
		if currentTokens + msg.Tokens > hardThreshold {
			break
		}
		
		// 插入到系统消息之后
		insertIdx := len(result)
		for j, m := range result {
			if m.Role != "system" {
				insertIdx = j
				break
			}
		}
		
		// 插入消息
		result = append(result[:insertIdx], append([]Message{msg}, result[insertIdx:]...)...)
		currentTokens += msg.Tokens
	}
	
	return result
}

// evaluateImportance 评估消息重要性
func (p *Pruner) evaluateImportance(msg Message) float64 {
	// 如果已有评分，直接返回
	if msg.Importance > 0 {
		return msg.Importance
	}
	
	importance := 0.5 // 基础分
	
	// 工具相关消息更重要
	if msg.Role == "tool" || msg.ToolCallID != "" {
		importance += 0.2
	}
	
	// 包含代码的消息更重要
	if strings.Contains(msg.Content, "```") {
		importance += 0.15
	}
	
	// 包含错误信息的更重要
	lowerContent := strings.ToLower(msg.Content)
	if strings.Contains(lowerContent, "error") || 
	   strings.Contains(lowerContent, "failed") ||
	   strings.Contains(lowerContent, "exception") {
		importance += 0.1
	}
	
	// 较长的消息通常包含更多信息
	if len(msg.Content) > 500 {
		importance += 0.05
	}
	
	// 限制在 0-1 范围
	if importance > 1.0 {
		importance = 1.0
	}
	
	return importance
}

// EstimateTokens 估算消息列表的 token 数
func (p *Pruner) EstimateTokens(messages []Message) int {
	return p.calculateTotalTokens(messages)
}

// NeedsPruning 检查是否需要修剪
func (p *Pruner) NeedsPruning(messages []Message) bool {
	totalTokens := p.calculateTotalTokens(messages)
	softThreshold := int(float64(p.config.MaxTokens) * p.config.SoftTrimRatio)
	return totalTokens >= softThreshold
}
