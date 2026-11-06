package ceph

import (
	"context"
	"encoding/json"
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
	result, err := capability.Destroy(context.Background(), e)
	if err != nil || result.Outcome != reconciliation.OutcomeChanged || runner.calls != 1 {
		t.Fatalf("destroy %+v %v", result, err)
	}
	if runner.request.Deadline < 660*time.Second {
		t.Fatal("cleanup deadline cannot contain the native call")
	}
	runner.result.Outcome = "unchanged"
	observed, err := capability.ObserveRemoval(context.Background(), e)
	if err != nil || observed.Effect != reconciliation.EffectCompleted || runner.calls != 2 {
		t.Fatalf("observe %+v %v", observed, err)
	}
	if runner.request.MaterialValues["removal"] != "true" {
		t.Fatal("observation did not select read-only native removal")
	}
	proof.Ready = true
	runner.result.Evidence, _ = json.Marshal(proof)
	if got, err := capability.ObserveRemoval(context.Background(), e); err == nil || got.Effect == reconciliation.EffectCompleted {
		t.Fatal("malformed native absence completed")
	}
	proof.Ready = false
	proof.Removed = false
	proof.NativeRemoved = false
	runner.result.Evidence, _ = json.Marshal(proof)
	if got, err := capability.ObserveRemoval(context.Background(), e); err != nil || got.Effect != reconciliation.EffectPartial {
		t.Fatalf("partial cleanup %+v %v", got, err)
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
