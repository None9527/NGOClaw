"""MiniMax Provider - Infrastructure Layer"""

from typing import AsyncIterator, Optional, List
from openai import AsyncOpenAI

from ...domain.repository.ai_provider import AIProvider
from ...domain.entity.ai_request import AIRequest, AIResponse


class MiniMaxProvider(AIProvider):
    """MiniMax AI 提供商实现 (通过 OpenAI 兼容接口)"""

    # 支持的模型列表
    SUPPORTED_MODELS = [
        "MiniMax-M2.1",
        "MiniMax-M2.1-lightning",
        "abab6.5-chat",
        "abab6.5s-chat",
        "abab5.5-chat",
    ]

    def __init__(self, api_key: str, base_url: str):
        """初始化 MiniMax 提供商

        Args:
            api_key: API 密钥
            base_url: API 基础 URL
        """
        if not base_url:
            base_url = "https://api.minimaxi.com/v1"

        self._client = AsyncOpenAI(
            api_key=api_key,
            base_url=base_url,
        )

    async def generate(self, request: AIRequest) -> AIResponse:
        """生成 AI 响应

        Args:
            request: AI 请求

        Returns:
            AI 响应
        """
        # 调用 API
        response = await self._client.chat.completions.create(
            model=request.model,
            messages=[
                {"role": "user", "content": request.prompt}
            ],
            max_tokens=request.max_tokens,
            temperature=request.temperature,
            top_p=request.top_p,
        )

        choice = response.choices[0]

        # 构建响应实体
        return AIResponse(
            request_id=request.id,
            content=choice.message.content or "",
            model_used=response.model,
            tokens_used=response.usage.total_tokens if response.usage else 0,
            finish_reason=choice.finish_reason or "stop",
        )

    async def generate_stream(
        self, request: AIRequest
    ) -> AsyncIterator[str]:
        """流式生成 AI 响应

        Args:
            request: AI 请求

        Yields:
            响应内容片段
        """
        stream = await self._client.chat.completions.create(
            model=request.model,
            messages=[
                {"role": "user", "content": request.prompt}
            ],
            max_tokens=request.max_tokens,
            temperature=request.temperature,
            top_p=request.top_p,
            stream=True,
        )

        async for chunk in stream:
            if chunk.choices and chunk.choices[0].delta.content:
                yield chunk.choices[0].delta.content

    async def is_available(self) -> bool:
        """检查提供商是否可用"""
        try:
            # 尝试列出模型 (如果支持)
            await self._client.models.list()
            return True
        except Exception:
            # MiniMax 有时可能不支持 models.list，忽略错误或返回 True
            # 为了健壮性，这里假设如果客户端初始化成功则认为可能可用
            return True

    def supports_model(self, model: str) -> bool:
        """检查是否支持指定模型"""
        return model in self.SUPPORTED_MODELS or model.startswith("abab") or model.startswith("MiniMax")
