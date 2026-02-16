"""Generate Image Use Case - Application Layer"""

from ...domain.entity.image import ImageRequest, ImageResponse
from ...domain.service.image_router import ImageRouter


class GenerateImageUseCase:
    """生成图像用例"""

    def __init__(self, image_router: ImageRouter):
        self._image_router = image_router

    async def execute(self, request: ImageRequest) -> ImageResponse:
        """执行生成图像"""
        # 1. 获取提供商
        provider = self._image_router.get_provider(request)

        # 2. 生成图像
        response = await provider.generate_image(request)

        return response
