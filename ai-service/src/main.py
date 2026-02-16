"""AI Service Main Entry Point"""

import asyncio
import logging
import signal
import sys
from pathlib import Path

# 添加项目根目录到 Python 路径
project_root = Path(__file__).parent.parent
sys.path.insert(0, str(project_root))

from src.infrastructure.config.settings import load_settings
from src.infrastructure.grpc_server.ai_service_server import create_grpc_server
from src.application.usecase.generate_response import GenerateResponseUseCase
from src.application.usecase.generate_image import GenerateImageUseCase
from src.application.usecase.execute_skill import ExecuteSkillUseCase
from src.domain.service.model_router import ModelRouter
from src.domain.service.image_router import ImageRouter
from src.infrastructure.llm.gemini_provider import GeminiProvider
from src.infrastructure.llm.anthropic_provider import AnthropicProvider
from src.infrastructure.llm.minimax_provider import MiniMaxProvider
from src.infrastructure.llm.openai_provider import OpenAIProvider
from src.infrastructure.llm.mock_provider import MockProvider
from src.infrastructure.image.openai_image_provider import OpenAIImageProvider
from src.infrastructure.skill.registry import InMemorySkillRegistry
from src.infrastructure.skill.basic_skills import TimeSkill, EchoSkill, CalculatorSkill


# 全局变量用于优雅关闭
shutdown_event = asyncio.Event()


def setup_logging(level: str = "INFO", log_format: str = "json") -> None:
    """配置日志系统

    Args:
        level: 日志级别
        log_format: 日志格式 (json/text)
    """
    log_level = getattr(logging, level.upper(), logging.INFO)

    if log_format == "json":
        # JSON 格式日志
        logging.basicConfig(
            level=log_level,
            format='{"time":"%(asctime)s","level":"%(levelname)s","name":"%(name)s","message":"%(message)s"}',
            datefmt="%Y-%m-%d %H:%M:%S",
        )
    else:
        # 文本格式日志
        logging.basicConfig(
            level=log_level,
            format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
            datefmt="%Y-%m-%d %H:%M:%S",
        )


def create_providers(config) -> dict:
    """创建 AI 提供商实例

    Args:
        config: 配置对象

    Returns:
        提供商字典 {name: provider_instance}
    """
    providers = {}

    # 创建 Antigravity 提供商 (使用 OpenAI 兼容接口)
    if "antigravity" in config.providers and config.providers["antigravity"].enabled:
        antigravity_config = config.providers["antigravity"]
        if antigravity_config.api_key:
            # Antigravity 是 OpenAI 兼容的代理，使用 OpenAIProvider
            providers["antigravity"] = OpenAIProvider(
                api_key=antigravity_config.api_key,
                base_url=antigravity_config.base_url,
            )
            logging.info(f"Initialized Antigravity provider (OpenAI-compatible) with {len(antigravity_config.models)} models")
        else:
            logging.warning("Antigravity provider enabled but API key not provided")

    # Create Anthropic Provider
    if "anthropic" in config.providers and config.providers["anthropic"].enabled:
        anthropic_config = config.providers["anthropic"]
        if anthropic_config.api_key:
            providers["anthropic"] = AnthropicProvider(
                api_key=anthropic_config.api_key,
                base_url=anthropic_config.base_url if anthropic_config.base_url else None,
            )
            logging.info(f"Initialized Anthropic provider with {len(anthropic_config.models)} models")
        else:
            logging.warning("Anthropic provider enabled but API key not provided")

    # Create MiniMax Provider
    if "minimax" in config.providers and config.providers["minimax"].enabled:
        minimax_config = config.providers["minimax"]
        if minimax_config.api_key:
            providers["minimax"] = MiniMaxProvider(
                api_key=minimax_config.api_key,
                base_url=minimax_config.base_url,
            )
            logging.info(f"Initialized MiniMax provider with {len(minimax_config.models)} models")
        else:
            logging.warning("MiniMax provider enabled but API key not provided")

    # Bailian (Qwen3) - OpenAI-compatible
    if "bailian" in config.providers and config.providers["bailian"].enabled:
        bailian_config = config.providers["bailian"]
        if bailian_config.api_key:
            providers["bailian"] = OpenAIProvider(
                api_key=bailian_config.api_key,
                base_url=bailian_config.base_url,
            )
            logging.info(f"Initialized Bailian provider (OpenAI-compatible) with {len(bailian_config.models)} models")
        else:
            logging.warning("Bailian provider enabled but API key not provided")

    # Ollama - OpenAI-compatible (no API key needed)
    if "ollama" in config.providers and config.providers["ollama"].enabled:
        ollama_config = config.providers["ollama"]
        providers["ollama"] = OpenAIProvider(
            api_key="ollama",  # Ollama doesn't need a real key
            base_url=ollama_config.base_url + "/v1" if not ollama_config.base_url.endswith("/v1") else ollama_config.base_url,
        )
        logging.info(f"Initialized Ollama provider with {len(ollama_config.models)} models")

    # Add Mock Provider if explicitly enabled or if no other providers are active
    if not providers:
        logging.info("No external AI providers configured. Initializing Mock Provider.")
        providers["mock"] = MockProvider()
        providers["antigravity"] = providers["mock"]

    if not providers:
        logging.error("No AI providers configured! Service will not function properly.")

    return providers


def create_image_providers(config) -> dict:
    """创建图像提供商实例"""
    providers = {}

    # Check for OpenAI provider configuration
    if "openai" in config.providers and config.providers["openai"].enabled:
        openai_config = config.providers["openai"]
        if openai_config.api_key:
            providers["openai"] = OpenAIImageProvider(
                api_key=openai_config.api_key,
                base_url=openai_config.base_url if openai_config.base_url else None,
            )
            logging.info("Initialized OpenAI Image provider")

    return providers


def create_skill_registry(config) -> InMemorySkillRegistry:
    """创建并初始化技能注册表"""
    registry = InMemorySkillRegistry()

    # 注册基础技能
    registry.register_skill(TimeSkill())
    registry.register_skill(EchoSkill())
    registry.register_skill(CalculatorSkill())

    # Additional skills are loaded via sideload protocol or config-driven registration

    return registry


def signal_handler(signum, frame):
    """信号处理器"""
    logging.info(f"Received signal {signum}, initiating shutdown...")
    shutdown_event.set()


async def main():
    """主函数"""
    logger = logging.getLogger(__name__)
    logger.info("Starting NGOClaw AI Service...")

    # 1. 加载配置
    try:
        config = load_settings()
        logger.info(f"Configuration loaded: gRPC port={config.server.grpc_port}")
    except Exception as e:
        logger.error(f"Failed to load configuration: {e}")
        sys.exit(1)

    # 2. 配置日志
    setup_logging(config.logging.level, config.logging.format)

    # 3. 创建 AI 提供商
    providers = create_providers(config)
    if not providers:
        logger.error("Failed to initialize any AI providers")
        sys.exit(1)

    # 4. 创建领域服务（模型路由器）
    model_router = ModelRouter(providers=providers)
    logger.info(f"Model router initialized with {len(providers)} provider(s)")

    # Initialize Image Providers and Router
    image_providers = create_image_providers(config)
    image_router = ImageRouter(providers=image_providers)
    if image_providers:
        logger.info(f"Image router initialized with {len(image_providers)} provider(s)")
    else:
        logger.warning("No image providers configured")

    # Initialize Skill Registry
    skill_registry = create_skill_registry(config)
    logger.info(f"Skill registry initialized with {len(skill_registry.list_skills())} skills")

    # 5. 创建应用用例
    generate_use_case = GenerateResponseUseCase(model_router=model_router)
    generate_image_use_case = GenerateImageUseCase(image_router=image_router) if image_providers else None
    execute_skill_use_case = ExecuteSkillUseCase(skill_registry=skill_registry)
    logger.info("Use cases initialized")

    # 6. 创建 gRPC 服务器
    try:
        grpc_server = await create_grpc_server(
            generate_use_case=generate_use_case,
            generate_image_use_case=generate_image_use_case,
            execute_skill_use_case=execute_skill_use_case,
            port=config.server.grpc_port,
        )
        logger.info(f"gRPC server created on port {config.server.grpc_port}")
    except Exception as e:
        logger.error(f"Failed to create gRPC server: {e}")
        sys.exit(1)

    # 7. 启动服务器
    try:
        await grpc_server.start()
        logger.info(f"✓ AI Service started successfully on [::]:{config.server.grpc_port}")
        logger.info("Service is ready to accept requests")
    except Exception as e:
        logger.error(f"Failed to start gRPC server: {e}")
        sys.exit(1)

    # 8. 等待关闭信号
    try:
        await shutdown_event.wait()
    except KeyboardInterrupt:
        logger.info("Received keyboard interrupt")

    # 9. 优雅关闭
    logger.info("Shutting down gracefully...")
    try:
        await grpc_server.stop(grace=5.0)
        logger.info("✓ AI Service stopped successfully")
    except Exception as e:
        logger.error(f"Error during shutdown: {e}")
        sys.exit(1)


async def run_sideload(config, providers, skill_registry):
    """Run in sideload mode: JSON-RPC 2.0 over stdin/stdout."""
    from src.sideload.handler import SideloadHandler
    from src.sideload.provider import ProviderHandler
    from src.sideload.tool import ToolHandler
    from src.domain.service.model_router import ModelRouter

    logger = logging.getLogger(__name__)
    logger.info("Starting AI Service in SIDELOAD mode (JSON-RPC 2.0 over stdio)")

    # Redirect logging to stderr (stdout is for JSON-RPC)
    for h in logging.root.handlers[:]:
        logging.root.removeHandler(h)
    logging.basicConfig(
        level=logging.INFO,
        format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
        datefmt="%Y-%m-%d %H:%M:%S",
        stream=sys.stderr,
    )

    # Create domain services
    model_router = ModelRouter(providers=providers)
    provider_handler = ProviderHandler(model_router, config)
    tool_handler = ToolHandler(skill_registry)

    # Create JSON-RPC handler
    handler = SideloadHandler()

    # Register the 'initialize' method
    async def handle_initialize(params):
        return {
            "name": "ai-service",
            "version": "1.0.0",
            "capabilities": {
                "providers": provider_handler.get_capabilities(),
                "tools": tool_handler.get_capabilities(),
                "hooks": ["chat.params"],
            }
        }

    # Register the 'shutdown' method
    async def handle_shutdown(params):
        logger.info("Received shutdown, stopping sideload handler")
        handler.stop()
        return {}

    # Register methods
    handler.register_method("initialize", handle_initialize)
    handler.register_method("shutdown", handle_shutdown)
    handler.register_method("provider/generate", provider_handler.handle_generate)
    handler.register_method("tool/execute", tool_handler.handle_execute)
    handler.register_method("ping", lambda p: {"pong": True})

    # Run the handler
    await handler.run()
    logger.info("Sideload mode exited")


if __name__ == "__main__":
    # Check for --sideload flag
    sideload_mode = "--sideload" in sys.argv

    # 注册信号处理器
    signal.signal(signal.SIGINT, signal_handler)
    signal.signal(signal.SIGTERM, signal_handler)

    if sideload_mode:
        # Sideload mode: JSON-RPC over stdio
        try:
            config = load_settings()
            providers = create_providers(config)
            skill_registry = create_skill_registry(config)
            asyncio.run(run_sideload(config, providers, skill_registry))
        except Exception as e:
            logging.error(f"Fatal sideload error: {e}", exc_info=True)
            sys.exit(1)
    else:
        # Normal gRPC mode
        try:
            asyncio.run(main())
        except Exception as e:
            logging.error(f"Fatal error: {e}", exc_info=True)
            sys.exit(1)

