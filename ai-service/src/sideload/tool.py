"""Sideload Tool Handler

Handles 'tool/execute' JSON-RPC calls by delegating to the
existing SkillRegistry infrastructure.
"""

import logging
from typing import Dict, Any

from ..infrastructure.skill.registry import InMemorySkillRegistry

logger = logging.getLogger(__name__)


class ToolHandler:
    """Handles tool/execute requests via the existing SkillRegistry."""

    def __init__(self, skill_registry: InMemorySkillRegistry):
        self._registry = skill_registry

    async def handle_execute(self, params: dict) -> dict:
        """Handle a tool/execute JSON-RPC request.

        Params match the Go-side ToolExecuteParams struct:
        {
            "name": "stock_analysis",
            "arguments": {"symbol": "300383", "period": "daily"},
            "context": {"session_id": "abc"}
        }
        """
        name = params.get("name", "")
        arguments = params.get("arguments", {})

        # Look up skill
        skill = self._registry.get_skill(name)
        if skill is None:
            return {
                "output": f"Tool '{name}' not found",
                "success": False,
                "error": "tool_not_found",
            }

        try:
            # Skills take (input_text: str, config: dict)
            input_text = arguments.get("input", arguments.get("text", ""))
            config = {k: v for k, v in arguments.items() if k not in ("input", "text")}
            result = await skill.execute(input_text, config)
            return {
                "output": str(result),
                "success": True,
            }
        except Exception as e:
            logger.exception(f"Tool execute error for '{name}'")
            return {
                "output": f"Error executing '{name}': {e}",
                "success": False,
                "error": str(e),
            }

    def get_capabilities(self) -> list:
        """Return tool capabilities for the initialize response."""
        caps = []
        for skill in self._registry.list_skills():
            caps.append({
                "name": skill.name,
                "description": skill.description,
            })
        return caps
