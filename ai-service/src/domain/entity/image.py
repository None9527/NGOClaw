"""Image Entities - Domain Layer"""

from dataclasses import dataclass, field
from datetime import datetime
from typing import List, Optional
from uuid import uuid4


@dataclass
class ImageRequest:
    """图像生成请求实体"""

    id: str = field(default_factory=lambda: str(uuid4()))
    prompt: str = ""
    model: str = "dall-e-3"
    provider: str = "openai"
    width: int = 1024
    height: int = 1024
    num_images: int = 1
    created_at: datetime = field(default_factory=datetime.now)

    def __post_init__(self) -> None:
        """验证实体"""
        if not self.prompt:
            raise ValueError("Prompt cannot be empty")

        if self.width <= 0 or self.height <= 0:
            raise ValueError("Dimensions must be positive")

        if self.num_images <= 0:
            raise ValueError("Number of images must be positive")


@dataclass
class ImageResponse:
    """图像生成响应实体"""

    id: str = field(default_factory=lambda: str(uuid4()))
    request_id: str = ""
    image_urls: List[str] = field(default_factory=list)
    model_used: str = ""
    created_at: datetime = field(default_factory=datetime.now)

    def __post_init__(self) -> None:
        if not self.request_id:
            raise ValueError("Request ID cannot be empty")
