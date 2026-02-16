"""AI Service gRPC Server - Infrastructure Layer"""

import uuid
import logging
from typing import Optional
import grpc
from grpc import aio

# 导入生成的 gRPC 代码
from ...generated import ai_service_pb2
from ...generated import ai_service_pb2_grpc

# 导入应用层用例
from ...application.usecase.generate_response import GenerateResponseUseCase
from ...application.usecase.generate_image import GenerateImageUseCase
from ...application.usecase.execute_skill import ExecuteSkillUseCase

# 导入领域实体
from ...domain.entity.ai_request import AIRequest, ChatMessage
from ...domain.entity.image import ImageRequest
from ...domain.entity.skill import SkillRequest

logger = logging.getLogger(__name__)


class AIServiceServicer(ai_service_pb2_grpc.AIServiceServicer):
    """AI Service gRPC 服务端实现

    实现 AI Service 的 gRPC 接口，处理来自 Gateway 的请求
    """

    def __init__(
        self,
        generate_use_case: GenerateResponseUseCase,
        generate_image_use_case: Optional[GenerateImageUseCase] = None,
        execute_skill_use_case: Optional[ExecuteSkillUseCase] = None,
    ):
        """初始化服务端

        Args:
            generate_use_case: 生成响应用例
            generate_image_use_case: 生成图像用例 (可选)
            execute_skill_use_case: 执行技能用例 (可选)
        """
        self._generate_use_case = generate_use_case
        self._generate_image_use_case = generate_image_use_case
        self._execute_skill_use_case = execute_skill_use_case
        logger.info("AIServiceServicer initialized")

    async def Generate(
        self,
        request: ai_service_pb2.GenerateRequest,
        context: grpc.aio.ServicerContext
    ) -> ai_service_pb2.GenerateResponse:
        """生成 AI 响应（非流式）

        Args:
            request: gRPC 请求
            context: gRPC 上下文

        Returns:
            gRPC 响应
        """
        try:
            logger.info(f"Received Generate request: provider={request.provider}, model={request.model}")

            # 转换 gRPC 请求为领域实体
            ai_request = self._to_domain_request(request)

            # 执行用例
            ai_response = await self._generate_use_case.execute(ai_request)

            # 转换领域响应为 gRPC 响应
            grpc_response = ai_service_pb2.GenerateResponse(
                request_id=ai_response.request_id,
                content=ai_response.content,
                model_used=ai_response.model_used,
                tokens_used=ai_response.tokens_used,
                finish_reason=ai_response.finish_reason,
                metadata=ai_response.metadata or {},
            )

            logger.info(f"Generated response: request_id={ai_response.request_id}, tokens={ai_response.tokens_used}")
            return grpc_response

        except ValueError as e:
            logger.error(f"Invalid request: {e}")
            await context.abort(grpc.StatusCode.INVALID_ARGUMENT, str(e))
        except RuntimeError as e:
            logger.error(f"Provider error: {e}")
            await context.abort(grpc.StatusCode.UNAVAILABLE, str(e))
        except Exception as e:
            logger.error(f"Unexpected error: {e}", exc_info=True)
            await context.abort(grpc.StatusCode.INTERNAL, "Internal server error")

    async def GenerateStream(
        self,
        request: ai_service_pb2.GenerateRequest,
        context: grpc.aio.ServicerContext
    ):
        """流式生成 AI 响应

        Args:
            request: gRPC 请求
            context: gRPC 上下文

        Yields:
            流式响应块
        """
        try:
            logger.info(f"Received GenerateStream request: provider={request.provider}, model={request.model}")

            # 转换请求
            ai_request = self._to_domain_request(request)
            request_id = ai_request.id

            # 流式执行用例
            async for chunk in self._generate_use_case.execute_stream(ai_request):
                yield ai_service_pb2.GenerateStreamChunk(
                    request_id=request_id,
                    content=chunk,
                    is_final=False,
                )

            # 发送最终块
            yield ai_service_pb2.GenerateStreamChunk(
                request_id=request_id,
                content="",
                is_final=True,
            )

            logger.info(f"Stream completed: request_id={request_id}")

        except ValueError as e:
            logger.error(f"Invalid request: {e}")
            await context.abort(grpc.StatusCode.INVALID_ARGUMENT, str(e))
        except RuntimeError as e:
            logger.error(f"Provider error: {e}")
            await context.abort(grpc.StatusCode.UNAVAILABLE, str(e))
        except Exception as e:
            logger.error(f"Unexpected error in stream: {e}", exc_info=True)
            await context.abort(grpc.StatusCode.INTERNAL, "Internal server error")

    async def GenerateImage(
        self,
        request: ai_service_pb2.ImageRequest,
        context: grpc.aio.ServicerContext
    ) -> ai_service_pb2.ImageResponse:
        """生成图像

        Args:
            request: 图像请求
            context: gRPC 上下文

        Returns:
            图像响应
        """
        if not self._generate_image_use_case:
            logger.warning("GenerateImage use case not initialized")
            await context.abort(grpc.StatusCode.UNIMPLEMENTED, "Image generation not configured")

        try:
            logger.info(f"Received GenerateImage request: prompt='{request.prompt[:50]}...', model={request.model}")

            # 转换请求
            image_request = ImageRequest(
                id=str(uuid.uuid4()),
                prompt=request.prompt,
                model=request.model,
                width=request.width if request.width > 0 else 1024,
                height=request.height if request.height > 0 else 1024,
                num_images=request.num_images if request.num_images > 0 else 1,
            )

            # 执行用例
            image_response = await self._generate_image_use_case.execute(image_request)

            # 转换响应
            return ai_service_pb2.ImageResponse(
                image_urls=image_response.image_urls,
                model_used=image_response.model_used,
            )

        except ValueError as e:
            logger.error(f"Invalid image request: {e}")
            await context.abort(grpc.StatusCode.INVALID_ARGUMENT, str(e))
        except RuntimeError as e:
            logger.error(f"Image provider error: {e}")
            await context.abort(grpc.StatusCode.UNAVAILABLE, str(e))
        except Exception as e:
            logger.error(f"Unexpected error in GenerateImage: {e}", exc_info=True)
            await context.abort(grpc.StatusCode.INTERNAL, "Internal server error")

    async def ExecuteSkill(
        self,
        request: ai_service_pb2.SkillRequest,
        context: grpc.aio.ServicerContext
    ) -> ai_service_pb2.SkillResponse:
        """执行技能

        Args:
            request: 技能请求
            context: gRPC 上下文

        Returns:
            技能响应
        """
        if not self._execute_skill_use_case:
            logger.warning("ExecuteSkill use case not initialized")
            await context.abort(grpc.StatusCode.UNIMPLEMENTED, "Skill execution not configured")

        try:
            logger.info(f"Received ExecuteSkill request: skill_id={request.skill_id}")

            # 转换请求
            skill_request = SkillRequest(
                id=str(uuid.uuid4()),
                skill_id=request.skill_id,
                input=request.input,
                config=dict(request.config) if request.config else {},
            )

            # 执行用例
            skill_response = await self._execute_skill_use_case.execute(skill_request)

            # 转换响应
            return ai_service_pb2.SkillResponse(
                output=skill_response.output,
                success=skill_response.success,
                error_message=skill_response.error_message or "",
            )

        except ValueError as e:
            logger.error(f"Invalid skill request: {e}")
            await context.abort(grpc.StatusCode.INVALID_ARGUMENT, str(e))
        except Exception as e:
            logger.error(f"Unexpected error in ExecuteSkill: {e}", exc_info=True)
            await context.abort(grpc.StatusCode.INTERNAL, "Internal server error")

    async def HealthCheck(
        self,
        request: ai_service_pb2.HealthCheckRequest,
        context: grpc.aio.ServicerContext
    ) -> ai_service_pb2.HealthCheckResponse:
        """健康检查

        Args:
            request: 健康检查请求
            context: gRPC 上下文

        Returns:
            健康检查响应
        """
        logger.debug("Health check request received")

        # Check actual provider availability
        providers_status = {}
        if hasattr(self, '_generate_service') and hasattr(self._generate_service, '_router'):
            for name, provider in self._generate_service._router._providers.items():
                try:
                    available = await provider.is_available()
                    providers_status[name] = available
                except Exception:
                    providers_status[name] = False
        else:
            providers_status["unknown"] = True

        return ai_service_pb2.HealthCheckResponse(
            status=ai_service_pb2.HealthCheckResponse.SERVING,
            version="0.1.0",
            providers_status=providers_status,
        )

    def _to_domain_request(self, grpc_request: ai_service_pb2.GenerateRequest) -> AIRequest:
        """转换 gRPC 请求为领域实体

        Args:
            grpc_request: gRPC 请求

        Returns:
            领域请求实体
        """
        history = []
        for msg in grpc_request.history:
            history.append(ChatMessage(
                role=msg.role,
                content=msg.content,
                name=msg.name if msg.name else None
            ))

        return AIRequest(
            id=str(uuid.uuid4()),
            prompt=grpc_request.prompt,
            model=grpc_request.model,
            provider=grpc_request.provider,
            max_tokens=grpc_request.max_tokens if grpc_request.max_tokens > 0 else 4096,
            temperature=grpc_request.temperature if grpc_request.temperature > 0 else 0.7,
            top_p=grpc_request.top_p if grpc_request.top_p > 0 else 1.0,
            metadata=dict(grpc_request.metadata) if grpc_request.metadata else {},
            history=history,
            system_instruction=grpc_request.system_instruction if grpc_request.system_instruction else None,
        )


async def create_grpc_server(
    generate_use_case: GenerateResponseUseCase,
    generate_image_use_case: Optional[GenerateImageUseCase] = None,
    execute_skill_use_case: Optional[ExecuteSkillUseCase] = None,
    port: int = 50051,
    max_workers: int = 10,
) -> aio.Server:
    """创建并配置 gRPC 服务器

    Args:
        generate_use_case: 生成响应用例
        generate_image_use_case: 生成图像用例
        execute_skill_use_case: 执行技能用例
        port: 监听端口
        max_workers: 最大工作线程数

    Returns:
        配置好的 gRPC 服务器
    """
    server = aio.server()

    # 注册服务
    ai_service_pb2_grpc.add_AIServiceServicer_to_server(
        AIServiceServicer(
            generate_use_case,
            generate_image_use_case,
            execute_skill_use_case
        ),
        server
    )

    # 绑定端口
    listen_addr = f"[::]:{port}"
    server.add_insecure_port(listen_addr)

    logger.info(f"gRPC server configured on {listen_addr}")
    return server
