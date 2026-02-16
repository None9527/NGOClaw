package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// Kind 工具操作类型 — 驱动权限策略自动决策
type Kind string

const (
	KindRead        Kind = "read"        // 只读操作 (read_file, list_dir...)
	KindEdit        Kind = "edit"        // 修改文件 (write_file, patch...)
	KindExecute     Kind = "execute"     // 执行命令 (shell, run...)
	KindDelete      Kind = "delete"      // 删除操作
	KindSearch      Kind = "search"      // 搜索操作 (web_search, grep...)
	KindFetch       Kind = "fetch"       // 网络获取 (fetch_url...)
	KindThink       Kind = "think"       // 纯思考 (save_memory, plan...)
	KindCommunicate Kind = "communicate" // 交互 (ask_user, notify...)
)

// MutatorKinds 需要用户确认的操作类型 (AskMode 下自动拦截)
var MutatorKinds = map[Kind]bool{
	KindEdit:    true,
	KindDelete:  true,
	KindExecute: true,
}

// SafeKinds 自动放行的安全操作类型
var SafeKinds = map[Kind]bool{
	KindRead:   true,
	KindSearch: true,
	KindThink:  true,
}

// Tool 工具接口 - 所有可执行工具的抽象
type Tool interface {
	// Name 返回工具名称
	Name() string
	// Description 返回工具描述
	Description() string
	// Kind 返回工具操作类型 (驱动权限策略自动决策)
	Kind() Kind
	// Schema 返回参数的 JSON Schema
	Schema() map[string]interface{}
	// Execute 执行工具
	Execute(ctx context.Context, args map[string]interface{}) (*Result, error)
}

// Result 工具执行结果
type Result struct {
	Output   string                 // 给 LLM 的精简结果
	Display  string                 // 给 UI 的富文本渲染 (为空时 fallback 到 Output)
	Success  bool                   // 是否成功
	Metadata map[string]interface{} // 元数据
	Error    string                 // 错误信息
}

// DisplayOrOutput 返回 Display (优先) 或回退到 Output
func (r *Result) DisplayOrOutput() string {
	if r.Display != "" {
		return r.Display
	}
	return r.Output
}

// Definition 工具定义，用于传递给模型
type Definition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// Registry 工具注册表接口
type Registry interface {
	// Register 注册工具
	Register(tool Tool) error
	// Unregister 注销工具
	Unregister(name string) error
	// Get 获取工具
	Get(name string) (Tool, bool)
	// List 列出所有工具
	List() []Definition
	// Has 检查工具是否存在
	Has(name string) bool
}

// InMemoryRegistry 内存工具注册表
type InMemoryRegistry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewInMemoryRegistry 创建内存注册表
func NewInMemoryRegistry() *InMemoryRegistry {
	return &InMemoryRegistry{
		tools: make(map[string]Tool),
	}
}

// Register 注册工具
func (r *InMemoryRegistry) Register(tool Tool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := tool.Name()
	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("tool %s already registered", name)
	}

	r.tools[name] = tool
	return nil
}

// Unregister 注销工具
func (r *InMemoryRegistry) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.tools[name]; !exists {
		return fmt.Errorf("tool %s not found", name)
	}

	delete(r.tools, name)
	return nil
}

// Get 获取工具
func (r *InMemoryRegistry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tool, exists := r.tools[name]
	return tool, exists
}

// List 列出所有工具定义
func (r *InMemoryRegistry) List() []Definition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	defs := make([]Definition, 0, len(r.tools))
	for _, tool := range r.tools {
		defs = append(defs, Definition{
			Name:        tool.Name(),
			Description: tool.Description(),
			Parameters:  tool.Schema(),
		})
	}
	return defs
}

// Has 检查工具是否存在
func (r *InMemoryRegistry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, exists := r.tools[name]
	return exists
}

// ExecutionContext 执行上下文类型
type ExecutionContext int

const (
	ExecContextGateway ExecutionContext = iota // 直接在网关进程执行
	ExecContextSandbox                         // 在沙箱中执行
	ExecContextRemote                          // 远程节点执行
)

// String 返回执行上下文的字符串表示
func (c ExecutionContext) String() string {
	switch c {
	case ExecContextGateway:
		return "gateway"
	case ExecContextSandbox:
		return "sandbox"
	case ExecContextRemote:
		return "remote"
	default:
		return "unknown"
	}
}

// Executor 工具执行器接口
type Executor interface {
	// Execute 执行工具
	Execute(ctx context.Context, tool Tool, args map[string]interface{}) (*Result, error)
	// SetContext 设置执行上下文
	SetContext(execCtx ExecutionContext)
}

// Policy 工具策略
type Policy struct {
	Profile     string   // 预定义配置：minimal, coding, messaging, full
	AllowList   []string // 允许的工具列表
	DenyList    []string // 禁止的工具列表
	AskMode     bool     // 执行前是否需要用户确认
	MaxExecTime int      // 最大执行时间(秒)
}

// IsAllowed 检查工具是否被允许 (支持 Kind 自动决策)
func (p *Policy) IsAllowed(toolName string) bool {
	// 检查禁止列表
	for _, denied := range p.DenyList {
		if denied == toolName {
			return false
		}
	}

	// 如果允许列表为空，默认允许
	if len(p.AllowList) == 0 {
		return true
	}

	// 检查允许列表
	for _, allowed := range p.AllowList {
		if allowed == toolName {
			return true
		}
	}

	return false
}

// NeedsConfirmation 检查工具是否需要用户确认 (基于 Kind 自动判断)
func (p *Policy) NeedsConfirmation(kind Kind) bool {
	if !p.AskMode {
		return false
	}
	// SafeKinds 在 AskMode 下也自动放行
	if SafeKinds[kind] {
		return false
	}
	// MutatorKinds 需要确认
	return MutatorKinds[kind]
}

// PolicyEnforcer 策略执行器
type PolicyEnforcer struct {
	policy   *Policy
	registry Registry
}

// NewPolicyEnforcer 创建策略执行器
func NewPolicyEnforcer(policy *Policy, registry Registry) *PolicyEnforcer {
	return &PolicyEnforcer{
		policy:   policy,
		registry: registry,
	}
}

// FilteredList 返回策略过滤后的工具列表
func (e *PolicyEnforcer) FilteredList() []Definition {
	all := e.registry.List()
	filtered := make([]Definition, 0)

	for _, def := range all {
		if e.policy.IsAllowed(def.Name) {
			filtered = append(filtered, def)
		}
	}

	return filtered
}

// CanExecute 检查是否可以执行工具
func (e *PolicyEnforcer) CanExecute(toolName string) bool {
	return e.policy.IsAllowed(toolName)
}

// NeedsApproval 检查是否需要用户批准
func (e *PolicyEnforcer) NeedsApproval() bool {
	return e.policy.AskMode
}

// MarshalJSON 序列化工具结果
func (r *Result) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]interface{}{
		"output":   r.Output,
		"display":  r.Display,
		"success":  r.Success,
		"metadata": r.Metadata,
		"error":    r.Error,
	})
}
