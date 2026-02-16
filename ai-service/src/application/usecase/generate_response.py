"""Generate Response Use Case - Application Layer"""

from typing import AsyncIterator

from ...domain.entity.ai_request import AIRequest, AIResponse
from ...domain.service.model_router import ModelRouter
from ...domain.repository.ai_provider import AIProvider


class GenerateResponseUseCase:
    """生成响应用例

    编排领域对象完成生成 AI 响应的业务流程
    """

    def __init__(self, model_router: ModelRouter):
        """初始化用例

        Args:
            model_router: 模型路由服务
        """
        self._model_router = model_router

    async def execute(self, request: AIRequest) -> AIResponse:
        """执行用例：生成 AI 响应

        Args:
            request: AI 请求

        Returns:
            AI 响应

        Raises:
            ValueError: 请求无效
            ModelRouterError: 路由失败
            AIProviderError: 提供商错误
        """
        # 1. 验证请求（领域实体已在 __post_init__ 中验证）

        # 2. 路由到合适的提供商
        provider = self._model_router.get_provider(request)

        # 3. 检查提供商可用性
        if not await provider.is_available():
            raise RuntimeError(f"Provider '{request.provider}' is not available")

        # 4. 调用提供商生成响应
        response = await provider.generate(request)

        # 5. 返回响应
        return response

    async def execute_stream(
        self, request: AIRequest
    ) -> AsyncIterator[str]:
        """执行用例：流式生成 AI 响应

        Args:
            request: AI 请求

        Yields:
            响应内容片段

        Raises:
            ValueError: 请求无效
            ModelRouterError: 路由失败
            AIProviderError: 提供商错误
        """
        # 1. 验证请求

        # 2. 路由到合适的提供商
        provider = self._model_router.get_provider(request)

        # 3. 检查提供商可用性
        if not await provider.is_available():
            raise RuntimeError(f"Provider '{request.provider}' is not available")

        # 4. 流式生成响应
        async for chunk in provider.generate_stream(request):
            yield chunk
