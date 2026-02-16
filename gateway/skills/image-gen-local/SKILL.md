---
name: image-gen-local
description: Generate images via local OpenAI-compatible API (Gemini image model). Use when user wants to create images or transform existing images using local LLM server at http://127.0.0.1:8045.
---

# Image Gen Local
Generate images using local Gemini image model via OpenAI-compatible API.

## Core Logic (Important!)
**你必须根据用户意图推理出提示词，不能直接复制用户的话！**
### 处理流程：
1. **主会话接收请求并推理提示词**：
    *   根据用户意图，推理出图像生成所需的提示词和参数（例如 `--image`, `--resolution`, `--aspect-ratio`）。
    *   如果需要图片输入，则识别图片内容。
2. **生成子会话任务**：
    *   主会话将 `gen.py` 命令（包含推理出的提示词和参数）封装成一个 `sessions_spawn` 任务。
    *   例如：`sessions_spawn(task="source /home/none/moltbot/.venv/bin/activate && python3 skills/image-gen-local/scripts/gen.py 'a cute cat, cartoon style, vibrant colors'")`
3. **子会话执行生成**：
    *   子会话运行 `gen.py` 脚本，生成图片。
    *   `gen.py` 脚本完成后，会在标准输出打印 `[IMAGE_PATH]/path/to/image.png[/IMAGE_PATH]`。
4. **主会话接收反馈并发送图片**：
    *   主会话监控子会话的输出，提取图片路径。
    *   使用 `message` 工具将图片作为媒体文件发送给用户。
    *   例如：`message(action='send', to='user_id', media='/path/to/image.png')`

### 推理原则：
- **理解本质**：用户说"转化成动漫" → 本质是"风格转换" → 提示词用 "anime style, Japanese anime art"
- **图片内容**：如果提供了 `--image` 参数，**必须**详细识别并描述图片中的主要内容（例如：人物、物体、场景、背景等）。这些内容描述应作为提示词的基石。
- **保持简洁**：在描述图片内容时，力求简洁而准确，避免冗余。但当 `--image` 参数存在时，内容的描述应优先于极端的简洁，确保所有关键视觉元素都已纳入提示词。
- **风格明确**：动漫→"anime style, Japanese anime"，油画→"oil painting style"，写实→"photorealistic"

### 任务完成与用户交互：
- **任务完成标准**：图片生成后，必须将图片发送给用户，任务才算完全结束。
- **避免等待提示**：生成过程中，不要使用“请稍等”或类似引导用户等待的短语。直接进行生成，完成后立即发送图片。

## Quick Start (通过 sessions_spawn 启动子会话)
```bash
# 纯文本生成
sessions_spawn(task="source /home/none/moltbot/.venv/bin/activate && python3 skills/image-gen-local/scripts/gen.py 'a futuristic city'")
```

## Image-to-Image (Style Transfer) (通过 sessions_spawn 启动子会话)
```bash
# Convert image to anime style
sessions_spawn(task="source /home/none/moltbot/.venv/bin/activate && python3 skills/image-gen-local/scripts/gen.py 'anime style' --image /path/to/image.jpg")
# Any style transformation
sessions_spawn(task="source /home/none/moltbot/.venv/bin/activate && python3 skills/image-gen-local/scripts/gen.py 'oil painting style' --image /path/to/image.jpg")
```

## Options
| Flag | Description | Default |
|------|-------------|---------|
| `--image` | Input image path for transformation | None (text-to-image) |
| `--size` | Image size (e.g., 1024x1024). Overrides `--resolution` and `--aspect-ratio` if provided. | `1024x1024` (fallback) |
| `--resolution` | Base resolution (e.g., `1k`, `2k`, `4k`) | `1k` |
| `--aspect-ratio` | Aspect ratio (e.g., `1:1`, `16:9`, `3:4`, `4:3`, `21:9`, `9:21`) | `1:1` |
### Supported Resolutions
- `1k` (Mapped to `quality: standard`) -> 1024x1024 (1:1)
- `2k` (Mapped to `quality: medium`) -> 2048x2048 (1:1)
- `4k` (Mapped to `quality: hd`) -> 4096x4096 (1:1)

### Supported Aspect Ratios
- `1:1` (Square)
- `16:9` (Widescreen)
- `9:16` (Portrait)
- `3:4` (Traditional Portrait)
- `4:3` (Traditional Landscape)
- `21:9` (Ultrawide)
- `9:21` (Tall Portrait)

## Examples (通过 sessions_spawn 启动子会话)
```bash
# Text to image
sessions_spawn(task="source /home/none/moltbot/.venv/bin/activate && python3 skills/image-gen-local/scripts/gen.py 'a cute cat'")
# Image transformation (推理后的提示词)
sessions_spawn(task="source /home/none/moltbot/.venv/bin/activate && python3 skills/image-gen-local/scripts/gen.py 'anime style, soft lighting' --image /home/none/.clawdbot/media/inbound/file.jpg")
# Different size (using resolution and aspect ratio)
sessions_spawn(task="source /home/none/moltbot/.venv/bin/activate && python3 skills/image-gen-local/scripts/gen.py 'landscape with mountains' --resolution 2k --aspect-ratio 16:9")
# High quality 4K generation
sessions_spawn(task="source /home/none/moltbot/.venv/bin/activate && python3 skills/image-gen-local/scripts/gen.py 'hyper-realistic portrait' --resolution 4k")

## Output
Images are saved to `/home/none/clawd/tmp/image-gen/` with timestamps and prompt slugs.

## Requirements
- Python with `openai` package
- Local API server running at `http://127.0.0.1:8045`
