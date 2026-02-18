# NGOClaw Python SDK

Python client for the NGOClaw Agent Platform. Supports sync and async streaming.

## Install

```bash
pip install ngoclaw-sdk
# or from source
cd sdk/python && pip install -e .
```

## Usage

```python
from ngoclaw import NGOClawClient

client = NGOClawClient(
    base_url="http://localhost:18789",
    api_key="your-api-key",
    timeout=300.0,
)
```

### Streaming

```python
# Synchronous streaming
for event in client.run("Analyze this repository", model="openai/gpt-4o"):
    if event.is_text:
        print(event.content, end="")
    elif event.is_error:
        print(f"Error: {event.error_message}")
    elif event.is_done:
        print("\n--- Done ---")
```

### Async Streaming

```python
import asyncio

async def main():
    async for event in client.arun("Explain this code"):
        if event.is_text:
            print(event.content, end="")

asyncio.run(main())
```

### Synchronous (Wait for Result)

```python
result = client.run_sync("What is 2 + 2?")
print(f"Answer: {result.content}")
print(f"Steps: {result.total_steps}, Tokens: {result.total_tokens}")
```

### List Tools

```python
tools = client.list_tools()
for t in tools:
    print(f"{t.name:20s} {t.description}")
```

### Health Check

```python
if client.health():
    print("Server is healthy")
```

## Event Types

| Property | Type | Description |
|----------|------|-------------|
| `event.is_text` | bool | Text content (thinking or response) |
| `event.is_done` | bool | Agent loop completed |
| `event.is_error` | bool | Error occurred |
| `event.content` | str | Text content |
| `event.error_message` | str | Error message |

## Dependencies

- `httpx >= 0.25.0` (HTTP client with streaming support)
- Python 3.10+
