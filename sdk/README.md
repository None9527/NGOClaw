# NGOClaw SDK

Client SDKs for integrating with the NGOClaw Agent Platform.

## Available SDKs

| SDK | Language | Transport | Status |
|-----|----------|-----------|--------|
| [Go SDK](go/) | Go 1.23+ | HTTP/SSE | ✅ Stable |
| [Python SDK](python/) | Python 3.10+ | HTTP/SSE (httpx) | ✅ Stable |

## Quick Start

### Go

```go
import "github.com/ngoclaw/ngoclaw/sdk/go/ngoclaw"

client := ngoclaw.NewClient("http://localhost:18789",
    ngoclaw.WithAPIKey("your-key"),
)

// Stream events
events, _ := client.Run(ctx, &ngoclaw.AgentRequest{
    Message: "Analyze this codebase",
})
for event := range events {
    if event.IsText() {
        fmt.Print(event.Content())
    }
}

// Or wait for result
result, _ := client.RunSync(ctx, &ngoclaw.AgentRequest{
    Message: "What files are in the project?",
})
fmt.Println(result.Content)
```

### Python

```python
from ngoclaw import NGOClawClient

client = NGOClawClient("http://localhost:18789", api_key="your-key")

# Stream events
for event in client.run("Analyze this codebase"):
    if event.is_text:
        print(event.content, end="")

# Or wait for result
result = client.run_sync("What files are in the project?")
print(result.content)

# Async
async for event in client.arun("Explain this code"):
    print(event.content, end="")
```

## API Endpoints

The SDKs connect to NGOClaw's HTTP API:

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/v1/agent` | POST | Run agent loop (SSE streaming) |
| `/api/v1/agent/tools` | GET | List available tools |
| `/health` | GET | Health check |

## Event Types

| Event | Description |
|-------|-------------|
| `thinking` | Agent reasoning/planning text |
| `text_delta` | Response text chunk |
| `tool_call` | Tool invocation |
| `tool_result` | Tool execution result |
| `step_done` | Agent loop step completed |
| `error` | Error occurred |
| `done` | Agent loop finished |
