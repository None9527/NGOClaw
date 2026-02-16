package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"
)

// MCPToolDef MCP 工具定义 (从 MCP Server 发现)
type MCPToolDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

// MCPAdapter 将外部 MCP Server 的工具接入 ToolExecutor
type MCPAdapter struct {
	name     string // MCP Server 名称
	endpoint string // MCP Server 地址
	client   *http.Client
	logger   *zap.Logger
	tools    []MCPToolDef
	mu       sync.RWMutex
}

// NewMCPAdapter 创建 MCP 适配器
func NewMCPAdapter(name, endpoint string, logger *zap.Logger) *MCPAdapter {
	return &MCPAdapter{
		name:     name,
		endpoint: endpoint,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: logger,
	}
}

// ─────────────────── JSON-RPC 2.0 ───────────────────

type jsonRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ─────────────────── 核心方法 ───────────────────

// DiscoverTools 连接 MCP Server, 发现可用工具
func (a *MCPAdapter) DiscoverTools(ctx context.Context) ([]MCPToolDef, error) {
	resp, err := a.call(ctx, "tools/list", nil)
	if err != nil {
		return nil, fmt.Errorf("MCP tools/list failed for %s: %w", a.name, err)
	}

	var result struct {
		Tools []MCPToolDef `json:"tools"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("failed to parse MCP tools response: %w", err)
	}

	a.mu.Lock()
	a.tools = result.Tools
	a.mu.Unlock()

	a.logger.Info("MCP tools discovered",
		zap.String("server", a.name),
		zap.Int("tool_count", len(result.Tools)),
	)

	return result.Tools, nil
}

// CallTool 调用 MCP Server 上的工具
func (a *MCPAdapter) CallTool(ctx context.Context, name string, args map[string]interface{}) (string, error) {
	params := map[string]interface{}{
		"name":      name,
		"arguments": args,
	}

	resp, err := a.call(ctx, "tools/call", params)
	if err != nil {
		return "", fmt.Errorf("MCP tools/call failed for %s.%s: %w", a.name, name, err)
	}

	// MCP 标准响应: { content: [{ type: "text", text: "..." }] }
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		// 降级: 直接返回原始 JSON
		return string(resp), nil
	}

	if result.IsError {
		if len(result.Content) > 0 {
			return "", fmt.Errorf("MCP tool error: %s", result.Content[0].Text)
		}
		return "", fmt.Errorf("MCP tool returned error without message")
	}

	// 拼接所有 text content
	var output string
	for _, c := range result.Content {
		if c.Type == "text" {
			output += c.Text
		}
	}
	return output, nil
}

// GetTools 返回已发现的工具列表
func (a *MCPAdapter) GetTools() []MCPToolDef {
	a.mu.RLock()
	defer a.mu.RUnlock()
	result := make([]MCPToolDef, len(a.tools))
	copy(result, a.tools)
	return result
}

// Name 返回 MCP Server 名称
func (a *MCPAdapter) Name() string {
	return a.name
}

// ─────────────────── JSON-RPC 传输层 ───────────────────

var rpcIDCounter int
var rpcIDMu sync.Mutex

func nextRPCID() int {
	rpcIDMu.Lock()
	defer rpcIDMu.Unlock()
	rpcIDCounter++
	return rpcIDCounter
}

func (a *MCPAdapter) call(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	reqBody := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      nextRPCID(),
		Method:  method,
		Params:  params,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal JSON-RPC request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("MCP HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("MCP server returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var rpcResp jsonRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return nil, fmt.Errorf("failed to decode JSON-RPC response: %w", err)
	}

	if rpcResp.Error != nil {
		return nil, fmt.Errorf("MCP RPC error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	return rpcResp.Result, nil
}
