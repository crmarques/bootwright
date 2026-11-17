package power

import (
	"context"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type realizationReader struct {
	current, applied []lifecycle.BlockEvidence
	asked            []string
}

func (r *realizationReader) Evidence(context.Context, string, string, string) ([]lifecycle.BlockEvidence, error) {
	r.asked = append(r.asked, "current")
	return r.current, nil
}

func (r *realizationReader) AppliedEvidence(context.Context, string, string, string) ([]lifecycle.BlockEvidence, error) {
	r.asked = append(r.asked, "applied")
	return r.applied, nil
}

func TestARealizationIsTheBlockTheBoundImplementationsClaimAndNoOther(t *testing.T) {
	t.Parallel()
	block := func(implementation string, verb reconciliation.Verb, state reconciliation.BlockState) lifecycle.BlockEvidence {
		return lifecycle.BlockEvidence{Kind: "Machine", Object: "node", Implementation: implementation, Verb: verb, State: state}
	}
	realizes := func(implementation string) bool { return implementation == "realizer" }
	removed := machine.OwnershipState{Verb: machine.VerbDestroy, State: machine.BlockDone}
	for name, test := range map[string]struct {
		reader *realizationReader
		want   machine.OwnershipState
		found  bool
		asked  []string
	}{
		"the current plan's block wins over a later block": {&realizationReader{current: []lifecycle.BlockEvidence{
			block("other", reconciliation.Apply, reconciliation.BlockFailed), block("realizer", reconciliation.Apply, reconciliation.BlockDone),
		}}, machine.OwnershipState{Verb: "apply", State: "done"}, true, []string{"current"}},
		"a block the replaced destroy removed":        {&realizationReader{applied: []lifecycle.BlockEvidence{block("realizer", reconciliation.Apply, reconciliation.BlockDone)}}, removed, true, []string{"current", "applied"}},
		"an apply that never completed":               {&realizationReader{applied: []lifecycle.BlockEvidence{block("realizer", reconciliation.Apply, reconciliation.BlockFailed)}}, machine.OwnershipState{}, false, []string{"current", "applied"}},
		"only a block no bound implementation claims": {&realizationReader{current: []lifecycle.BlockEvidence{block("other", reconciliation.Apply, reconciliation.BlockDone)}}, machine.OwnershipState{}, false, []string{"current", "applied"}},
	} {
		t.Run(name, func(t *testing.T) {
			state, found, err := NewEvidenceRealization(test.reader, realizes).Realization(context.Background(), "lab", "node")
			if err != nil || found != test.found || state != test.want || !slices.Equal(test.reader.asked, test.asked) {
				t.Fatalf("realization = %+v, %t (%v) after %v", state, found, err, test.reader.asked)
			}
		})
	}
}
