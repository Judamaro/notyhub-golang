package notifications

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/Judamaro/notyhub-golang/internal/application"
	"github.com/Judamaro/notyhub-golang/internal/domain"
)

// SendCommand sends a notification to a target.
type SendCommand struct {
	Target  domain.Target
	Type    string
	Payload json.RawMessage
}

// SendResponse is the result of sending a notification.
type SendResponse struct {
	NotificationID       uuid.UUID `json:"notificationId"`
	DeliveredConnections int       `json:"deliveredConnections"`
	SentAt               time.Time `json:"sentAt"`
}

// SendValidator validates a SendCommand: the target is required and the type is required with at most
// domain.NotificationTypeMaxLength characters.
type SendValidator struct{}

// Validate returns the failures of the command grouped by field.
func (SendValidator) Validate(cmd SendCommand) map[string][]string {
	errs := map[string][]string{}
	if cmd.Target == nil {
		errs["Target"] = append(errs["Target"], "'Target' must not be empty.")
	}
	if strings.TrimSpace(cmd.Type) == "" {
		errs["Type"] = append(errs["Type"], "'Type' must not be empty.")
	} else if n := utf8.RuneCountInString(cmd.Type); n > domain.NotificationTypeMaxLength {
		errs["Type"] = append(errs["Type"], fmt.Sprintf("The length of 'Type' must be %d characters or fewer. You entered %d characters.", domain.NotificationTypeMaxLength, n))
	}
	return errs
}

// SendHandler creates a notification, resolves its recipients through the strategy of the target and
// dispatches it.
type SendHandler struct {
	resolver   *StrategyResolver
	dispatcher application.NotificationDispatcher
	clock      application.Clock
	metrics    application.Metrics
}

// NewSendHandler creates a SendHandler.
func NewSendHandler(resolver *StrategyResolver, dispatcher application.NotificationDispatcher, clock application.Clock, metrics application.Metrics) *SendHandler {
	return &SendHandler{resolver: resolver, dispatcher: dispatcher, clock: clock, metrics: metrics}
}

// Handle sends the notification. It fails with a NotFound error when the target has no connections.
func (h *SendHandler) Handle(ctx context.Context, cmd SendCommand) (SendResponse, error) {
	notification, err := domain.NewNotification(cmd.Type, cmd.Payload, h.clock())
	if err != nil {
		return SendResponse{}, err
	}

	strategy, err := h.resolver.Resolve(cmd.Target)
	if err != nil {
		return SendResponse{}, err
	}

	recipients, err := strategy.ResolveRecipients(ctx, cmd.Target)
	if err != nil {
		if e, ok := domain.AsError(err); ok && e.Type == domain.ErrorTypeNotFound {
			h.metrics.NotificationNotFound(cmd.Target)
		}
		return SendResponse{}, err
	}

	start := time.Now()
	if err := h.dispatcher.Send(ctx, recipients, notification); err != nil {
		return SendResponse{}, fmt.Errorf("dispatch notification: %w", err)
	}
	h.metrics.NotificationDelivered(cmd.Target, len(recipients), time.Since(start))

	return SendResponse{
		NotificationID:       notification.ID,
		DeliveredConnections: len(recipients),
		SentAt:               notification.CreatedAt,
	}, nil
}
