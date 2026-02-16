"""NGOClaw Python SDK

Provides a Python client for the NGOClaw Agent Platform.
Supports both HTTP/SSE (streaming agent events) and gRPC transports.
"""

__version__ = "0.1.0"

from .client import NGOClawClient
from .types import AgentRequest, AgentEvent, ToolDefinition
