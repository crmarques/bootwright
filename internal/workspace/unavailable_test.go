package workspace

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/availability"
)

func TestUnavailableComponents(t *testing.T) {
	tests := []struct {
		name   string
		invoke func(context.Context) error
	}{
		{"context init", func(ctx context.Context) error { return (Contexts{}).Init(ctx, InitRequest{}) }},
		{"context update", func(ctx context.Context) error { return (Contexts{}).Update(ctx, UpdateRequest{}) }},
		{"context use", func(ctx context.Context) error { return (Contexts{}).Use(ctx, UseRequest{}) }},
		{"context list", func(ctx context.Context) error { return (Contexts{}).List(ctx, ListRequest{}) }},
		{"context current", func(ctx context.Context) error { return (Contexts{}).Current(ctx, CurrentRequest{}) }},
		{"context delete", func(ctx context.Context) error { return (Contexts{}).Delete(ctx, DeleteRequest{}) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.invoke(context.Background()); !errors.Is(err, availability.ErrNotImplemented) {
				t.Fatalf("unavailable result = %v", err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err := tt.invoke(ctx); !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled result = %v", err)
			}
			ctx, cancel = context.WithDeadline(context.Background(), time.Time{})
			defer cancel()
			if err := tt.invoke(ctx); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("expired result = %v", err)
			}
		})
	}
}
