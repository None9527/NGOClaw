"""Ollama AI Provider - Infrastructure Layer"""

from typing import AsyncIterator, Optional
import httpx
import json

from ...domain.repository.ai_provider import AIProvider
from ...domain.entity.ai_request import AIRequest, AIResponse


class OllamaProvider(AIProvider):
    """Ollama 本地/远程 LLM 提供商实现"""

    # 常用 Ollama 模型
    SUPPORTED_MODELS = [
        "llama3.1",
        "llama3.2",
        "llama3.3",
        "qwen2.5",
        "qwen2.5-coder",
        "deepseek-r1",
        "mistral",
        "mixtral",
        "codellama",
        "phi3",
        "gemma2",
        # 支持任意模型 (通过 /api/tags 查询)
        "*",
    ]

    def __init__(
        self,
        base_url: str = "http://localhost:11434",
        timeout: float = 300.0,
    ):
        """初始化 Ollama 提供商

        Args:
            base_url: Ollama 服务地址 (默认本地)
            timeout: 请求超时 (秒)
        """
        self._base_url = base_url.rstrip("/")
        self._timeout = timeout

    async def generate(self, request: AIRequest) -> AIResponse:
        """生成 AI 响应"""
        # Ollama 使用 /api/chat 端点
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

        payload = {
            "model": request.model,
            "messages": messages,
            "stream": False,
            "options": {
                "temperature": request.temperature,
                "num_predict": request.max_tokens,
            }
        }

        async with httpx.AsyncClient(timeout=self._timeout) as client:
            response = await client.post(
                f"{self._base_url}/api/chat",
                json=payload
            )
            response.raise_for_status()
            data = response.json()

        message = data.get("message", {})
        
        return AIResponse(
            request_id=request.id,
            content=message.get("content", ""),
            model_used=data.get("model", request.model),
            tokens_used=data.get("eval_count", 0) + data.get("prompt_eval_count", 0),
            finish_reason="stop" if data.get("done") else "length",
        )

    async def generate_stream(
        self, request: AIRequest
    ) -> AsyncIterator[str]:
        """流式生成 AI 响应"""
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
            "stream": True,
            "options": {
                "temperature": request.temperature,
                "num_predict": request.max_tokens,
            }
        }

        async with httpx.AsyncClient(timeout=self._timeout) as client:
            async with client.stream(
                "POST",
                f"{self._base_url}/api/chat",
                json=payload
            ) as response:
                async for line in response.aiter_lines():
                    if line:
                        data = json.loads(line)
                        if message := data.get("message"):
                            if content := message.get("content"):
                                yield content
                        if data.get("done"):
                            break

    async def is_available(self) -> bool:
        """检查提供商是否可用"""
        try:
            async with httpx.AsyncClient(timeout=5.0) as client:
                response = await client.get(f"{self._base_url}/api/tags")
                return response.status_code == 200
        except Exception:
            return False

    def supports_model(self, model: str) -> bool:
        """检查是否支持指定模型"""
        # 支持任意 Ollama 模型
        return True

    async def list_models(self) -> list[str]:
        """列出可用模型"""
        try:
            async with httpx.AsyncClient(timeout=10.0) as client:
                response = await client.get(f"{self._base_url}/api/tags")
                if response.status_code == 200:
                    data = response.json()
                    return [m["name"] for m in data.get("models", [])]
        except Exception:
            pass
        return []

    async def pull_model(self, model: str) -> bool:
        """拉取模型"""
        try:
            async with httpx.AsyncClient(timeout=3600.0) as client:
                response = await client.post(
                    f"{self._base_url}/api/pull",
                    json={"name": model}
                )
                return response.status_code == 200
        except Exception:
            return False
