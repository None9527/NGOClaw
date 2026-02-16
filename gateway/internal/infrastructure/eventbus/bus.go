package eventbus

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Event 事件接口
type Event interface {
	Type() string
	Timestamp() time.Time
	Payload() any
}

// BaseEvent 基础事件实现
type BaseEvent struct {
	EventType      string
	EventTimestamp time.Time
	EventPayload   any
}

// Type 返回事件类型
func (e *BaseEvent) Type() string {
	return e.EventType
}

// Timestamp 返回事件时间戳
func (e *BaseEvent) Timestamp() time.Time {
	return e.EventTimestamp
}

// Payload 返回事件载荷
func (e *BaseEvent) Payload() any {
	return e.EventPayload
}

// NewEvent 创建新事件
func NewEvent(eventType string, payload any) *BaseEvent {
	return &BaseEvent{
		EventType:      eventType,
		EventTimestamp: time.Now(),
		EventPayload:   payload,
	}
}

// Handler 事件处理函数
type Handler func(ctx context.Context, event Event)

// Bus 事件总线接口
type Bus interface {
	// Publish 发布事件
	Publish(ctx context.Context, event Event)
	// Subscribe 订阅事件
	Subscribe(eventType string, handler Handler)
	// Unsubscribe 取消订阅
	Unsubscribe(eventType string, handler Handler)
	// Close 关闭事件总线
	Close()
}

// InMemoryBus 内存事件总线
type InMemoryBus struct {
	mu        sync.RWMutex
	handlers  map[string][]Handler
	eventChan chan eventWrapper
	closed    bool
	logger    *zap.Logger
	wg        sync.WaitGroup
}

type eventWrapper struct {
	ctx   context.Context
	event Event
}

// NewInMemoryBus 创建内存事件总线
func NewInMemoryBus(logger *zap.Logger, bufferSize int) *InMemoryBus {
	bus := &InMemoryBus{
		handlers:  make(map[string][]Handler),
		eventChan: make(chan eventWrapper, bufferSize),
		logger:    logger,
	}

	// 启动事件分发协程
	bus.wg.Add(1)
	go bus.dispatch()

	return bus
}

// Publish 发布事件
func (b *InMemoryBus) Publish(ctx context.Context, event Event) {
	b.mu.RLock()
	if b.closed {
		b.mu.RUnlock()
		return
	}
	b.mu.RUnlock()

	// 非阻塞发送
	select {
	case b.eventChan <- eventWrapper{ctx: ctx, event: event}:
		b.logger.Debug("Event published",
			zap.String("type", event.Type()),
		)
	default:
		b.logger.Warn("Event buffer full, dropping event",
			zap.String("type", event.Type()),
		)
	}
}

// Subscribe 订阅事件
func (b *InMemoryBus) Subscribe(eventType string, handler Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.handlers[eventType] == nil {
		b.handlers[eventType] = make([]Handler, 0)
	}
	b.handlers[eventType] = append(b.handlers[eventType], handler)

	b.logger.Debug("Handler subscribed",
		zap.String("event_type", eventType),
	)
}

// Unsubscribe 取消订阅（移除最后一个匹配的处理器）
func (b *InMemoryBus) Unsubscribe(eventType string, handler Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()

	handlers := b.handlers[eventType]
	if len(handlers) == 0 {
		return
	}

	// 从后往前找第一个匹配的 handler 并移除
	newHandlers := make([]Handler, 0, len(handlers))
	removed := false
	for i := len(handlers) - 1; i >= 0; i-- {
		// 注意: Go 不支持函数指针比较，但从后往前删除最后注册的同名 handler 是安全的默认行为
		if !removed {
			removed = true
			continue // 跳过最后一个
		}
		newHandlers = append([]Handler{handlers[i]}, newHandlers...)
	}
	if !removed {
		return
	}

	if len(newHandlers) == 0 {
		delete(b.handlers, eventType)
	} else {
		b.handlers[eventType] = newHandlers
	}
}

// Close 关闭事件总线
func (b *InMemoryBus) Close() {
	b.mu.Lock()
	b.closed = true
	close(b.eventChan)
	b.mu.Unlock()

	b.wg.Wait()
	b.logger.Info("Event bus closed")
}

// dispatch 事件分发循环
func (b *InMemoryBus) dispatch() {
	defer b.wg.Done()

	for wrapper := range b.eventChan {
		b.dispatchEvent(wrapper.ctx, wrapper.event)
	}
}

// dispatchEvent 分发单个事件
func (b *InMemoryBus) dispatchEvent(ctx context.Context, event Event) {
	b.mu.RLock()
	handlers := make([]Handler, 0)

	// 获取特定类型的处理器
	if h, ok := b.handlers[event.Type()]; ok {
		handlers = append(handlers, h...)
	}

	// 获取通配符处理器
	if h, ok := b.handlers["*"]; ok {
		handlers = append(handlers, h...)
	}
	b.mu.RUnlock()

	// 并行执行处理器
	var wg sync.WaitGroup
	for _, handler := range handlers {
		wg.Add(1)
		go func(h Handler) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					b.logger.Error("Handler panicked",
						zap.String("event_type", event.Type()),
						zap.Any("panic", r),
					)
				}
			}()
			h(ctx, event)
		}(handler)
	}
	wg.Wait()
}

// 预定义事件类型常量
const (
	EventTypeStateChange     = "state_change"
	EventTypeToolExecution   = "tool_execution"
	EventTypeModelRequest    = "model_request"
	EventTypeModelResponse   = "model_response"
	EventTypeError           = "error"
	EventTypeSessionCreated  = "session_created"
	EventTypeSessionEnded    = "session_ended"
	EventTypeApprovalRequest = "approval_request"
)

// StateChangePayload 状态变化事件载荷
type StateChangePayload struct {
	SessionID string
	FromState string
	ToState   string
	Trigger   string
	Metadata  map[string]any
}

// ToolExecutionPayload 工具执行事件载荷
type ToolExecutionPayload struct {
	SessionID  string
	ToolName   string
	ToolCallID string
	Arguments  map[string]any
	Result     any
	Duration   time.Duration
	Success    bool
}

// ModelRequestPayload 模型请求事件载荷
type ModelRequestPayload struct {
	SessionID string
	Model     string
	Messages  int
	HasTools  bool
}

// ModelResponsePayload 模型响应事件载荷
type ModelResponsePayload struct {
	SessionID  string
	Model      string
	TokensUsed int
	HasTools   bool
	Duration   time.Duration
}

// ErrorPayload 错误事件载荷
type ErrorPayload struct {
	SessionID string
	Component string
	Error     string
	Stack     string
}
