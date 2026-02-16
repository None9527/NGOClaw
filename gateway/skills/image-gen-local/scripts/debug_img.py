import os
from openai import OpenAI

# Verify against local API
client = OpenAI(
    base_url="http://127.0.0.1:8045/v1", 
    api_key="sk-0286ab855f464fc3bcd0feda93c1f5ee"
)

try:
    print("Sending request for 4K image...")
    response = client.chat.completions.create(
        model="gemini-3-pro-image",
        messages=[{"role": "user", "content": "A simple red cube, 4k resolution"}],
        extra_body={
            "size": "1024x1024", # Base size (will be scaled by quality)
            "quality": "hd",     # This triggers 4K resolution (4096x4096)
            "aspect_ratio": "1:1"
        }
    )
    content = response.choices[0].message.content
    print(f"DEBUG: Response content type: {type(content)}")
    print(f"DEBUG: Response content length: {len(content)}")
    if len(content) > 100:
        print("DEBUG: Success! Received large payload.")
    else:
        print(f"DEBUG: Content: {content}")
except Exception as e:
    print(f"Error: {e}")
