package websocket

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // 允许所有来源 (生产环境应限制)
	},
}

// MessageType 消息类型
type MessageType string

const (
	MessageTypeChat       MessageType = "chat"
	MessageTypeStream     MessageType = "stream"
	MessageTypeToolCall   MessageType = "tool_call"
	MessageTypeToolResult MessageType = "tool_result"
	MessageTypeApproval   MessageType = "approval"
	MessageTypeError      MessageType = "error"
	MessageTypePing       MessageType = "ping"
	MessageTypePong       MessageType = "pong"
)

// WSMessage WebSocket 消息
type WSMessage struct {
	Type      MessageType            `json:"type"`
	ID        string                 `json:"id,omitempty"`
	Content   string                 `json:"content,omitempty"`
	SessionID string                 `json:"session_id,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	Timestamp int64                  `json:"timestamp"`
}

// Client WebSocket 客户端
type Client struct {
	ID        string
	UserID    string
	SessionID string
	conn      *websocket.Conn
	send      chan []byte
	hub       *Hub
	logger    *zap.Logger
}

// Hub WebSocket 连接中心
type Hub struct {
	clients    map[string]*Client
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
	logger     *zap.Logger
	mu         sync.RWMutex

	// 回调
	onMessage func(client *Client, msg *WSMessage)
}

// NewHub 创建连接中心
func NewHub(logger *zap.Logger) *Hub {
	return &Hub{
		clients:    make(map[string]*Client),
		broadcast:  make(chan []byte),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		logger:     logger,
	}
}

// SetMessageHandler 设置消息处理器
func (h *Hub) SetMessageHandler(handler func(client *Client, msg *WSMessage)) {
	h.onMessage = handler
}

// Run 运行连接中心
func (h *Hub) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client.ID] = client
			h.mu.Unlock()
			h.logger.Info("Client connected",
				zap.String("client_id", client.ID),
				zap.String("user_id", client.UserID),
			)
		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client.ID]; ok {
				delete(h.clients, client.ID)
				close(client.send)
			}
			h.mu.Unlock()
			h.logger.Info("Client disconnected",
				zap.String("client_id", client.ID),
			)
		case message := <-h.broadcast:
			h.mu.RLock()
			for _, client := range h.clients {
				select {
				case client.send <- message:
				default:
					close(client.send)
					delete(h.clients, client.ID)
				}
			}
			h.mu.RUnlock()
		}
	}
}

// SendToClient 发送消息到指定客户端
func (h *Hub) SendToClient(clientID string, msg *WSMessage) error {
	h.mu.RLock()
	client, exists := h.clients[clientID]
	h.mu.RUnlock()

	if !exists {
		return nil
	}

	msg.Timestamp = time.Now().Unix()
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	select {
	case client.send <- data:
		return nil
	default:
		return nil
	}
}

// SendToSession 发送消息到指定会话的所有客户端
func (h *Hub) SendToSession(sessionID string, msg *WSMessage) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	msg.Timestamp = time.Now().Unix()
	data, _ := json.Marshal(msg)

	for _, client := range h.clients {
		if client.SessionID == sessionID {
			select {
			case client.send <- data:
			default:
			}
		}
	}
}

// GetClientCount 获取客户端数量
func (h *Hub) GetClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// Handler WebSocket 处理器
type Handler struct {
	hub    *Hub
	logger *zap.Logger
}

// NewHandler 创建 WebSocket 处理器
func NewHandler(hub *Hub, logger *zap.Logger) *Handler {
	return &Handler{
		hub:    hub,
		logger: logger,
	}
}

// ServeWS 处理 WebSocket 连接
func (h *Handler) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Error("Failed to upgrade connection", zap.Error(err))
		return
	}

	// 从查询参数获取用户信息
	userID := r.URL.Query().Get("user_id")
	sessionID := r.URL.Query().Get("session_id")
	clientID := r.URL.Query().Get("client_id")

	if clientID == "" {
		clientID = userID + "_" + time.Now().Format("20060102150405")
	}

	client := &Client{
		ID:        clientID,
		UserID:    userID,
		SessionID: sessionID,
		conn:      conn,
		send:      make(chan []byte, 256),
		hub:       h.hub,
		logger:    h.logger,
	}

	h.hub.register <- client

	// 启动读写协程
	go client.writePump()
	go client.readPump()
}

// readPump 读取消息
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(512 * 1024) // 512KB
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.logger.Error("WebSocket read error", zap.Error(err))
			}
			break
		}

		var msg WSMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			c.logger.Error("Failed to parse message", zap.Error(err))
			continue
		}

		// 处理 ping
		if msg.Type == MessageTypePing {
			c.send <- mustMarshal(&WSMessage{
				Type:      MessageTypePong,
				Timestamp: time.Now().Unix(),
			})
			continue
		}

		// 调用消息处理器
		if c.hub.onMessage != nil {
			c.hub.onMessage(c, &msg)
		}
	}
}

// writePump 写入消息
func (c *Client) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			if err := w.Close(); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// SendMessage 发送消息给客户端
func (c *Client) SendMessage(msg *WSMessage) {
	msg.Timestamp = time.Now().Unix()
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	c.send <- data
}

// GetID 获取客户端 ID
func (c *Client) GetID() string {
	return c.ID
}

// GetUserID 获取用户 ID
func (c *Client) GetUserID() string {
	return c.UserID
}

// GetSessionID 获取会话 ID
func (c *Client) GetSessionID() string {
	return c.SessionID
}

func mustMarshal(v interface{}) []byte {
	data, _ := json.Marshal(v)
	return data
}
