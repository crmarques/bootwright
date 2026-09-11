package custody

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

func TestUnconfiguredServiceAndCancellation(t *testing.T) {
	tests := []struct {
		name   string
		invoke func(context.Context) error
	}{
		{"secret set", func(ctx context.Context) error { _, err := (Service{}).Set(ctx, SetRequest{}); return err }},
		{"secret generate", func(ctx context.Context) error { _, err := (Service{}).Generate(ctx, GenerateRequest{}); return err }},
		{"secret check", func(ctx context.Context) error { _, err := (Service{}).Check(ctx, CheckRequest{}); return err }},
		{"secret list", func(ctx context.Context) error { _, err := (Service{}).List(ctx, ListRequest{}); return err }},
		{"secret show", func(ctx context.Context) error { _, err := (Service{}).Show(ctx, ShowRequest{}); return err }},
		{"secret delete", func(ctx context.Context) error { _, err := (Service{}).Delete(ctx, DeleteRequest{}); return err }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.invoke(context.Background()); len(diagnostics.Of(err)) != 1 {
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
