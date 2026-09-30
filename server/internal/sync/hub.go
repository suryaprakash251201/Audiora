// Package sync keeps playback state in step across a user's devices.
package sync

import (
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/audiora/audiora/server/internal/db"
)

// State is the shared playback snapshot. Only one device is the controller
// (the one actually producing sound); the rest mirror it.
type State struct {
	TrackID    int64   `json:"trackId"`
	PositionMS int64   `json:"positionMs"`
	Playing    bool    `json:"playing"`
	Volume     float64 `json:"volume"`
	Shuffle    bool    `json:"shuffle"`
	Repeat     string  `json:"repeat"`
	Queue      []int64 `json:"queue"`
	UpdatedAt  int64   `json:"updatedAt"`
	// Profile is the transcode profile the controller is using, so a
	// mirroring device can match its quality settings.
	Profile string `json:"profile"`
}

// Envelope is the message format on the wire.
type Envelope struct {
	Type string          `json:"type"`
	Seq  int64           `json:"seq"`
	From string          `json:"from"`
	Data json.RawMessage `json:"data,omitempty"`
}

// Message types.
const (
	TypeState  = "state"  // a full playback snapshot
	TypeHello  = "hello"  // server greeting, carries the current state
	TypeWho    = "who"    // request the current controller
	TypeEvents = "events" // scan progress and other server-side notices
)

// Client is one connected device. The fields stay unexported so only this
// package decides how a client is stored and closed; the HTTP layer uses
// Send and the Hub methods.
type Client struct {
	id       string
	userID   int64
	send     chan []byte
	device   string
	closeOne sync.Once
}

// Send is the channel the hub writes broadcast messages to. It is closed when
// the client is unregistered, so a ranging read loop ends on its own.
func (c *Client) Send() <-chan []byte { return c.send }

// DeviceName is the human-readable label this device registered with, used
// only in log lines.
func (c *Client) DeviceName() string { return c.device }

// Hub fans playback state out to a user's connected devices and elects a
// single controller.
type Hub struct {
	db *db.DB

	mu sync.Mutex
	// clients is keyed by user then device id.
	clients map[int64]map[string]*Client
	// controller is the device currently producing audio, per user.
	controller map[int64]string
	// seq is a monotonic per-user counter used to drop stale updates.
	seq map[int64]int64
}

func NewHub(database *db.DB) *Hub {
	return &Hub{
		db:         database,
		clients:    make(map[int64]map[string]*Client),
		controller: make(map[int64]string),
		seq:        make(map[int64]int64),
	}
}

// Register adds a device and returns it. The caller owns the send channel
// and must close the connection when it returns.
func (h *Hub) Register(userID int64, deviceID, deviceName string) *Client {
	if deviceID == "" {
		// Without an id a client cannot be distinguished from a reconnect,
		// so every one would fight over the controller role.
		deviceID = "unknown"
	}
	c := &Client{
		id:     deviceID,
		userID: userID,
		device: deviceName,
		send:   make(chan []byte, 32),
	}

	h.mu.Lock()
	if h.clients[userID] == nil {
		h.clients[userID] = make(map[string]*Client)
	}
	// A reconnect on the same device replaces the old registration, so a
	// flaky phone does not accumulate dead sockets.
	if old, ok := h.clients[userID][deviceID]; ok {
		old.closeOne.Do(func() { close(old.send) })
	}
	h.clients[userID][deviceID] = c
	h.mu.Unlock()

	return c
}

// Unregister removes a device. If it was the controller, playback is stopped
// so the remaining devices do not keep mirroring a session that no longer
// has an owner.
func (h *Hub) Unregister(c *Client) {
	h.mu.Lock()
	if set, ok := h.clients[c.userID]; ok {
		if current, ok := set[c.id]; ok && current == c {
			delete(set, c.id)
		}
		if len(set) == 0 {
			delete(h.clients, c.userID)
		}
	}
	wasController := h.controller[c.userID] == c.id
	if wasController {
		delete(h.controller, c.userID)
	}
	empty := len(h.clients[c.userID]) == 0
	h.mu.Unlock()

	c.closeOne.Do(func() { close(c.send) })

	if wasController && !empty {
		slog.Info("playback controller disconnected", "user", c.userID, "device", c.device)
		h.broadcast(c.userID, TypeState, State{Playing: false, UpdatedAt: db.Now()})
	}
}

// Controller returns the device id currently producing audio.
func (h *Hub) Controller(userID int64) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.controller[userID]
}

// Claim makes a device the controller, stopping whichever device held the
// role before. This is what prevents two devices playing the same track out
// of loud at once.
func (h *Hub) Claim(userID int64, deviceID string) {
	h.mu.Lock()
	previous := h.controller[userID]
	h.controller[userID] = deviceID
	h.mu.Unlock()

	if previous != "" && previous != deviceID {
		// Tell the old controller to pause. It knows it is no longer in
		// charge because the broadcast names a different device.
		h.sendTo(userID, previous, Envelope{
			Type: "replaced",
			From: deviceID,
			Data: json.RawMessage(`{"reason":"another device took over playback"}`),
		})
	}
}

// currentState returns the last known state for a user, read from the
// database so a device joining mid-song sees the right position.
func (h *Hub) currentState(userID int64) State {
	var state State
	var raw string
	err := h.db.QueryRow(`SELECT state_json FROM sync_state WHERE user_id = ?`, userID).Scan(&raw)
	if err != nil || raw == "" {
		return State{}
	}
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		slog.Warn("could not decode stored sync state", "user", userID, "err", err)
		return State{}
	}
	return state
}

// Publish records a new state and pushes it to every other device. The
// sequence number is bumped under the same lock that assigns it, so two
// devices publishing at once cannot be assigned the same number.
func (h *Hub) Publish(userID int64, deviceID string, state State) int64 {
	h.mu.Lock()
	h.seq[userID]++
	seq := h.seq[userID]
	// Only a device that is actually playing holds the controller role.
	if state.Playing {
		h.controller[userID] = deviceID
	} else if h.controller[userID] == deviceID {
		delete(h.controller, userID)
	}
	h.mu.Unlock()

	state.UpdatedAt = db.Now()
	state.Queue = dedupeTail(state.Queue)

	encoded, err := json.Marshal(state)
	if err != nil {
		slog.Error("could not encode playback state", "err", err)
		return seq
	}

	// Persist so a device that connects later gets a sensible starting point.
	// The controller field is cleared when nothing is playing.
	if _, err := h.db.Exec(`
		INSERT INTO sync_state (user_id, controller, seq, state_json, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
		    controller = excluded.controller,
		    seq        = excluded.seq,
		    state_json = excluded.state_json,
		    updated_at = excluded.updated_at`,
		userID, h.Controller(userID), seq, string(encoded), db.Now()); err != nil {
		// Losing the snapshot is not fatal; the live broadcast still went out.
		slog.Warn("could not persist sync state", "user", userID, "err", err)
	}

	h.broadcastFrom(userID, deviceID, Envelope{Type: TypeState, Seq: seq, From: deviceID, Data: encoded})
	return seq
}

// broadcast sends a message to every device of a user, including the sender.
func (h *Hub) broadcast(userID int64, msgType string, state State) {
	encoded, err := json.Marshal(state)
	if err != nil {
		return
	}
	h.broadcastFrom(userID, "", Envelope{Type: msgType, From: "", Data: encoded})
}

// broadcastFrom sends a message to every device of a user. When from is
// non-empty the sender is skipped, since it already applied the change.
func (h *Hub) broadcastFrom(userID int64, from string, env Envelope) {
	payload, err := json.Marshal(env)
	if err != nil {
		slog.Error("could not encode sync envelope", "err", err)
		return
	}

	h.mu.Lock()
	targets := make([]*Client, 0, len(h.clients[userID]))
	for id, c := range h.clients[userID] {
		if id == from {
			continue
		}
		targets = append(targets, c)
	}
	h.mu.Unlock()

	for _, c := range targets {
		select {
		case c.send <- payload:
		default:
			// A device that cannot keep up is dropped rather than allowed to
			// block the broadcaster. It will re-sync on reconnect.
			slog.Warn("dropping slow sync client", "user", userID, "device", c.device)
			h.Unregister(c)
		}
	}
}

// sendTo delivers a message to one device, if it is connected.
func (h *Hub) sendTo(userID int64, deviceID string, env Envelope) {
	payload, err := json.Marshal(env)
	if err != nil {
		return
	}
	h.mu.Lock()
	c, ok := h.clients[userID][deviceID]
	h.mu.Unlock()
	if !ok {
		return
	}
	select {
	case c.send <- payload:
	default:
	}
}

// Snapshot returns the current state, the controlling device, and the
// sequence number a client should use to reject anything older.
func (h *Hub) Snapshot(userID int64) (State, string, int64) {
	h.mu.Lock()
	seq := h.seq[userID]
	controller := h.controller[userID]
	h.mu.Unlock()
	return h.currentState(userID), controller, seq
}

// Hello returns the greeting payload for a newly connected device: the
// current state, the sequence number it should reject anything below, and
// which device is in charge.
func (h *Hub) Hello(userID int64) Envelope {
	state := h.currentState(userID)
	h.mu.Lock()
	seq := h.seq[userID]
	controller := h.controller[userID]
	h.mu.Unlock()

	data, err := json.Marshal(map[string]any{
		"state":      state,
		"controller": controller,
	})
	if err != nil {
		data = []byte(`{}`)
	}
	return Envelope{Type: TypeHello, Seq: seq, Data: data}
}

// Announce pushes a server-side notice to a user's devices, used for scan
// progress so the admin UI updates live on a phone too.
func (h *Hub) Announce(userID int64, payload any) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return
	}
	h.broadcastFrom(userID, "", Envelope{Type: TypeEvents, Data: encoded})
}

// DeviceCount reports how many devices a user currently has connected.
func (h *Hub) DeviceCount(userID int64) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients[userID])
}

// dedupeTail removes consecutive duplicate track ids, which appear whenever
// a client re-sends an unchanged queue.
func dedupeTail(queue []int64) []int64 {
	if len(queue) < 2 {
		return queue
	}
	out := make([]int64, 0, len(queue))
	for i, id := range queue {
		if i > 0 && id == queue[i-1] {
			continue
		}
		out = append(out, id)
	}
	return out
}

// HeartbeatInterval is how often a client is expected to send a ping. The
// server drops connections that go quiet for much longer.
const HeartbeatInterval = 30 * time.Second
