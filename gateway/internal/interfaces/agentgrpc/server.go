package agentgrpc

import (
	"context"
	"fmt"
	"net"

	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/entity"
	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/service"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Server implements the gRPC AgentService for VS Code extension and other clients.
// It wraps the AgentLoop to expose agent capabilities over gRPC with server-side streaming.
type Server struct {
	agentLoop *service.AgentLoop
	toolExec  service.ToolExecutor
	logger    *zap.Logger
	server    *grpc.Server
	port      int
}

// NewServer creates a new gRPC agent server
func NewServer(agentLoop *service.AgentLoop, toolExec service.ToolExecutor, port int, logger *zap.Logger) *Server {
	return &Server{
		agentLoop: agentLoop,
		toolExec:  toolExec,
		logger:    logger.With(zap.String("component", "agent-grpc")),
		port:      port,
	}
}

// Start starts the gRPC server
func (s *Server) Start() error {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", s.port))
	if err != nil {
		return fmt.Errorf("listen port %d: %w", s.port, err)
	}

	s.server = grpc.NewServer()
	// Register would happen here once proto is generated:
	// pb.RegisterAgentServiceServer(s.server, s)

	s.logger.Info("Starting gRPC agent server", zap.Int("port", s.port))

	go func() {
		if err := s.server.Serve(lis); err != nil {
			s.logger.Error("gRPC server failed", zap.Error(err))
		}
	}()

	return nil
}

// Stop gracefully stops the gRPC server
func (s *Server) Stop() {
	if s.server != nil {
		s.server.GracefulStop()
		s.logger.Info("gRPC agent server stopped")
	}
}

// --- gRPC Service Method Implementations ---
// These follow the proto service definition and will be connected
// once proto generation is set up.

// RunAgentRequest is the inbound request for ExecuteAgent RPC
type RunAgentRequest struct {
	Message      string `json:"message"`
	SystemPrompt string `json:"system_prompt"`
	Model        string `json:"model"`
	SessionID    string `json:"session_id"`
}

// AgentEvent is the streaming response event for ExecuteAgent RPC
type AgentEvent struct {
	Type      string                 `json:"type"`
	Content   string                 `json:"content,omitempty"`
	ToolName  string                 `json:"tool_name,omitempty"`
	ToolID    string                 `json:"tool_id,omitempty"`
	ToolArgs  map[string]interface{} `json:"tool_args,omitempty"`
	ToolOut   string                 `json:"tool_output,omitempty"`
	Success   bool                   `json:"success,omitempty"`
	Step      int                    `json:"step,omitempty"`
	Tokens    int                    `json:"tokens,omitempty"`
	Model     string                 `json:"model,omitempty"`
	Error     string                 `json:"error,omitempty"`
}

// ToolDefinition describes a tool for the ListTools RPC
type ToolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// ExecuteAgent runs the agent loop and streams events back.
// This method can be called via gRPC server-side streaming once
// proto generation is set up. For now, it exposes the logic directly.
func (s *Server) ExecuteAgent(ctx context.Context, req *RunAgentRequest, sendEvent func(*AgentEvent) error) error {
	if req.Message == "" {
		return status.Error(codes.InvalidArgument, "message is required")
	}

	s.logger.Info("gRPC ExecuteAgent",
		zap.String("session", req.SessionID),
		zap.String("model", req.Model),
	)

	_, eventCh := s.agentLoop.Run(ctx, req.SystemPrompt, req.Message, nil, "")

	for event := range eventCh {
		grpcEvent := convertToGRPCEvent(event)
		if err := sendEvent(grpcEvent); err != nil {
			return err
		}
	}

	return nil
}

// ListTools returns available tool definitions
func (s *Server) ListTools() []ToolDefinition {
	defs := s.toolExec.GetDefinitions()
	result := make([]ToolDefinition, 0, len(defs))
	for _, d := range defs {
		result = append(result, ToolDefinition{
			Name:        d.Name,
			Description: d.Description,
			Parameters:  d.Parameters,
		})
	}
	return result
}

func convertToGRPCEvent(event entity.AgentEvent) *AgentEvent {
	ge := &AgentEvent{}

	switch event.Type {
	case entity.EventThinking:
		ge.Type = "thinking"
		ge.Content = event.Content
	case entity.EventTextDelta:
		ge.Type = "text_delta"
		ge.Content = event.Content
	case entity.EventToolCall:
		ge.Type = "tool_call"
		if event.ToolCall != nil {
			ge.ToolName = event.ToolCall.Name
			ge.ToolID = event.ToolCall.ID
			ge.ToolArgs = event.ToolCall.Arguments
		}
	case entity.EventToolResult:
		ge.Type = "tool_result"
		if event.ToolCall != nil {
			ge.ToolName = event.ToolCall.Name
			ge.ToolID = event.ToolCall.ID
			ge.ToolOut = event.ToolCall.Output
			ge.Success = event.ToolCall.Success
		}
	case entity.EventStepDone:
		ge.Type = "step_done"
		if event.StepInfo != nil {
			ge.Step = event.StepInfo.Step
			ge.Tokens = event.StepInfo.TokensUsed
			ge.Model = event.StepInfo.ModelUsed
		}
	case entity.EventError:
		ge.Type = "error"
		ge.Error = event.Error
	case entity.EventDone:
		ge.Type = "done"
	}

	return ge
}
