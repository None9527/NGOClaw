package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/entity"
	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/service"
	"github.com/ngoclaw/ngoclaw/gateway/internal/infrastructure/prompt"
	"go.uber.org/zap"
)

// AgentHandler handles agent loop interactions with SSE streaming.
// This is the primary endpoint for the VS Code extension and Web UI.
type AgentHandler struct {
	agentLoop    *service.AgentLoop
	toolExec     service.ToolExecutor
	promptEngine *prompt.PromptEngine
	logger       *zap.Logger
}

// NewAgentHandler creates a handler for agent loop SSE streaming
func NewAgentHandler(agentLoop *service.AgentLoop, toolExec service.ToolExecutor, promptEngine *prompt.PromptEngine, logger *zap.Logger) *AgentHandler {
	return &AgentHandler{
		agentLoop:    agentLoop,
		toolExec:     toolExec,
		promptEngine: promptEngine,
		logger:       logger.With(zap.String("handler", "agent")),
	}
}

// AgentRequest is the JSON body for POST /api/v1/agent
type AgentRequest struct {
	Message      string               `json:"message" binding:"required"`
	SystemPrompt string               `json:"system_prompt,omitempty"`
	Model        string               `json:"model,omitempty"`
	SessionID    string               `json:"session_id,omitempty"`
	History      []service.LLMMessage `json:"history,omitempty"`
}

// SSEEvent represents a single Server-Sent Event
type SSEEvent struct {
	Event string      `json:"event"`
	Data  interface{} `json:"data"`
}

// RunAgent handles POST /api/v1/agent — streams agent events via SSE
func (h *AgentHandler) RunAgent(c *gin.Context) {
	var req AgentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Set SSE headers
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.WriteHeader(http.StatusOK)

	ctx := c.Request.Context()

	// Assemble system prompt from the prompt engine
	systemPrompt := h.assemblePrompt(req)

	h.logger.Info("Agent request received",
		zap.String("session", req.SessionID),
		zap.String("model", req.Model),
		zap.Int("history_len", len(req.History)),
		zap.Int("prompt_chars", len(systemPrompt)),
	)

	// Run agent loop (returns immediately, streams events)
	result, eventCh := h.agentLoop.Run(ctx, systemPrompt, req.Message, req.History, "")

	// Stream events as SSE
	flusher, _ := c.Writer.(http.Flusher)

	for event := range eventCh {
		sseEvent := h.convertEvent(event)
		data, _ := json.Marshal(sseEvent)

		fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", sseEvent.Event, data)
		if flusher != nil {
			flusher.Flush()
		}
	}

	// Send final result
	finalData, _ := json.Marshal(map[string]interface{}{
		"content":      result.FinalContent,
		"total_steps":  result.TotalSteps,
		"total_tokens": result.TotalTokens,
		"model_used":   result.ModelUsed,
		"tools_used":   result.ToolsUsed,
	})
	fmt.Fprintf(c.Writer, "event: done\ndata: %s\n\n", finalData)
	if flusher != nil {
		flusher.Flush()
	}
}

// assemblePrompt builds the system prompt using the PromptEngine.
// If the request includes a custom system_prompt, it's appended.
func (h *AgentHandler) assemblePrompt(req AgentRequest) string {
	if h.promptEngine == nil {
		// Fallback: use request's system_prompt directly
		return req.SystemPrompt
	}

	// Build prompt context with runtime information
	toolNames := make([]string, 0)
	for _, d := range h.toolExec.GetDefinitions() {
		toolNames = append(toolNames, d.Name)
	}

	pctx := prompt.PromptContext{
		Channel:         "api",
		RegisteredTools: toolNames,
		ModelName:       req.Model,
		UserMessage:     req.Message,
	}

	// Assemble from SOUL + Components + Variants
	assembled := h.promptEngine.Assemble(pctx)

	// If request also has a custom system_prompt, append it
	if req.SystemPrompt != "" {
		assembled += "\n\n---\n\n## Additional Instructions\n" + req.SystemPrompt
	}

	return assembled
}

// GetTools handles GET /api/v1/agent/tools — lists available tools
func (h *AgentHandler) GetTools(c *gin.Context) {
	defs := h.toolExec.GetDefinitions()
	tools := make([]map[string]interface{}, 0, len(defs))
	for _, d := range defs {
		tools = append(tools, map[string]interface{}{
			"name":        d.Name,
			"description": d.Description,
			"parameters":  d.Parameters,
		})
	}
	c.JSON(http.StatusOK, gin.H{"tools": tools})
}

func (h *AgentHandler) convertEvent(event entity.AgentEvent) SSEEvent {
	switch event.Type {
	case entity.EventThinking:
		return SSEEvent{Event: "thinking", Data: map[string]string{
			"content": event.Content,
		}}
	case entity.EventTextDelta:
		return SSEEvent{Event: "text_delta", Data: map[string]string{
			"content": event.Content,
		}}
	case entity.EventToolCall:
		return SSEEvent{Event: "tool_call", Data: event.ToolCall}
	case entity.EventToolResult:
		return SSEEvent{Event: "tool_result", Data: event.ToolCall}
	case entity.EventStepDone:
		return SSEEvent{Event: "step_done", Data: event.StepInfo}

	case entity.EventError:
		return SSEEvent{Event: "error", Data: map[string]string{
			"error": event.Error,
		}}
	case entity.EventDone:
		return SSEEvent{Event: "complete", Data: map[string]string{
			"timestamp": event.Timestamp.Format(time.RFC3339),
		}}
	default:
		return SSEEvent{Event: "unknown", Data: event}
	}
}
