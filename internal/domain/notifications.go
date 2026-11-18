package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// NotificationTypeMaxLength is the maximum length of a notification type.
const NotificationTypeMaxLength = 100

// Notification errors.
var (
	ErrNotificationTypeEmpty   = NewValidation("Notification.TypeEmpty", "The notification type is required.")
	ErrNotificationTypeTooLong = NewValidation("Notification.TypeTooLong", fmt.Sprintf("The notification type cannot exceed %d characters.", NotificationTypeMaxLength))
)

// ErrUnsupportedTarget reports that no delivery strategy handles a kind of target.
func ErrUnsupportedTarget(target Target) *Error {
	name := "<nil>"
	if target != nil {
		name = reflect.TypeOf(target).Name()
	}
	return NewFailure("Notification.UnsupportedTarget", fmt.Sprintf("No delivery strategy is registered for '%s'.", name))
}

// Notification is an event pushed to connections. Payload is arbitrary JSON delivered as is; nil means no payload.
type Notification struct {
	ID        uuid.UUID
	Type      string
	Payload   json.RawMessage
	CreatedAt time.Time
}

// NewNotification creates a notification with a time-ordered id (UUIDv7). The type is trimmed and must
// have between 1 and NotificationTypeMaxLength characters. A JSON null payload is stored as no payload.
func NewNotification(notificationType string, payload json.RawMessage, createdAt time.Time) (Notification, error) {
	trimmed := strings.TrimSpace(notificationType)
	if trimmed == "" {
		return Notification{}, ErrNotificationTypeEmpty
	}
	if utf8.RuneCountInString(trimmed) > NotificationTypeMaxLength {
		return Notification{}, ErrNotificationTypeTooLong
	}

	id, err := uuid.NewV7()
	if err != nil {
		return Notification{}, fmt.Errorf("generate notification id: %w", err)
	}

	var copied json.RawMessage
	if trimmedPayload := bytes.TrimSpace(payload); len(trimmedPayload) > 0 && !bytes.Equal(trimmedPayload, []byte("null")) {
		copied = bytes.Clone(trimmedPayload)
	}

	return Notification{ID: id, Type: trimmed, Payload: copied, CreatedAt: createdAt}, nil
}

// Target is who a notification is delivered to. New kinds of targets (group, broadcast, ...) are new types
// plus a new delivery strategy. The interface is sealed to this package.
type Target interface {
	isTarget()
}

// ClientTarget delivers to every connection of a client.
type ClientTarget struct {
	ClientID ClientID
}

// UserTarget delivers to the connection of one user of a client.
type UserTarget struct {
	ClientID ClientID
	UserID   UserID
}

func (ClientTarget) isTarget() {}
func (UserTarget) isTarget()   {}
