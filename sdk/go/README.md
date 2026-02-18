# NGOClaw Go SDK

Go client for the NGOClaw Agent Platform. Zero external dependencies.

## Install

```bash
go get github.com/ngoclaw/ngoclaw/sdk/go
```

## Usage

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/ngoclaw/ngoclaw/sdk/go/ngoclaw"
)

func main() {
    client := ngoclaw.NewClient("http://localhost:18789",
        ngoclaw.WithAPIKey("your-api-key"),
        ngoclaw.WithTimeout(5*time.Minute),
    )

    ctx := context.Background()

    // Health check
    if !client.Health(ctx) {
        panic("server is not healthy")
    }

    // Stream agent events
    events, err := client.Run(ctx, &ngoclaw.AgentRequest{
        Message: "List all Go files in the project",
        Model:   "openai/gpt-4o",
    })
    if err != nil {
        panic(err)
    }

    for event := range events {
        switch {
        case event.IsText():
            fmt.Print(event.Content())
        case event.IsError():
            fmt.Printf("\nError: %s\n", event.ErrorMessage())
        case event.IsDone():
            fmt.Println("\n--- Done ---")
        }
    }
}
```

### Synchronous Mode

```go
result, err := client.RunSync(ctx, &ngoclaw.AgentRequest{
    Message: "What is 2 + 2?",
})
if err != nil {
    panic(err)
}
fmt.Printf("Answer: %s\n", result.Content)
fmt.Printf("Steps: %d, Tokens: %d\n", result.TotalSteps, result.TotalTokens)
```

### List Tools

```go
tools, err := client.ListTools(ctx)
for _, t := range tools {
    fmt.Printf("%-20s %s\n", t.Name, t.Description)
}
```

## API

### Client Options

| Option | Description |
|--------|-------------|
| `WithAPIKey(key)` | Set Bearer token authentication |
| `WithTimeout(d)` | Set HTTP timeout (default: 5min) |

### Event Types

| Method | Description |
|--------|-------------|
| `IsText()` | Text content (thinking or response) |
| `IsDone()` | Agent loop completed |
| `IsError()` | Error occurred |
| `Content()` | Get text content |
| `ErrorMessage()` | Get error message |
