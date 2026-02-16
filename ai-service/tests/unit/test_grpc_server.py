import pytest
import sys
from pathlib import Path
from unittest.mock import AsyncMock, MagicMock

# Add src to path
project_root = Path(__file__).parent.parent.parent
sys.path.insert(0, str(project_root))

from src.infrastructure.grpc_server.ai_service_server import AIServiceServicer
from src.generated import ai_service_pb2
from src.domain.entity.ai_request import AIResponse

@pytest.mark.asyncio
async def test_generate_success():
    # Setup
    mock_use_case = AsyncMock()
    servicer = AIServiceServicer(generate_use_case=mock_use_case)

    # Mock behavior
    expected_response = AIResponse(
        request_id="req-123",
        content="Hello from AI",
        model_used="provider/model",
        tokens_used=10,
        finish_reason="stop"
    )
    mock_use_case.execute.return_value = expected_response

    # Input
    request = ai_service_pb2.GenerateRequest(
        prompt="Hello",
        model="gemini-3-pro",
        provider="antigravity",
        max_tokens=100,
        temperature=0.7
    )
    context = MagicMock()

    # Execute
    response = await servicer.Generate(request, context)

    # Verify
    assert response.content == "Hello from AI"
    assert response.model_used == "provider/model"
    assert response.tokens_used == 10

    # Verify use case was called with correct parameters
    mock_use_case.execute.assert_called_once()
    call_args = mock_use_case.execute.call_args[0][0]
    assert call_args.prompt == "Hello"
    assert call_args.model == "gemini-3-pro"
    assert call_args.provider == "antigravity"

@pytest.mark.asyncio
async def test_health_check():
    # Setup
    mock_use_case = AsyncMock()
    servicer = AIServiceServicer(generate_use_case=mock_use_case)

    request = ai_service_pb2.HealthCheckRequest()
    context = MagicMock()

    # Execute
    response = await servicer.HealthCheck(request, context)

    # Verify
    assert response.status == ai_service_pb2.HealthCheckResponse.SERVING
    assert response.providers_status["gemini"] is True
