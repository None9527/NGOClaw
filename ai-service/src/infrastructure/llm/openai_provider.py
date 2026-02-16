"""OpenAI AI Provider - Infrastructure Layer"""

from typing import AsyncIterator, Optional
import httpx

from ...domain.repository.ai_provider import AIProvider
from ...domain.entity.ai_request import AIRequest, AIResponse


class OpenAIProvider(AIProvider):
    """OpenAI API 提供商实现 (支持 OpenAI 兼容接口)"""

    # 支持的模型列表
    SUPPORTED_MODELS = [
        # OpenAI 官方模型
        "gpt-4o",
        "gpt-4o-mini",
        "gpt-4-turbo",
        "gpt-4",
        "gpt-3.5-turbo",
        "o1-preview",
        "o1-mini",
        # 通用兼容 (通过 base_url 配置)
        "*",
    ]

    def __init__(
        self,
        api_key: str,
        base_url: Optional[str] = None,
        organization: Optional[str] = None,
    ):
        """初始化 OpenAI 提供商

        Args:
            api_key: API 密钥
            base_url: API 基础 URL (默认 OpenAI 官方)
            organization: 组织 ID (可选)
        """
        self._api_key = api_key
        self._base_url = base_url or "https://api.openai.com/v1"
        self._organization = organization

    async def generate(self, request: AIRequest) -> AIResponse:
        """生成 AI 响应"""
        headers = {
            "Authorization": f"Bearer {self._api_key}",
            "Content-Type": "application/json",
        }
        if self._organization:
            headers["OpenAI-Organization"] = self._organization

        # 构建对话历史
        messages = []
        if request.system_instruction:
            messages.append({
                "role": "system",
                "content": request.system_instruction
            })
        
        for msg in request.history:
            messages.append({
                "role": msg.role,
                "content": msg.content
            })

        messages.append({
            "role": "user",
            "content": request.prompt
        })

        # 构建请求体
        payload = {
            "model": request.model,
            "messages": messages,
            "max_tokens": request.max_tokens,
            "temperature": request.temperature,
        }
        if request.top_p:
            payload["top_p"] = request.top_p

        # 添加工具定义 (如果有)
        if request.tools:
            payload["tools"] = [
                {
                    "type": "function",
                    "function": {
                        "name": tool.name,
                        "description": tool.description,
                        "parameters": tool.parameters,
                    }
                }
                for tool in request.tools
            ]

        async with httpx.AsyncClient(timeout=120.0) as client:
            response = await client.post(
                f"{self._base_url}/chat/completions",
                headers=headers,
                json=payload
            )
            response.raise_for_status()
            data = response.json()

        choice = data["choices"][0]
        message = choice["message"]

        # 解析工具调用
        tool_calls = []
        if message.get("tool_calls"):
            import json
            for tc in message["tool_calls"]:
                tool_calls.append({
                    "id": tc["id"],
                    "name": tc["function"]["name"],
                    "arguments": json.loads(tc["function"]["arguments"]),
                })

        return AIResponse(
            request_id=request.id,
            content=message.get("content", ""),
            model_used=data.get("model", request.model),
            tokens_used=data.get("usage", {}).get("total_tokens", 0),
            finish_reason=choice.get("finish_reason", "stop"),
            tool_calls=tool_calls,
        )

    async def generate_stream(
        self, request: AIRequest
    ) -> AsyncIterator[str]:
        """流式生成 AI 响应"""
        headers = {
            "Authorization": f"Bearer {self._api_key}",
            "Content-Type": "application/json",
        }

        messages = []
        if request.system_instruction:
            messages.append({
                "role": "system",
                "content": request.system_instruction
            })
        
        for msg in request.history:
            messages.append({"role": msg.role, "content": msg.content})

        messages.append({"role": "user", "content": request.prompt})

        payload = {
            "model": request.model,
            "messages": messages,
            "max_tokens": request.max_tokens,
            "temperature": request.temperature,
            "stream": True,
        }

        async with httpx.AsyncClient(timeout=120.0) as client:
            async with client.stream(
                "POST",
                f"{self._base_url}/chat/completions",
                headers=headers,
                json=payload
            ) as response:
                async for line in response.aiter_lines():
                    if line.startswith("data: "):
                        data_str = line[6:]
                        if data_str == "[DONE]":
                            break
                        import json
                        data = json.loads(data_str)
                        delta = data["choices"][0].get("delta", {})
                        if content := delta.get("content"):
                            yield content

    async def is_available(self) -> bool:
        """检查提供商是否可用"""
        # 某些兼容接口 (如阿里云百炼 coding endpoint) 不支持 /models 列表
        # 直接返回 True，让后续调用处理错误
        return True

    def supports_model(self, model: str) -> bool:
        """检查是否支持指定模型"""
        # 支持通配符，允许任意模型 (通过 base_url 配置兼容接口)
        if "*" in self.SUPPORTED_MODELS:
            return True
        return model in self.SUPPORTED_MODELS
