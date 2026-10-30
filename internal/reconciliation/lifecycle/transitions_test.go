package lifecycle

import (
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

func TestClassifyTransitionKinds(t *testing.T) {
	const (
		apply   = reconciliation.Apply
		destroy = reconciliation.Destroy
	)
	type blocks struct{ pending, unknown bool }
	var (
		settled = blocks{}
		behind  = blocks{pending: true}
		lost    = blocks{pending: true, unknown: true}
	)
	held := func(verb reconciliation.Verb, state reconciliation.OperationState, shape blocks) standing {
		return standing{held: true, verb: verb, state: state, pending: shape.pending, unknown: shape.unknown}
	}
	running, paused, failed, unknown, done := reconciliation.OperationRunning, reconciliation.OperationPaused, reconciliation.OperationFailed,
		reconciliation.OperationUnknown, reconciliation.OperationDone
	for name, row := range map[string]struct {
		requested reconciliation.Verb
		held      standing
		want      transitionKind
		chosen    reconciliation.Verb
	}{
		"an apply over no operation":                         {apply, standing{}, transitionFresh, apply},
		"a destroy over no operation":                        {destroy, standing{}, transitionSettle, apply},
		"an apply over a running apply":                      {apply, held(apply, running, behind), transitionContinue, apply},
		"an apply over a paused apply":                       {apply, held(apply, paused, behind), transitionContinue, apply},
		"an apply over a failed apply":                       {apply, held(apply, failed, behind), transitionContinue, apply},
		"an apply over an unknown apply":                     {apply, held(apply, unknown, lost), transitionResolve, apply},
		"an apply over a failed apply holding an unknown":    {apply, held(apply, failed, lost), transitionResolve, apply},
		"an apply over a running apply, every block done":    {apply, held(apply, running, settled), transitionFinalize, apply},
		"an apply over an unknown apply, every block done":   {apply, held(apply, unknown, settled), transitionFinalize, apply},
		"an apply over a failed apply, every block done":     {apply, held(apply, failed, settled), transitionFinalize, apply},
		"an apply over a paused apply, every block done":     {apply, held(apply, paused, settled), transitionContinue, apply},
		"an apply over a completed apply":                    {apply, held(apply, done, settled), transitionSettle, destroy},
		"an apply over a completed apply missing a block":    {apply, held(apply, done, behind), transitionSettle, destroy},
		"a destroy over a running apply":                     {destroy, held(apply, running, behind), transitionReplaceRemoval, apply},
		"a destroy over a paused apply":                      {destroy, held(apply, paused, behind), transitionReplaceRemoval, apply},
		"a destroy over a failed apply":                      {destroy, held(apply, failed, behind), transitionReplaceRemoval, apply},
		"a destroy over an unknown apply":                    {destroy, held(apply, unknown, lost), transitionReplaceRemoval, apply},
		"a destroy over a failed apply, every block done":    {destroy, held(apply, failed, settled), transitionReplaceRemoval, apply},
		"a destroy over a completed apply":                   {destroy, held(apply, done, settled), transitionFresh, destroy},
		"a destroy over a completed apply missing a block":   {destroy, held(apply, done, behind), transitionFresh, destroy},
		"a destroy over a running destroy":                   {destroy, held(destroy, running, behind), transitionContinue, destroy},
		"a destroy over an unknown destroy":                  {destroy, held(destroy, unknown, lost), transitionResolve, destroy},
		"a destroy over a failed destroy":                    {destroy, held(destroy, failed, behind), transitionReplaceRemoval, destroy},
		"a destroy over a failed destroy holding an unknown": {destroy, held(destroy, failed, lost), transitionReplaceRemoval, destroy},
		"a destroy over a running destroy, every block done": {destroy, held(destroy, running, settled), transitionFinalize, destroy},
		"a destroy over a failed destroy, every block done":  {destroy, held(destroy, failed, settled), transitionFinalize, destroy},
		"a destroy over a completed destroy":                 {destroy, held(destroy, done, settled), transitionSettle, apply},
		"a destroy over a completed destroy missing a block": {destroy, held(destroy, done, behind), transitionRefuse, apply},
		"an apply over a running destroy":                    {apply, held(destroy, running, behind), transitionRefuse, destroy},
		"an apply over an unknown destroy":                   {apply, held(destroy, unknown, lost), transitionRefuse, destroy},
		"an apply over a failed destroy":                     {apply, held(destroy, failed, behind), transitionRefuse, destroy},
		"an apply over a failed destroy, every block done":   {apply, held(destroy, failed, settled), transitionRefuse, destroy},
		"an apply over a completed destroy":                  {apply, held(destroy, done, settled), transitionFresh, apply},
		"an apply over a completed destroy missing a block":  {apply, held(destroy, done, behind), transitionRefuse, apply},
	} {
		if got := classify(row.requested, row.held); got != row.want {
			t.Errorf("%s: classify = %q, want %q", name, got, row.want)
		}
		if got := chosenVerb(row.held); got != row.chosen {
			t.Errorf("%s: chosenVerb = %q, want %q", name, got, row.chosen)
		}
	}
}

func TestSupersedableIsTheReplacementClassification(t *testing.T) {
	for _, verb := range []reconciliation.Verb{reconciliation.Apply, reconciliation.Destroy} {
		for _, state := range []reconciliation.OperationState{
			reconciliation.OperationRunning, reconciliation.OperationPaused, reconciliation.OperationFailed,
			reconciliation.OperationUnknown, reconciliation.OperationDone,
		} {
			want := verb == reconciliation.Apply && state != reconciliation.OperationDone ||
				verb == reconciliation.Destroy && state == reconciliation.OperationFailed
			if got := supersedable(operationstore.Operation{Verb: verb, State: state}); got != want {
				t.Errorf("a %s %s is supersedable = %t, want %t", state, verb, got, want)
			}
		}
	}
}
