package realtime

import (
	"encoding/json"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"github.com/Judamaro/notyhub-golang/internal/domain"
)

// Events of the WebSocket protocol. Every frame is a JSON text message {"event": "...", "data": ...}.
const (
	// EventConnected is sent by the server when the connection opens; data: {connectionId}.
	EventConnected = "Connected"
	// EventReceiveNotification delivers a notification; data: {id, type, payload, createdAt}.
	EventReceiveNotification = "ReceiveNotification"
	// EventConnectionReplaced is sent right before closing a connection replaced by a new connection of the same user.
	EventConnectionReplaced = "ConnectionReplaced"
	// EventConnectionID answers a GetConnectionId request; data: {connectionId}.
	EventConnectionID = "ConnectionId"
	// MethodGetConnectionID is sent by clients to learn the id of their connection.
	MethodGetConnectionID = "GetConnectionId"
)

// StatusConnectionReplaced is the close code of a connection replaced by a new connection of the same user.
const StatusConnectionReplaced websocket.StatusCode = 4000

// Frame is the envelope of every WebSocket message.
type Frame struct {
	Event string `json:"event"`
	Data  any    `json:"data,omitempty"`
}

// NotificationMessage is the data of a ReceiveNotification frame.
type NotificationMessage struct {
	ID        uuid.UUID       `json:"id"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"createdAt"`
}

// ConnectionInfo is the data of the Connected and ConnectionId frames.
type ConnectionInfo struct {
	ConnectionID string `json:"connectionId"`
}

// NewNotificationMessage maps a notification to its wire contract.
func NewNotificationMessage(n domain.Notification) NotificationMessage {
	return NotificationMessage{ID: n.ID, Type: n.Type, Payload: n.Payload, CreatedAt: n.CreatedAt}
}

func encodeFrame(event string, data any) []byte {
	b, err := json.Marshal(Frame{Event: event, Data: data})
	if err != nil {
		// Frames only hold values that always marshal (strings, times, UUIDs and already valid JSON).
		panic(err)
	}
	return b
}
