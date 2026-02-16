"""NGOClaw SDK Types"""

from dataclasses import dataclass, field
from typing import Dict, Any, List, Optional


@dataclass
class AgentRequest:
    """Request to run the agent loop."""
    message: str
    system_prompt: str = ""
    model: str = ""
    session_id: str = ""
    history: List[Dict[str, str]] = field(default_factory=list)


@dataclass
class AgentEvent:
    """An event streamed from the agent loop."""
    event: str    # thinking, text_delta, tool_call, tool_result, step_done, error, done
    data: Dict[str, Any] = field(default_factory=dict)

    @property
    def is_text(self) -> bool:
        return self.event in ("text_delta", "thinking")

    @property
    def content(self) -> str:
        return self.data.get("content", "")

    @property
    def is_done(self) -> bool:
        return self.event in ("done", "complete")

    @property
    def is_error(self) -> bool:
        return self.event == "error"

    @property
    def error_message(self) -> str:
        return self.data.get("error", "")


@dataclass
class ToolDefinition:
    """Describes an available tool."""
    name: str
    description: str
    parameters: Dict[str, Any] = field(default_factory=dict)


@dataclass
class AgentResult:
    """Final result after the agent loop completes."""
    content: str = ""
    total_steps: int = 0
    total_tokens: int = 0
    model_used: str = ""
    tools_used: List[str] = field(default_factory=list)
