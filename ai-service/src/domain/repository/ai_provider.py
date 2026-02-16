"""AI Provider Repository Interface - Domain Layer"""

from abc import ABC, abstractmethod
from typing import AsyncIterator, Optional

from ..entity.ai_request import AIRequest, AIResponse


class AIProvider(ABC):
    """AI 提供商接口（遵循依赖倒置原则）

    定义在领域层，实现在基础设施层
    """

    @abstractmethod
    async def generate(self, request: AIRequest) -> AIResponse:
        """生成 AI 响应

        Args:
            request: AI 请求

        Returns:
            AI 响应

        Raises:
            AIProviderError: AI 提供商错误
        """
        pass

    @abstractmethod
    async def generate_stream(
        self, request: AIRequest
    ) -> AsyncIterator[str]:
        """流式生成 AI 响应

        Args:
            request: AI 请求

        Yields:
            响应内容片段

        Raises:
            AIProviderError: AI 提供商错误
        """
        pass

    @abstractmethod
    async def is_available(self) -> bool:
        """检查提供商是否可用

        Returns:
            是否可用
        """
        pass

    @abstractmethod
    def supports_model(self, model: str) -> bool:
        """检查是否支持指定模型

        Args:
            model: 模型名称

        Returns:
            是否支持
        """
        pass
