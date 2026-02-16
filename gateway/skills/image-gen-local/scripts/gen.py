#!/usr/bin/env python3
"""
Image generation via local OpenAI-compatible API (Gemini image model).

Usage:
    # 纯文本生成
    python3 gen.py "a futuristic city"

    # 基于图片生成（转换风格）
    python3 gen.py "anime style" --image /path/to/image.jpg

    # 带参数
    python3 gen.py "anime style" --image /path/to/image.jpg --resolution 2k --aspect-ratio 16:9
"""

import argparse
import base64
import os
import sys
import math
from datetime import datetime
from pathlib import Path
from openai import OpenAI

DEFAULT_BASE_URL = "http://127.0.0.1:8045/v1"
DEFAULT_API_KEY = "sk-0286ab855f464fc3bcd0feda93c1f5ee"
DEFAULT_MODEL = "gemini-3-pro-image"

# Default fallback if resolution/aspect-ratio are not provided
DEFAULT_SIZE = "1024x1024"

# Output directory relative to workspace
OUTPUT_DIR = Path("/home/none/clawd/tmp/image-gen")
OUTPUT_DIR.mkdir(parents=True, exist_ok=True)

# Define base resolution values
RESOLUTIONS = {
    "1k": 1024,
    "2k": 2048,
    "4k": 4096,
}

# Define quality mapping
QUALITY_MAP = {
    "1k": "standard",
    "2k": "medium",
    "4k": "hd",
}

# Define aspect ratios (width_ratio, height_ratio)
ASPECT_RATIOS = {
    "1:1": (1, 1),
    "3:4": (3, 4),
    "4:3": (4, 3),
    "21:9": (21, 9),
    "9:21": (9, 21),
    "16:9": (16, 9),
    "9:16": (9, 16),
}


def calculate_size(resolution: str, aspect_ratio: str) -> str:
    """Calculate width x height string from resolution and aspect ratio."""
    if resolution not in RESOLUTIONS:
        raise ValueError(f"Invalid resolution: {resolution}. Must be one of {list(RESOLUTIONS.keys())}")
    if aspect_ratio not in ASPECT_RATIOS:
        raise ValueError(f"Invalid aspect ratio: {aspect_ratio}. Must be one of {list(ASPECT_RATIOS.keys())}")

    base_res = RESOLUTIONS[resolution]
    width_ratio, height_ratio = ASPECT_RATIOS[aspect_ratio]

    # Prioritize larger dimension for base_res
    if width_ratio >= height_ratio:
        width = base_res
        height = round(base_res * height_ratio / width_ratio)
    else:
        height = base_res
        width = round(base_res * width_ratio / height_ratio)

    return f"{width}x{height}"


def create_client(base_url: str = DEFAULT_BASE_URL, api_key: str = DEFAULT_API_KEY) -> OpenAI:
    return OpenAI(base_url=base_url, api_key=api_key)


def generate_image(
    client: OpenAI,
    prompt: str,
    model: str = DEFAULT_MODEL,
    size: str = DEFAULT_SIZE,
    quality: str = "standard",
    aspect_ratio: str = "1:1",
    image_path: str = None,
) -> str:
    """Generate image and save to file. Returns the file path."""
    # Build message content
    if image_path:
        # Read image and convert to base64
        with open(image_path, "rb") as f:
            image_b64 = base64.b64encode(f.read()).decode()

        # Detect image type
        ext = Path(image_path).suffix.lower()
        mime_type = "image/jpeg"
        if ext == '.png':
            mime_type = "image/png"
        elif ext == '.gif':
            mime_type = "image/gif"
        elif ext == '.webp':
            mime_type = "image/webp"
        image_url = f"data:{mime_type};base64,{image_b64}"

        messages = [
            {
                "role": "user",
                "content": [
                    {"type": "text", "text": prompt},
                    {"type": "image_url", "image_url": {"url": image_url}}
                ]
            }
        ]
    else:
        messages = [{"role": "user", "content": prompt}]

    # Pass size and aspect_ratio in extra_body as the backend is likely ignoring prompt-appended strings
    response = client.chat.completions.create(
        model=model,
        messages=messages,
        extra_body={
            "size": size,
            "quality": quality,
            "aspect_ratio": aspect_ratio
        }
    )

    # Get base64 image data
    image_b64 = response.choices[0].message.content
    if not image_b64:
        raise RuntimeError("No image data in response")

    # Decode and save
    timestamp = datetime.now().strftime("%Y%m%d-%H%M%S")
    slug = "".join(c if c.isalnum() else "_" for c in prompt[:30])
    filename = f"{timestamp}_{slug}.png"
    filepath = OUTPUT_DIR / filename

    # Handle data URI format if present
    if "," in image_b64:
        image_b64 = image_b64.split(",")[1]

    image_data = base64.b64decode(image_b64)
    filepath.write_bytes(image_data)
    return str(filepath)


def main():
    parser = argparse.ArgumentParser(
        description="Generate images via local OpenAI-compatible API (Gemini image model)."
    )
    parser.add_argument("prompt", nargs='?', help="Image prompt (or describe what you want)")
    parser.add_argument("--base-url", default=DEFAULT_BASE_URL, help=f"API endpoint URL (default: {DEFAULT_BASE_URL})")
    parser.add_argument("--api-key", default=DEFAULT_API_KEY, help="API key")
    parser.add_argument("--model", default=DEFAULT_MODEL, help=f"Model name (default: {DEFAULT_MODEL})")
    parser.add_argument(
        "--size",
        default=None,  # Changed default to None
        help=f"Image size (e.g., 1024x1024). Overrides --resolution and --aspect-ratio if provided."
    )
    parser.add_argument("--image", default=None, help="Input image path (for image-to-image transformation)")
    parser.add_argument(
        "--resolution",
        default="1k",
        choices=list(RESOLUTIONS.keys()),
        help=f"Base resolution (e.g., 1k, 2k, 4k). (default: 1k)"
    )
    parser.add_argument(
        "--aspect-ratio",
        default="1:1",
        choices=list(ASPECT_RATIOS.keys()),
        help=f"Aspect ratio (e.g., 1:1, 16:9). (default: 1:1)"
    )
    parser.add_argument(
        "--read-prompt-from-file",
        help="Path to a file containing the prompt. If provided, overrides the direct prompt argument."
    )
    args = parser.parse_args()

    if args.read_prompt_from_file:
        with open(args.read_prompt_from_file, "r") as f:
            args.prompt = f.read().strip()

    # Calculate size if not explicitly provided
    size_to_use = args.size
    if size_to_use is None:
        try:
            size_to_use = calculate_size(args.resolution, args.aspect_ratio)
        except ValueError as e:
            print(f"Error: {e}. Falling back to default size {DEFAULT_SIZE}", file=sys.stderr)
            size_to_use = DEFAULT_SIZE

    try:
        client = create_client(base_url=args.base_url, api_key=args.api_key)
        filepath = generate_image(
            client,
            args.prompt,
            model=args.model,
            size=size_to_use,
            quality=QUALITY_MAP.get(args.resolution, "standard"),
            aspect_ratio=args.aspect_ratio,
            image_path=args.image,
        )
        print(f"[IMAGE_PATH]{filepath}[/IMAGE_PATH]")
    except Exception as e:
        print(f"Error: {e}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
