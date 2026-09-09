package encryption

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/desiredstate"
)

func TestUnconfiguredServiceAndCancellation(t *testing.T) {
	tests := []struct {
		name   string
		invoke func(context.Context) error
	}{
		{"secret encryption init", func(ctx context.Context) error { _, err := (Service{}).Init(ctx, EncryptionInitRequest{}); return err }},
		{"secret encryption status", func(ctx context.Context) error {
			_, err := (Service{}).Status(ctx, EncryptionStatusRequest{})
			return err
		}},
		{"secret encryption rotate", func(ctx context.Context) error {
			_, err := (Service{}).Rotate(ctx, EncryptionRotateRequest{})
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.invoke(context.Background()); len(desiredstate.DiagnosticsOf(err)) != 1 {
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
