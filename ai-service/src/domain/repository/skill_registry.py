"""Skill Registry Repository Interface - Domain Layer"""

from abc import ABC, abstractmethod
from typing import List, Optional
from ..entity.skill import Skill


class SkillRegistry(ABC):
    """技能注册表接口"""

    @abstractmethod
    def get_skill(self, skill_id: str) -> Optional[Skill]:
        """获取技能

        Args:
            skill_id: 技能ID

        Returns:
            技能实例或 None
        """
        pass

    @abstractmethod
    def list_skills(self) -> List[Skill]:
        """列出所有技能"""
        pass

    @abstractmethod
    def register_skill(self, skill: Skill) -> None:
        """注册技能"""
        pass
