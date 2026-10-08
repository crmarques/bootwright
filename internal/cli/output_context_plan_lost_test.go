package cli

import (
	"bytes"
	"context"
	"testing"

	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// A lost context's keyring went with its directory, so its deletion plan says
// that instead of promising to remove it.
func TestALostContextsPlanSaysItsKeyringIsAlreadyGone(t *testing.T) {
	plan := contexts.DeletionPlan{
		Context: "retired", Mode: contexts.Ready, Revision: "rev-3b8d0f5a9c1e4d7b2a6f8c0e1d3b5a7f",
		Reservations: []string{"unit:retired"}, Abandons: contexts.AbandonsUnlisted, Reason: "its directory is gone", Lost: true,
	}
	var out, errOut bytes.Buffer
	if err := NewContextPlanPresenter(&out, &errOut).PresentDeletion(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	matchesTextGolden(t, "context-delete-lost-plan", out.Bytes())
	if errOut.Len() != 0 {
		t.Fatalf("standard error = %q, want empty", errOut.String())
	}
}
