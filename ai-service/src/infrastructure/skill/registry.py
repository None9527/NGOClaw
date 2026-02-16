"""In-Memory Skill Registry - Infrastructure Layer"""

from typing import Dict, List, Optional
from ...domain.repository.skill_registry import SkillRegistry
from ...domain.entity.skill import Skill


class InMemorySkillRegistry(SkillRegistry):
    """内存技能注册表"""

    def __init__(self):
        self._skills: Dict[str, Skill] = {}

    def get_skill(self, skill_id: str) -> Optional[Skill]:
        """获取技能"""
        return self._skills.get(skill_id)

    def list_skills(self) -> List[Skill]:
        """列出所有技能"""
        return list(self._skills.values())

    def register_skill(self, skill: Skill) -> None:
        """注册技能"""
        # 使用技能名称作为ID，或者增加专门的ID字段
        # 这里假设 name 是唯一的标识符
        self._skills[skill.name] = skill
