package environment

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
		{"preflight infra", func(ctx context.Context) error {
			return (Inspection{}).PreflightInfrastructure(ctx, InfrastructurePreflightRequest{})
		}},
		{"preflight clusters", func(ctx context.Context) error {
			return (Inspection{}).PreflightClusters(ctx, ClustersPreflightRequest{})
		}},
		{"preflight all", func(ctx context.Context) error { return (Inspection{}).PreflightAll(ctx, AllPreflightRequest{}) }},
		{"cluster list", func(ctx context.Context) error { return (Inspection{}).ListClusters(ctx, ListClustersRequest{}) }},
		{"cluster info", func(ctx context.Context) error { return (Inspection{}).ClusterInfo(ctx, ClusterInfoRequest{}) }},
		{"cluster rsh", func(ctx context.Context) error { return (Inspection{}).ClusterRsh(ctx, ClusterRshRequest{}) }},
		{"cluster exec", func(ctx context.Context) error { return (Inspection{}).ClusterExec(ctx, ClusterExecRequest{}) }},
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
