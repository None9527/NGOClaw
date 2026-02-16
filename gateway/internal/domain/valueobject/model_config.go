package valueobject

// ModelConfig 模型配置值对象（不可变）
type ModelConfig struct {
	provider    string
	model       string
	maxTokens   int
	temperature float64
	topP        float64
	stream      bool // 是否启用流式响应
}

// NewModelConfig 创建模型配置
func NewModelConfig(provider, model string, maxTokens int, temperature, topP float64, stream bool) ModelConfig {
	return ModelConfig{
		provider:    provider,
		model:       model,
		maxTokens:   maxTokens,
		temperature: temperature,
		topP:        topP,
		stream:      stream,
	}
}

// DefaultModelConfig 默认模型配置
func DefaultModelConfig() ModelConfig {
	return ModelConfig{
		provider:    "bailian",
		model:       "qwen3-max-2026-01-23",
		maxTokens:   8192,
		temperature: 0.7,
		topP:        0.95,
		stream:      true, // 默认启用流式
	}
}

// Provider 返回提供商
func (mc ModelConfig) Provider() string {
	return mc.provider
}

// Model 返回模型名称
func (mc ModelConfig) Model() string {
	return mc.model
}

// MaxTokens 返回最大令牌数
func (mc ModelConfig) MaxTokens() int {
	return mc.maxTokens
}

// Temperature 返回温度参数
func (mc ModelConfig) Temperature() float64 {
	return mc.temperature
}

// TopP 返回 Top-P 参数
func (mc ModelConfig) TopP() float64 {
	return mc.topP
}

// FullModelName 返回完整模型名称
func (mc ModelConfig) FullModelName() string {
	return mc.provider + "/" + mc.model
}

// Stream 返回是否启用流式响应
func (mc ModelConfig) Stream() bool {
	return mc.stream
}

// WithTemperature 创建新的配置（修改温度）
func (mc ModelConfig) WithTemperature(temp float64) ModelConfig {
	return ModelConfig{
		provider:    mc.provider,
		model:       mc.model,
		maxTokens:   mc.maxTokens,
		temperature: temp,
		topP:        mc.topP,
		stream:      mc.stream,
	}
}

// WithMaxTokens 创建新的配置（修改最大令牌数）
func (mc ModelConfig) WithMaxTokens(tokens int) ModelConfig {
	return ModelConfig{
		provider:    mc.provider,
		model:       mc.model,
		maxTokens:   tokens,
		temperature: mc.temperature,
		topP:        mc.topP,
		stream:      mc.stream,
	}
}

// Equals 值对象相等性比较
func (mc ModelConfig) Equals(other ModelConfig) bool {
	return mc.provider == other.provider &&
		mc.model == other.model &&
		mc.maxTokens == other.maxTokens &&
		mc.temperature == other.temperature &&
		mc.topP == other.topP &&
		mc.stream == other.stream
}
