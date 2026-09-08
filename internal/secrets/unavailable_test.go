package secrets

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
		{"secret set", func(ctx context.Context) error { return (Store{}).Set(ctx, SetRequest{}) }},
		{"secret generate", func(ctx context.Context) error { return (Store{}).Generate(ctx, GenerateRequest{}) }},
		{"secret check", func(ctx context.Context) error { return (Store{}).Check(ctx, CheckRequest{}) }},
		{"secret list", func(ctx context.Context) error { return (Store{}).List(ctx, ListRequest{}) }},
		{"secret show", func(ctx context.Context) error { return (Store{}).Show(ctx, ShowRequest{}) }},
		{"secret delete", func(ctx context.Context) error { return (Store{}).Delete(ctx, DeleteRequest{}) }},
		{"secret encryption init", func(ctx context.Context) error { return (Encryption{}).Init(ctx, EncryptionInitRequest{}) }},
		{"secret encryption status", func(ctx context.Context) error { return (Encryption{}).Status(ctx, EncryptionStatusRequest{}) }},
		{"secret encryption rotate", func(ctx context.Context) error { return (Encryption{}).Rotate(ctx, EncryptionRotateRequest{}) }},
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
