package hostfirewall

import (
	"context"
	"encoding/json"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func (Capability) CarryRemovalPreparation() bool { return true }

func removalReceipt(e lifecycle.Execution) (json.RawMessage, error) {
	var saved json.RawMessage
	for _, receipt := range e.RemovalPreparations {
		r, err := DecodeFirewallRequest(e.Block.Request)
		if err != nil {
			return nil, err
		}
		proof, err := decodeFirewallEvidence(receipt, e.Block.RequestDigest)
		if err != nil || !proof.Absent || !proof.Postcondition || r.Direct || firewallEffect(proof, r, nil, true) != reconciliation.EffectCompleted {
			return nil, firewallEvidenceMismatch.Fail("the stored firewall removal receipt does not prove its services absent", "restore the original removal evidence")
		}
		saved = receipt
	}
	return saved, nil
}

func (c Capability) removeFromInstalled(ctx context.Context, e lifecycle.Execution, request FirewallRequest) (lifecycle.Result, error) {
	failed := lifecycle.Result{Outcome: reconciliation.OutcomeFailed}
	unknown := lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}
	if e.PowerMachine == nil || e.PrepareRemoval == nil || lifecycle.ReadOnlyRun(e) {
		return failed, firewallUnconfigured.Fail("a managed firewall removal requires a registered power transition", "")
	}
	receipt, err := removalReceipt(e)
	if err != nil {
		return failed, err
	}
	name := request.Identity.Object
	if receipt == nil {
		if err := e.PowerMachine(ctx, name, true); err != nil {
			return unknown, err
		}
		result, err := c.mutate(ctx, e, "destroy")
		if err != nil || !result.Outcome.Settled() {
			return result, err
		}
		receipt = result.Evidence
		if err := e.PrepareRemoval(ctx, receipt); err != nil {
			return unknown, err
		}
	}
	if err := e.PowerMachine(ctx, name, false); err != nil {
		return unknown, err
	}
	return lifecycle.Result{Outcome: reconciliation.OutcomeChanged, Evidence: receipt}, nil
}

func (c Capability) observeInstalledRemoval(ctx context.Context, e lifecycle.Execution) (lifecycle.Observation, error) {
	r, err := DecodeFirewallRequest(e.Block.Request)
	if err != nil {
		return lifecycle.Observation{Effect: reconciliation.EffectUnknown}, err
	}
	if r.Direct {
		return c.observe(ctx, e, true)
	}
	if e.MachineStopped != nil {
		stopped, err := e.MachineStopped(ctx, r.Identity.Object)
		if err != nil {
			return lifecycle.Observation{Effect: reconciliation.EffectUnknown}, err
		}
		if stopped {
			proof, err := json.Marshal(struct {
				Machine         string `json:"machine"`
				Request         string `json:"request"`
				StoppedOrAbsent bool   `json:"stoppedOrAbsent"`
			}{r.Identity.Object, e.Block.RequestDigest, true})
			return lifecycle.Observation{Effect: reconciliation.EffectPartial, Evidence: proof}, err
		}
	}
	observed, err := c.observe(ctx, e, true)
	if err == nil && observed.Effect == reconciliation.EffectCompleted {
		observed.Effect = reconciliation.EffectPartial
	}
	return observed, err
}
