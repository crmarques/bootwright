package addons

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
		{"add-ons list", func(ctx context.Context) error { return (Catalog{}).List(ctx, ListRequest{}) }},
		{"add-ons add", func(ctx context.Context) error { return (Catalog{}).Add(ctx, AddRequest{}) }},
		{"add-ons delete", func(ctx context.Context) error { return (Catalog{}).Delete(ctx, DeleteRequest{}) }},
		{"preflight add-ons", func(ctx context.Context) error { return (Prerequisites{}).Check(ctx, PreflightRequest{}) }},
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
