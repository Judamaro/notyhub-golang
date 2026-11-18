package application_test

import (
	"context"
	"io"
	"log/slog"
	"reflect"
	"testing"

	"github.com/Judamaro/notyhub-golang/internal/application"
	"github.com/Judamaro/notyhub-golang/internal/domain"
)

type command struct{ Name string }

type validatorFunc func(command) map[string][]string

func (f validatorFunc) Validate(c command) map[string][]string { return f(c) }

func TestWithValidation(t *testing.T) {
	calls := 0
	inner := application.CommandHandlerFunc[command, string](func(context.Context, command) (string, error) {
		calls++
		return "ok", nil
	})

	nameRequired := validatorFunc(func(c command) map[string][]string {
		if c.Name == "" {
			return map[string][]string{"Name": {"'Name' must not be empty."}}
		}
		return nil
	})
	duplicate := validatorFunc(func(c command) map[string][]string {
		if c.Name == "" {
			return map[string][]string{"Name": {"'Name' must not be empty.", "other"}}
		}
		return nil
	})

	handler := application.WithValidation(inner, nameRequired, duplicate)

	t.Run("invalid command does not reach the handler", func(t *testing.T) {
		_, err := handler.Handle(context.Background(), command{})
		e, ok := domain.AsError(err)
		if !ok || e.Code != domain.ValidationErrorCode {
			t.Fatalf("error = %v", err)
		}
		want := map[string][]string{"Name": {"'Name' must not be empty.", "other"}}
		if !reflect.DeepEqual(e.Fields, want) {
			t.Fatalf("fields = %v, want %v (grouped, without duplicates)", e.Fields, want)
		}
		if calls != 0 {
			t.Fatalf("handler called %d times", calls)
		}
	})

	t.Run("valid command reaches the handler", func(t *testing.T) {
		got, err := handler.Handle(context.Background(), command{Name: "x"})
		if err != nil || got != "ok" || calls != 1 {
			t.Fatalf("got %q, %v, calls %d", got, err, calls)
		}
	})
}

func TestDecorateReturnsInnerResult(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	failure := domain.NewNotFound("X.NotFound", "missing")
	inner := application.CommandHandlerFunc[command, int](func(context.Context, command) (int, error) { return 0, failure })

	_, err := application.Decorate[command, int](inner, logger).Handle(context.Background(), command{Name: "x"})
	if err != failure { //nolint:errorlint // the same error value must pass through
		t.Fatalf("error = %v, want the inner error", err)
	}
}
