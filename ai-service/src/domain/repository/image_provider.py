"""Image Provider Repository Interface - Domain Layer"""

from abc import ABC, abstractmethod
from ..entity.image import ImageRequest, ImageResponse


class ImageProvider(ABC):
    """图像生成提供商接口"""

    @abstractmethod
    async def generate_image(self, request: ImageRequest) -> ImageResponse:
        """生成图像

        Args:
            request: 图像生成请求

        Returns:
            图像生成响应
        """
        pass

    @abstractmethod
    def supports_model(self, model: str) -> bool:
        """检查是否支持指定模型"""
        pass
