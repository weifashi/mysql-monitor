package web

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"mysql-monitor/internal/monitor"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // same-origin enforced by cookie auth
	},
}

const (
	writeWait  = 10 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = (pongWait * 9) / 10
)

type Client struct {
	hub        *Hub
	conn       *websocket.Conn
	send       chan []byte
	room       string // "slow-queries" or "monitor-logs"
	databaseID int64  // filter for slow-queries room (0 = all)
}

type Hub struct {
	clients    map[*Client]struct{}
	register   chan *Client
	unregister chan *Client
	eventBus   *monitor.EventBus
}

func NewHub(eb *monitor.EventBus) *Hub {
	return &Hub{
		clients:    make(map[*Client]struct{}),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		eventBus:   eb,
	}
}

func (h *Hub) Run() {
	events := h.eventBus.Subscribe()
	defer h.eventBus.Unsubscribe(events)

	for {
		select {
		case client := <-h.register:
			h.clients[client] = struct{}{}

		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}

		case event := <-events:
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}
			for client := range h.clients {
				if !shouldSend(client, event) {
					continue
				}
				select {
				case client.send <- data:
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}
		}
	}
}

func shouldSend(c *Client, e monitor.MonitorEvent) bool {
	switch c.room {
	case "monitor-logs":
		// monitor-logs gets all event types except raw slow_query data
		return e.Type != "slow_query"
	case "slow-queries":
		// slow-queries room only gets slow_query events
		if e.Type != "slow_query" {
			return false
		}
		// filter by database if client specified one
		if c.databaseID > 0 && c.databaseID != e.DatabaseID {
			return false
		}
		return true
	case "rocketmq-logs":
		return strings.HasPrefix(e.Type, "rocketmq_")
	case "healthcheck-logs":
		return strings.HasPrefix(e.Type, "healthcheck_")
	case "grafana-logs":
		return strings.HasPrefix(e.Type, "grafana_")
	}
	return false
}

func (h *Hub) ServeWs(w http.ResponseWriter, r *http.Request, room string) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("websocket upgrade error: %v", err)
		return
	}

	var dbID int64
	if s := r.URL.Query().Get("database_id"); s != "" {
		dbID, _ = strconv.ParseInt(s, 10, 64)
	}

	client := &Client{
		hub:        h,
		conn:       conn,
		send:       make(chan []byte, 32),
		room:       room,
		databaseID: dbID,
	}
	h.register <- client

	go client.writePump()
	go client.readPump()
}

func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()
	c.conn.SetReadLimit(512)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			break
		}
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
