package machine

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
		{"machine list", func(ctx context.Context) error { return (Inventory{}).List(ctx, ListRequest{}) }},
		{"machine rsh", func(ctx context.Context) error { return (Access{}).Rsh(ctx, RshRequest{}) }},
		{"machine exec", func(ctx context.Context) error { return (Access{}).Exec(ctx, ExecRequest{}) }},
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
