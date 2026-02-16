"""Model Router Domain Service - Domain Layer"""

from typing import Dict, Optional
from ..repository.ai_provider import AIProvider
from ..entity.ai_request import AIRequest


class ModelRouterError(Exception):
    """模型路由错误"""
    pass


class ModelRouter:
    """模型路由领域服务

    负责根据请求选择合适的 AI 提供商
    """

    def __init__(self, providers: Dict[str, AIProvider]):
        """初始化模型路由器

        Args:
            providers: 提供商映射 {provider_name: provider_instance}
        """
        self._providers = providers

    def get_provider(self, request: AIRequest) -> AIProvider:
        """获取处理请求的提供商

        Args:
            request: AI 请求

        Returns:
            AI 提供商实例

        Raises:
            ModelRouterError: 找不到合适的提供商
        """
        provider_name = request.provider

        # 查找提供商
        provider = self._providers.get(provider_name)
        if provider is None:
            raise ModelRouterError(f"Provider '{provider_name}' not found")

        # 检查提供商是否支持该模型
        if not provider.supports_model(request.model):
            raise ModelRouterError(
                f"Provider '{provider_name}' does not support model '{request.model}'"
            )

        return provider

    def list_available_models(self) -> Dict[str, list[str]]:
        """列出所有可用的模型

        Returns:
            {provider_name: [model1, model2, ...]}
        """
        # Collect models from each registered provider
        result: Dict[str, list[str]] = {}
        for name, provider in self._providers.items():
            if hasattr(provider, 'SUPPORTED_MODELS'):
                result[name] = list(provider.SUPPORTED_MODELS)
            else:
                result[name] = []
        return result

    def register_provider(self, name: str, provider: AIProvider) -> None:
        """注册新的提供商

        Args:
            name: 提供商名称
            provider: 提供商实例
        """
        self._providers[name] = provider

    def unregister_provider(self, name: str) -> None:
        """注销提供商

        Args:
            name: 提供商名称
        """
        self._providers.pop(name, None)
