package contextguard

import (
	"context"
	"errors"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// Evidence the guard cannot read refuses as the one failure the context
// service tells apart, keeping its context.state diagnostic, and only a
// completed apply reads as applied.
func TestUnreadableEvidenceIsToldApartAndOnlyACompletedApplyReadsApplied(t *testing.T) {
	for _, data := range []string{"", "{", `{"version":2,"operation":"none","ownership":"none"}`} {
		_, err := (Guard{}).Check(context.Background(), []byte(data))
		reported := diagnostics.Of(err)
		if !errors.Is(err, contexts.ErrUnreadableEvidence) || len(reported) != 1 || reported[0].Code != "context.state" ||
			reported[0].Message != "context mutation evidence is missing, corrupt or unsupported" {
			t.Fatalf("%q refused with %v: %#v", data, err, reported)
		}
	}
	for _, operation := range []reconciliation.MutationOperation{reconciliation.MutationNone, reconciliation.MutationPending, reconciliation.MutationFailed, reconciliation.MutationUnknown, reconciliation.MutationApplied} {
		data, err := reconciliation.Evidence{Operation: operation, Ownership: reconciliation.OwnershipRetained}.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		got, err := (Guard{}).Check(context.Background(), data)
		if err != nil || got.Applied != (operation == reconciliation.MutationApplied) {
			t.Fatalf("%s reads %+v (%v)", operation, got, err)
		}
	}
}
