package tool

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	domaintool "github.com/ngoclaw/ngoclaw/gateway/internal/domain/tool"
	"go.uber.org/zap"
)

// LSPTool wraps language servers (gopls, typescript-language-server, pylsp, rust-analyzer)
// and exposes go-to-definition, find-references, hover, diagnostics, symbols via the Tool interface.
type LSPTool struct {
	servers       map[string]*lspServer // language -> running server
	mu            sync.Mutex
	workspaceRoot string
	logger        *zap.Logger
}

// lspServer represents a running language server process.
type lspServer struct {
	cmd              *exec.Cmd
	stdin            io.WriteCloser
	reader           *bufio.Reader
	reqID            int64 // atomic counter
	mu               sync.Mutex
	opened           map[string]bool           // URI -> didOpen sent
	diagnosticsCache map[string]json.RawMessage // URI -> latest pushed diagnostics
	diagMu           sync.RWMutex              // protects diagnosticsCache
	pendingResp      chan *jsonrpcResponse      // responses forwarded by bg reader
	stopBg           chan struct{}              // signal to stop background reader
}

// NewLSPTool creates an LSP tool with a workspace root.
func NewLSPTool(workspaceRoot string, logger *zap.Logger) *LSPTool {
	return &LSPTool{
		servers:       make(map[string]*lspServer),
		workspaceRoot: workspaceRoot,
		logger:        logger,
	}
}

func (t *LSPTool) Name() string        { return "lsp" }
func (t *LSPTool) Kind() domaintool.Kind { return domaintool.KindRead }

func (t *LSPTool) Description() string {
	return `Language Server Protocol tool. Provides code intelligence via language servers (gopls, typescript-language-server, pylsp, rust-analyzer).
Supported actions:
  - definition: Jump to definition of a symbol at file:line:col
  - references: Find all references to a symbol at file:line:col
  - hover: Get type info / documentation for symbol at file:line:col
  - diagnostics: Get errors/warnings for a file
  - symbols: List all symbols (functions, types, variables) in a file
  - completion: Get code completion suggestions at file:line:col`
}

func (t *LSPTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"definition", "references", "hover", "diagnostics", "symbols", "completion"},
				"description": "The LSP operation to perform.",
			},
			"file": map[string]interface{}{
				"type":        "string",
				"description": "Absolute path to the file.",
			},
			"line": map[string]interface{}{
				"type":        "integer",
				"description": "1-indexed line number (required for definition, references, hover, completion).",
			},
			"column": map[string]interface{}{
				"type":        "integer",
				"description": "1-indexed column number (required for definition, references, hover, completion).",
			},
		},
		"required": []string{"action", "file"},
	}
}

func (t *LSPTool) Execute(ctx context.Context, args map[string]interface{}) (*Result, error) {
	action, _ := args["action"].(string)
	filePath, _ := args["file"].(string)
	line := intArg(args, "line", 1)
	col := intArg(args, "column", 1)

	if action == "" || filePath == "" {
		return &Result{Output: "action and file are required", Success: false}, nil
	}

	// Resolve file path
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(t.workspaceRoot, filePath)
	}

	// Check file exists
	if _, err := os.Stat(filePath); err != nil {
		return &Result{Output: fmt.Sprintf("file not found: %s", filePath), Success: false}, nil
	}

	// Detect language from extension
	lang := detectLanguage(filePath)
	if lang == "" {
		return &Result{Output: fmt.Sprintf("unsupported file type: %s", filepath.Ext(filePath)), Success: false}, nil
	}

	// Get or start language server
	srv, err := t.getOrStartServer(ctx, lang)
	if err != nil {
		return &Result{
			Output:  fmt.Sprintf("failed to start language server for %s: %s", lang, err.Error()),
			Success: false,
		}, nil
	}

	// Ensure file is opened
	if err := t.ensureOpened(srv, filePath, lang); err != nil {
		t.logger.Warn("didOpen failed", zap.Error(err))
	}

	// Convert to 0-indexed LSP positions
	lspLine := line - 1
	lspCol := col - 1

	uri := pathToURI(filePath)

	switch action {
	case "definition":
		return t.doDefinition(srv, uri, lspLine, lspCol)
	case "references":
		return t.doReferences(srv, uri, lspLine, lspCol)
	case "hover":
		return t.doHover(srv, uri, lspLine, lspCol)
	case "diagnostics":
		return t.doDiagnostics(srv, uri)
	case "symbols":
		return t.doSymbols(srv, uri)
	case "completion":
		return t.doCompletion(srv, uri, lspLine, lspCol)
	default:
		return &Result{Output: "unknown action: " + action, Success: false}, nil
	}
}

// Shutdown gracefully closes all running language servers.
func (t *LSPTool) Shutdown() {
	t.mu.Lock()
	defer t.mu.Unlock()

	for lang, srv := range t.servers {
		t.logger.Info("Shutting down language server", zap.String("lang", lang))
		// Stop background reader
		close(srv.stopBg)
		// Send shutdown request (best-effort)
		srv.mu.Lock()
		id := atomic.AddInt64(&srv.reqID, 1)
		_ = writeJSONRPC(srv.stdin, id, "shutdown", nil)
		_ = writeJSONRPC(srv.stdin, 0, "exit", nil)
		srv.mu.Unlock()
		_ = srv.cmd.Process.Kill()
	}
	t.servers = make(map[string]*lspServer)
}

// --- LSP operations ---

func (t *LSPTool) doDefinition(srv *lspServer, uri string, line, col int) (*Result, error) {
	params := map[string]interface{}{
		"textDocument": map[string]string{"uri": uri},
		"position":     map[string]int{"line": line, "character": col},
	}
	resp, err := t.sendRequest(srv, "textDocument/definition", params)
	if err != nil {
		return &Result{Output: "definition request failed: " + err.Error(), Success: false}, nil
	}
	return t.formatLocations("Definition", resp)
}

func (t *LSPTool) doReferences(srv *lspServer, uri string, line, col int) (*Result, error) {
	params := map[string]interface{}{
		"textDocument": map[string]string{"uri": uri},
		"position":     map[string]int{"line": line, "character": col},
		"context":      map[string]bool{"includeDeclaration": true},
	}
	resp, err := t.sendRequest(srv, "textDocument/references", params)
	if err != nil {
		return &Result{Output: "references request failed: " + err.Error(), Success: false}, nil
	}
	return t.formatLocations("References", resp)
}

func (t *LSPTool) doHover(srv *lspServer, uri string, line, col int) (*Result, error) {
	params := map[string]interface{}{
		"textDocument": map[string]string{"uri": uri},
		"position":     map[string]int{"line": line, "character": col},
	}
	resp, err := t.sendRequest(srv, "textDocument/hover", params)
	if err != nil {
		return &Result{Output: "hover request failed: " + err.Error(), Success: false}, nil
	}
	return t.formatHover(resp)
}

func (t *LSPTool) doDiagnostics(srv *lspServer, uri string) (*Result, error) {
	// 1. Check push-based cache first (most language servers use this)
	srv.diagMu.RLock()
	cached, hasCached := srv.diagnosticsCache[uri]
	srv.diagMu.RUnlock()

	if hasCached {
		return t.formatPushDiagnostics(cached)
	}

	// 2. Try pull-based documentDiagnostic (LSP 3.17+)
	params := map[string]interface{}{
		"textDocument": map[string]string{"uri": uri},
	}
	resp, err := t.sendRequest(srv, "textDocument/diagnostic", params)
	if err == nil {
		return t.formatDiagnostics(resp)
	}

	// 3. Fallback: no diagnostics available yet
	return &Result{
		Output:  "Diagnostics: no issues reported yet (file may need a save/edit to trigger diagnostics push).",
		Success: true,
	}, nil
}

func (t *LSPTool) doSymbols(srv *lspServer, uri string) (*Result, error) {
	params := map[string]interface{}{
		"textDocument": map[string]string{"uri": uri},
	}
	resp, err := t.sendRequest(srv, "textDocument/documentSymbol", params)
	if err != nil {
		return &Result{Output: "symbols request failed: " + err.Error(), Success: false}, nil
	}
	return t.formatSymbols(resp)
}

func (t *LSPTool) doCompletion(srv *lspServer, uri string, line, col int) (*Result, error) {
	params := map[string]interface{}{
		"textDocument": map[string]string{"uri": uri},
		"position":     map[string]int{"line": line, "character": col},
	}
	resp, err := t.sendRequest(srv, "textDocument/completion", params)
	if err != nil {
		return &Result{Output: "completion request failed: " + err.Error(), Success: false}, nil
	}
	return t.formatCompletion(resp)
}

// --- Server lifecycle ---

func (t *LSPTool) getOrStartServer(ctx context.Context, lang string) (*lspServer, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if srv, ok := t.servers[lang]; ok {
		// Check process is still alive
		if srv.cmd.ProcessState == nil {
			return srv, nil
		}
		// Process exited, remove and restart
		delete(t.servers, lang)
	}

	cmdName, cmdArgs := languageServerCommand(lang)
	if cmdName == "" {
		return nil, fmt.Errorf("no language server configured for %s", lang)
	}

	// Check if the binary exists
	if _, err := exec.LookPath(cmdName); err != nil {
		return nil, fmt.Errorf("language server binary not found: %s (install with: %s)", cmdName, installHint(lang))
	}

	cmd := exec.CommandContext(ctx, cmdName, cmdArgs...)
	cmd.Env = append(os.Environ(), "GOPATH="+os.Getenv("GOPATH"))

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	// Discard stderr
	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", cmdName, err)
	}

	srv := &lspServer{
		cmd:              cmd,
		stdin:            stdin,
		reader:           bufio.NewReaderSize(stdout, 1024*1024), // 1MB buffer
		opened:           make(map[string]bool),
		diagnosticsCache: make(map[string]json.RawMessage),
		pendingResp:      make(chan *jsonrpcResponse, 64),
		stopBg:           make(chan struct{}),
	}

	// Start background reader that continuously consumes notifications
	go t.backgroundReader(srv)

	t.logger.Info("Started language server",
		zap.String("lang", lang),
		zap.String("cmd", cmdName),
		zap.Int("pid", cmd.Process.Pid),
	)

	// Send initialize
	if err := t.initialize(srv); err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("initialize handshake failed: %w", err)
	}

	t.servers[lang] = srv
	return srv, nil
}

func (t *LSPTool) initialize(srv *lspServer) error {
	initParams := map[string]interface{}{
		"processId": os.Getpid(),
		"rootUri":   pathToURI(t.workspaceRoot),
		"capabilities": map[string]interface{}{
			"textDocument": map[string]interface{}{
				"definition":     map[string]interface{}{},
				"references":     map[string]interface{}{},
				"hover":          map[string]interface{}{},
				"documentSymbol": map[string]interface{}{},
				"completion":     map[string]interface{}{},
				"diagnostic":     map[string]interface{}{},
			},
		},
	}

	_, err := t.sendRequest(srv, "initialize", initParams)
	if err != nil {
		return err
	}

	// Send initialized notification
	srv.mu.Lock()
	defer srv.mu.Unlock()
	return writeJSONRPC(srv.stdin, 0, "initialized", map[string]interface{}{})
}

func (t *LSPTool) ensureOpened(srv *lspServer, filePath, lang string) error {
	uri := pathToURI(filePath)

	srv.mu.Lock()
	defer srv.mu.Unlock()

	if srv.opened[uri] {
		return nil
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	params := map[string]interface{}{
		"textDocument": map[string]interface{}{
			"uri":        uri,
			"languageId": lang,
			"version":    1,
			"text":       string(content),
		},
	}

	if err := writeJSONRPC(srv.stdin, 0, "textDocument/didOpen", params); err != nil {
		return err
	}
	srv.opened[uri] = true
	return nil
}

// --- JSON-RPC transport ---

type jsonrpcRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int64       `json:"id,omitempty"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Method  string          `json:"method,omitempty"`  // present in notifications
	Params  json.RawMessage `json:"params,omitempty"`  // present in notifications
	Result  json.RawMessage `json:"result,omitempty"`  // present in responses
	Error   *jsonrpcError   `json:"error,omitempty"`
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func writeJSONRPC(w io.Writer, id int64, method string, params interface{}) error {
	req := jsonrpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))
	if _, err := io.WriteString(w, header); err != nil {
		return err
	}
	_, err = w.Write(body)
	return err
}

func readJSONRPC(r *bufio.Reader) (*jsonrpcResponse, error) {
	// Read headers
	var contentLen int
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("read header: %w", err)
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break // End of headers
		}
		if strings.HasPrefix(line, "Content-Length:") {
			_, _ = fmt.Sscanf(line, "Content-Length: %d", &contentLen)
		}
	}

	if contentLen <= 0 {
		return nil, fmt.Errorf("invalid Content-Length: %d", contentLen)
	}

	// Read body
	body := make([]byte, contentLen)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	var resp jsonrpcResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	return &resp, nil
}

func (t *LSPTool) sendRequest(srv *lspServer, method string, params interface{}) (json.RawMessage, error) {
	srv.mu.Lock()
	defer srv.mu.Unlock()

	id := atomic.AddInt64(&srv.reqID, 1)
	if err := writeJSONRPC(srv.stdin, id, method, params); err != nil {
		return nil, fmt.Errorf("write request: %w", err)
	}

	// Wait for the matching response from the background reader channel
	for i := 0; i < 200; i++ {
		resp, ok := <-srv.pendingResp
		if !ok {
			return nil, fmt.Errorf("language server connection closed")
		}

		if resp.ID == id {
			if resp.Error != nil {
				return nil, fmt.Errorf("LSP error %d: %s", resp.Error.Code, resp.Error.Message)
			}
			return resp.Result, nil
		}
	}

	return nil, fmt.Errorf("timeout: no response for request %d after 200 messages", id)
}

// backgroundReader continuously reads from the language server stdout,
// caching push diagnostics and forwarding responses to the pendingResp channel.
func (t *LSPTool) backgroundReader(srv *lspServer) {
	for {
		select {
		case <-srv.stopBg:
			return
		default:
		}

		resp, err := readJSONRPC(srv.reader)
		if err != nil {
			// Server closed or read error — stop
			if t.logger != nil {
				t.logger.Debug("LSP background reader stopped", zap.Error(err))
			}
			close(srv.pendingResp)
			return
		}

		// Check if this is a notification (id == 0 and has method)
		if resp.ID == 0 {
			// Try to parse as notification to check for publishDiagnostics
			t.handleNotification(srv, resp)
			continue
		}

		// Forward response to request handler
		select {
		case srv.pendingResp <- resp:
		case <-srv.stopBg:
			return
		}
	}
}

// jsonrpcNotification is used to parse notification messages that have a method field.
type jsonrpcNotification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// handleNotification processes LSP server notifications, caching diagnostics.
func (t *LSPTool) handleNotification(srv *lspServer, resp *jsonrpcResponse) {
	// publishDiagnostics notifications have method="textDocument/publishDiagnostics"
	// and params={uri, diagnostics[]}
	if resp.Method != "textDocument/publishDiagnostics" || resp.Params == nil {
		return
	}

	var diagParams struct {
		URI         string          `json:"uri"`
		Diagnostics json.RawMessage `json:"diagnostics"`
	}
	if err := json.Unmarshal(resp.Params, &diagParams); err == nil && diagParams.URI != "" && diagParams.Diagnostics != nil {
		srv.diagMu.Lock()
		srv.diagnosticsCache[diagParams.URI] = diagParams.Diagnostics
		srv.diagMu.Unlock()

		if t.logger != nil {
			t.logger.Debug("Cached push diagnostics", zap.String("uri", diagParams.URI))
		}
	}
}

// --- Formatting helpers ---

func (t *LSPTool) formatLocations(label string, raw json.RawMessage) (*Result, error) {
	if raw == nil || string(raw) == "null" {
		return &Result{Output: label + ": no results found", Success: true}, nil
	}

	// Can be a single Location or []Location
	var locations []struct {
		URI   string `json:"uri"`
		Range struct {
			Start struct {
				Line      int `json:"line"`
				Character int `json:"character"`
			} `json:"start"`
		} `json:"range"`
	}

	// Try as array first
	if err := json.Unmarshal(raw, &locations); err != nil {
		// Try as single location
		var single struct {
			URI   string `json:"uri"`
			Range struct {
				Start struct {
					Line      int `json:"line"`
					Character int `json:"character"`
				} `json:"start"`
			} `json:"range"`
		}
		if err2 := json.Unmarshal(raw, &single); err2 != nil {
			return &Result{Output: label + ": " + string(raw), Success: true}, nil
		}
		locations = append(locations, single)
	}

	if len(locations) == 0 {
		return &Result{Output: label + ": no results found", Success: true}, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s (%d result(s)):\n", label, len(locations)))
	for _, loc := range locations {
		path := uriToPath(loc.URI)
		sb.WriteString(fmt.Sprintf("  %s:%d:%d\n", path, loc.Range.Start.Line+1, loc.Range.Start.Character+1))
	}
	return &Result{Output: sb.String(), Success: true}, nil
}

func (t *LSPTool) formatHover(raw json.RawMessage) (*Result, error) {
	if raw == nil || string(raw) == "null" {
		return &Result{Output: "Hover: no information available", Success: true}, nil
	}

	var hover struct {
		Contents interface{} `json:"contents"`
	}
	if err := json.Unmarshal(raw, &hover); err != nil {
		return &Result{Output: "Hover: " + string(raw), Success: true}, nil
	}

	// contents can be string, MarkupContent{kind,value}, or []
	text := extractHoverText(hover.Contents)
	return &Result{Output: "Hover:\n" + text, Success: true}, nil
}

// formatDiagnostics handles pull-based diagnostics response (LSP 3.17 textDocument/diagnostic)
func (t *LSPTool) formatDiagnostics(raw json.RawMessage) (*Result, error) {
	if raw == nil || string(raw) == "null" {
		return &Result{Output: "Diagnostics: no issues", Success: true}, nil
	}

	var result struct {
		Items []diagnosticItem `json:"items"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return &Result{Output: "Diagnostics: " + string(raw), Success: true}, nil
	}

	return t.renderDiagnosticItems(result.Items), nil
}

// formatPushDiagnostics handles push-based diagnostics from publishDiagnostics cache
func (t *LSPTool) formatPushDiagnostics(raw json.RawMessage) (*Result, error) {
	if raw == nil || string(raw) == "null" || string(raw) == "[]" {
		return &Result{Output: "Diagnostics: no issues (push)", Success: true}, nil
	}

	var items []diagnosticItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return &Result{Output: "Diagnostics (push): " + string(raw), Success: true}, nil
	}

	result := t.renderDiagnosticItems(items)
	result.Output = strings.Replace(result.Output, "Diagnostics", "Diagnostics (push)", 1)
	return result, nil
}

// diagnosticItem represents a single LSP diagnostic.
type diagnosticItem struct {
	Range struct {
		Start struct {
			Line int `json:"line"`
		} `json:"start"`
	} `json:"range"`
	Severity int    `json:"severity"`
	Message  string `json:"message"`
	Source   string `json:"source"`
}

// renderDiagnosticItems formats diagnostic items into a human-readable string.
func (t *LSPTool) renderDiagnosticItems(items []diagnosticItem) *Result {
	if len(items) == 0 {
		return &Result{Output: "Diagnostics: no issues", Success: true}
	}

	severityNames := []string{"", "Error", "Warning", "Info", "Hint"}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Diagnostics (%d issue(s)):\n", len(items)))
	for _, d := range items {
		sev := "Unknown"
		if d.Severity > 0 && d.Severity < len(severityNames) {
			sev = severityNames[d.Severity]
		}
		sb.WriteString(fmt.Sprintf("  L%d [%s] %s", d.Range.Start.Line+1, sev, d.Message))
		if d.Source != "" {
			sb.WriteString(fmt.Sprintf(" (%s)", d.Source))
		}
		sb.WriteString("\n")
	}
	return &Result{Output: sb.String(), Success: true}
}

func (t *LSPTool) formatSymbols(raw json.RawMessage) (*Result, error) {
	if raw == nil || string(raw) == "null" {
		return &Result{Output: "Symbols: no symbols found", Success: true}, nil
	}

	var symbols []struct {
		Name     string `json:"name"`
		Kind     int    `json:"kind"`
		Location struct {
			Range struct {
				Start struct{ Line int } `json:"start"`
			} `json:"range"`
		} `json:"location"`
		Range struct {
			Start struct{ Line int } `json:"start"`
		} `json:"range"`
		Children []struct {
			Name string `json:"name"`
			Kind int    `json:"kind"`
		} `json:"children"`
	}
	if err := json.Unmarshal(raw, &symbols); err != nil {
		return &Result{Output: "Symbols: " + string(raw), Success: true}, nil
	}

	kindNames := map[int]string{
		1: "File", 2: "Module", 3: "Namespace", 4: "Package",
		5: "Class", 6: "Method", 7: "Property", 8: "Field",
		9: "Constructor", 10: "Enum", 11: "Interface", 12: "Function",
		13: "Variable", 14: "Constant", 15: "String", 16: "Number",
		17: "Boolean", 18: "Array", 19: "Object", 20: "Key",
		21: "Null", 22: "EnumMember", 23: "Struct", 24: "Event",
		25: "Operator", 26: "TypeParameter",
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Symbols (%d):\n", len(symbols)))
	for _, sym := range symbols {
		kind := kindNames[sym.Kind]
		if kind == "" {
			kind = fmt.Sprintf("Kind(%d)", sym.Kind)
		}
		line := sym.Range.Start.Line
		if line == 0 {
			line = sym.Location.Range.Start.Line
		}
		sb.WriteString(fmt.Sprintf("  L%d  [%s] %s\n", line+1, kind, sym.Name))
	}
	return &Result{Output: sb.String(), Success: true}, nil
}

func (t *LSPTool) formatCompletion(raw json.RawMessage) (*Result, error) {
	if raw == nil || string(raw) == "null" {
		return &Result{Output: "Completion: no suggestions", Success: true}, nil
	}

	// Can be CompletionList{items} or []CompletionItem
	var items []struct {
		Label  string `json:"label"`
		Kind   int    `json:"kind"`
		Detail string `json:"detail"`
	}

	// Try as CompletionList first
	var list struct {
		Items []struct {
			Label  string `json:"label"`
			Kind   int    `json:"kind"`
			Detail string `json:"detail"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &list); err == nil && len(list.Items) > 0 {
		items = list.Items
	} else {
		_ = json.Unmarshal(raw, &items)
	}

	if len(items) == 0 {
		return &Result{Output: "Completion: no suggestions", Success: true}, nil
	}

	// Limit to top 20
	limit := 20
	if len(items) < limit {
		limit = len(items)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Completion (%d suggestion(s), showing top %d):\n", len(items), limit))
	for i := 0; i < limit; i++ {
		item := items[i]
		detail := ""
		if item.Detail != "" {
			detail = " — " + item.Detail
		}
		sb.WriteString(fmt.Sprintf("  %s%s\n", item.Label, detail))
	}
	return &Result{Output: sb.String(), Success: true}, nil
}

// --- Utility functions ---

func detectLanguage(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".go":
		return "go"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx":
		return "javascript"
	case ".py":
		return "python"
	case ".rs":
		return "rust"
	default:
		return ""
	}
}

func languageServerCommand(lang string) (string, []string) {
	switch lang {
	case "go":
		return "gopls", []string{"serve"}
	case "typescript", "javascript":
		return "typescript-language-server", []string{"--stdio"}
	case "python":
		return "pylsp", nil
	case "rust":
		return "rust-analyzer", nil
	default:
		return "", nil
	}
}

func installHint(lang string) string {
	switch lang {
	case "go":
		return "go install golang.org/x/tools/gopls@latest"
	case "typescript", "javascript":
		return "npm install -g typescript-language-server typescript"
	case "python":
		return "pip install python-lsp-server"
	case "rust":
		return "rustup component add rust-analyzer"
	default:
		return "unknown"
	}
}

func pathToURI(p string) string {
	abs, _ := filepath.Abs(p)
	return "file://" + abs
}

func uriToPath(uri string) string {
	return strings.TrimPrefix(uri, "file://")
}

func intArg(args map[string]interface{}, key string, def int) int {
	if v, ok := args[key]; ok {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		case json.Number:
			i, _ := n.Int64()
			return int(i)
		}
	}
	return def
}

func extractHoverText(contents interface{}) string {
	switch v := contents.(type) {
	case string:
		return v
	case map[string]interface{}:
		if val, ok := v["value"]; ok {
			return fmt.Sprintf("%v", val)
		}
		b, _ := json.MarshalIndent(v, "", "  ")
		return string(b)
	case []interface{}:
		var parts []string
		for _, item := range v {
			parts = append(parts, extractHoverText(item))
		}
		return strings.Join(parts, "\n---\n")
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}
