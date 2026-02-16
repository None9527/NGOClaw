package sideload

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// Transport is the interface for JSON-RPC communication with a module
type Transport interface {
	// Send sends a JSON-RPC request and returns the response
	Send(ctx context.Context, req *Request) (*Response, error)
	// SendNotification sends a JSON-RPC notification (no response expected)
	SendNotification(req *Request) error
	// OnNotification registers a handler for incoming notifications from the module
	OnNotification(handler func(req *Request))
	// Close shuts down the transport
	Close() error
}

// StdioTransport communicates via stdin/stdout of a child process.
// This is the default transport for sideload modules, matching MCP convention.
type StdioTransport struct {
	stdin  io.WriteCloser
	stdout io.ReadCloser
	reader *bufio.Reader

	pending       map[interface{}]chan *Response
	mu            sync.Mutex
	notifyHandler func(req *Request)
	done          chan struct{}
	closeOnce     sync.Once
}

// NewStdioTransport creates a transport over stdin/stdout of a child process
func NewStdioTransport(stdin io.WriteCloser, stdout io.ReadCloser) *StdioTransport {
	t := &StdioTransport{
		stdin:   stdin,
		stdout:  stdout,
		reader:  bufio.NewReaderSize(stdout, 64*1024),
		pending: make(map[interface{}]chan *Response),
		done:    make(chan struct{}),
	}

	go t.readLoop()
	return t
}

func (t *StdioTransport) readLoop() {
	defer close(t.done)

	for {
		line, err := t.reader.ReadBytes('\n')
		if err != nil {
			return
		}

		// Try parsing as Response first
		var resp Response
		if err := json.Unmarshal(line, &resp); err == nil && resp.ID != nil {
			t.mu.Lock()
			ch, exists := t.pending[normalizeID(resp.ID)]
			if exists {
				delete(t.pending, normalizeID(resp.ID))
			}
			t.mu.Unlock()

			if ch != nil {
				ch <- &resp
			}
			continue
		}

		// Try as notification (Request without id)
		var req Request
		if err := json.Unmarshal(line, &req); err == nil && req.Method != "" {
			if t.notifyHandler != nil {
				go t.notifyHandler(&req)
			}
		}
	}
}

// Send sends a request and waits for the response
func (t *StdioTransport) Send(ctx context.Context, req *Request) (*Response, error) {
	ch := make(chan *Response, 1)

	t.mu.Lock()
	t.pending[normalizeID(req.ID)] = ch
	t.mu.Unlock()

	if err := t.write(req); err != nil {
		t.mu.Lock()
		delete(t.pending, normalizeID(req.ID))
		t.mu.Unlock()
		return nil, err
	}

	select {
	case resp := <-ch:
		return resp, nil
	case <-ctx.Done():
		t.mu.Lock()
		delete(t.pending, normalizeID(req.ID))
		t.mu.Unlock()
		return nil, ctx.Err()
	case <-t.done:
		return nil, fmt.Errorf("transport closed")
	}
}

// SendNotification sends without expecting a response
func (t *StdioTransport) SendNotification(req *Request) error {
	return t.write(req)
}

// OnNotification registers a handler for incoming notifications
func (t *StdioTransport) OnNotification(handler func(req *Request)) {
	t.notifyHandler = handler
}

// Close shuts down the transport
func (t *StdioTransport) Close() error {
	var err error
	t.closeOnce.Do(func() {
		err = t.stdin.Close()
	})
	return err
}

func (t *StdioTransport) write(msg interface{}) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}
	data = append(data, '\n')

	t.mu.Lock()
	defer t.mu.Unlock()

	_, err = t.stdin.Write(data)
	return err
}

// normalizeID ensures consistent key type for pending map
func normalizeID(id interface{}) interface{} {
	// JSON numbers decode as float64
	if f, ok := id.(float64); ok {
		return int(f)
	}
	return id
}
