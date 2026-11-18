package domain

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// IdentifierMaxLength is the maximum length of a client id or user id.
const IdentifierMaxLength = 128

// ConnectionIDMaxLength is the maximum length of a connection id.
const ConnectionIDMaxLength = 128

// Connection errors.
var (
	ErrClientIDEmpty            = NewValidation("ClientId.Empty", "The client id is required.")
	ErrClientIDContainsSpace    = NewValidation("ClientId.ContainsWhitespace", "The client id cannot contain whitespace.")
	ErrClientIDTooLong          = NewValidation("ClientId.TooLong", fmt.Sprintf("The client id cannot exceed %d characters.", IdentifierMaxLength))
	ErrClientIDMismatch         = NewValidation("ClientId.Mismatch", "The client id does not match the client id of the access token.")
	ErrUserIDEmpty              = NewValidation("UserId.Empty", "The user id is required.")
	ErrUserIDContainsSpace      = NewValidation("UserId.ContainsWhitespace", "The user id cannot contain whitespace.")
	ErrUserIDTooLong            = NewValidation("UserId.TooLong", fmt.Sprintf("The user id cannot exceed %d characters.", IdentifierMaxLength))
	ErrConnectionIDEmpty        = NewValidation("ConnectionId.Empty", "The connection id is required.")
	ErrConnectionIDTooLong      = NewValidation("ConnectionId.TooLong", fmt.Sprintf("The connection id cannot exceed %d characters.", ConnectionIDMaxLength))
	errClientHasNoConnectionsID = "Client.NoConnections"
	errUserNotConnectedID       = "User.NotConnected"
	errConnectionNotFoundID     = "Connection.NotFound"
)

// ErrClientHasNoConnections reports that a client has no active connections.
func ErrClientHasNoConnections(clientID ClientID) *Error {
	return NewNotFound(errClientHasNoConnectionsID, fmt.Sprintf("The client '%s' has no active connections.", clientID))
}

// ErrUserNotConnected reports that a user of a client has no active connection.
func ErrUserNotConnected(clientID ClientID, userID UserID) *Error {
	return NewNotFound(errUserNotConnectedID, fmt.Sprintf("The user '%s' of the client '%s' has no active connection.", userID, clientID))
}

// ErrConnectionNotFound reports that a connection is not registered.
func ErrConnectionNotFound(connectionID ConnectionID) *Error {
	return NewNotFound(errConnectionNotFoundID, fmt.Sprintf("The connection '%s' was not found.", connectionID))
}

// ClientID identifies the client (tenant or application) that owns connections. It is normalized to lowercase.
type ClientID string

// NewClientID validates and normalizes a client id: required, no whitespace, at most 128 characters.
func NewClientID(value string) (ClientID, error) {
	normalized, err := normalizeIdentifier(value, ErrClientIDEmpty, ErrClientIDContainsSpace, ErrClientIDTooLong)
	if err != nil {
		return "", err
	}
	return ClientID(normalized), nil
}

// String returns the client id.
func (id ClientID) String() string { return string(id) }

// UserID identifies a user within a client. It is normalized to lowercase.
type UserID string

// NewUserID validates and normalizes a user id: required, no whitespace, at most 128 characters.
func NewUserID(value string) (UserID, error) {
	normalized, err := normalizeIdentifier(value, ErrUserIDEmpty, ErrUserIDContainsSpace, ErrUserIDTooLong)
	if err != nil {
		return "", err
	}
	return UserID(normalized), nil
}

// String returns the user id.
func (id UserID) String() string { return string(id) }

// ConnectionID identifies a realtime connection; the hub generates it.
type ConnectionID string

// NewConnectionID validates a connection id: required, trimmed, at most 128 characters.
func NewConnectionID(value string) (ConnectionID, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", ErrConnectionIDEmpty
	}
	if utf8.RuneCountInString(trimmed) > ConnectionIDMaxLength {
		return "", ErrConnectionIDTooLong
	}
	return ConnectionID(trimmed), nil
}

// String returns the connection id.
func (id ConnectionID) String() string { return string(id) }

// ClientConnection is an open connection of a user of a client.
type ClientConnection struct {
	ConnectionID ConnectionID
	ClientID     ClientID
	UserID       UserID
	ConnectedAt  time.Time
	UserAgent    string
}

// normalizeIdentifier rejects empty values, values with whitespace anywhere and values longer than
// IdentifierMaxLength, and lowercases the rest. Whitespace is rejected, never trimmed.
func normalizeIdentifier(value string, empty, containsSpace, tooLong *Error) (string, error) {
	if value == "" {
		return "", empty
	}
	if strings.ContainsFunc(value, unicode.IsSpace) {
		return "", containsSpace
	}
	if utf8.RuneCountInString(value) > IdentifierMaxLength {
		return "", tooLong
	}
	return strings.ToLower(value), nil
}
