package server

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/swarmviz/swarmviz/pkg/hub"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 20 * time.Second
	pingPeriod     = 15 * time.Second
	maxMessageSize = 512 * 1024

	// writePump coalesces every envelope pending in the send queue into one
	// text frame, separated by this byte. Clients MUST split on it before
	// parsing — a frame is 1..N envelopes, never guaranteed to be exactly one.
	// Go escapes newlines inside JSON strings, so this byte only ever appears
	// as a separator. The browser side of this contract lives in
	// frontend/src/hooks/useSwarmSocket.ts (ws.onmessage).
	envelopeSeparator = '\n'
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type Client struct {
	server    *Server
	conn      *websocket.Conn
	send      chan []byte
	mu        sync.Mutex
	closed    bool
	closeOnce sync.Once
}

func (c *Client) safeSend(data []byte) (sent bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return false
	}
	defer func() {
		if r := recover(); r != nil {
			c.closed = true
			sent = false
		}
	}()
	select {
	case c.send <- data:
		return true
	default:
		return false
	}
}

func (c *Client) close() {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		close(c.send)
		if c.conn != nil {
			_ = c.conn.Close()
		}
		c.mu.Unlock()
	})
}

type ClientMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

type AgentControlData struct {
	AgentID string            `json:"agent_id"`
	Action  hub.ControlAction `json:"action"`
}

func (c *Client) readPump() {
	defer func() {
		c.server.unregisterClient(c)
	}()

	if c.conn == nil {
		return
	}

	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			break
		}

		var clientMsg ClientMessage
		if err := json.Unmarshal(message, &clientMsg); err != nil {
			continue
		}

		if clientMsg.Type == "AGENT_CONTROL" {
			var ctrlData AgentControlData
			if err := json.Unmarshal(clientMsg.Data, &ctrlData); err != nil {
				continue
			}

			var ctrlErr error
			if c.server.eventHub != nil {
				ctrlErr = c.server.eventHub.ExecuteControlAction(ctrlData.AgentID, ctrlData.Action)
			} else {
				ctrlErr = hub.ErrControlNotImplemented
			}

			if ctrlErr != nil {
				errEnv := WSEnvelope{
					Version:   1,
					Seq:       0,
					Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
					Type:      "ERROR",
					Data: map[string]interface{}{
						"message":     ctrlErr.Error(),
						"recoverable": true,
					},
				}
				data, jsonErr := json.Marshal(errEnv)
				if jsonErr == nil {
					c.safeSend(data)
				}
			}
		}
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		if c.conn != nil {
			_ = c.conn.Close()
		}
	}()

	for {
		select {
		case message, ok := <-c.send:
			if c.conn == nil {
				return
			}
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			_, _ = w.Write(message)

			n := len(c.send)
			for i := 0; i < n; i++ {
				_, _ = w.Write([]byte{envelopeSeparator})
				_, _ = w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			if c.conn == nil {
				return
			}
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
