package prerequisites

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
		invoke func(context.Context) (*Report, error)
	}{
		{"preflight controller", func(ctx context.Context) (*Report, error) { return (Service{}).Check(ctx, CheckRequest{}) }},
		{"setup", func(ctx context.Context) (*Report, error) { return (Service{}).Setup(ctx, SetupRequest{}) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.invoke(context.Background()); !errors.Is(err, availability.ErrNotImplemented) {
				t.Fatalf("unavailable result = %v", err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if _, err := tt.invoke(ctx); !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled result = %v", err)
			}
			ctx, cancel = context.WithDeadline(context.Background(), time.Time{})
			defer cancel()
			if _, err := tt.invoke(ctx); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("expired result = %v", err)
			}
		})
	}
}
