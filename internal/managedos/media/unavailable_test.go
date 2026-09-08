package media

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
		{"media add", func(ctx context.Context) error { return (Service{}).Add(ctx, AddMediaRequest{}) }},
		{"media list", func(ctx context.Context) error { return (Service{}).List(ctx, ListMediaRequest{}) }},
		{"media delete", func(ctx context.Context) error { return (Service{}).Delete(ctx, DeleteMediaRequest{}) }},
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
