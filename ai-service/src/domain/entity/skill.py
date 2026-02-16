"""Skill Entity - Domain Layer"""

from abc import ABC, abstractmethod
from dataclasses import dataclass, field
from datetime import datetime
from typing import Dict, Any, Optional
from uuid import uuid4


@dataclass
class SkillRequest:
    """技能执行请求"""

    skill_id: str
    input: str
    config: Dict[str, str] = field(default_factory=dict)
    id: str = field(default_factory=lambda: str(uuid4()))
    created_at: datetime = field(default_factory=datetime.now)


@dataclass
class SkillResponse:
    """技能执行响应"""

    output: str
    success: bool
    error_message: Optional[str] = None
    id: str = field(default_factory=lambda: str(uuid4()))
    created_at: datetime = field(default_factory=datetime.now)


class Skill(ABC):
    """技能接口 (抽象基类)"""

    @property
    @abstractmethod
    def name(self) -> str:
        """技能名称"""
        pass

    @property
    @abstractmethod
    def description(self) -> str:
        """技能描述"""
        pass

    @abstractmethod
    async def execute(self, input_text: str, config: Dict[str, Any]) -> str:
        """执行技能

        Args:
            input_text: 输入文本
            config: 配置信息

        Returns:
            执行结果
        """
        pass
