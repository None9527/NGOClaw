"""Configuration Management - Infrastructure Layer"""

import os
from pathlib import Path
from typing import Dict, List, Optional
from dataclasses import dataclass, field
import yaml


@dataclass
class ServerConfig:
    """服务器配置"""
    host: str = "0.0.0.0"
    grpc_port: int = 50051
    http_port: int = 8080


@dataclass
class ProviderConfig:
    """AI 提供商配置"""
    base_url: str = ""
    api_key: str = ""
    enabled: bool = True
    models: List[str] = field(default_factory=list)


@dataclass
class LoggingConfig:
    """日志配置"""
    level: str = "INFO"
    format: str = "json"


@dataclass
class Settings:
    """应用配置"""
    server: ServerConfig = field(default_factory=ServerConfig)
    providers: Dict[str, ProviderConfig] = field(default_factory=dict)
    logging: LoggingConfig = field(default_factory=LoggingConfig)

    @classmethod
    def load(cls, config_path: Optional[Path] = None) -> "Settings":
        """加载配置

        Args:
            config_path: 配置文件路径（可选）

        Returns:
            配置对象
        """
        # 1. 确定配置文件路径
        if config_path is None:
            # 默认路径：项目根目录/config/config.yaml
            project_root = Path(__file__).parent.parent.parent.parent
            config_path = project_root / "config" / "config.yaml"

        # 2. 加载 YAML 配置
        config_data = {}
        if config_path.exists():
            with open(config_path, "r", encoding="utf-8") as f:
                config_data = yaml.safe_load(f) or {}

        # 3. 解析服务器配置
        server_data = config_data.get("server", {})
        server = ServerConfig(
            host=server_data.get("host", "0.0.0.0"),
            grpc_port=server_data.get("grpc_port", 50051),
            http_port=server_data.get("http_port", 8080),
        )

        # 4. 解析提供商配置（支持环境变量覆盖）
        providers = {}
        providers_data = config_data.get("providers", {})

        for provider_name, provider_data in providers_data.items():
            # 从环境变量读取 API Key
            api_key_env = f"{provider_name.upper()}_API_KEY"
            api_key = os.getenv(api_key_env, provider_data.get("api_key", ""))

            providers[provider_name] = ProviderConfig(
                base_url=provider_data.get("base_url", ""),
                api_key=api_key,
                enabled=provider_data.get("enabled", True),
                models=provider_data.get("models", []),
            )

        # 5. 解析日志配置
        logging_data = config_data.get("logging", {})
        logging_config = LoggingConfig(
            level=os.getenv("LOG_LEVEL", logging_data.get("level", "INFO")),
            format=logging_data.get("format", "json"),
        )

        return cls(
            server=server,
            providers=providers,
            logging=logging_config,
        )


def load_settings(config_path: Optional[Path] = None) -> Settings:
    """加载配置的便捷函数

    Args:
        config_path: 配置文件路径（可选）

    Returns:
        配置对象
    """
    return Settings.load(config_path)
