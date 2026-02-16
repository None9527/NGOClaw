"""Image Router Domain Service - Domain Layer"""

from typing import Dict
from ..repository.image_provider import ImageProvider
from ..entity.image import ImageRequest


class ImageRouterError(Exception):
    """图像路由错误"""
    pass


class ImageRouter:
    """图像路由服务"""

    def __init__(self, providers: Dict[str, ImageProvider]):
        self._providers = providers

    def get_provider(self, request: ImageRequest) -> ImageProvider:
        """获取图像提供商"""
        provider_name = request.provider

        provider = self._providers.get(provider_name)
        if provider is None:
            # 如果指定提供商不存在，尝试查找支持该模型的提供商
            for name, p in self._providers.items():
                if p.supports_model(request.model):
                    return p
            raise ImageRouterError(f"Provider '{provider_name}' not found")

        if not provider.supports_model(request.model):
            raise ImageRouterError(
                f"Provider '{provider_name}' does not support model '{request.model}'"
            )

        return provider
