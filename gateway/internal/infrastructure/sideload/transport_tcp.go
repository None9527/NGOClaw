package sideload

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
)

// TCPTransport communicates via a TCP or Unix socket connection.
// Used for modules running as persistent services.
type TCPTransport struct {
	conn    net.Conn
	reader  *bufio.Reader

	pending       map[interface{}]chan *Response
	mu            sync.Mutex
	notifyHandler func(req *Request)
	done          chan struct{}
	closeOnce     sync.Once
}

// NewTCPTransport creates a transport over an existing net.Conn (TCP or Unix)
func NewTCPTransport(conn net.Conn) *TCPTransport {
	t := &TCPTransport{
		conn:    conn,
		reader:  bufio.NewReaderSize(conn, 64*1024),
		pending: make(map[interface{}]chan *Response),
		done:    make(chan struct{}),
	}

	go t.tcpReadLoop()
	return t
}

// DialTCP connects to a TCP address and creates a transport
func DialTCP(ctx context.Context, address string) (*TCPTransport, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("dial TCP %s: %w", address, err)
	}
	return NewTCPTransport(conn), nil
}

// DialUnix connects to a Unix socket and creates a transport
func DialUnix(ctx context.Context, path string) (*TCPTransport, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		return nil, fmt.Errorf("dial Unix %s: %w", path, err)
	}
	return NewTCPTransport(conn), nil
}

func (t *TCPTransport) tcpReadLoop() {
	defer close(t.done)

	for {
		line, err := t.reader.ReadBytes('\n')
		if err != nil {
			return
		}

		// Try as Response
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

		// Try as notification
		var req Request
		if err := json.Unmarshal(line, &req); err == nil && req.Method != "" {
			if t.notifyHandler != nil {
				go t.notifyHandler(&req)
			}
		}
	}
}

// Send sends a request and waits for the response
func (t *TCPTransport) Send(ctx context.Context, req *Request) (*Response, error) {
	ch := make(chan *Response, 1)

	t.mu.Lock()
	t.pending[normalizeID(req.ID)] = ch
	t.mu.Unlock()

	if err := t.tcpWrite(req); err != nil {
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
func (t *TCPTransport) SendNotification(req *Request) error {
	return t.tcpWrite(req)
}

// OnNotification registers a handler for incoming notifications
func (t *TCPTransport) OnNotification(handler func(req *Request)) {
	t.notifyHandler = handler
}

// Close shuts down the transport
func (t *TCPTransport) Close() error {
	var err error
	t.closeOnce.Do(func() {
		err = t.conn.Close()
	})
	return err
}

func (t *TCPTransport) tcpWrite(msg interface{}) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}
	data = append(data, '\n')

	t.mu.Lock()
	defer t.mu.Unlock()

	_, err = t.conn.Write(data)
	return err
}
