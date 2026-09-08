package contexts

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/availability"
)

func TestUnavailableService(t *testing.T) {
	tests := []struct {
		name   string
		invoke func(context.Context) error
	}{
		{"context init", func(ctx context.Context) error { _, err := (Service{}).Init(ctx, InitRequest{}); return err }},
		{"context update", func(ctx context.Context) error { _, err := (Service{}).Update(ctx, UpdateRequest{}); return err }},
		{"context use", func(ctx context.Context) error { _, err := (Service{}).Use(ctx, UseRequest{}); return err }},
		{"context list", func(ctx context.Context) error { _, err := (Service{}).List(ctx, ListRequest{}); return err }},
		{"context current", func(ctx context.Context) error { _, err := (Service{}).Current(ctx, CurrentRequest{}); return err }},
		{"context delete", func(ctx context.Context) error { _, err := (Service{}).Delete(ctx, DeleteRequest{}); return err }},
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
