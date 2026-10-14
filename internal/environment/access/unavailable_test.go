package access

import (
	"context"
	"errors"
	"testing"

	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func TestUnwiredOrEndedClusterSessionsOpenNothing(t *testing.T) {
	if _, err := (Service{}).ClusterRsh(context.Background(), ClusterRshRequest{Name: "ceph"}); !errors.Is(err, availability.ErrNotImplemented) {
		t.Fatalf("unwired rsh = %v", err)
	}
	sessions := &recordingSessions{}
	service := New(&fixedGraph{catalog: sessionCatalog(labNodes()...)}, sessions, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.ClusterExec(ctx, ClusterExecRequest{ContextName: "lab", Names: []string{"ceph"}, Command: []string{"true"}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled exec = %v", err)
	}
	if _, err := service.ClusterRsh(context.Background(), ClusterRshRequest{Name: "ceph"}); diagnostics.Of(err) == nil || diagnostics.Of(err)[0].Code != "context.state" {
		t.Fatalf("no selection = %v", err)
	}
	if len(sessions.opened) != 0 {
		t.Fatalf("opened %+v", sessions.opened)
	}
}
