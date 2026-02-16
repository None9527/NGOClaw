"""Execute Skill Use Case - Application Layer"""

from ...domain.entity.skill import SkillRequest, SkillResponse
from ...domain.repository.skill_registry import SkillRegistry


class ExecuteSkillUseCase:
    """执行技能用例"""

    def __init__(self, skill_registry: SkillRegistry):
        self._skill_registry = skill_registry

    async def execute(self, request: SkillRequest) -> SkillResponse:
        """执行技能"""
        # 1. 获取技能
        skill = self._skill_registry.get_skill(request.skill_id)
        if not skill:
            return SkillResponse(
                output="",
                success=False,
                error_message=f"Skill '{request.skill_id}' not found",
                id=request.id
            )

        # 2. 执行技能
        try:
            output = await skill.execute(request.input, request.config)
            return SkillResponse(
                output=output,
                success=True,
                id=request.id
            )
        except Exception as e:
            return SkillResponse(
                output="",
                success=False,
                error_message=f"Error executing skill: {str(e)}",
                id=request.id
            )
