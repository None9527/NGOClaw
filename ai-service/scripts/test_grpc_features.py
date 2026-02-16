import asyncio
import grpc
import sys
import os

# Add src to path
sys.path.append(os.path.join(os.path.dirname(__file__), ".."))

from src.generated import ai_service_pb2
from src.generated import ai_service_pb2_grpc

async def run():
    async with grpc.aio.insecure_channel('localhost:50051') as channel:
        stub = ai_service_pb2_grpc.AIServiceStub(channel)

        print("--- Testing Generate ---")
        try:
            response = await stub.Generate(ai_service_pb2.GenerateRequest(
                prompt="Hello, world!",
                model="gemini-3-flash",
                provider="antigravity"
            ))
            print(f"Generate Response: {response.content}")
        except Exception as e:
            print(f"Generate failed: {e}")

        print("\n--- Testing ExecuteSkill (Time) ---")
        try:
            response = await stub.ExecuteSkill(ai_service_pb2.SkillRequest(
                skill_id="current_time",
                input=""
            ))
            print(f"Time Skill Response: {response.output}")
        except Exception as e:
            print(f"ExecuteSkill failed: {e}")

        print("\n--- Testing ExecuteSkill (Calculator) ---")
        try:
            response = await stub.ExecuteSkill(ai_service_pb2.SkillRequest(
                skill_id="calculator",
                input="1 + 2 * 3"
            ))
            print(f"Calculator Skill Response: {response.output}")
        except Exception as e:
            print(f"ExecuteSkill failed: {e}")

        print("\n--- Testing GenerateImage (Mock/OpenAI) ---")
        # Note: This might fail if OpenAI API key is not set, but we want to see it reach the handler
        try:
            response = await stub.GenerateImage(ai_service_pb2.ImageRequest(
                prompt="A cute cat",
                model="dall-e-3",
                width=1024,
                height=1024,
                num_images=1
            ))
            print(f"GenerateImage Response: {response.image_urls}")
        except Exception as e:
            print(f"GenerateImage failed (expected if no API key): {e}")

if __name__ == '__main__':
    asyncio.run(run())
