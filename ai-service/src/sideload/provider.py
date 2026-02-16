"""Sideload Provider Handler

Handles 'provider/generate' JSON-RPC calls by delegating to the
existing ModelRouter and AIProvider infrastructure.
"""

import logging
from typing import Dict, Any

from ..domain.entity.ai_request import AIRequest, AIResponse, ChatMessage
from ..domain.service.model_router import ModelRouter

logger = logging.getLogger(__name__)


class ProviderHandler:
    """Handles provider/generate requests via the existing ModelRouter."""

    def __init__(self, model_router: ModelRouter, config):
        self._router = model_router
        self._config = config

    async def handle_generate(self, params: dict) -> dict:
        """Handle a provider/generate JSON-RPC request.

        Params match the Go-side GenerateParams struct:
        {
            "provider": "bailian",
            "model": "qwen3-coder-plus",
            "messages": [{"role": "user", "content": "hello"}],
            "tools": [...],
            "stream": false,
            "options": {"temperature": 0.7, ...}
        }
        """
        provider_name = params.get("provider", "antigravity")
        model = params.get("model", "")
        messages = params.get("messages", [])
        options = params.get("options", {})

        # Build prompt from messages
        prompt = ""
        history = []
        system_instruction = None

        for msg in messages:
            role = msg.get("role", "user")
            content = msg.get("content", "")
            if role == "system":
                system_instruction = content
            elif role == "user":
                if not prompt:
                    prompt = content
                else:
                    history.append(ChatMessage(role="user", content=content))
            elif role == "assistant":
                history.append(ChatMessage(role="assistant", content=content))
            elif role == "tool":
                history.append(ChatMessage(
                    role="tool", content=content, name=msg.get("name")
                ))

        # If no user message was found, use last message as prompt
        if not prompt and history:
            last = history.pop()
            prompt = last.content

        if not prompt:
            return {
                "content": "",
                "finish_reason": "error",
                "model_used": model,
                "tokens_used": 0,
            }

        # Build AIRequest
        request = AIRequest(
            prompt=prompt,
            model=model,
            provider=provider_name,
            max_tokens=options.get("max_tokens", 8192),
            temperature=options.get("temperature", 0.7),
            history=history,
            system_instruction=system_instruction,
        )

        try:
            provider = self._router.get_provider(request)
            response: AIResponse = await provider.generate(request)

            return {
                "content": response.content,
                "finish_reason": response.finish_reason,
                "model_used": response.model_used,
                "tokens_used": response.tokens_used,
            }
        except Exception as e:
            logger.exception(f"Provider generate error: {e}")
            return {
                "content": f"Error: {e}",
                "finish_reason": "error",
                "model_used": model,
                "tokens_used": 0,
            }

    def get_capabilities(self) -> list:
        """Return provider capabilities for the initialize response."""
        caps = []

        if not hasattr(self._config, "providers"):
            return caps

        for name, prov_config in self._config.providers.items():
            if not prov_config.enabled:
                continue
            models = list(prov_config.models) if prov_config.models else []
            caps.append({
                "id": name,
                "models": models,
            })

        return caps
