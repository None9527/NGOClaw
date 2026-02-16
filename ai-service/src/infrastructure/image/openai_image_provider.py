"""OpenAI Image Provider - Infrastructure Layer"""

from typing import List, Optional
from openai import AsyncOpenAI

from ...domain.repository.image_provider import ImageProvider
from ...domain.entity.image import ImageRequest, ImageResponse


class OpenAIImageProvider(ImageProvider):
    """OpenAI 图像生成提供商实现"""

    SUPPORTED_MODELS = [
        "dall-e-3",
        "dall-e-2",
    ]

    def __init__(self, api_key: str, base_url: Optional[str] = None):
        """初始化 OpenAI 图像提供商"""
        self._client = AsyncOpenAI(
            api_key=api_key,
            base_url=base_url,
        )

    async def generate_image(self, request: ImageRequest) -> ImageResponse:
        """生成图像"""
        response = await self._client.images.generate(
            model=request.model,
            prompt=request.prompt,
            n=request.num_images,
            size=f"{request.width}x{request.height}",
            response_format="url",
        )

        image_urls = [data.url for data in response.data if data.url]

        return ImageResponse(
            request_id=request.id,
            image_urls=image_urls,
            model_used=request.model,
        )

    def supports_model(self, model: str) -> bool:
        """检查是否支持指定模型"""
        return model in self.SUPPORTED_MODELS
