"""Anthropic Provider - Infrastructure Layer"""

from typing import AsyncIterator, List, Optional
from anthropic import AsyncAnthropic

from ...domain.repository.ai_provider import AIProvider
from ...domain.entity.ai_request import AIRequest, AIResponse


class AnthropicProvider(AIProvider):
    """Anthropic AI 提供商实现"""

    # 支持的模型列表
    SUPPORTED_MODELS = [
        "claude-3-opus-20240229",
        "claude-3-sonnet-20240229",
        "claude-3-haiku-20240307",
        "claude-2.1",
        "claude-2.0",
        "claude-sonnet-4-5",
    ]

    def __init__(self, api_key: str, base_url: Optional[str] = None):
        """初始化 Anthropic 提供商

        Args:
            api_key: API 密钥
            base_url: API 基础 URL（可选）
        """
        self._client = AsyncAnthropic(
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
        # 映射模型名称 (处理别名)
        model = self._map_model(request.model)

        # 准备消息列表
        messages = []
        for msg in request.history:
            messages.append({
                "role": msg.role,
                "content": msg.content
            })

        # 添加当前提示词
        messages.append({"role": "user", "content": request.prompt})

        # 调用 API
        # 准备参数
        kwargs = {
            "model": model,
            "max_tokens": request.max_tokens,
            "temperature": request.temperature,
            "top_p": request.top_p,
            "messages": messages,
            "metadata": request.metadata.get("anthropic_metadata", None),
        }

        # 添加系统提示词（如果有）
        if request.system_instruction:
            kwargs["system"] = request.system_instruction

        response = await self._client.messages.create(**kwargs)

        # 提取内容
        content = ""
        if response.content and len(response.content) > 0:
            content = response.content[0].text

        # 构建响应实体
        return AIResponse(
            request_id=request.id,
            content=content,
            model_used=response.model,
            tokens_used=response.usage.output_tokens + response.usage.input_tokens,
            finish_reason=response.stop_reason or "stop",
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
        model = self._map_model(request.model)

        # 准备消息列表
        messages = []
        for msg in request.history:
            messages.append({
                "role": msg.role,
                "content": msg.content
            })
        messages.append({"role": "user", "content": request.prompt})

        # 准备参数
        kwargs = {
            "max_tokens": request.max_tokens,
            "messages": messages,
            "model": model,
            "temperature": request.temperature,
            "top_p": request.top_p,
        }

        if request.system_instruction:
            kwargs["system"] = request.system_instruction

        async with self._client.messages.stream(**kwargs) as stream:
            async for text in stream.text_stream:
                yield text

    async def is_available(self) -> bool:
        """检查提供商是否可用"""
        try:
            # 轻量级检查：列出模型（如果 API 支持）或者发送一个简单的请求
            # Anthropic 目前没有 list_models API，尝试发送一个极简请求
            # 注意：这会消耗 Token，实际生产中可能只需要检查 API Key 格式或不做预检查
            return True
        except Exception:
            return False

    def supports_model(self, model: str) -> bool:
        """检查是否支持指定模型"""
        # 简单包含检查，也可以支持通配符
        return model in self.SUPPORTED_MODELS or model.startswith("claude-")

    def _map_model(self, model: str) -> str:
        """映射内部模型名称到 API 模型名称"""
        # 这里可以处理一些别名映射
        if model == "claude-sonnet-4-5":
             # 假设这是未来模型的别名，或者映射到当前的 Sonnet
             return "claude-3-sonnet-20240229"
        return model
