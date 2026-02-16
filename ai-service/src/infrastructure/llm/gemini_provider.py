"""Gemini AI Provider - Infrastructure Layer"""

from typing import AsyncIterator, Optional
import google.generativeai as genai

from ...domain.repository.ai_provider import AIProvider
from ...domain.entity.ai_request import AIRequest, AIResponse


class GeminiProvider(AIProvider):
    """Gemini AI 提供商实现"""

    # 支持的模型列表
    SUPPORTED_MODELS = [
        "gemini-3-flash",
        "gemini-3-pro-high",
        "gemini-3-pro-low",
        "gemini-2.5-flash",
        "gemini-2.5-flash-thinking",
    ]

    def __init__(self, api_key: str, base_url: Optional[str] = None):
        """初始化 Gemini 提供商

        Args:
            api_key: API 密钥
            base_url: API 基础 URL（可选）
        """
        self._api_key = api_key
        self._base_url = base_url

        # 配置 SDK (custom base_url via client_options when provided)
        configure_kwargs = {"api_key": api_key}
        if base_url:
            configure_kwargs["client_options"] = {"api_endpoint": base_url}
        genai.configure(**configure_kwargs)

    async def generate(self, request: AIRequest) -> AIResponse:
        """生成 AI 响应

        Args:
            request: AI 请求

        Returns:
            AI 响应
        """
        # 创建模型实例
        # 注意：Gemini 的 system_instruction 是在模型初始化时传入的
        model = genai.GenerativeModel(
            model_name=request.model,
            system_instruction=request.system_instruction
        )

        # 配置生成参数
        generation_config = genai.GenerationConfig(
            max_output_tokens=request.max_tokens,
            temperature=request.temperature,
            top_p=request.top_p,
        )

        # 构建对话历史
        contents = []
        for msg in request.history:
            role = "user" if msg.role == "user" else "model"
            contents.append({
                "role": role,
                "parts": [msg.content]
            })

        # 添加当前提示词
        contents.append({
            "role": "user",
            "parts": [request.prompt]
        })

        # 调用 API
        response = await model.generate_content_async(
            contents,
            generation_config=generation_config,
        )

        # 构建响应实体
        return AIResponse(
            request_id=request.id,
            content=response.text,
            model_used=request.full_model_name,
            tokens_used=response.usage_metadata.total_token_count
            if hasattr(response, "usage_metadata")
            else 0,
            finish_reason=self._map_finish_reason(response),
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
        # 创建模型实例
        model = genai.GenerativeModel(
            model_name=request.model,
            system_instruction=request.system_instruction
        )

        # 配置生成参数
        generation_config = genai.GenerationConfig(
            max_output_tokens=request.max_tokens,
            temperature=request.temperature,
            top_p=request.top_p,
        )

        # 构建对话历史
        contents = []
        for msg in request.history:
            role = "user" if msg.role == "user" else "model"
            contents.append({
                "role": role,
                "parts": [msg.content]
            })

        # 添加当前提示词
        contents.append({
            "role": "user",
            "parts": [request.prompt]
        })

        # 流式调用 API
        response = await model.generate_content_async(
            contents,
            generation_config=generation_config,
            stream=True,
        )

        async for chunk in response:
            if chunk.text:
                yield chunk.text

    async def is_available(self) -> bool:
        """检查提供商是否可用"""
        try:
            # 尝试列出模型（轻量级检查）
            models = genai.list_models()
            return True
        except Exception:
            return False

    def supports_model(self, model: str) -> bool:
        """检查是否支持指定模型"""
        return model in self.SUPPORTED_MODELS

    def _map_finish_reason(self, response) -> str:
        """映射完成原因"""
        if not hasattr(response, "candidates") or not response.candidates:
            return "stop"
        candidate = response.candidates[0]
        reason = getattr(candidate, "finish_reason", None)
        reason_map = {
            1: "stop",       # STOP
            2: "length",     # MAX_TOKENS
            3: "safety",     # SAFETY
            4: "recitation", # RECITATION
            5: "other",      # OTHER
        }
        return reason_map.get(reason, "stop")
