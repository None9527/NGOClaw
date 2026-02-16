# NGOClaw - OpenClaw 重构版

基于 DDD（领域驱动设计）和 SOLID 原则的 OpenClaw 重构项目。

## 🏗️ 架构设计

采用微服务架构，使用 Go + Python 技术栈：

- **Gateway Service (Go)**: 核心网关服务，处理消息路由、Telegram Bot、HTTP API
- **AI Service (Python)**: AI 模型调用、图像生成、技能脚本执行
- **Shared**: 共享的 Protocol Buffers 定义和工具

## 📁 项目结构

```
ngoclaw/
├── gateway/              # Go 网关服务
│   ├── cmd/             # 应用程序入口
│   ├── internal/        # 内部包（遵循 DDD 分层）
│   │   ├── domain/      # 领域层：实体、值对象、仓储接口、领域服务
│   │   ├── application/ # 应用层：用例、DTO、应用服务
│   │   ├── infrastructure/ # 基础设施层：配置、持久化、外部集成
│   │   └── interfaces/  # 接口层：HTTP、gRPC、事件处理
│   ├── pkg/             # 可被外部引用的公共包
│   ├── api/             # API 定义（proto、OpenAPI）
│   └── config/          # 配置文件
│
├── ai-service/          # Python AI 服务
│   ├── src/
│   │   ├── domain/      # 领域层
│   │   ├── application/ # 应用层
│   │   ├── infrastructure/ # 基础设施层
│   │   └── interfaces/  # 接口层
│   ├── tests/           # 测试
│   └── config/          # 配置文件
│
├── shared/              # 共享资源
│   ├── proto/           # Protocol Buffers 定义
│   └── docs/            # 共享文档
│
├── docs/                # 项目文档
├── scripts/             # 构建和部署脚本
└── deployments/         # 部署配置（Docker、K8s）
```

## 🎯 DDD 分层架构

### Domain Layer (领域层)
- **Entity**: 领域实体（具有唯一标识）
- **Value Object**: 值对象（无唯一标识，不可变）
- **Repository**: 仓储接口（数据持久化抽象）
- **Service**: 领域服务（核心业务逻辑）

### Application Layer (应用层)
- **UseCase**: 用例（编排领域对象完成业务流程）
- **DTO**: 数据传输对象
- **Service**: 应用服务（协调用例执行）

### Infrastructure Layer (基础设施层)
- **Config**: 配置管理
- **Persistence**: 数据持久化实现
- **External Integration**: 外部服务集成（Telegram、AI API）
- **Logger**: 日志系统

### Interface Layer (接口层)
- **HTTP/gRPC**: API 接口
- **Event Handler**: 事件处理器

## 🛠️ SOLID 原则

- **S - 单一职责原则**: 每个模块只负责一个功能
- **O - 开闭原则**: 对扩展开放，对修改封闭（通过接口和依赖注入）
- **L - 里氏替换原则**: 子类可以替换父类
- **I - 接口隔离原则**: 细粒度的接口设计
- **D - 依赖倒置原则**: 依赖抽象而非具体实现

## 🚀 快速开始

### 前置要求
- Go 1.21+
- Python 3.11+
- Docker & Docker Compose

### 开发环境设置

```bash
# 1. 初始化 Go 服务
cd gateway
go mod init github.com/ngoclaw/ngoclaw/gateway
go mod tidy

# 2. 初始化 Python 服务
cd ../ai-service
python -m venv venv
source venv/bin/activate
pip install -r requirements.txt

# 3. 生成 gRPC 代码
cd ../shared
./scripts/generate-proto.sh

# 4. 启动服务
docker-compose up -d
```

## 📚 技术栈

### Gateway Service (Go)
- **Framework**: Gin (HTTP), gRPC
- **Configuration**: Viper
- **Logging**: Zap
- **Telegram**: telegram-bot-api
- **Database**: SQLite/PostgreSQL (gorm)

### AI Service (Python)
- **Framework**: FastAPI, gRPC
- **AI SDK**: google-generativeai, anthropic, openai
- **Image Gen**: diffusers, pillow
- **Configuration**: pydantic-settings

## 🔧 配置管理

兼容原有的 `openclaw.json` 配置格式，同时支持环境变量覆盖。

## 📖 文档

- [架构设计文档](docs/ARCHITECTURE.md)
- [API 文档](docs/API.md)
- [开发指南](docs/DEVELOPMENT.md)
- [部署指南](docs/DEPLOYMENT.md)

## 📝 许可证

MIT License
