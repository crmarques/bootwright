package inspection

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/availability"
)

func TestUnwiredOrEndedServiceReadsNothing(t *testing.T) {
	tests := []struct {
		name   string
		invoke func(Service, context.Context) error
	}{
		{"cluster list", func(s Service, ctx context.Context) error {
			_, err := s.ListClusters(ctx, ListClustersRequest{})
			return err
		}},
		{"cluster info", func(s Service, ctx context.Context) error {
			_, err := s.ClusterInfo(ctx, ClusterInfoRequest{})
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.invoke(Service{}, context.Background()); !errors.Is(err, availability.ErrNotImplemented) {
				t.Fatalf("unwired result = %v", err)
			}
			wired := newFixture(t).service("lab")
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err := tt.invoke(wired, ctx); !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled result = %v", err)
			}
			ctx, cancel = context.WithDeadline(context.Background(), time.Time{})
			defer cancel()
			if err := tt.invoke(wired, ctx); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("expired result = %v", err)
			}
		})
	}
}
