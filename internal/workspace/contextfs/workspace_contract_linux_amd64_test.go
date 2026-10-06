//go:build linux && amd64

package contextfs

import (
	"context"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle/workspacecontract"
)

func TestLifecycleWorkspaceHonoursTheWorkspaceContract(t *testing.T) {
	workspacecontract.Verify(t, func(t *testing.T) workspacecontract.Subject {
		store, record := lifecycleFixture(t)
		reserveFixture(t, store, record)
		subject := workspacecontract.Subject{Workspace: store, Context: record.Name, KeptRuns: maxBoundedRuns}
		if err := store.ReadController(context.Background(), "", func(view prerequisites.StorageView) error {
			subject.Host = view.State.Host
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return subject
	})
}
