package ceph

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type retainedHostProofs struct{ hostProofs }

func (retainedHostProofs) PhysicalMachine(block reconciliation.Block, _ []byte) (PhysicalIdentity, bool, error) {
	if block.Implementation != "machine-baremetal-v1" {
		return PhysicalIdentity{}, false, nil
	}
	return PhysicalIdentity{UUID: "11111111-1111-4111-8111-111111111111", MACs: []string{"02:00:00:00:00:01"}}, true, nil
}

func TestRetainedHostDestroyAndReadonlyRemovalUseNativeProof(t *testing.T) {
	e, r, proofs := hostExecution(t, "node-1")
	for i := range r.Cluster.Hosts {
		if r.Cluster.Hosts[i].Name == r.Host {
			r.Cluster.Hosts[i].Physical = true
			r.Cluster.Hosts[i].RetainRoot = true
			r.Cluster.Hosts[i].Devices = []Device{}
		}
	}
	raw, err := r.canonical()
	if err != nil {
		t.Fatal(err)
	}
	e.Block = retirementBlock(t, e.Block.ID, e.Block.Kind, e.Block.Object, e.Block.Implementation, raw)
	e.RetiredWithHosts = true
	host, _ := r.target()
	claim := retirementBlock(t, "claim-"+host.Machine, string(api.Machine), host.Machine, "machine-baremetal-v1", []byte(`{}`))
	e.ApplyProved = append(e.ApplyProved, retirementRecord(claim, reconciliation.Apply, []byte(`{}`)))
	policy := PhysicalRetirementRequest{Version: PhysicalRetirementImplementation, Cluster: r.Cluster.Cluster, Hosts: []PhysicalRetirementHost{{Name: host.Name, Machine: host.Machine, RetainRoot: true, Devices: []Device{}, EraseTargets: []PhysicalEraseTarget{}}}}
	policyRaw, _ := policy.Canonical()
	block := retirementBlock(t, PhysicalRetirementBlockID(policy.Cluster), string(api.CephCluster), policy.Cluster, PhysicalRetirementImplementation, policyRaw)
	policyProof, _ := json.Marshal(PhysicalRetirementEvidence{Version: PhysicalRetirementImplementation, Cluster: policy.Cluster, Owner: e.ApplyOperation, FSID: FSIDFor(e.ApplyOperation, policy.Cluster), Request: block.RequestDigest, Prepared: true, Receipts: []PhysicalRetirementReceipt{}})
	e.ApplyProved = append(e.ApplyProved, retirementRecord(block, reconciliation.Apply, policyProof))
	proof := HostEvidence{Version: HostEvidenceVersion, Request: e.Block.RequestDigest, Host: r.Host, Owner: e.ApplyOperation, FSID: FSIDFor(e.ApplyOperation, policy.Cluster), Image: PinnedRepository + "@" + resolvedDigest, Removed: true, NativeRemoved: true, Devices: []HostDevice{}}
	evidence, _ := json.Marshal(proof)
	runner := &recordingRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: evidence}}
	capability := NewHost(runner).WithProofs(retainedHostProofs{proofs})
	core := Request{Version: Version, Action: "cluster", Cluster: policy.Cluster, Hosts: r.Cluster.Hosts,
		Delegated: []DelegatedService{{Name: "crash", Hosts: []string{r.Host}}, {Name: "rgw.objects", Hosts: []string{r.Host}}}}
	coreRaw, _ := core.Canonical()
	coreBlock := retirementBlock(t, ClusterBlockID(core.Cluster), string(api.CephCluster), core.Cluster, Implementation, coreRaw)
	e.OriginalRemovalDependents = []lifecycle.BlockEvidence{retirementRecord(coreBlock, reconciliation.Apply, []byte(`{}`))}
	e.MachineStopped = func(context.Context, string) (bool, error) { return false, errors.New("unproved power") }
	if got, err := capability.ObserveRemoval(context.Background(), e); err == nil || got.Effect != reconciliation.EffectUnknown || runner.calls != 0 {
		t.Fatalf("uncertain power must not permit native observation or completion: %+v %v", got, err)
	}
	e.MachineStopped = func(context.Context, string) (bool, error) { return false, nil }
	if got, err := capability.ObserveRemoval(context.Background(), e); err != nil || got.Effect != reconciliation.EffectPartial || runner.calls != 1 {
		t.Fatalf("the running original host should be observed before a receipt exists: %+v %v", got, err)
	}
	if runner.request.Operation != "observe" || runner.request.MaterialValues["removal"] != "true" {
		t.Fatal("observation did not use the pinned read-only removal phase")
	}
	runner.calls = 0
	e.Attempt = 1
	power := []bool{}
	e.PowerMachine = func(_ context.Context, machine string, on bool) error {
		if machine != host.Machine {
			t.Fatal("attempt powered another Machine")
		}
		power = append(power, on)
		if !on && len(power) == 2 {
			return errors.New("interrupted shutdown")
		}
		return nil
	}
	e.PrepareRemoval = func(_ context.Context, data json.RawMessage) error {
		e.RemovalPreparations = append(e.RemovalPreparations, data)
		return nil
	}
	result, err := capability.Destroy(context.Background(), e)
	if err == nil || result.Outcome != reconciliation.OutcomeUnknown || runner.calls != 1 || len(e.RemovalPreparations) != 1 {
		t.Fatalf("destroy did not keep its receipt before interrupted shutdown %+v %v", result, err)
	}
	if runner.request.Deadline < 660*time.Second {
		t.Fatal("cleanup deadline cannot contain the native call")
	}
	var request HostRequest
	if err := json.Unmarshal(runner.request.Canonical, &request); err != nil || len(request.Cluster.Delegated) != 2 {
		t.Fatal("cleanup did not receive the frozen daemon placement", err)
	}
	e.MachineStopped = func(context.Context, string) (bool, error) { return false, nil }
	if got, err := capability.ObserveRemoval(context.Background(), e); err != nil || got.Effect != reconciliation.EffectPartial || runner.calls != 1 {
		t.Fatalf("cleanup receipt alone should not settle power %+v %v", got, err)
	}
	result, err = capability.Destroy(context.Background(), e)
	if err != nil || result.Outcome != reconciliation.OutcomeChanged || runner.calls != 1 || !slices.Equal(power, []bool{true, false, false}) {
		t.Fatalf("retry repeated guest cleanup or skipped shutdown: %+v %v %v", result, err, power)
	}
	e.MachineStopped = func(context.Context, string) (bool, error) { return true, nil }
	if got, err := capability.ObserveRemoval(context.Background(), e); err != nil || got.Effect != reconciliation.EffectCompleted || runner.calls != 1 {
		t.Fatalf("receipt and stopped Machine should settle removal %+v %v", got, err)
	}
	proof.Ready = true
	e.RemovalPreparations[0], _ = json.Marshal(proof)
	if got, err := capability.ObserveRemoval(context.Background(), e); err == nil || got.Effect == reconciliation.EffectCompleted {
		t.Fatal("malformed native absence completed")
	}
	machines, err := capability.HostMachines(e.Block)
	if err != nil || len(machines) != len(r.Cluster.Hosts) {
		t.Fatal("complete host-removal gate lost", machines, err)
	}
	q, err := capability.Quiescent(context.Background(), lifecycle.Probe{Block: e.Block})
	if err != nil || q.State != lifecycle.Unproved {
		t.Fatal("surviving hosts allowed cleanup", q, err)
	}
	if !strings.Contains(q.Reason, "outlive") {
		t.Fatal("unhelpful surviving-host refusal")
	}
}

func TestProvidedArbiterDoesNotInheritPhysicalPeerRemoval(t *testing.T) {
	e, r, proofs := hostExecution(t, "node-1")
	for i := range r.Cluster.Hosts {
		h := &r.Cluster.Hosts[i]
		if h.Name == r.Host {
			h.Provided, h.HostKeyRef, h.KnownHostsRef = true, "", "arbiter-pin"
			h.Devices = []Device{}
			continue
		}
		h.Physical = true
		claim := retirementBlock(t, "claim-"+h.Machine, string(api.Machine), h.Machine, "machine-baremetal-v1", []byte(`{}`))
		e.ApplyProved = append(e.ApplyProved, retirementRecord(claim, reconciliation.Apply, []byte(`{}`)))
	}
	raw, _ := r.canonical()
	e.Block = retirementBlock(t, e.Block.ID, e.Block.Kind, e.Block.Object, e.Block.Implementation, raw)
	e.ApplyOperation = "apply-original"
	proof := HostEvidence{Version: HostEvidenceVersion, Request: e.Block.RequestDigest, Host: r.Host, Owner: e.ApplyOperation,
		FSID: FSIDFor(e.ApplyOperation, r.Cluster.Cluster), Image: PinnedRepository + "@" + resolvedDigest, Removed: true, Devices: []HostDevice{}}
	evidence, _ := json.Marshal(proof)
	runner := &recordingRunner{result: lifecycle.RunResult{Outcome: reconciliation.OutcomeUnchanged, Evidence: evidence}}
	capability := NewHost(runner).WithProofs(retainedHostProofs{proofs})
	result, err := capability.Destroy(context.Background(), e)
	if err != nil || result.Outcome != reconciliation.OutcomeUnchanged || runner.calls != 1 {
		t.Fatalf("provided release = %+v (%v)", result, err)
	}
	observed, err := capability.ObserveRemoval(context.Background(), e)
	if err != nil || observed.Effect != reconciliation.EffectCompleted || runner.calls != 1 {
		t.Fatalf("provided release observation = %+v (%v)", observed, err)
	}
}
