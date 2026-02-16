"""NGOClaw Python SDK Client

Connects to the NGOClaw Agent Platform via HTTP/SSE.
Supports streaming agent events in real time.
"""

import json
import logging
from typing import AsyncIterator, Iterator, Optional, List

import httpx

from .types import AgentRequest, AgentEvent, AgentResult, ToolDefinition

logger = logging.getLogger(__name__)


class NGOClawClient:
    """NGOClaw Agent Platform client.

    Usage:
        client = NGOClawClient("http://localhost:8080")

        # Streaming
        for event in client.run("Explain this code", model="qwen3-coder-plus"):
            if event.is_text:
                print(event.content, end="")
            elif event.is_done:
                print("\\nDone!")

        # Async streaming
        async for event in client.arun("Explain this code"):
            print(event.content, end="")
    """

    def __init__(
        self,
        base_url: str = "http://localhost:8080",
        api_key: Optional[str] = None,
        timeout: float = 300.0,
    ):
        self.base_url = base_url.rstrip("/")
        self.api_key = api_key
        self.timeout = timeout

    def _headers(self) -> dict:
        headers = {"Content-Type": "application/json"}
        if self.api_key:
            headers["Authorization"] = f"Bearer {self.api_key}"
        return headers

    # --- Synchronous API ---

    def run(
        self,
        message: str,
        system_prompt: str = "",
        model: str = "",
        session_id: str = "",
    ) -> Iterator[AgentEvent]:
        """Run the agent and stream events synchronously."""
        req = AgentRequest(
            message=message,
            system_prompt=system_prompt,
            model=model,
            session_id=session_id,
        )

        with httpx.Client(timeout=self.timeout) as client:
            with client.stream(
                "POST",
                f"{self.base_url}/api/v1/agent",
                json=req.__dict__,
                headers=self._headers(),
            ) as response:
                response.raise_for_status()
                for line in response.iter_lines():
                    event = self._parse_sse_line(line)
                    if event:
                        yield event

    def run_sync(
        self,
        message: str,
        system_prompt: str = "",
        model: str = "",
    ) -> AgentResult:
        """Run the agent and wait for the final result."""
        result = AgentResult()
        for event in self.run(message, system_prompt, model):
            if event.event == "text_delta":
                result.content += event.content
            elif event.event == "complete" or event.event == "done":
                if "total_steps" in event.data:
                    result.total_steps = event.data["total_steps"]
                    result.total_tokens = event.data.get("total_tokens", 0)
                    result.model_used = event.data.get("model_used", "")
                    result.tools_used = event.data.get("tools_used", [])
        return result

    def list_tools(self) -> List[ToolDefinition]:
        """List available tools."""
        with httpx.Client(timeout=30) as client:
            resp = client.get(
                f"{self.base_url}/api/v1/agent/tools",
                headers=self._headers(),
            )
            resp.raise_for_status()
            data = resp.json()
            return [
                ToolDefinition(
                    name=t["name"],
                    description=t.get("description", ""),
                    parameters=t.get("parameters", {}),
                )
                for t in data.get("tools", [])
            ]

    def health(self) -> bool:
        """Check if the server is healthy."""
        try:
            with httpx.Client(timeout=5) as client:
                resp = client.get(f"{self.base_url}/health")
                return resp.status_code == 200
        except Exception:
            return False

    # --- Async API ---

    async def arun(
        self,
        message: str,
        system_prompt: str = "",
        model: str = "",
        session_id: str = "",
    ) -> AsyncIterator[AgentEvent]:
        """Run the agent and stream events asynchronously."""
        req = AgentRequest(
            message=message,
            system_prompt=system_prompt,
            model=model,
            session_id=session_id,
        )

        async with httpx.AsyncClient(timeout=self.timeout) as client:
            async with client.stream(
                "POST",
                f"{self.base_url}/api/v1/agent",
                json=req.__dict__,
                headers=self._headers(),
            ) as response:
                response.raise_for_status()
                async for line in response.aiter_lines():
                    event = self._parse_sse_line(line)
                    if event:
                        yield event

    async def arun_sync(
        self,
        message: str,
        system_prompt: str = "",
        model: str = "",
    ) -> AgentResult:
        """Run the agent async and wait for the final result."""
        result = AgentResult()
        async for event in self.arun(message, system_prompt, model):
            if event.event == "text_delta":
                result.content += event.content
            elif event.event in ("complete", "done"):
                if "total_steps" in event.data:
                    result.total_steps = event.data["total_steps"]
                    result.total_tokens = event.data.get("total_tokens", 0)
                    result.model_used = event.data.get("model_used", "")
                    result.tools_used = event.data.get("tools_used", [])
        return result

    # --- SSE Parser ---

    @staticmethod
    def _parse_sse_line(line: str) -> Optional[AgentEvent]:
        """Parse a single SSE line into an AgentEvent."""
        if not line or not line.startswith("data:"):
            if line and line.startswith("event:"):
                return None  # event type line, skip
            return None

        data_str = line[5:].strip()
        if data_str == "[DONE]":
            return AgentEvent(event="done")

        try:
            data = json.loads(data_str)
            event_type = data.get("event", data.get("type", "unknown"))
            event_data = data.get("data", data)
            return AgentEvent(event=event_type, data=event_data)
        except json.JSONDecodeError:
            return None
