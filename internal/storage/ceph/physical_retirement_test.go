package ceph

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func retirementBlock(t *testing.T, id, kind, object, implementation string, data []byte) reconciliation.Block {
	t.Helper()
	definition := reconciliation.BlockDefinition{ID: id, Kind: kind, Object: object, Implementation: implementation, ContentDigest: strings.Repeat("f", 64), Request: data}
	digest, err := reconciliation.RequestDigest(definition)
	if err != nil {
		t.Fatal(err)
	}
	return reconciliation.Block{BlockDefinition: definition, RequestDigest: digest}
}

func retirementRecord(block reconciliation.Block, verb reconciliation.Verb, data []byte) lifecycle.BlockEvidence {
	return lifecycle.BlockEvidence{Kind: block.Kind, Object: block.Object, Implementation: block.Implementation, Block: block, State: reconciliation.BlockDone, Verb: verb, Evidence: data}
}

func physicalRetirementFixture(t *testing.T) (PhysicalRetirementCapability, lifecycle.Execution, Request, map[string]PhysicalErasure) {
	t.Helper()
	catalog, object := physicalCatalog()
	cluster, err := baseRequest(catalog, object, "synthetic")
	if err != nil {
		t.Fatal(err)
	}
	request := physicalRetirementRequest(catalog, cluster)
	for i := range request.Hosts {
		request.Hosts[i].EraseWWNs = []string{request.Hosts[i].Devices[0].WWN}
	}
	data, _ := request.Canonical()
	block := retirementBlock(t, PhysicalRetirementBlockID(cluster.Cluster), string(api.CephCluster), cluster.Cluster, PhysicalRetirementImplementation, data)
	execution := lifecycle.Execution{ApplyOperation: "apply-original", Operation: "remove-original", Block: block}
	identity := PhysicalIdentity{UUID: "11111111-1111-4111-8111-111111111111", MACs: []string{"02:00:00:00:00:01"}}
	proofs := map[string]PhysicalErasure{}
	cluster.Packages = &PackageRequest{Sources: []PackageSource{}}
	for _, host := range cluster.Hosts {
		claim := retirementBlock(t, "claim-"+host.Machine, string(api.Machine), host.Machine, "machine-baremetal-v1", []byte(`{}`))
		execution.ApplyProved = append(execution.ApplyProved, retirementRecord(claim, reconciliation.Apply, []byte(`{}`)))
		cluster.Packages.Sources = append(cluster.Packages.Sources, PackageSource{Host: host.Name})
		proofs[host.Machine] = PhysicalErasure{MACs: slices.Clone(identity.MACs), Targets: []Device{{WWN: host.Devices[0].WWN, Serial: "SYNTHETIC-" + host.Machine, SizeBytes: 1 << 40}}}
	}
	for _, host := range cluster.Hosts {
		hostRequest := HostRequest{Version: HostVersion, Cluster: scopeOf(cluster), Host: host.Name}
		data, _ := hostRequest.canonical()
		hostBlock := retirementBlock(t, HostBlockID(cluster.Cluster, host.Machine), string(api.CephCluster), cluster.Cluster, HostImplementation, data)
		hostExecution := execution
		hostExecution.Block = hostBlock
		evidence := preparedEvidence(t, hostExecution, hostRequest, func(value *HostEvidence) {
			value.Devices = []HostDevice{{Path: host.Devices[0].Path, WWN: host.Devices[0].WWN, Serial: "SYNTHETIC-" + host.Machine,
				KernelPath: "/dev/sdb", DeviceID: "SYNTHETIC_" + host.Machine, SizeBytes: 1 << 40}}
		})
		execution.RemovalProved = append(execution.RemovalProved, lifecycle.RemovalEvidence{
			Applied: retirementRecord(hostBlock, reconciliation.Apply, evidence), Removed: retirementRecord(hostBlock, reconciliation.Destroy, []byte(`{"pendingErase":true}`))})
		eraseBlock := retirementBlock(t, "erase-"+host.Machine, string(api.Machine), host.Machine, "fixture-erase", []byte(`{}`))
		execution.RemovalProved = append(execution.RemovalProved, lifecycle.RemovalEvidence{
			Applied: retirementRecord(eraseBlock, reconciliation.Apply, []byte(`{}`)), Removed: retirementRecord(eraseBlock, reconciliation.Destroy, []byte(`{"fullyVerified":true}`))})
	}
	capability := NewPhysicalRetirement().WithProofs(physicalProofs{identity: identity}).WithErasureProofs(func(applied reconciliation.Block, _ lifecycle.BlockEvidence, uuid string) (PhysicalErasure, bool, error) {
		if applied.Implementation != "fixture-erase" {
			return PhysicalErasure{}, false, nil
		}
		if uuid != identity.UUID {
			return PhysicalErasure{}, false, errors.New("synthetic wrong UUID")
		}
		return proofs[applied.Object], true, nil
	})
	return capability, execution, cluster, proofs
}

func TestPhysicalRetirementGoldenRequiresOriginalDiskBindingAndEveryCompleteReceipt(t *testing.T) {
	capability, execution, _, _ := physicalRetirementFixture(t)
	result, err := capability.Destroy(context.Background(), execution)
	if err != nil || result.Outcome != reconciliation.OutcomeUnchanged {
		t.Fatalf("complete physical retirement refused: %v", err)
	}
	var proof PhysicalRetirementEvidence
	if err := json.Unmarshal(result.Evidence, &proof); err != nil || !proof.Retired || !proof.Prepared || len(proof.Receipts) != 3 {
		t.Fatalf("missing terminal retirement: %+v %v", proof, err)
	}
	matchesGolden(t, "request-physical-retirement", execution.Block.Request, false)
	matchesGolden(t, "evidence-physical-retirement", result.Evidence, false)
	execution.ReadOnly = true
	observed, err := capability.ObserveRemoval(context.Background(), execution)
	if err != nil || observed.Effect != reconciliation.EffectCompleted || string(observed.Evidence) != string(result.Evidence) {
		t.Fatal("read-only resolution lost the original erasure proof", err)
	}
}

func TestPhysicalCephRetirementRecordsPendingDispositionWithoutNativeEffects(t *testing.T) {
	capability, execution, cluster, _ := physicalRetirementFixture(t)
	prepared, err := capability.Apply(context.Background(), execution)
	if err != nil {
		t.Fatal(err)
	}
	execution.ApplyProved = append(execution.ApplyProved, retirementRecord(execution.Block, reconciliation.Apply, prepared.Evidence))
	canonical, _ := cluster.Canonical()
	core := retirementBlock(t, ClusterBlockID(cluster.Cluster), string(api.CephCluster), cluster.Cluster, Implementation, canonical)
	execution.Block, execution.RetiredWithHosts = core, true
	runner := &recordingRunner{}
	storage := New(api.CephCluster, runner).WithProofs(capability.proofs)
	result, err := storage.Destroy(context.Background(), execution)
	var pending Retirement
	if err != nil || json.Unmarshal(result.Evidence, &pending) != nil || !pending.PendingErase || runner.calls != 0 {
		t.Fatal("physical core retirement claimed absence or reached the native runner", err, pending, runner.calls)
	}
	if _, err := EvaluateRetirement(result.Evidence, cluster, true); err == nil {
		t.Fatal("unbound FSID fixture admitted unrelated retirement")
	}
	for _, pair := range execution.RemovalProved {
		if pair.Applied.Implementation != HostImplementation {
			continue
		}
		execution.Block = pair.Applied.Block
		host, err := NewHost(runner).WithProofs(capability.proofs).Destroy(context.Background(), execution)
		if err != nil || json.Unmarshal(host.Evidence, &pending) != nil || !pending.PendingErase || runner.calls != 0 {
			t.Fatal("physical host release claimed erasure or reached the native runner", err, pending, runner.calls)
		}
	}
	execution.ApplyProved = append(execution.ApplyProved, retirementRecord(core, reconciliation.Apply, []byte(`{}`)))
	clusters, err := pendingPhysicalClusters(execution, []string{cluster.Cluster}, requestMachines(cluster))
	if err != nil || !slices.Equal(clusters, []string{cluster.Cluster}) {
		t.Fatal("a physical consumer lost the pending erasure obligation", clusters, err)
	}
	if _, err := pendingPhysicalClusters(execution, []string{cluster.Cluster}, []string{cluster.Hosts[0].Machine}); err == nil {
		t.Fatal("a physical consumer admitted partial host removal")
	}
}

func TestPhysicalRetirementRejectsMissingForeignOrIncompleteErasure(t *testing.T) {
	for _, damage := range []string{"missing-receipt", "duplicate-receipt", "missing-host", "wrong-serial", "wrong-size", "missing-disk", "wrong-macs", "unproved-erase", "other-owner"} {
		t.Run(damage, func(t *testing.T) {
			capability, execution, _, proofs := physicalRetirementFixture(t)
			switch damage {
			case "missing-receipt":
				execution.RemovalProved = slices.Delete(execution.RemovalProved, 1, 2)
			case "duplicate-receipt":
				execution.RemovalProved = append(execution.RemovalProved, execution.RemovalProved[1])
			case "missing-host":
				execution.RemovalProved = slices.Delete(execution.RemovalProved, 0, 1)
			case "unproved-erase":
				execution.RemovalProved[1].Removed.State = reconciliation.BlockUnknown
			case "other-owner":
				execution.ApplyOperation = "another-apply"
			default:
				value := proofs["node-1"]
				switch damage {
				case "wrong-serial":
					value.Targets[0].Serial = "replacement"
				case "wrong-size":
					value.Targets[0].SizeBytes--
				case "missing-disk":
					value.Targets = nil
				case "wrong-macs":
					value.MACs = []string{"02:00:00:00:00:ff"}
				}
				proofs["node-1"] = value
			}
			result, err := capability.Destroy(context.Background(), execution)
			if err == nil || result.Outcome == reconciliation.OutcomeChanged || result.Outcome == reconciliation.OutcomeUnchanged {
				t.Fatal("incomplete or foreign erasure counted as terminal retirement")
			}
		})
	}
}

func TestPhysicalHostReleaseRejectsAnotherFrozenIdentity(t *testing.T) {
	capability, execution, _, _ := physicalRetirementFixture(t)
	prepared, err := capability.Apply(context.Background(), execution)
	if err != nil {
		t.Fatal(err)
	}
	execution.ApplyProved = append(execution.ApplyProved, retirementRecord(execution.Block, reconciliation.Apply, prepared.Evidence))
	execution.Block = execution.RemovalProved[0].Applied.Block
	for _, damage := range []string{"block", "owner", "fsid"} {
		t.Run(damage, func(t *testing.T) {
			changed := execution
			request, err := DecodeHostRequest(changed.Block.Request)
			if err != nil {
				t.Fatal(err)
			}
			switch damage {
			case "block":
				changed.Block.ID += "-foreign"
			case "owner":
				request.Cluster.Owner = "another-apply"
			case "fsid":
				request.Cluster.FSID = FSIDFor("another-apply", request.Cluster.Cluster)
			}
			changed.Block.Request, _ = request.canonical()
			runner := &recordingRunner{}
			result, err := NewHost(runner).Destroy(context.Background(), changed)
			if err == nil || result.Outcome != reconciliation.OutcomeFailed || runner.calls != 0 || len(result.Evidence) != 0 {
				t.Fatal("foreign physical host release accepted", result, err, runner.calls)
			}
		})
	}
}

func TestPhysicalRetirementIntentOnlyRequiresPositiveUnstartedCore(t *testing.T) {
	for _, state := range []reconciliation.BlockState{"", reconciliation.BlockPending, reconciliation.BlockFailed, reconciliation.BlockUnknown, reconciliation.BlockDone} {
		t.Run(string(state), func(t *testing.T) {
			capability, execution, cluster, _ := physicalRetirementFixture(t)
			execution.RemovalProved = slices.DeleteFunc(execution.RemovalProved, func(pair lifecycle.RemovalEvidence) bool { return pair.Applied.Implementation == HostImplementation })
			if state != "" {
				data, _ := cluster.Canonical()
				block := retirementBlock(t, ClusterBlockID(cluster.Cluster), string(api.CephCluster), cluster.Cluster, Implementation, data)
				proof := retirementRecord(block, reconciliation.Apply, nil)
				proof.State = state
				execution.OriginalRemovalDependents = []lifecycle.BlockEvidence{proof}
			}
			_, err := capability.Destroy(context.Background(), execution)
			if (err == nil) != (state == reconciliation.BlockPending) {
				t.Fatalf("state %s produced %v", state, err)
			}
		})
	}
}

func TestPhysicalRetirementPolicyRefusesMissingDataDBOrWALIntent(t *testing.T) {
	catalog, object := physicalCatalog()
	cluster, err := baseRequest(catalog, object, "synthetic")
	if err != nil {
		t.Fatal(err)
	}
	request := physicalRetirementRequest(catalog, cluster)
	if err := request.authorized(); err == nil {
		t.Fatal("retained physical hosts admitted as retirement")
	}
	for i := range request.Hosts {
		request.Hosts[i].EraseWWNs = []string{request.Hosts[i].Devices[0].WWN}
	}
	if err := request.authorized(); err != nil {
		t.Fatal(err)
	}
	request.Hosts[0].Devices = append(request.Hosts[0].Devices, Device{Path: "/dev/disk/by-id/wwn-0x7000000000000001", WWN: "0x7000000000000001"})
	if err := request.authorized(); err == nil {
		t.Fatal("uncovered auxiliary disk admitted as retirement")
	}
}

func TestPhysicalRetirementOmissionRetainsAndCannotAuthorizeLaterErasure(t *testing.T) {
	capability, execution, _, _ := physicalRetirementFixture(t)
	request, err := DecodePhysicalRetirementRequest(execution.Block.Request)
	if err != nil {
		t.Fatal(err)
	}
	for index := range request.Hosts {
		request.Hosts[index].EraseWWNs = []string{}
	}
	data, _ := request.Canonical()
	execution.Block = retirementBlock(t, execution.Block.ID, execution.Block.Kind, execution.Block.Object, execution.Block.Implementation, data)
	capability.erasure = func(reconciliation.Block, lifecycle.BlockEvidence, string) (PhysicalErasure, bool, error) {
		t.Fatal("an omitted original erase policy reached the erasure reader")
		return PhysicalErasure{}, false, nil
	}
	if _, err := capability.Apply(context.Background(), execution); err != nil {
		t.Fatal("omission must still admit retaining installation", err)
	}
	if _, err := capability.Removal(context.Background(), execution.Block); err != nil {
		t.Fatal("read-only policy verification must remain describable", err)
	}
	result, err := capability.Destroy(context.Background(), execution)
	if err == nil || result.Outcome == reconciliation.OutcomeUnchanged || len(result.Evidence) != 0 {
		t.Fatal("omission proved retirement", result, err)
	}
	if string(execution.Block.Request) != string(data) {
		t.Fatal("the inverse changed the original frozen erase policy")
	}
}

func TestMonitorOnlyPhysicalHostStillNeedsItsOriginalRootErasePolicy(t *testing.T) {
	capability, execution, _, _ := physicalRetirementFixture(t)
	request, err := DecodePhysicalRetirementRequest(execution.Block.Request)
	if err != nil {
		t.Fatal(err)
	}
	request.Hosts = request.Hosts[:1]
	request.Hosts[0].Devices = []Device{}
	data, _ := request.Canonical()
	if _, err := DecodePhysicalRetirementRequest(data); err != nil {
		t.Fatal("a monitor-only host was rejected", err)
	}
	if err := request.authorized(); err != nil {
		t.Fatal("a monitor-only host with a frozen root target was refused", err)
	}
	request.Hosts[0].EraseWWNs = []string{}
	data, _ = request.Canonical()
	execution.Block = retirementBlock(t, execution.Block.ID, execution.Block.Kind, execution.Block.Object, execution.Block.Implementation, data)
	if _, err := capability.Apply(context.Background(), execution); err != nil {
		t.Fatal(err)
	}
	if err := request.authorized(); err == nil {
		t.Fatal("an empty data-disk list bypassed root erasure authorization")
	}
}

func TestPhysicalPolicyCleanupGoldenRequiresPositiveCoreAndInstallationNoEffectProof(t *testing.T) {
	for _, state := range []reconciliation.BlockState{"", reconciliation.BlockPending, reconciliation.BlockUnknown, reconciliation.BlockDone} {
		t.Run(string(state), func(t *testing.T) {
			capability, execution, cluster, _ := physicalRetirementFixture(t)
			execution.RemovalProved = nil
			request, _ := DecodePhysicalRetirementRequest(execution.Block.Request)
			for index := range request.Hosts {
				request.Hosts[index].EraseWWNs = []string{}
			}
			data, _ := request.Canonical()
			execution.Block = retirementBlock(t, execution.Block.ID, execution.Block.Kind, execution.Block.Object, execution.Block.Implementation, data)
			calls := 0
			capability = capability.WithUninstalledProofs(func(_ []lifecycle.BlockEvidence, _ []lifecycle.RemovalEvidence, machine string) (bool, error) {
				calls++
				return machine != "", nil
			}).WithErasureProofs(func(reconciliation.Block, lifecycle.BlockEvidence, string) (PhysicalErasure, bool, error) {
				t.Fatal("positive no-install cleanup reached a disk erasure proof")
				return PhysicalErasure{}, false, nil
			})
			if state != "" {
				data, _ := cluster.Canonical()
				block := retirementBlock(t, ClusterBlockID(cluster.Cluster), string(api.CephCluster), cluster.Cluster, Implementation, data)
				proof := retirementRecord(block, reconciliation.Apply, nil)
				proof.State = state
				execution.OriginalRemovalDependents = []lifecycle.BlockEvidence{proof}
			}
			result, err := capability.Destroy(context.Background(), execution)
			if state != reconciliation.BlockPending {
				if err == nil || calls != 0 {
					t.Fatal("unknown core history admitted no-install cleanup", err, calls)
				}
				return
			}
			var proof PhysicalRetirementEvidence
			if err != nil || json.Unmarshal(result.Evidence, &proof) != nil || !proof.Retired || calls != 3 {
				t.Fatal("positive no-install cleanup refused", err, calls)
			}
			matchesGolden(t, "evidence-physical-retained-unstarted", result.Evidence, false)
			for _, receipt := range proof.Receipts {
				if !receipt.RetainedWithoutInstall || receipt.Block != "" || receipt.Digest != "" || receipt.Request != "" {
					t.Fatal("retained policy claimed disk erasure", receipt)
				}
			}
		})
	}
}
