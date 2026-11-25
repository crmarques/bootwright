package ceph

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
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

// eraseTargetsOf declares each device as the fixture's erase receipts name it:
// a WWN disk by its WWN beside its synthetic serial, a serial disk by its own.
func eraseTargetsOf(machine string, devices []Device) []PhysicalEraseTarget {
	targets := []PhysicalEraseTarget{}
	for disk, device := range devices {
		if device.WWN == "" {
			targets = append(targets, PhysicalEraseTarget{Serial: device.Serial, SizeBytes: device.SizeBytes})
			continue
		}
		targets = append(targets, PhysicalEraseTarget{Serial: syntheticSerial(machine, disk), SizeBytes: 1 << 40, WWN: device.WWN})
	}
	slices.SortFunc(targets, func(a, b PhysicalEraseTarget) int { return strings.Compare(a.Serial, b.Serial) })
	return targets
}

func physicalRetirementFixture(t *testing.T) (PhysicalRetirementCapability, lifecycle.Execution, Request, map[string]PhysicalErasure) {
	t.Helper()
	return physicalRetirementFixtureFrom(t, physicalCatalog)
}

func physicalRetirementFixtureFrom(t *testing.T, catalogOf func() (api.Catalog, api.Object)) (PhysicalRetirementCapability, lifecycle.Execution, Request, map[string]PhysicalErasure) {
	t.Helper()
	catalog, object := catalogOf()
	cluster, err := baseRequest(catalog, object, "synthetic")
	if err != nil {
		t.Fatal(err)
	}
	for i := range cluster.Hosts {
		cluster.Hosts[i].RetainRoot = false
	}
	request := physicalRetirementRequest(catalog, cluster)
	for i := range request.Hosts {
		request.Hosts[i].EraseTargets = eraseTargetsOf(request.Hosts[i].Machine, request.Hosts[i].Devices)
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
		erasure := PhysicalErasure{MACs: slices.Clone(identity.MACs), Targets: []Device{}}
		for disk, device := range host.Devices {
			if device.WWN == "" {
				erasure.Targets = append(erasure.Targets, Device{Serial: device.Serial, SizeBytes: device.SizeBytes})
				continue
			}
			erasure.Targets = append(erasure.Targets, Device{WWN: device.WWN, Serial: syntheticSerial(host.Machine, disk), SizeBytes: 1 << 40})
		}
		proofs[host.Machine] = erasure
	}
	for _, host := range cluster.Hosts {
		hostRequest := HostRequest{Version: HostVersion, Cluster: scopeOf(cluster), Host: host.Name, ImagesSeconds: hostImagesBudget, Pulls: []ImagePull{}}
		data, _ := hostRequest.canonical()
		hostBlock := retirementBlock(t, HostBlockID(cluster.Cluster, host.Machine), string(api.CephCluster), cluster.Cluster, HostImplementation, data)
		hostExecution := execution
		hostExecution.Block = hostBlock
		evidence := preparedEvidence(t, hostExecution, hostRequest, func(value *HostEvidence) {
			value.Devices = []HostDevice{}
			for disk, device := range host.Devices {
				if device.WWN == "" {
					value.Devices = append(value.Devices, HostDevice{Path: device.Path, Serial: device.Serial, SizeBytes: device.SizeBytes, KernelPath: syntheticKernelPath(disk),
						DeviceID: "SYNTHETIC_" + device.Serial, Alias: "/dev/disk/by-id/nvme-SYNTHETIC_" + device.Serial})
					continue
				}
				value.Devices = append(value.Devices, HostDevice{Path: device.Path, WWN: device.WWN, Serial: syntheticSerial(host.Machine, disk),
					KernelPath: syntheticKernelPath(disk), DeviceID: "SYNTHETIC_" + syntheticSerial(host.Machine, disk), SizeBytes: 1 << 40})
			}
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

func syntheticSerial(machine string, disk int) string {
	if disk == 0 {
		return "SYNTHETIC-" + machine
	}
	return fmt.Sprintf("SYNTHETIC-%s-%d", machine, disk)
}

func syntheticKernelPath(disk int) string {
	if disk == 0 {
		return "/dev/sdb"
	}
	return fmt.Sprintf("/dev/nvme%dn1", disk)
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

// A retained root's cleanup runs rm-cluster while the cluster's other hosts still run, so a live manager would write the FSID's layout
// back onto the cleaned host through the SSH connection it kept: the cluster block, which every host block's removal follows, stops
// every physical host's managers first and records no retirement until no manager runs or stays enabled.
func TestARetainedRootClusterRetirementStopsEveryManagerBeforeAnyHostCleanup(t *testing.T) {
	capability, execution, cluster, _ := physicalRetirementFixture(t)
	policy, err := DecodePhysicalRetirementRequest(execution.Block.Request)
	if err != nil {
		t.Fatal(err)
	}
	policy.Hosts[0].RetainRoot = true
	data, _ := policy.Canonical()
	execution.Block = retirementBlock(t, execution.Block.ID, execution.Block.Kind, execution.Block.Object, execution.Block.Implementation, data)
	prepared, err := capability.Apply(context.Background(), execution)
	if err != nil {
		t.Fatal(err)
	}
	execution.ApplyProved = append(execution.ApplyProved, retirementRecord(execution.Block, reconciliation.Apply, prepared.Evidence),
		imageProof(t, cluster.Cluster, cluster.Image, resolvedDigest))
	for _, pair := range execution.RemovalProved {
		if pair.Applied.Implementation == HostImplementation {
			execution.ApplyProved = append(execution.ApplyProved, pair.Applied)
		}
	}
	cluster.Hosts[0].RetainRoot, cluster.Controller = true, "controller"
	if execution.Material == nil {
		execution.Material = map[string]secrets.Material{}
	}
	for i := range cluster.Hosts {
		if cluster.Hosts[i].HostKeyRef == "" {
			cluster.Hosts[i].HostKeyRef = cluster.Hosts[i].Name + "-host-key"
		}
		execution.Material[cluster.Hosts[i].HostKeyRef] = secrets.NewMaterial(map[secrets.Part][]byte{secrets.PublicKeyPart: hostPublicKey()})
	}
	canonical, _ := cluster.Canonical()
	execution.Block, execution.RetiredWithHosts = retirementBlock(t, ClusterBlockID(cluster.Cluster), string(api.CephCluster), cluster.Cluster, Implementation, canonical), true
	execution.Operation, execution.Attempt = "destroy-later", 1
	for name, test := range map[string]struct {
		answer  func(lifecycle.RunRequest) (lifecycle.RunResult, error)
		retired bool
	}{
		"stopped": {func(request lifecycle.RunRequest) (lifecycle.RunResult, error) { return managersAnswer(request, true) }, true},
		"running": {func(request lifecycle.RunRequest) (lifecycle.RunResult, error) { return managersAnswer(request, false) }, false},
		"refused": {func(request lifecycle.RunRequest) (lifecycle.RunResult, error) {
			return lifecycle.RunResult{}, request.Refusals["managers-running"]
		}, false},
		"unreachable": {func(request lifecycle.RunRequest) (lifecycle.RunResult, error) {
			return lifecycle.RunResult{}, request.Refusals["transport"]
		}, false},
	} {
		t.Run(name, func(t *testing.T) {
			e := execution
			hosts := installedMachines(cluster)
			running := map[string]bool{}
			var calls []string
			e.PowerMachine = func(_ context.Context, name string, on bool) error {
				if !slices.Contains(hosts, name) {
					t.Fatalf("unexpected power target %q", name)
				}
				calls = append(calls, fmt.Sprintf("%s:%t", name, on))
				running[name] = on
				return nil
			}
			e.MachineStopped = func(_ context.Context, name string) (bool, error) { return !running[name], nil }
			e.PrepareRemoval = func(_ context.Context, receipt json.RawMessage) error {
				calls = append(calls, "receipt")
				e.RemovalPreparations = append(e.RemovalPreparations, receipt)
				return nil
			}
			runner := &tiebreakerRunner{managers: func(request lifecycle.RunRequest) (lifecycle.RunResult, error) {
				if slices.ContainsFunc(hosts, func(name string) bool { return !running[name] }) {
					t.Fatal("Ceph contacted the fleet before every installed Machine was powered on")
				}
				calls = append(calls, request.Operation)
				return test.answer(request)
			}}
			core := New(api.CephCluster, runner).WithProofs(capability.proofs)
			ctx := context.Background()
			if name == "stopped" {
				for _, powered := range []bool{false, true} {
					running[hosts[0]] = powered
					observed, err := core.ObserveRemoval(ctx, e)
					if err != nil || observed.Effect != reconciliation.EffectPartial || len(calls) != 0 {
						t.Fatalf("a power-on interruption was not safely retriable: %+v (%v), calls %v", observed, err, calls)
					}
				}
				running[hosts[0]] = false
			}
			result, err := core.Destroy(ctx, e)
			if len(runner.runs) != 1 || runner.runs[0].Operation != "destroy" || runner.runs[0].MaterialValues["retirement"] != managersRetirement || runner.runs[0].ReadOnly {
				t.Fatalf("the managers did not stop before any host cleanup: %d runs", len(runner.runs))
			}
			if test.retired != (err == nil && result.Evidence != nil) {
				t.Fatalf("destroy %+v %+v", result, diagnostics.Of(err))
			}
			if !test.retired && (result.Outcome == reconciliation.OutcomeChanged || len(diagnostics.Of(err)) != 1 || diagnostics.Of(err)[0].Code != "ceph.boundary-unproved") {
				t.Fatalf("a running manager retired the cluster: %+v %+v", result, diagnostics.Of(err))
			}
			var wantCalls []string
			for _, name := range hosts {
				wantCalls = append(wantCalls, name+":true")
			}
			wantCalls = append(wantCalls, "destroy")
			if test.retired {
				wantCalls = append(wantCalls, "receipt")
				for _, name := range hosts {
					wantCalls = append(wantCalls, name+":false")
				}
			}
			if !slices.Equal(calls, wantCalls) {
				t.Fatalf("retirement performed %v, want %v", calls, wantCalls)
			}
			observed, err := core.ObserveRemoval(ctx, e)
			want := reconciliation.EffectCompleted
			if !test.retired {
				want = reconciliation.EffectNoEffect
			}
			if name == "stopped" {
				if err != nil || observed.Effect != want || len(runner.runs) != 1 || !slices.Equal(calls, wantCalls) {
					t.Fatalf("a recorded proof did not settle with read-only power: %+v %+v, calls %v", observed, diagnostics.Of(err), calls)
				}
				assertManagerShutdownReplay(t, e, hosts, running, &calls, core)
			} else if name == "running" {
				if err != nil || observed.Effect != want || runner.runs[1].Operation != "observe" || runner.runs[1].MaterialValues["retirement"] != managersRetirement {
					t.Fatalf("observation %+v %+v", observed, diagnostics.Of(err))
				}
			} else if err == nil || observed.Effect != reconciliation.EffectUnknown {
				t.Fatalf("an unanswered observation %+v proved an effect", observed)
			}
		})
	}
}

func assertManagerShutdownReplay(t *testing.T, e lifecycle.Execution, hosts []string, running map[string]bool, calls *[]string, core Capability) {
	t.Helper()
	ctx := context.Background()
	running[hosts[0]] = true
	*calls = nil
	observation, err := core.ObserveRemoval(ctx, e)
	if err != nil || observation.Effect != reconciliation.EffectPartial || len(*calls) != 0 {
		t.Fatalf("a pending shutdown did not stay read-only and partial: %+v (%v), %v", observation, err, *calls)
	}
	result, err := core.Destroy(ctx, e)
	var want []string
	for _, host := range hosts {
		want = append(want, host+":false")
	}
	if err != nil || !result.Outcome.Settled() || !slices.Equal(*calls, want) {
		t.Fatalf("replay did more than finish shutdown: %+v (%v), %v", result, err, *calls)
	}
	*calls = nil
	e.RemovalPreparations = []json.RawMessage{json.RawMessage(`{}`)}
	if result, err := core.Destroy(ctx, e); err == nil || result.Outcome.Settled() || len(*calls) != 0 {
		t.Fatalf("an invalid receipt reached the guests or power control: %+v (%v), %v", result, err, *calls)
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
		request.Hosts[i].EraseTargets = eraseTargetsOf(request.Hosts[i].Machine, request.Hosts[i].Devices[:1])
	}
	if err := request.authorized(); err != nil {
		t.Fatal(err)
	}
	request.Hosts[0].Devices = append(request.Hosts[0].Devices, Device{Path: "/dev/disk/by-id/wwn-0x7000000000000001", WWN: "0x7000000000000001"})
	if err := request.authorized(); err == nil {
		t.Fatal("uncovered auxiliary disk admitted as retirement")
	}
	raw, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	block := retirementBlock(t, PhysicalRetirementBlockID(request.Cluster), string(api.CephCluster), request.Cluster, PhysicalRetirementImplementation, raw)
	warnings := NewPhysicalRetirement().RemovalWarnings(block)
	if len(warnings) != 1 || warnings[0].Code != cephRetirementUnavailable.Code ||
		!strings.Contains(warnings[0].Message, request.Hosts[0].Devices[1].Path) {
		t.Fatalf("warning should identify the uncovered disk: %+v", warnings)
	}
}

func TestPhysicalRetirementOmissionRetainsAndCannotAuthorizeLaterErasure(t *testing.T) {
	capability, execution, _, _ := physicalRetirementFixture(t)
	request, err := DecodePhysicalRetirementRequest(execution.Block.Request)
	if err != nil {
		t.Fatal(err)
	}
	for index := range request.Hosts {
		request.Hosts[index].EraseTargets = []PhysicalEraseTarget{}
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
	request.Hosts[0].EraseTargets = []PhysicalEraseTarget{}
	data, _ = request.Canonical()
	execution.Block = retirementBlock(t, execution.Block.ID, execution.Block.Kind, execution.Block.Object, execution.Block.Implementation, data)
	if _, err := capability.Apply(context.Background(), execution); err != nil {
		t.Fatal(err)
	}
	if err := request.authorized(); err == nil {
		t.Fatal("an empty data-disk list bypassed root erasure authorization")
	}
	warnings := capability.RemovalWarnings(execution.Block)
	if len(warnings) != 1 || warnings[0].Code != cephRetirementUnavailable.Code ||
		!strings.Contains(warnings[0].Message, "no original erase target") {
		t.Fatalf("missing root erasure warning: %+v", warnings)
	}
}

func TestPhysicalPolicyCleanupGoldenRequiresPositiveCoreAndInstallationNoEffectProof(t *testing.T) {
	for _, state := range []reconciliation.BlockState{"", reconciliation.BlockPending, reconciliation.BlockUnknown, reconciliation.BlockDone} {
		t.Run(string(state), func(t *testing.T) {
			capability, execution, cluster, _ := physicalRetirementFixture(t)
			execution.RemovalProved = nil
			request, _ := DecodePhysicalRetirementRequest(execution.Block.Request)
			for index := range request.Hosts {
				request.Hosts[index].EraseTargets = []PhysicalEraseTarget{}
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

func TestPhysicalRetirementPolicyCountsEitherEraseMethodAndNothingElse(t *testing.T) {
	for method, counted := range map[string]bool{"logicalOverwrite": true, "metadataClear": true, "retain": false} {
		t.Run(method, func(t *testing.T) {
			catalog, object := physicalCatalog()
			objects := catalog.Objects()
			for i, machine := range objects {
				if machine.Kind() == api.Machine {
					disk := requestMap("wwn", "0x7000000000000001", "serialNumber", "root-serial", "sizeBytes", 1<<40, "onRemoval", method)
					objects[i] = machine.WithSpec(machine.Spec().WithPath(api.ListValue(disk), "hardware", "dataDisks"))
				}
			}
			catalog = api.NewCatalog(objects)
			cluster, err := baseRequest(catalog, object, "synthetic")
			if err != nil {
				t.Fatal(err)
			}
			for _, host := range physicalRetirementRequest(catalog, cluster).Hosts {
				if got := slices.Equal(host.EraseTargets, []PhysicalEraseTarget{{Serial: "root-serial", SizeBytes: 1 << 40, WWN: "0x7000000000000001"}}); got != counted {
					t.Fatalf("%s policy on %s recorded erase targets %v", method, host.Machine, host.EraseTargets)
				}
			}
		})
	}
}

func serialRetirementCatalog() (api.Catalog, api.Object) { return physicalSerialCatalog(nil) }

func TestPhysicalRetirementCoversASerialSelectionByItsSerialNumberAndSize(t *testing.T) {
	capability, execution, _, _ := physicalRetirementFixtureFrom(t, serialRetirementCatalog)
	request, err := DecodePhysicalRetirementRequest(execution.Block.Request)
	if err != nil || request.Hosts[1].Devices[0] != (Device{Path: "/dev/disk/by-serial/SER-2", Serial: "SER-2", SizeBytes: serialSize}) {
		t.Fatalf("serial retirement policy %+v: %v", request, err)
	}
	if err := request.authorized(); err != nil {
		t.Fatal(err)
	}
	request.Hosts[1].EraseTargets = []PhysicalEraseTarget{{Serial: "SER-2", SizeBytes: serialSize + 4096}}
	if err := request.authorized(); err == nil {
		t.Fatal("an erase target of another size covered a serial selection")
	}
	result, err := capability.Destroy(context.Background(), execution)
	var proof PhysicalRetirementEvidence
	if err != nil || json.Unmarshal(result.Evidence, &proof) != nil || !proof.Retired || len(proof.Receipts) != 3 {
		t.Fatalf("complete serial retirement refused: %+v %v", proof, err)
	}
	for _, damage := range []string{"other-size", "other-serial", "declared-wwn-unobserved", "declared-wwn-other"} {
		t.Run(damage, func(t *testing.T) {
			capability, execution, _, proofs := physicalRetirementFixtureFrom(t, serialRetirementCatalog)
			value := proofs["node-2"]
			switch damage {
			case "other-size":
				value.Targets[0].SizeBytes += 4096
			case "other-serial":
				value.Targets[0].Serial = "ANOTHER"
			case "declared-wwn-unobserved":
				value.Targets[0].WWN = "0x7000000000000001"
			default:
				value.Targets[0].WWN = "naa.7000000000000002"
				setObservedWWN(t, &execution, "node-2", "0x7000000000000001")
			}
			proofs["node-2"] = value
			if result, err := capability.Destroy(context.Background(), execution); err == nil || result.Outcome == reconciliation.OutcomeUnchanged {
				t.Fatal("an erase receipt that does not cover the serial selection counted as retirement")
			}
		})
	}
	t.Run("declared-wwn-observed", func(t *testing.T) {
		capability, execution, _, proofs := physicalRetirementFixtureFrom(t, serialRetirementCatalog)
		value := proofs["node-2"]
		value.Targets[0].WWN = "naa.7000000000000001"
		proofs["node-2"] = value
		setObservedWWN(t, &execution, "node-2", "0x7000000000000001")
		if _, err := capability.Destroy(context.Background(), execution); err != nil {
			t.Fatal("the WWN a serial disk reported, spelled as its erase target declares it, was refused", err)
		}
	})
}

// setObservedWWN rewrites the original host binding of machine as if its
// serial disk had reported wwn, keeping the binding proved.
func setObservedWWN(t *testing.T, execution *lifecycle.Execution, machine, wwn string) {
	t.Helper()
	for i, pair := range execution.RemovalProved {
		if pair.Applied.Implementation != HostImplementation || pair.Applied.Block.ID != HostBlockID("example", machine) {
			continue
		}
		evidence, err := decodeHostEvidence(pair.Applied.Evidence, pair.Applied.Block.RequestDigest)
		if err != nil {
			t.Fatal(err)
		}
		evidence.Devices[0].WWN = wwn
		data, _ := json.Marshal(evidence)
		execution.RemovalProved[i].Applied.Evidence = data
		return
	}
	t.Fatal("no host binding for", machine)
}

func TestUnstartedSerialRetirementCarriesTheFrozenSerialIntent(t *testing.T) {
	_, execution, _, _ := physicalRetirementFixtureFrom(t, serialRetirementCatalog)
	request, _ := DecodePhysicalRetirementRequest(execution.Block.Request)
	host := policyHosts(request)[1]
	binding := physicalDiskBinding{Devices: []HostDevice{{Path: host.Devices[0].Path, Serial: host.Devices[0].Serial, SizeBytes: host.Devices[0].SizeBytes}}}
	if err := proveErasedDevices([]Device{{Serial: "SER-2", SizeBytes: serialSize}}, binding); err != nil {
		t.Fatal(err)
	}
	if err := proveErasedDevices([]Device{{Serial: "SER-2", SizeBytes: serialSize - 4096}}, binding); err == nil {
		t.Fatal("unstarted serial intent admitted an erase receipt of another size")
	}
}

// Retaining the root needs a native-absence receipt in addition to every disk erase.
func TestRetainedRootRetirementRequiresNativeCleanupAndAllDiskReceipts(t *testing.T) {
	capability, e, cluster, _ := physicalRetirementFixture(t)
	policy, err := DecodePhysicalRetirementRequest(e.Block.Request)
	if err != nil {
		t.Fatal(err)
	}
	for i := range policy.Hosts {
		policy.Hosts[i].RetainRoot = true
	}
	data, _ := policy.Canonical()
	e.Block = retirementBlock(t, e.Block.ID, e.Block.Kind, e.Block.Object, e.Block.Implementation, data)
	if _, err := capability.Destroy(context.Background(), e); err == nil {
		t.Fatal("disk erasure alone claimed retained-root retirement")
	}
	if !strings.HasPrefix(cluster.Image, PinnedRepository+":") {
		t.Fatalf("the fixture authors %s, not a tag of %s", cluster.Image, PinnedRepository)
	}
	hostCleanup := func(image string) lifecycle.Execution {
		cleaned := e
		cleaned.RemovalProved = slices.Clone(e.RemovalProved)
		for i, pair := range cleaned.RemovalProved {
			if pair.Applied.Implementation != HostImplementation {
				continue
			}
			request, err := DecodeHostRequest(pair.Applied.Block.Request)
			if err != nil {
				t.Fatal(err)
			}
			for n := range request.Cluster.Hosts {
				request.Cluster.Hosts[n].RetainRoot = true
			}
			raw, _ := request.canonical()
			block := retirementBlock(t, pair.Applied.Block.ID, pair.Applied.Kind, pair.Applied.Object, pair.Applied.Implementation, raw)
			proof, _ := json.Marshal(HostEvidence{Version: HostEvidenceVersion, Request: block.RequestDigest, Owner: e.ApplyOperation, FSID: FSIDFor(e.ApplyOperation, cluster.Cluster), Host: request.Host, Image: image, Removed: true, NativeRemoved: true, Devices: []HostDevice{}})
			var applied HostEvidence
			if err := json.Unmarshal(pair.Applied.Evidence, &applied); err != nil {
				t.Fatal(err)
			}
			applied.Request = block.RequestDigest
			appliedRaw, _ := json.Marshal(applied)
			cleaned.RemovalProved[i] = lifecycle.RemovalEvidence{Applied: retirementRecord(block, reconciliation.Apply, appliedRaw), Removed: retirementRecord(block, reconciliation.Destroy, proof)}
		}
		return cleaned
	}
	for name, image := range map[string]string{
		"the authored tag a cleanup never runs":  cluster.Image,
		"another repository":                     "cp.icr.io/cp/ibm-ceph/other@" + resolvedDigest,
		"a repository the authored one prefixes": PinnedRepository + "-other@" + resolvedDigest,
		"no manifest digest":                     PinnedRepository + "@sha256:short",
	} {
		reported := diagnostics.Of(func() error { _, err := capability.Destroy(context.Background(), hostCleanup(image)); return err }())
		if len(reported) != 1 || reported[0].Message != "the physical Ceph retirement policy is incomplete or inconsistent" {
			t.Fatalf("a host cleanup proof over %s counted: %+v", name, reported)
		}
	}
	e = hostCleanup(PinnedRepository + "@" + resolvedDigest)
	result, err := capability.Destroy(context.Background(), e)
	if err != nil {
		t.Fatal(err)
	}
	var proof PhysicalRetirementEvidence
	if err := json.Unmarshal(result.Evidence, &proof); err != nil {
		t.Fatal(err)
	}
	for _, receipt := range proof.Receipts {
		if !hex64(receipt.Cleanup) || !hex64(receipt.Digest) {
			t.Fatal("native cleanup or disk proof lost")
		}
	}
	replaced := e
	replaced.RemovalProved = slices.Clone(e.RemovalProved)
	for i := range replaced.RemovalProved {
		replaced.RemovalProved[i].Applied.Operation = e.ApplyOperation
		replaced.RemovalProved[i].Removed.Operation = "remove-replaced"
	}
	if lent, err := capability.Destroy(context.Background(), replaced); err != nil || string(lent.Evidence) != string(result.Evidence) {
		t.Fatalf("cleanup and erase receipts a replaced removal proved were not accepted: %s (%v)", lent.Evidence, err)
	}
	for i, pair := range e.RemovalProved {
		if pair.Applied.Implementation == "fixture-erase" {
			e.RemovalProved = append(e.RemovalProved[:i:i], e.RemovalProved[i+1:]...)
			break
		}
	}
	if _, err := capability.Destroy(context.Background(), e); err == nil {
		t.Fatal("native cleanup alone claimed disk erasure")
	}
}

func TestRetainedMonitorWithoutOSDsNeedsNativeReceiptInsteadOfRootErase(t *testing.T) {
	catalogOf := func() (api.Catalog, api.Object) {
		catalog, cluster := physicalCatalog()
		services := cluster.Spec().Get("services").Items()
		services = append(services[:2:2], services[3:]...)
		cluster = cluster.WithSpec(cluster.Spec().WithPath(api.ListValue(services...), "services"))
		objects := catalog.Objects()
		for i, o := range objects {
			if o.Kind() == api.CephCluster && o.Name() == cluster.Name() {
				objects[i] = cluster
			}
		}
		return api.NewCatalog(objects), cluster
	}
	capability, e, cluster, _ := physicalRetirementFixtureFrom(t, catalogOf)
	policy, err := DecodePhysicalRetirementRequest(e.Block.Request)
	if err != nil {
		t.Fatal(err)
	}
	if len(policy.Hosts[0].Devices) != 0 || len(policy.Hosts[0].EraseTargets) != 0 {
		t.Fatal("monitor fixture owns an OSD")
	}
	policy.Hosts[0].RetainRoot = true
	data, _ := policy.Canonical()
	e.Block = retirementBlock(t, e.Block.ID, e.Block.Kind, e.Block.Object, e.Block.Implementation, data)
	if _, err := capability.Destroy(context.Background(), e); err == nil {
		t.Fatal("zero-OSD monitor skipped native cleanup proof")
	}
	for i, pair := range e.RemovalProved {
		if pair.Applied.Implementation != HostImplementation {
			continue
		}
		request, err := DecodeHostRequest(pair.Applied.Block.Request)
		if err != nil {
			t.Fatal(err)
		}
		target, _ := request.target()
		if target.Machine != policy.Hosts[0].Machine {
			continue
		}
		for n := range request.Cluster.Hosts {
			if request.Cluster.Hosts[n].Machine == target.Machine {
				request.Cluster.Hosts[n].RetainRoot = true
			}
		}
		raw, _ := request.canonical()
		block := retirementBlock(t, pair.Applied.Block.ID, pair.Applied.Kind, pair.Applied.Object, pair.Applied.Implementation, raw)
		native, _ := json.Marshal(HostEvidence{Version: HostEvidenceVersion, Request: block.RequestDigest, Host: request.Host, Owner: e.ApplyOperation, FSID: FSIDFor(e.ApplyOperation, cluster.Cluster), Image: PinnedRepository + "@" + resolvedDigest, Removed: true, NativeRemoved: true, Devices: []HostDevice{}})
		var applied HostEvidence
		if err := json.Unmarshal(pair.Applied.Evidence, &applied); err != nil {
			t.Fatal(err)
		}
		applied.Request = block.RequestDigest
		original, _ := json.Marshal(applied)
		e.RemovalProved[i] = lifecycle.RemovalEvidence{Applied: retirementRecord(block, reconciliation.Apply, original), Removed: retirementRecord(block, reconciliation.Destroy, native)}
	}
	result, err := capability.Destroy(context.Background(), e)
	if err != nil {
		t.Fatal(err)
	}
	var terminal PhysicalRetirementEvidence
	if err := json.Unmarshal(result.Evidence, &terminal); err != nil {
		t.Fatal(err)
	}
	if !terminal.Retired || !hex64(terminal.Receipts[0].Cleanup) || terminal.Receipts[0].Digest != "" {
		t.Fatal("monitor retirement lacks native-only receipt", terminal.Receipts[0])
	}
}

func TestRetirementOfInstalledHostWithUnstartedCephPreparation(t *testing.T) {
	capability, e, cluster, _ := physicalRetirementFixture(t)
	policy, err := DecodePhysicalRetirementRequest(e.Block.Request)
	if err != nil {
		t.Fatal(err)
	}
	for i := range policy.Hosts {
		policy.Hosts[i].RetainRoot = true
	}
	raw, _ := policy.Canonical()
	e.Block = retirementBlock(t, PhysicalRetirementBlockID(cluster.Cluster), string(api.CephCluster), cluster.Cluster, PhysicalRetirementImplementation, raw)
	coreRaw, _ := cluster.Canonical()
	core := retirementBlock(t, ClusterBlockID(cluster.Cluster), string(api.CephCluster), cluster.Cluster, Implementation, coreRaw)
	unstarted := retirementRecord(core, reconciliation.Apply, nil)
	unstarted.State = reconciliation.BlockPending
	e.OriginalRemovalDependents = []lifecycle.BlockEvidence{unstarted}
	for _, pair := range e.RemovalProved {
		if pair.Applied.Implementation != HostImplementation {
			continue
		}
		original, err := DecodeHostRequest(pair.Applied.Block.Request)
		if err != nil {
			t.Fatal(err)
		}
		for i := range original.Cluster.Hosts {
			original.Cluster.Hosts[i].RetainRoot = true
		}
		raw, _ := original.canonical()
		pending := retirementRecord(retirementBlock(t, pair.Applied.Block.ID, pair.Applied.Kind, pair.Applied.Object, HostImplementation, raw), reconciliation.Apply, nil)
		pending.State = reconciliation.BlockPending
		e.OriginalRemovalDependents = append(e.OriginalRemovalDependents, pending)
	}
	e.RemovalProved = slices.DeleteFunc(e.RemovalProved, func(pair lifecycle.RemovalEvidence) bool {
		return pair.Applied.Implementation == HostImplementation
	})
	result, err := capability.Destroy(context.Background(), e)
	var evidence PhysicalRetirementEvidence
	if err != nil || json.Unmarshal(result.Evidence, &evidence) != nil || !evidence.Retired || len(evidence.Receipts) != len(policy.Hosts) {
		t.Fatalf("terminal retirement failed with positively unstarted Ceph hosts: %+v %v", evidence, err)
	}
	for _, receipt := range evidence.Receipts {
		if !receipt.HostPreparationUnstarted || receipt.Digest == "" {
			t.Fatal("receipt did not prove both unstarted preparation and disk erasure", receipt)
		}
	}
	e.OriginalRemovalDependents = e.OriginalRemovalDependents[:len(e.OriginalRemovalDependents)-1]
	if _, err := capability.Destroy(context.Background(), e); err == nil {
		t.Fatal("missing host history was accepted as positive absence")
	}
}

func TestNoEraseRetirementAppliesOnlyToItsOwnStorage(t *testing.T) {
	const cluster = "storage"
	const machine = "monitor"
	request := PhysicalRetirementRequest{Version: PhysicalRetirementImplementation, Cluster: cluster,
		Hosts: []PhysicalRetirementHost{{Name: "monitor", Machine: machine, RetainRoot: true, Devices: []Device{}, EraseTargets: []PhysicalEraseTarget{}}}}
	raw, _ := request.Canonical()
	terminal := retirementBlock(t, PhysicalRetirementBlockID(cluster), string(api.CephCluster), cluster, PhysicalRetirementImplementation, raw)
	core := Request{Action: "cluster", Cluster: cluster, Version: Version}
	raw, _ = core.Canonical()
	dependent := retirementBlock(t, ClusterBlockID(cluster), string(api.CephCluster), cluster, Implementation, raw)
	capability := NewPhysicalRetirement()
	if !capability.RetiresHost(terminal, dependent, machine) {
		t.Fatal("a retained MON with no erase targets needs a terminal cleanup proof")
	}
	firewall := retirementBlock(t, "firewall-monitor", string(api.Machine), machine, "fixture-firewall", []byte(`{}`))
	if capability.RetiresHost(terminal, firewall, machine) {
		t.Fatal("a Ceph storage proof cannot cover installed firewall removal")
	}
	if capability.RetiresHost(terminal, dependent, "another-machine") {
		t.Fatal("the terminal proof cannot cover another Machine")
	}
}
