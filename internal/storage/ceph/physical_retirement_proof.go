package ceph

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type PhysicalRetirementReceipt struct {
	Block                  string `json:"block,omitempty"`
	Digest                 string `json:"digest,omitempty"`
	Machine                string `json:"machine"`
	Request                string `json:"request,omitempty"`
	RetainedWithoutInstall bool   `json:"retainedWithoutInstall,omitempty"`
}

type PhysicalRetirementEvidence struct {
	Cluster  string                      `json:"cluster"`
	FSID     string                      `json:"fsid"`
	Owner    string                      `json:"owner"`
	Prepared bool                        `json:"prepared"`
	Receipts []PhysicalRetirementReceipt `json:"receipts"`
	Request  string                      `json:"request"`
	Retired  bool                        `json:"retired"`
	Version  string                      `json:"version"`
}

type physicalDiskBinding struct {
	Devices  []HostDevice
	Observed bool
}

func physicalPolicy(e lifecycle.Execution) (PhysicalRetirementRequest, error) {
	r, err := DecodePhysicalRetirementRequest(e.Block.Request)
	if err != nil {
		return r, err
	}
	if e.ApplyOperation == "" || e.Block.Kind != string(api.CephCluster) || e.Block.Object != r.Cluster || e.Block.Implementation != PhysicalRetirementImplementation || e.Block.ID != PhysicalRetirementBlockID(r.Cluster) || !hex64(e.Block.RequestDigest) {
		return r, physicalPolicyFailure()
	}
	return r, nil
}

func policyEvidence(e lifecycle.Execution, r PhysicalRetirementRequest, receipts []PhysicalRetirementReceipt, retired bool) (json.RawMessage, error) {
	return json.Marshal(PhysicalRetirementEvidence{Version: PhysicalRetirementImplementation, Cluster: r.Cluster, Owner: e.ApplyOperation,
		FSID: FSIDFor(e.ApplyOperation, r.Cluster), Request: e.Block.RequestDigest, Prepared: true, Retired: retired, Receipts: receipts})
}

func policyHosts(r PhysicalRetirementRequest) []Host {
	hosts := []Host{}
	for _, host := range r.Hosts {
		hosts = append(hosts, Host{Name: host.Name, Machine: host.Machine, Devices: slices.Clone(host.Devices), Physical: true})
	}
	return hosts
}

func (c PhysicalRetirementCapability) Apply(ctx context.Context, e lifecycle.Execution) (lifecycle.Result, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.Result{}, err
	}
	r, err := physicalPolicy(e)
	if err != nil {
		return lifecycle.Result{}, err
	}
	if err := bindPhysicalHosts(policyHosts(r), e, c.proofs); err != nil {
		return lifecycle.Result{}, err
	}
	evidence, err := policyEvidence(e, r, []PhysicalRetirementReceipt{}, false)
	if err != nil {
		return lifecycle.Result{Outcome: reconciliation.OutcomeFailed}, err
	}
	return lifecycle.Result{Outcome: reconciliation.OutcomeUnchanged, Evidence: evidence}, nil
}

func (c PhysicalRetirementCapability) Observe(ctx context.Context, e lifecycle.Execution) (lifecycle.Observation, error) {
	result, err := c.Apply(ctx, e)
	if err != nil {
		return lifecycle.Observation{Effect: reconciliation.EffectUnknown}, err
	}
	return lifecycle.Observation{Effect: reconciliation.EffectCompleted, Outcome: result.Outcome, Evidence: result.Evidence}, nil
}

func (c PhysicalRetirementCapability) completed(ctx context.Context, e lifecycle.Execution) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r, err := physicalPolicy(e)
	if err != nil {
		return nil, err
	}
	hosts := policyHosts(r)
	if err := bindPhysicalHosts(hosts, e, c.proofs); err != nil {
		return nil, err
	}
	unstarted, err := originalCoreUnstarted(e, r.Cluster)
	if err != nil {
		return nil, err
	}
	receipts := []PhysicalRetirementReceipt{}
	for index, host := range hosts {
		if unstarted {
			retained, err := c.retainedUninstalled(e, host)
			if err != nil {
				return nil, err
			}
			if retained {
				receipts = append(receipts, PhysicalRetirementReceipt{Machine: host.Machine, RetainedWithoutInstall: true})
				continue
			}
		}
		if err := (PhysicalRetirementRequest{Hosts: r.Hosts[index : index+1]}).authorized(); err != nil {
			return nil, err
		}
		devices, err := originalPhysicalDevices(e, r.Cluster, host)
		if err != nil {
			return nil, err
		}
		receipt, err := c.erasedHost(e, host, devices)
		if err != nil {
			return nil, err
		}
		receipts = append(receipts, receipt)
	}
	return policyEvidence(e, r, receipts, true)
}

func (c PhysicalRetirementCapability) retainedUninstalled(e lifecycle.Execution, host Host) (bool, error) {
	if c.uninstalled == nil {
		return false, nil
	}
	return c.uninstalled(e.OriginalRemovalDependents, e.RemovalProved, host.Machine)
}

func (c PhysicalRetirementCapability) Destroy(ctx context.Context, e lifecycle.Execution) (lifecycle.Result, error) {
	evidence, err := c.completed(ctx, e)
	if err != nil {
		return lifecycle.Result{Outcome: reconciliation.OutcomeFailed}, err
	}
	return lifecycle.Result{Outcome: reconciliation.OutcomeUnchanged, Evidence: evidence}, nil
}

func (c PhysicalRetirementCapability) ObserveRemoval(ctx context.Context, e lifecycle.Execution) (lifecycle.Observation, error) {
	evidence, err := c.completed(ctx, e)
	if err != nil {
		return lifecycle.Observation{Effect: reconciliation.EffectUnknown}, err
	}
	return lifecycle.Observation{Effect: reconciliation.EffectCompleted, Outcome: reconciliation.OutcomeUnchanged, Evidence: evidence}, nil
}

func originalPhysicalDevices(e lifecycle.Execution, cluster string, host Host) (physicalDiskBinding, error) {
	var found []HostDevice
	for _, pair := range e.RemovalProved {
		proof := pair.Applied
		if proof.Implementation != HostImplementation || proof.Object != cluster {
			continue
		}
		request, err := DecodeHostRequest(proof.Block.Request)
		if err != nil {
			return physicalDiskBinding{}, err
		}
		target, ok := request.target()
		if !ok || target.Machine != host.Machine {
			continue
		}
		if !matchingRemovalPair(pair) || proof.Kind != string(api.CephCluster) || request.Cluster.Cluster != cluster || target.Name != host.Name || !target.Physical || !slices.Equal(target.Devices, host.Devices) {
			return physicalDiskBinding{}, physicalPolicyFailure()
		}
		if proof.State != reconciliation.BlockDone {
			continue
		}
		if found != nil {
			return physicalDiskBinding{}, physicalPolicyFailure()
		}
		evidence, err := decodeHostEvidence(proof.Evidence, proof.Block.RequestDigest)
		if err != nil {
			return physicalDiskBinding{}, err
		}
		if evidence.Owner != e.ApplyOperation || evidence.FSID != FSIDFor(e.ApplyOperation, cluster) || evidence.Host != host.Name || !evidence.prepared(request) {
			return physicalDiskBinding{}, physicalPolicyFailure()
		}
		found = slices.Clone(evidence.Devices)
		if found == nil {
			found = []HostDevice{}
		}
	}
	if found == nil {
		unstarted, err := originalCoreUnstarted(e, cluster)
		if err != nil {
			return physicalDiskBinding{}, err
		}
		if !unstarted {
			return physicalDiskBinding{}, failure("lifecycle.state", "physical retirement lacks the original completed Ceph host disk binding", "restore the original apply and removal records")
		}
		for _, device := range host.Devices {
			found = append(found, HostDevice{Path: device.Path, WWN: device.WWN})
		}
		return physicalDiskBinding{Devices: found}, nil
	}
	return physicalDiskBinding{Devices: found, Observed: true}, nil
}

func originalRemovalPair(pair lifecycle.RemovalEvidence) bool {
	return pair.Applied.State == reconciliation.BlockDone && matchingRemovalPair(pair)
}

func matchingRemovalPair(pair lifecycle.RemovalEvidence) bool {
	a, r := pair.Applied, pair.Removed
	return a.Verb == reconciliation.Apply && r.Verb == reconciliation.Destroy && r.State == reconciliation.BlockDone &&
		a.Kind == a.Block.Kind && a.Object == a.Block.Object && a.Implementation == a.Block.Implementation &&
		r.Kind == r.Block.Kind && r.Object == r.Block.Object && r.Implementation == r.Block.Implementation &&
		a.Kind == r.Kind && a.Object == r.Object && a.Implementation == r.Implementation && a.Block.ID == r.Block.ID &&
		hex64(a.Block.RequestDigest) && a.Block.RequestDigest == r.Block.RequestDigest && string(a.Block.Request) == string(r.Block.Request)
}

func (c PhysicalRetirementCapability) erasedHost(e lifecycle.Execution, host Host, devices physicalDiskBinding) (PhysicalRetirementReceipt, error) {
	var found *PhysicalRetirementReceipt
	if c.erasure == nil || host.Hardware == nil {
		return PhysicalRetirementReceipt{}, physicalPolicyFailure()
	}
	for _, pair := range e.RemovalProved {
		if pair.Applied.Object != host.Machine || pair.Applied.Kind != string(api.Machine) {
			continue
		}
		proof, recognized, err := c.erasure(pair.Applied.Block, pair.Removed, host.Hardware.UUID)
		if err != nil {
			return PhysicalRetirementReceipt{}, err
		}
		if !recognized {
			continue
		}
		if found != nil || !originalRemovalPair(pair) || !slices.Equal(proof.MACs, host.Hardware.MACs) {
			return PhysicalRetirementReceipt{}, physicalPolicyFailure()
		}
		if err := proveErasedDevices(proof.Targets, devices); err != nil {
			return PhysicalRetirementReceipt{}, err
		}
		digest := sha256.Sum256(pair.Removed.Evidence)
		found = &PhysicalRetirementReceipt{Machine: host.Machine, Block: pair.Applied.Block.ID, Request: pair.Applied.Block.RequestDigest, Digest: hex.EncodeToString(digest[:])}
	}
	if found == nil {
		return PhysicalRetirementReceipt{}, failure("lifecycle.state", "physical Ceph retirement lacks a completed verified erase receipt", "continue the original removal; a stopped Machine or released claim is not erase proof")
	}
	return *found, nil
}

func proveErasedDevices(targets []Device, binding physicalDiskBinding) error {
	seen := map[string]Device{}
	for _, target := range targets {
		if _, duplicate := seen[target.WWN]; duplicate || target.WWN == "" {
			return physicalPolicyFailure()
		}
		seen[target.WWN] = target
	}
	for _, device := range binding.Devices {
		target, found := seen[device.WWN]
		if !found || binding.Observed && (target.Serial != device.Serial || target.SizeBytes != device.SizeBytes) {
			return failure("lifecycle.state", "physical erase does not cover an original Ceph disk's WWN, serial and capacity", "restore the original disk binding and continue the same verified removal")
		}
	}
	return nil
}

func originalCoreUnstarted(e lifecycle.Execution, cluster string) (bool, error) {
	found := false
	unstarted := false
	for _, proof := range e.OriginalRemovalDependents {
		if proof.Block.ID != ClusterBlockID(cluster) {
			continue
		}
		request, err := DecodeRequest(proof.Block.Request)
		if err != nil {
			return false, err
		}
		if found || proof.Kind != string(api.CephCluster) || proof.Object != cluster || proof.Implementation != Implementation || proof.Verb != reconciliation.Apply ||
			proof.Block.Kind != proof.Kind || proof.Block.Object != proof.Object || proof.Block.Implementation != proof.Implementation || !hex64(proof.Block.RequestDigest) || request.Cluster != cluster || request.Action != "cluster" {
			return false, physicalPolicyFailure()
		}
		found = true
		unstarted = proof.State == reconciliation.BlockPending && len(proof.Evidence) == 0
	}
	return found && unstarted, nil
}

func authorizePhysicalRetirement(e lifecycle.Execution, r Request) error {
	var found *PhysicalRetirementRequest
	for _, proof := range provedFor(e) {
		if proof.Implementation != PhysicalRetirementImplementation || proof.Object != r.Cluster {
			continue
		}
		policy, err := DecodePhysicalRetirementRequest(proof.Block.Request)
		if err != nil {
			return err
		}
		evidence, err := reconciliation.DecodeEvidence[PhysicalRetirementEvidence](proof.Evidence, 16384, "physical Ceph retirement policy")
		if err != nil {
			return err
		}
		if found != nil || policy.Cluster != r.Cluster || proof.Kind != string(api.CephCluster) || proof.Verb != reconciliation.Apply || proof.State != reconciliation.BlockDone ||
			proof.Block.Kind != proof.Kind || proof.Block.Object != proof.Object || proof.Block.Implementation != proof.Implementation ||
			proof.Block.ID != PhysicalRetirementBlockID(r.Cluster) || evidence.Version != PhysicalRetirementImplementation || evidence.Cluster != r.Cluster ||
			!hex64(proof.Block.RequestDigest) || evidence.Request != proof.Block.RequestDigest || !evidence.Prepared || evidence.Retired || len(evidence.Receipts) != 0 ||
			evidence.Owner != e.ApplyOperation || evidence.FSID != FSIDFor(e.ApplyOperation, r.Cluster) {
			return physicalPolicyFailure()
		}
		if err := policy.authorized(); err != nil {
			return err
		}
		found = &policy
	}
	if found == nil {
		return physicalRemovalRefusal()
	}
	physical := policyHosts(*found)
	for _, host := range r.Hosts {
		if !host.Physical {
			continue
		}
		at := slices.IndexFunc(physical, func(candidate Host) bool { return candidate.Machine == host.Machine })
		if at < 0 || physical[at].Name != host.Name || !slices.Equal(physical[at].Devices, host.Devices) {
			return physicalPolicyFailure()
		}
		physical = slices.Delete(physical, at, at+1)
	}
	if len(physical) != 0 {
		return physicalPolicyFailure()
	}
	return nil
}

func physicalHostRelease(e lifecycle.Execution, frozen HostRequest) (json.RawMessage, error) {
	request := Request{Action: "host-preparation", Cluster: frozen.Cluster.Cluster, Hosts: frozen.Cluster.Hosts, FSID: FSIDFor(e.ApplyOperation, frozen.Cluster.Cluster)}
	target, found := frozen.target()
	if !found || !target.Physical || frozen.Cluster.Owner != "" || frozen.Cluster.FSID != "" || e.ApplyOperation == "" || e.Block.Kind != string(api.CephCluster) ||
		e.Block.Object != request.Cluster || e.Block.Implementation != HostImplementation || e.Block.ID != HostBlockID(request.Cluster, target.Machine) {
		return nil, physicalPolicyFailure()
	}
	if err := authorizePhysicalRetirement(e, request); err != nil {
		return nil, err
	}
	return json.Marshal(retirementFor(request))
}
