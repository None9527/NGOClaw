from typing import AsyncIterator, Dict, Any, Optional

from ...domain.repository.ai_provider import AIProvider
from ...domain.entity.ai_request import AIRequest, AIResponse


class MockProvider(AIProvider):
    """模拟 AI 提供商，用于测试"""

    def __init__(self):
        self._is_available = True

    async def generate(self, request: AIRequest) -> AIResponse:
        """生成 AI 响应"""
        return AIResponse(
            request_id="mock-req-id",
            content=f"Mock response to: {request.prompt}",
            model_used="mock-model",
            tokens_used=10,
            finish_reason="stop",
            metadata={}
        )

    async def generate_stream(self, request: AIRequest) -> AsyncIterator[str]:
        """流式生成 AI 响应"""
        response_text = f"Mock stream response to: {request.prompt}"
        words = response_text.split(" ")
        for word in words:
            yield word + " "
            # Simulate some delay if needed, but for tests speed is good

    async def is_available(self) -> bool:
        """检查提供商是否可用"""
        return self._is_available
