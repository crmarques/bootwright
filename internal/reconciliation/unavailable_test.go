package reconciliation

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
		{"plan", func(ctx context.Context) error { return (Lifecycle{}).Plan(ctx, PlanRequest{}) }},
		{"status", func(ctx context.Context) error { return (Lifecycle{}).Status(ctx, StatusRequest{}) }},
		{"apply", func(ctx context.Context) error { return (Lifecycle{}).Apply(ctx, ApplyRequest{}) }},
		{"destroy", func(ctx context.Context) error { return (Lifecycle{}).Destroy(ctx, DestroyRequest{}) }},
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
