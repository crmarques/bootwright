package installation

import (
	"context"
	"encoding/json"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// CarryRemovalPreparation lets a replacement finish the shutdown of an
// installation whose preceding removal already proved the consumer released.
func (Capability) CarryRemovalPreparation() bool { return true }

// The receipt is the validated, request-bound removal evidence. A recorded
// release leaves just an idempotent shutdown, even after a lost power response.
func releasedReceipt(e lifecycle.Execution, request Request) (json.RawMessage, error) {
	var saved json.RawMessage
	for _, proof := range e.RemovalPreparations {
		if err := ValidateAbsence(proof, e.Block.RequestDigest); err != nil {
			return nil, err
		}
		if err := ValidateReleased(proof, request, e.Block.RequestDigest, true); err != nil {
			return nil, err
		}
		saved = proof
	}
	return saved, nil
}

func (c Capability) removeInstallation(ctx context.Context, e lifecycle.Execution) (lifecycle.Result, error) {
	failed := lifecycle.Result{Outcome: reconciliation.OutcomeFailed}
	r, marker, err := c.prepare(ctx, e)
	if err != nil {
		return failed, err
	}
	receipt, err := releasedReceipt(e, r)
	if err != nil {
		return failed, err
	}
	unregister, err := unregisters(e, r, marker)
	if err != nil {
		return failed, err
	}
	if !unregister && receipt == nil {
		return c.mutate(ctx, e, "destroy")
	}
	if r.Identity.Object != e.Block.Object {
		return failed, installRecordsMissing.Fail("the subscription removal names a different Machine from its recorded block", "restore the owning apply records")
	}
	if e.PowerMachine == nil || e.PrepareRemoval == nil || lifecycle.ReadOnlyRun(e) {
		return failed, installUnconfigured.Fail("this installation removal lacks registered power control or release receipt custody", "repeat the destroy with the qualified lifecycle runtime")
	}
	if receipt == nil {
		if err := e.PowerMachine(ctx, r.Identity.Object, true); err != nil {
			return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}, err
		}
		result, err := c.mutate(ctx, e, "destroy")
		if err != nil || !result.Outcome.Settled() {
			return result, err
		}
		receipt = result.Evidence
		if err := e.PrepareRemoval(ctx, receipt); err != nil {
			return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}, err
		}
	}
	if err := e.PowerMachine(ctx, r.Identity.Object, false); err != nil {
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}, err
	}
	return lifecycle.Result{Outcome: reconciliation.OutcomeChanged, Evidence: receipt}, nil
}
