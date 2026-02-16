"""AI Request Entity - Domain Layer"""

from dataclasses import dataclass, field
from datetime import datetime
from typing import Dict, Any, Optional, List
from uuid import uuid4


@dataclass
class ChatMessage:
    """聊天消息实体"""
    role: str
    content: str
    name: Optional[str] = None


@dataclass
class AIRequest:
    """AI 请求实体（聚合根）"""

    id: str = field(default_factory=lambda: str(uuid4()))
    prompt: str = ""
    model: str = "gemini-3-pro-low"
    provider: str = "antigravity"
    max_tokens: int = 8192
    temperature: float = 0.7
    top_p: float = 0.95
    metadata: Dict[str, Any] = field(default_factory=dict)
    history: List[ChatMessage] = field(default_factory=list)
    system_instruction: Optional[str] = None
    created_at: datetime = field(default_factory=datetime.now)

    def __post_init__(self) -> None:
        """验证实体的不变量"""
        if not self.prompt:
            raise ValueError("Prompt cannot be empty")

        if self.max_tokens <= 0:
            raise ValueError("Max tokens must be positive")

        if not (0.0 <= self.temperature <= 2.0):
            raise ValueError("Temperature must be between 0.0 and 2.0")

        if not (0.0 <= self.top_p <= 1.0):
            raise ValueError("Top-p must be between 0.0 and 1.0")

    @property
    def full_model_name(self) -> str:
        """返回完整的模型名称"""
        return f"{self.provider}/{self.model}"

    def set_metadata(self, key: str, value: Any) -> None:
        """设置元数据"""
        self.metadata[key] = value

    def get_metadata(self, key: str, default: Any = None) -> Any:
        """获取元数据"""
        return self.metadata.get(key, default)


@dataclass
class AIResponse:
    """AI 响应实体"""

    id: str = field(default_factory=lambda: str(uuid4()))
    request_id: str = ""
    content: str = ""
    model_used: str = ""
    tokens_used: int = 0
    finish_reason: str = "stop"
    metadata: Dict[str, Any] = field(default_factory=dict)
    created_at: datetime = field(default_factory=datetime.now)

    def __post_init__(self) -> None:
        """验证实体的不变量"""
        if not self.request_id:
            raise ValueError("Request ID cannot be empty")

        if not self.content:
            raise ValueError("Content cannot be empty")

    @property
    def is_complete(self) -> bool:
        """判断响应是否完整"""
        return self.finish_reason in ["stop", "end_turn"]

    @property
    def was_truncated(self) -> bool:
        """判断响应是否被截断"""
        return self.finish_reason == "length"
