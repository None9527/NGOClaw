"""Basic Skills Implementation"""

import datetime
from typing import Dict, Any
from ...domain.entity.skill import Skill


class TimeSkill(Skill):
    """时间技能"""

    @property
    def name(self) -> str:
        return "current_time"

    @property
    def description(self) -> str:
        return "Get the current time and date."

    async def execute(self, input_text: str, config: Dict[str, Any]) -> str:
        return datetime.datetime.now().strftime("%Y-%m-%d %H:%M:%S")


class EchoSkill(Skill):
    """回显技能 (测试用)"""

    @property
    def name(self) -> str:
        return "echo"

    @property
    def description(self) -> str:
        return "Echoes back the input text."

    async def execute(self, input_text: str, config: Dict[str, Any]) -> str:
        return f"Echo: {input_text}"


class CalculatorSkill(Skill):
    """简单计算器技能 (仅支持基本运算)"""

    @property
    def name(self) -> str:
        return "calculator"

    @property
    def description(self) -> str:
        return "Evaluates a simple mathematical expression."

    async def execute(self, input_text: str, config: Dict[str, Any]) -> str:
        try:
            # 注意：eval 有安全风险，生产环境应使用安全的表达式解析库
            # 这里仅作演示
            allowed_chars = "0123456789+-*/(). "
            if not all(c in allowed_chars for c in input_text):
                return "Error: Invalid characters in expression"

            result = eval(input_text, {"__builtins__": None}, {})
            return str(result)
        except Exception as e:
            return f"Error: {str(e)}"
