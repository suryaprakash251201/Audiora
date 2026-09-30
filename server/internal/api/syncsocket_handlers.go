package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/audiora/audiora/server/internal/auth"
	"github.com/audiora/audiora/server/internal/sync"
	"github.com/gorilla/websocket"
)

const (
	// writeWait bounds a single frame write. A slow client must not be able
	// to hold a goroutine open indefinitely.
	writeWait = 10 * time.Second
	// pongWait is how long a connection may stay silent before it is assumed
	// dead. Mobile clients behind NAT get dropped silently, so this has to be
	// comfortably longer than the heartbeat interval.
	pongWait       = 2 * sync.HeartbeatInterval
	maxMessageSize = 16 << 10
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  2048,
	WriteBufferSize: 2048,
	// Auth already happened in the middleware, so the origin check here is
	// only defence in depth against a cross-site WebSocket hijack.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// inbound is what a client sends up the socket.
type inbound struct {
	Type   string          `json:"type"`
	From   string          `json:"from"`
	Device string          `json:"device"`
	Claim  bool            `json:"claim"`
	Data   json.RawMessage `json:"data"`
}

// handleSyncState returns the snapshot over plain HTTP, which is what the app
// uses on cold start before the socket is up.
func (s *Server) handleSyncState(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFrom(r.Context())
	state, controller, seq := s.Hub.Snapshot(userID)

	writeJSON(w, http.StatusOK, map[string]any{
		"state":      state,
		"controller": controller,
		"seq":        seq,
	})
}

// handleSyncSocket upgrades the connection and relays playback state.
func (s *Server) handleSyncSocket(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFrom(r.Context())

	// Read the hello frame first so the client can identify itself before
	// the upgrade, which also gives a place to report a bad payload.
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade has already written a response.
		slog.Debug("websocket upgrade failed", "err", err)
		return
	}

	var first inbound
	conn.SetReadLimit(maxMessageSize)
	_ = conn.SetReadDeadline(time.Now().Add(pongWait))
	if err := conn.ReadJSON(&first); err != nil {
		_ = conn.Close()
		return
	}
	if first.Type != "hello" || first.From == "" {
		_ = conn.WriteJSON(sync.Envelope{Type: "error", Data: json.RawMessage(`{"message":"first message must be hello"}`)})
		_ = conn.Close()
		return
	}

	deviceID := first.From
	c := s.Hub.Register(userID, deviceID, first.Device)
	defer func() {
		s.Hub.Unregister(c)
		_ = conn.Close()
	}()

	// Greet with the current state so a phone opening the app mid-song
	// immediately shows the right track.
	if err := conn.WriteJSON(s.Hub.Hello(userID)); err != nil {
		return
	}

	// A client that asks to take over becomes the playback controller, and
	// the previous one is told to pause.
	if first.Claim {
		s.Hub.Claim(userID, deviceID)
	}

	// One goroutine drains the broadcast channel; this one only reads.
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		for msg := range c.Send() {
			_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		}
	}()

	for {
		var in inbound
		_ = conn.SetReadDeadline(time.Now().Add(pongWait))
		if err := conn.ReadJSON(&in); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				slog.Debug("sync socket closed", "user", userID, "device", deviceID, "err", err)
			}
			break
		}

		switch in.Type {
		case "ping":
			_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
			_ = conn.WriteJSON(sync.Envelope{Type: "pong"})
		case "state":
			var st sync.State
			if err := json.Unmarshal(in.Data, &st); err != nil {
				slog.Warn("discarding malformed sync state", "err", err)
				continue
			}
			s.Hub.Publish(userID, deviceID, st)
		}
	}

	// Closing the send channel lets the writer goroutine exit.
	_ = conn.Close()
	<-closed
}
