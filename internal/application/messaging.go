package application

import (
	"context"
	"log/slog"
	"reflect"
	"slices"
	"time"

	"github.com/Judamaro/notyhub-golang/internal/domain"
)

// CommandHandler handles a command that changes state. Business errors are returned as *domain.Error.
type CommandHandler[C, R any] interface {
	Handle(ctx context.Context, command C) (R, error)
}

// QueryHandler handles a query that only reads state.
type QueryHandler[Q, R any] interface {
	Handle(ctx context.Context, query Q) (R, error)
}

// CommandHandlerFunc adapts a function to a CommandHandler.
type CommandHandlerFunc[C, R any] func(ctx context.Context, command C) (R, error)

// Handle calls f.
func (f CommandHandlerFunc[C, R]) Handle(ctx context.Context, command C) (R, error) {
	return f(ctx, command)
}

// Validator validates a command and returns its failures grouped by field (empty when valid).
type Validator[C any] interface {
	Validate(command C) map[string][]string
}

// Decorate wraps a command handler with the standard pipeline: logging, then validation, then the handler.
func Decorate[C, R any](inner CommandHandler[C, R], logger *slog.Logger, validators ...Validator[C]) CommandHandler[C, R] {
	return WithLogging(WithValidation(inner, validators...), logger)
}

// WithValidation runs the validators before the handler. When any fails, the handler is not called and a
// validation error with every failure, grouped by field and without duplicates, is returned.
func WithValidation[C, R any](inner CommandHandler[C, R], validators ...Validator[C]) CommandHandler[C, R] {
	if len(validators) == 0 {
		return inner
	}

	return CommandHandlerFunc[C, R](func(ctx context.Context, command C) (R, error) {
		errs := map[string][]string{}
		for _, v := range validators {
			for field, messages := range v.Validate(command) {
				for _, m := range messages {
					if !slices.Contains(errs[field], m) {
						errs[field] = append(errs[field], m)
					}
				}
			}
		}

		if len(errs) > 0 {
			var zero R
			return zero, domain.NewValidationErrors(errs)
		}

		return inner.Handle(ctx, command)
	})
}

// WithLogging logs the outcome and duration of every command.
func WithLogging[C, R any](inner CommandHandler[C, R], logger *slog.Logger) CommandHandler[C, R] {
	name := commandName[C]()

	return CommandHandlerFunc[C, R](func(ctx context.Context, command C) (R, error) {
		start := time.Now()
		logger.DebugContext(ctx, "Processing command", "command", name)

		result, err := inner.Handle(ctx, command)
		elapsed := float64(time.Since(start).Microseconds()) / 1000

		if err == nil {
			logger.InfoContext(ctx, "Command completed", "command", name, "elapsed_ms", elapsed)
		} else if e, ok := domain.AsError(err); ok {
			logger.WarnContext(ctx, "Command failed", "command", name, "error_code", e.Code, "error", e.Description, "elapsed_ms", elapsed)
		} else {
			logger.ErrorContext(ctx, "Command failed", "command", name, "error", err, "elapsed_ms", elapsed)
		}

		return result, err
	})
}

func commandName[C any]() string {
	t := reflect.TypeFor[C]()
	if t.Name() != "" {
		return t.Name()
	}
	return t.String()
}
