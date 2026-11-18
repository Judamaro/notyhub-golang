// Package notifications holds the use case that sends notifications and the strategies that resolve
// which connections a notification is delivered to.
package notifications

import (
	"context"

	"github.com/Judamaro/notyhub-golang/internal/application"
	"github.com/Judamaro/notyhub-golang/internal/domain"
)

// DeliveryStrategy resolves the connections a kind of target is delivered to (Strategy pattern).
type DeliveryStrategy interface {
	CanHandle(target domain.Target) bool
	ResolveRecipients(ctx context.Context, target domain.Target) ([]domain.ConnectionID, error)
}

// StrategyResolver picks the strategy that handles a target (Factory). A new kind of target only needs a new
// strategy registered here; the send handler does not change.
type StrategyResolver struct {
	strategies []DeliveryStrategy
}

// NewStrategyResolver creates a resolver over the given strategies, tried in order.
func NewStrategyResolver(strategies ...DeliveryStrategy) *StrategyResolver {
	return &StrategyResolver{strategies: strategies}
}

// DefaultStrategies returns the client and user strategies.
func DefaultStrategies(registry application.ConnectionRegistry) []DeliveryStrategy {
	return []DeliveryStrategy{NewClientDeliveryStrategy(registry), NewUserDeliveryStrategy(registry)}
}

// Resolve returns the first strategy that handles the target, or Notification.UnsupportedTarget.
func (r *StrategyResolver) Resolve(target domain.Target) (DeliveryStrategy, error) {
	for _, s := range r.strategies {
		if s.CanHandle(target) {
			return s, nil
		}
	}
	return nil, domain.ErrUnsupportedTarget(target)
}

// typedStrategy adapts a function over a concrete target type to a DeliveryStrategy.
type typedStrategy[T domain.Target] struct {
	resolve func(ctx context.Context, target T) ([]domain.ConnectionID, error)
}

func (s typedStrategy[T]) CanHandle(target domain.Target) bool {
	_, ok := target.(T)
	return ok
}

func (s typedStrategy[T]) ResolveRecipients(ctx context.Context, target domain.Target) ([]domain.ConnectionID, error) {
	typed, ok := target.(T)
	if !ok {
		return nil, domain.ErrUnsupportedTarget(target)
	}
	return s.resolve(ctx, typed)
}

// NewClientDeliveryStrategy delivers to every connection of a client; Client.NoConnections when it has none.
func NewClientDeliveryStrategy(registry application.ConnectionRegistry) DeliveryStrategy {
	return typedStrategy[domain.ClientTarget]{resolve: func(ctx context.Context, target domain.ClientTarget) ([]domain.ConnectionID, error) {
		conns, err := registry.GetByClient(ctx, target.ClientID)
		if err != nil {
			return nil, err
		}
		if len(conns) == 0 {
			return nil, domain.ErrClientHasNoConnections(target.ClientID)
		}

		ids := make([]domain.ConnectionID, 0, len(conns))
		for _, c := range conns {
			ids = append(ids, c.ConnectionID)
		}
		return ids, nil
	}}
}

// NewUserDeliveryStrategy delivers to the connection of a user; User.NotConnected when there is none.
func NewUserDeliveryStrategy(registry application.ConnectionRegistry) DeliveryStrategy {
	return typedStrategy[domain.UserTarget]{resolve: func(ctx context.Context, target domain.UserTarget) ([]domain.ConnectionID, error) {
		conn, err := registry.GetByUser(ctx, target.ClientID, target.UserID)
		if err != nil {
			return nil, err
		}
		if conn == nil {
			return nil, domain.ErrUserNotConnected(target.ClientID, target.UserID)
		}
		return []domain.ConnectionID{conn.ConnectionID}, nil
	}}
}
