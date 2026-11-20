package ceph

import (
	"context"
	"encoding/json"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func (HostCapability) CarryRemovalPreparation() bool { return true }

func nativeHostReceipt(e lifecycle.Execution, r HostRequest, data json.RawMessage) error {
	evidence, err := decodeHostEvidence(data, e.Block.RequestDigest)
	if err != nil || !evidence.NativeRemoved || !retained(evidence, r) {
		return cephUnproved.Fail("the retained-root Ceph host cleanup did not prove native absence", "continue the original removal")
	}
	return nil
}

func savedNativeHostReceipt(e lifecycle.Execution, r HostRequest) (json.RawMessage, error) {
	var receipt json.RawMessage
	for _, data := range e.RemovalPreparations {
		if err := nativeHostReceipt(e, r, data); err != nil {
			return nil, err
		}
		receipt = data
	}
	return receipt, nil
}

func (c HostCapability) removeFromRetainedHost(ctx context.Context, e lifecycle.Execution, machine string) (lifecycle.Result, error) {
	failed := lifecycle.Result{Outcome: reconciliation.OutcomeFailed}
	unknown := lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}
	if e.PowerMachine == nil || e.PrepareRemoval == nil || lifecycle.ReadOnlyRun(e) {
		return failed, cephUnconfigured.Fail("retained-root cleanup requires registered power control and receipt custody", "")
	}
	r, err := c.prepare(ctx, e)
	if err != nil {
		return failed, err
	}
	receipt, err := savedNativeHostReceipt(e, r)
	if err != nil {
		return failed, err
	}
	if receipt == nil {
		r.Cluster.Delegated, err = frozenHostDelegation(e, r)
		if err != nil {
			return failed, err
		}
		if err := e.PowerMachine(ctx, machine, true); err != nil {
			return unknown, err
		}
		result, err := c.run(ctx, e, "destroy", r)
		if err != nil {
			return unknown, err
		}
		if !result.Outcome.Settled() {
			return unknown, physicalPolicyFailure()
		}
		if err := nativeHostReceipt(e, r, result.Evidence); err != nil {
			return unknown, err
		}
		receipt = result.Evidence
		if err := e.PrepareRemoval(ctx, receipt); err != nil {
			return unknown, err
		}
	}
	if err := e.PowerMachine(ctx, machine, false); err != nil {
		return unknown, err
	}
	return lifecycle.Result{Outcome: reconciliation.OutcomeChanged, Evidence: receipt}, nil
}

func (c HostCapability) observeRetainedHostRemoval(ctx context.Context, e lifecycle.Execution, machine string) (lifecycle.Observation, error) {
	unknown := lifecycle.Observation{Effect: reconciliation.EffectUnknown}
	r, err := c.prepare(ctx, e)
	if err != nil {
		return unknown, err
	}
	receipt, err := savedNativeHostReceipt(e, r)
	if err != nil {
		return unknown, err
	}
	if e.MachineStopped == nil {
		return unknown, cephRecordsMissing.Fail("retained-root cleanup cannot prove Machine/"+machine+" power", "")
	}
	stopped, err := e.MachineStopped(ctx, machine)
	if err != nil {
		return unknown, err
	}
	if receipt != nil && stopped {
		return lifecycle.Observation{Effect: reconciliation.EffectCompleted, Outcome: reconciliation.OutcomeChanged, Evidence: receipt}, nil
	}
	if stopped || receipt != nil {
		return lifecycle.Observation{Effect: reconciliation.EffectPartial}, nil
	}
	r.Cluster.Delegated, err = frozenHostDelegation(e, r)
	if err != nil {
		return unknown, err
	}
	result, err := c.run(ctx, e, "observe", r, true)
	if err != nil {
		return unknown, err
	}
	proof, err := decodeHostEvidence(result.Evidence, e.Block.RequestDigest)
	if err != nil || !proof.identifies(r) || proof.NativeRemoved && !retained(proof, r) ||
		!proof.NativeRemoved && (!proof.empty() || proof.Removed) {
		return unknown, cephUnproved.Fail("the retained-root removal observation did not prove the original Ceph host", "")
	}
	return lifecycle.Observation{Effect: reconciliation.EffectPartial, Evidence: result.Evidence}, nil
}

func frozenHostDelegation(e lifecycle.Execution, r HostRequest) ([]DelegatedService, error) {
	var services []DelegatedService
	found := false
	for _, proof := range e.OriginalRemovalDependents {
		if proof.Block.ID != ClusterBlockID(r.Cluster.Cluster) {
			continue
		}
		core, err := DecodeRequest(proof.Block.Request)
		if err != nil {
			return nil, err
		}
		host, _ := r.target()
		if found || proof.Verb != reconciliation.Apply || proof.Kind != string(api.CephCluster) ||
			proof.Kind != proof.Block.Kind || proof.Object != proof.Block.Object || proof.Implementation != Implementation ||
			proof.Implementation != proof.Block.Implementation || !hex64(proof.Block.RequestDigest) ||
			proof.Object != r.Cluster.Cluster || core.Cluster != r.Cluster.Cluster || core.Action != "cluster" ||
			!slices.ContainsFunc(core.Hosts, func(h Host) bool {
				return h.Name == host.Name && h.Machine == host.Machine && h.Physical && h.RetainRoot
			}) {
			return nil, physicalPolicyFailure()
		}
		found = true
		for _, svc := range core.Delegated {
			if slices.Contains(svc.Hosts, host.Name) {
				services = append(services, DelegatedService{Name: svc.Name, Hosts: []string{host.Name}})
			}
		}
	}
	if !found {
		return nil, cephRecordsMissing.Fail("retained-root cleanup lacks the original cluster's daemon placement", "")
	}
	return services, nil
}
