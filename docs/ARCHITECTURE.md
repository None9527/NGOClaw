# NGOClaw 架构设计文档

## 概览

NGOClaw 是 OpenClaw 的重构版本，采用微服务架构和领域驱动设计（DDD），使用 Go 和 Python 技术栈。

## 架构原则

### 1. 领域驱动设计 (DDD)

#### 分层架构
```
┌─────────────────────────────────────────┐
│         Interface Layer (接口层)         │
│   HTTP API, gRPC, Event Handlers        │
├─────────────────────────────────────────┤
│        Application Layer (应用层)        │
│   Use Cases, Application Services       │
├─────────────────────────────────────────┤
│          Domain Layer (领域层)           │
│   Entities, Value Objects, Services     │
├─────────────────────────────────────────┤
│      Infrastructure Layer (基础设施层)   │
│   Database, External APIs, Messaging    │
└─────────────────────────────────────────┘
```

#### 核心概念

**聚合根 (Aggregate Root)**
- `Agent`: 代理聚合
- `Conversation`: 会话聚合
- `Skill`: 技能聚合
- `Channel`: 渠道聚合

**实体 (Entity)**
- `Message`: 消息实体
- `User`: 用户实体
- `Model`: AI 模型实体

**值对象 (Value Object)**
- `MessageContent`: 消息内容
- `ModelConfig`: 模型配置
- `ChannelConfig`: 渠道配置

### 2. SOLID 原则应用

#### 单一职责原则 (SRP)
每个模块只负责一个功能领域：
- `MessageRouter`: 仅负责消息路由
- `TelegramAdapter`: 仅负责 Telegram 集成
- `AIModelService`: 仅负责 AI 模型调用

#### 开闭原则 (OCP)
通过接口和依赖注入实现扩展：
```go
type ChannelAdapter interface {
    SendMessage(ctx context.Context, msg *Message) error
    ReceiveMessages(ctx context.Context) (<-chan *Message, error)
}

type TelegramAdapter struct { /* ... */ }
type DiscordAdapter struct { /* ... */ }
```

#### 里氏替换原则 (LSP)
所有实现必须可替换接口：
```go
type AIProvider interface {
    GenerateResponse(ctx context.Context, prompt string) (string, error)
}

type GeminiProvider struct { /* ... */ }
type ClaudeProvider struct { /* ... */ }
```

#### 接口隔离原则 (ISP)
细粒度的接口定义：
```go
type MessageSender interface {
    Send(ctx context.Context, msg *Message) error
}

type MessageReceiver interface {
    Receive(ctx context.Context) (<-chan *Message, error)
}
```

#### 依赖倒置原则 (DIP)
依赖抽象而非具体实现：
```go
type MessageUseCase struct {
    router     MessageRouter      // 依赖接口
    aiProvider AIProvider         // 依赖接口
    repo       MessageRepository  // 依赖接口
}
```

## 服务架构

### Gateway Service (Go)

**职责**:
- HTTP/WebSocket API 服务
- Telegram Bot 消息处理
- 消息路由和分发
- 会话管理
- 配置管理
- 插件生命周期管理

**核心组件**:

```go
// Domain Layer
package domain

type Message struct {
    ID          string
    ConvID      string
    Content     MessageContent
    Sender      User
    Timestamp   time.Time
}

type Agent struct {
    ID          string
    Name        string
    ModelConfig ModelConfig
    Skills      []Skill
}

// Application Layer
package usecase

type ProcessMessageUseCase struct {
    router     MessageRouter
    aiClient   AIServiceClient
    repo       MessageRepository
}

func (uc *ProcessMessageUseCase) Execute(ctx context.Context, msg *Message) error {
    // 1. 验证消息
    // 2. 路由到对应的代理
    // 3. 调用 AI 服务
    // 4. 返回响应
}

// Infrastructure Layer
package telegram

type TelegramAdapter struct {
    bot        *tgbotapi.BotAPI
    dispatcher MessageDispatcher
}

func (a *TelegramAdapter) HandleUpdate(update tgbotapi.Update) error {
    // 转换 Telegram 消息为领域消息
    // 分发到消息处理器
}
```

### AI Service (Python)

**职责**:
- AI 模型调用（Gemini、Claude、Minimax）
- 图像生成
- 技能脚本执行
- 提供 gRPC API 给 Gateway

**核心组件**:

```python
# Domain Layer
from dataclasses import dataclass
from typing import Protocol

@dataclass
class AIRequest:
    prompt: str
    model: str
    max_tokens: int
    temperature: float

class AIProvider(Protocol):
    async def generate(self, request: AIRequest) -> str:
        ...

# Application Layer
class GenerateResponseUseCase:
    def __init__(self, provider: AIProvider):
        self._provider = provider

    async def execute(self, request: AIRequest) -> str:
        # 1. 验证请求
        # 2. 调用 AI 提供商
        # 3. 处理响应
        return await self._provider.generate(request)

# Infrastructure Layer
class GeminiProvider:
    def __init__(self, api_key: str):
        self._client = genai.Client(api_key=api_key)

    async def generate(self, request: AIRequest) -> str:
        # 调用 Gemini API
        pass
```

## 服务间通信

### gRPC Protocol

```protobuf
syntax = "proto3";

package ngoclaw.ai.v1;

service AIService {
  rpc GenerateResponse(GenerateRequest) returns (GenerateResponse);
  rpc GenerateImage(ImageRequest) returns (ImageResponse);
  rpc ExecuteSkill(SkillRequest) returns (SkillResponse);
}

message GenerateRequest {
  string prompt = 1;
  string model = 2;
  int32 max_tokens = 3;
  float temperature = 4;
}

message GenerateResponse {
  string content = 1;
  string model_used = 2;
  int32 tokens_used = 3;
}
```

## 数据流

### 消息处理流程

```
┌─────────────┐
│  Telegram   │
└──────┬──────┘
       │ Webhook/Polling
       ▼
┌─────────────────────────────┐
│  Gateway Service (Go)        │
│  ┌──────────────────────┐   │
│  │ TelegramAdapter      │   │
│  └──────┬───────────────┘   │
│         │                    │
│         ▼                    │
│  ┌──────────────────────┐   │
│  │ ProcessMessageUseCase│   │
│  └──────┬───────────────┘   │
│         │                    │
│         ▼                    │
│  ┌──────────────────────┐   │
│  │ AIServiceClient      │   │
│  └──────┬───────────────┘   │
└─────────┼───────────────────┘
          │ gRPC
          ▼
┌─────────────────────────────┐
│  AI Service (Python)         │
│  ┌──────────────────────┐   │
│  │ gRPC Server          │   │
│  └──────┬───────────────┘   │
│         │                    │
│         ▼                    │
│  ┌──────────────────────┐   │
│  │ GenerateUseCase      │   │
│  └──────┬───────────────┘   │
│         │                    │
│         ▼                    │
│  ┌──────────────────────┐   │
│  │ GeminiProvider       │   │
│  └──────────────────────┘   │
└─────────────────────────────┘
```

## 配置管理

### 配置优先级
1. 环境变量 (最高优先级)
2. 配置文件 (openclaw.json)
3. 默认值 (最低优先级)

### 配置结构
```go
type Config struct {
    Gateway   GatewayConfig
    AIService AIServiceConfig
    Channels  []ChannelConfig
    Agents    []AgentConfig
}

type GatewayConfig struct {
    Host string `mapstructure:"host" env:"GATEWAY_HOST" default:"0.0.0.0"`
    Port int    `mapstructure:"port" env:"GATEWAY_PORT" default:"18789"`
}
```

## 扩展性设计

### 插件系统
```go
type Plugin interface {
    Name() string
    Initialize(ctx context.Context, config map[string]interface{}) error
    Shutdown(ctx context.Context) error
}

type SkillPlugin interface {
    Plugin
    Execute(ctx context.Context, input SkillInput) (SkillOutput, error)
}
```

### 事件驱动
```go
type Event interface {
    Type() string
    Timestamp() time.Time
    Payload() interface{}
}

type EventBus interface {
    Subscribe(eventType string, handler EventHandler) error
    Publish(ctx context.Context, event Event) error
}
```

## 性能优化

1. **连接池**: gRPC 连接池复用
2. **缓存**: Redis 缓存热点数据
3. **并发控制**: Go 协程池控制并发
4. **流式处理**: gRPC streaming 处理大数据

## 安全性

1. **认证**: JWT Token 认证
2. **授权**: RBAC 权限控制
3. **加密**: TLS 加密通信
4. **限流**: Token bucket 限流

## 监控和可观测性

1. **日志**: 结构化日志 (JSON)
2. **指标**: Prometheus metrics
3. **追踪**: OpenTelemetry tracing
4. **健康检查**: HTTP /health 端点

## 部署架构

### Docker Compose (开发环境)
```yaml
services:
  gateway:
    image: ngoclaw-gateway:latest
    ports:
      - "18789:18789"

  ai-service:
    image: ngoclaw-ai-service:latest
    ports:
      - "50051:50051"

  redis:
    image: redis:alpine
```

### Kubernetes (生产环境)
- Gateway: Deployment + Service (LoadBalancer)
- AI Service: Deployment + Service (ClusterIP)
- Redis: StatefulSet + Service
- Config: ConfigMap + Secret
