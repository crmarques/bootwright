package installation

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate"
	"github.com/crmarques/bootwright/internal/substrate/baremetal"
)

func eraseMachine() api.Object {
	machine := server(api.MapValue(text("wwn", "0x5000c500a1b2c3d4")))
	hardware := machine.Spec().Get("hardware").With("eraseOnRemoval", api.MapValue(text("method", "logicalOverwrite"), field("targets", api.ListValue(api.MapValue(text("deviceName", "/dev/disk/by-id/wwn-0x5000c500a1b2c3d4"), text("wwn", "0x5000c500a1b2c3d4"), text("serialNumber", "serial-example"), number("sizeBytes", "8192"))))))
	return api.NewObject(api.Machine, machine.Name(), api.Value{}, machine.Spec().With("hardware", hardware))
}

func TestPhysicalEraseFrozenRequestAndReceiptGoldens(t *testing.T) {
	machine := eraseMachine()
	install := requestOf(t, labCatalog(metalProvider(), machine, sshKeySecret(api.MapValue(field("generated", api.MapValue())))), machine.Name())
	request, err := eraseRequest(machine, install)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	canonical, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "erase-request-physical", canonical)
	if _, err := DecodeEraseRequest(canonical); err != nil {
		t.Fatal(err)
	}
	receipt := eraseFixtureReceipt(request)
	proof, _ := json.Marshal(EraseEvidence{Version: EraseImplementation, Request: receipt.Request, Prepared: true, Removed: true, Receipt: &receipt})
	matchesGolden(t, "evidence-physical-erase", proof)
	if err := eraseEvidence(proof, request, receipt.Request, true); err != nil {
		t.Fatal(err)
	}
}

func eraseFixtureReceipt(request EraseRequest) EraseReceipt {
	return EraseReceipt{Version: EraseImplementation, State: "complete", OriginalAttempt: "remove-1/" + EraseBlockID(request.Install.Identity.Object) + "/1", Request: strings.Repeat("a", 64), UUID: "example-system", MACs: []string{"52:54:00:9a:1b:01"}, Targets: []EraseCoverage{{EraseTarget: request.Targets[0], Written: request.Targets[0].SizeBytes, Verified: request.Targets[0].SizeBytes}}}
}

func TestPhysicalEraseBindsOriginalUUIDAndRemovalLineageBeforeAcknowledgement(t *testing.T) {
	machine := eraseMachine()
	install := requestOf(t, labCatalog(metalProvider(), machine, sshKeySecret(api.MapValue(field("generated", api.MapValue())))), machine.Name())
	request, err := eraseRequest(machine, install)
	if err != nil {
		t.Fatal(err)
	}
	canonical, _ := request.Canonical()
	receipt := eraseFixtureReceipt(request)
	data, _ := json.Marshal(receipt)
	execution := lifecycle.Execution{Operation: "remove-1", ApplyOperation: "apply-1", Attempt: 2, Block: reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{ID: EraseBlockID(machine.Name()), Request: canonical}, RequestDigest: receipt.Request}, Material: fleetMaterial()}
	originalInstall, _ := originalPhysicalErase(t)
	originalInstall.State = reconciliation.BlockDone
	execution.OriginalRemovalDependents = []lifecycle.BlockEvidence{originalInstall}
	uuid := base64.StdEncoding.EncodeToString([]byte(receipt.UUID))
	if err := eraseLineage(data, request, execution, uuid); err != nil {
		t.Fatal(err)
	}
	execution.RemovalPreparations = []json.RawMessage{data}
	if err := eraseLineage(data, request, execution, uuid); err != nil {
		t.Fatal(err)
	}
	for field, mutate := range map[string]func(*EraseReceipt){
		"uuid":           func(receipt *EraseReceipt) { receipt.UUID = "another-system" },
		"operation":      func(receipt *EraseReceipt) { receipt.OriginalAttempt = "other/" + execution.Block.ID + "/1" },
		"future":         func(receipt *EraseReceipt) { receipt.OriginalAttempt = "remove-1/" + execution.Block.ID + "/3" },
		"changed origin": func(receipt *EraseReceipt) { receipt.OriginalAttempt = "remove-1/" + execution.Block.ID + "/2" },
	} {
		t.Run(field, func(t *testing.T) {
			changed := receipt
			mutate(&changed)
			data, _ := json.Marshal(changed)
			if err := eraseLineage(data, request, execution, uuid); err == nil {
				t.Fatal("foreign receipt reached acknowledgement")
			}
		})
	}
	runner := &fakeRunner{}
	capability := NewErase(runner).WithIdentities(&pinReader{pinned: true, identity: machineref.HardwareIdentity{UUID: receipt.UUID}})
	if _, err := capability.Destroy(context.Background(), execution); err == nil || len(runner.requests) != 0 {
		t.Fatal("erase reached a runner without durable removal receipt authority")
	}
	execution.PrepareRemoval = func(context.Context, json.RawMessage) error { return nil }
	proof, _ := json.Marshal(EraseEvidence{Version: EraseImplementation, Request: receipt.Request, Prepared: true, Removed: true, Receipt: &receipt})
	runner.result = lifecycle.RunResult{Outcome: "changed", Evidence: proof}
	if _, err := capability.Destroy(context.Background(), execution); err != nil {
		t.Fatal(err)
	}
	sent := runner.requests[0]
	if sent.Prepare == nil || sent.Deadline != time.Duration(request.DeadlineSeconds)*time.Second+2*time.Hour || sent.Deadline > lifecycle.MaxDeadline {
		t.Fatalf("missing receipt handshake or wrong deadline: %+v", sent)
	}
	if err := sent.Prepare(context.Background(), data); err != nil {
		t.Fatal(err)
	}
}

func TestPhysicalEraseRetainsAnInstallationThatNeverStarted(t *testing.T) {
	machine := eraseMachine()
	install := requestOf(t, labCatalog(metalProvider(), machine, sshKeySecret(api.MapValue(field("generated", api.MapValue())))), machine.Name())
	request, err := eraseRequest(machine, install)
	if err != nil {
		t.Fatal(err)
	}
	canonical, _ := request.Canonical()
	originalInstall, _ := originalPhysicalErase(t)
	execution := lifecycle.Execution{Block: reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{Request: canonical}, RequestDigest: strings.Repeat("a", 64)}, OriginalRemovalDependents: []lifecycle.BlockEvidence{originalInstall}}
	runner := &fakeRunner{}
	capability := NewErase(runner)
	result, err := capability.Destroy(context.Background(), execution)
	if err != nil || result.Outcome != reconciliation.OutcomeUnchanged || len(runner.requests) != 0 {
		t.Fatalf("unused installation reached erase: %+v %v", result, err)
	}
	proof, err := reconciliation.DecodeEvidence[EraseEvidence](result.Evidence, 32768, "erase")
	if err != nil || !proof.Retained || proof.Receipt != nil {
		t.Fatal("retention claimed a verified erase receipt")
	}
	observation, err := capability.ObserveRemoval(context.Background(), execution)
	if err != nil || observation.Effect != reconciliation.EffectCompleted || len(runner.requests) != 0 {
		t.Fatalf("unused policy could not converge read-only: %+v %v", observation, err)
	}
	execution.OriginalRemovalDependents = nil
	if _, err := capability.Destroy(context.Background(), execution); err == nil || len(runner.requests) != 0 {
		t.Fatal("missing original evidence was interpreted as unused installation")
	}
}

func TestPhysicalEraseRequiresExactRootAndDistinctWholeTargets(t *testing.T) {
	machine := eraseMachine()
	request := requestOf(t, labCatalog(metalProvider(), machine, sshKeySecret(api.MapValue(field("generated", api.MapValue())))), machine.Name())
	erase, err := eraseRequest(machine, request)
	if err != nil || len(erase.Targets) != 1 {
		t.Fatalf("erase request: %+v %v", erase, err)
	}
	bad := machine.Spec().Get("hardware", "eraseOnRemoval").With("targets", api.ListValue(api.MapValue(text("deviceName", "/dev/other"), text("wwn", "wwn-example"), text("serialNumber", "serial-example"), number("sizeBytes", "8192"))))
	machine = api.NewObject(api.Machine, machine.Name(), api.Value{}, machine.Spec().With("hardware", machine.Spec().Get("hardware").With("eraseOnRemoval", bad)))
	if dataOnly, err := eraseRequest(machine, request); err != nil || len(dataOnly.Targets) != 1 {
		t.Fatalf("data-only erase failed: %v", err)
	}
}

// The installation's %pre and the erase worker prove the same root disk, so an
// erase root target naming another wwn or serial number than the Machine's
// declared root identity refuses when its request is built; the WWN compares
// without case or its 0x or naa. prefix, as the worker does.
func TestPhysicalEraseRootTargetAgreesWithTheDeclaredRootIdentity(t *testing.T) {
	const path = "/dev/disk/by-id/wwn-0x5000c500a1b2c3d4"
	for name, test := range map[string]struct {
		wwn, serial string
		refused     bool
	}{
		"the same identity":               {"0x5000c500a1b2c3d4", "ZC1A2B3C", false},
		"the same WWN in naa form":        {"naa.5000C500A1B2C3D4", "ZC1A2B3C", false},
		"another WWN":                     {"0x5000c500a1b2c3d5", "ZC1A2B3C", true},
		"another serial number":           {"0x5000c500a1b2c3d4", "ZC1A2B3D", true},
		"a serial number in another case": {"0x5000c500a1b2c3d4", "zc1a2b3c", true},
	} {
		t.Run(name, func(t *testing.T) {
			machine := server(api.MapValue(text("wwn", "0x5000c500a1b2c3d4"), text("serialNumber", "ZC1A2B3C")))
			hardware := machine.Spec().Get("hardware").With("eraseOnRemoval", api.MapValue(text("method", "logicalOverwrite"), field("targets", api.ListValue(
				api.MapValue(text("deviceName", path), text("wwn", test.wwn), text("serialNumber", test.serial), number("sizeBytes", "8192")),
				api.MapValue(text("deviceName", "/dev/disk/by-id/wwn-0x5000c500a1b2c3d9"), text("wwn", "0x5000c500a1b2c3d9"), text("serialNumber", "OTHER"), number("sizeBytes", "8192"))))))
			machine = api.NewObject(api.Machine, machine.Name(), api.Value{}, machine.Spec().With("hardware", hardware))
			install := requestOf(t, labCatalog(metalProvider(), machine, sshKeySecret(api.MapValue(field("generated", api.MapValue())))), machine.Name())
			_, err := eraseRequest(machine, install)
			if !test.refused {
				if err != nil {
					t.Fatal(diagnostics.Of(err))
				}
				return
			}
			expectRefusal(t, err, "api.invariant")
			if !strings.Contains(diagnostics.Of(err)[0].Message, "root target names another wwn, serialNumber or sizeBytes") {
				t.Fatalf("refusal = %v", diagnostics.Of(err))
			}
		})
	}
}

func TestPhysicalEraseOrdersAfterClaimAndBeforeInstallation(t *testing.T) {
	input := planInput(reconciliation.Apply)
	machine := eraseMachine()
	catalog := labCatalog(metalProvider(), machine, sshKeySecret(api.MapValue(field("generated", api.MapValue()))))
	input.State = compilation.NewState(catalog, catalog, nil)
	erase, err := NewErase(nil).WithMedia(labMedia()).Plan(context.Background(), input)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	install, err := New(nil).WithMedia(labMedia()).Plan(context.Background(), input)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	var target reconciliation.BlockDefinition
	for _, definition := range install.Definitions {
		if definition.Object == machine.Name() {
			target = definition
		}
	}
	definitions := append(erase.Definitions, target)
	dependencies := realizing(slices.DeleteFunc(slices.Clone(target.Requires), func(reference reconciliation.ObjectRef) bool { return reference.Family == EraseFamily }))
	for index := range dependencies {
		if dependencies[index].Kind == "Machine" && dependencies[index].Object == machine.Name() {
			dependencies[index].ID, dependencies[index].Family = baremetal.BlockID(machine.Name()), substrate.MachineFamily
		}
	}
	definitions = append(definitions, dependencies...)
	plan, err := reconciliation.NewPlan(reconciliation.Apply, definitions)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	ids := []string{}
	for _, block := range plan.Blocks {
		ids = append(ids, block.ID)
	}
	claim, erasure, installation := slices.Index(ids, baremetal.BlockID(machine.Name())), slices.Index(ids, EraseBlockID(machine.Name())), slices.Index(ids, BlockID(machine.Name()))
	frozen, err := DecodeEraseRequest(plan.Blocks[erasure].Request)
	if err != nil || frozen.InstallDigest != plan.Blocks[installation].RequestDigest {
		t.Fatalf("erase policy did not freeze the actual installation digest: %v", err)
	}
	if !(claim < erasure && erasure < installation) {
		t.Fatalf("unsafe apply ordering %v", ids)
	}
	inverse, err := plan.Inverse()
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	ids = nil
	for _, block := range inverse.Blocks {
		ids = append(ids, block.ID)
	}
	if !(slices.Index(ids, BlockID(machine.Name())) < slices.Index(ids, EraseBlockID(machine.Name())) && slices.Index(ids, EraseBlockID(machine.Name())) < slices.Index(ids, baremetal.BlockID(machine.Name()))) {
		t.Fatalf("unsafe destroy ordering %v", ids)
	}
	removal, err := NewErase(nil).Removal(context.Background(), plan.Blocks[erasure])
	if err != nil || !slices.Contains(removal.Consumes, reconciliation.AuthorizationDataLoss) {
		t.Fatal("erase did not require data-loss")
	}
}

type retiringPolicy struct{ family string }

func (p retiringPolicy) PhysicalRetirement(_ api.Catalog, machine string) ([]reconciliation.ObjectRef, error) {
	return []reconciliation.ObjectRef{{Kind: "CephCluster", Object: "holds-" + machine, Family: p.family}}, nil
}

// Installation and erasure wait for the retirement of every policy composition
// binds, each named by its API object and family, never by a block identity.
func TestPhysicalInstallationAndEraseWaitForEveryRetirementPolicy(t *testing.T) {
	input := planInput(reconciliation.Apply)
	machine := eraseMachine()
	catalog := labCatalog(metalProvider(), machine, sshKeySecret(api.MapValue(field("generated", api.MapValue()))))
	input.State = compilation.NewState(catalog, catalog, nil)
	policies := []RetirementPolicy{retiringPolicy{"first-retirement"}, retiringPolicy{"second-retirement"}}
	erase, err := NewErase(nil).WithMedia(labMedia()).WithRetirementPolicies(policies...).Plan(context.Background(), input)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	install, err := New(nil).WithMedia(labMedia()).WithRetirementPolicies(policies...).Plan(context.Background(), input)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	checked := 0
	for _, definition := range append(erase.Definitions, install.Definitions...) {
		if definition.Object != machine.Name() {
			continue
		}
		checked++
		for _, family := range []string{"first-retirement", "second-retirement"} {
			want := reconciliation.ObjectRef{Kind: "CephCluster", Object: "holds-" + machine.Name(), Family: family}
			if !slices.Contains(definition.Requires, want) || len(definition.Dependencies) != 0 {
				t.Fatalf("%s requires %v and depends on %v", definition.ID, definition.Requires, definition.Dependencies)
			}
		}
	}
	if checked != 2 {
		t.Fatalf("checked %d blocks of %s", checked, machine.Name())
	}
}

func TestPhysicalEraseReceiptRequiresAllReadbackAndOriginalIdentity(t *testing.T) {
	machine := eraseMachine()
	install := requestOf(t, labCatalog(metalProvider(), machine, sshKeySecret(api.MapValue(field("generated", api.MapValue())))), machine.Name())
	request, err := eraseRequest(machine, install)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	digest := strings.Repeat("a", 64)
	receipt := EraseReceipt{Version: EraseImplementation, State: "complete", OriginalAttempt: "original/1", Request: digest, UUID: "example-uuid", MACs: []string{"52:54:00:9a:1b:01"}, Targets: []EraseCoverage{{EraseTarget: request.Targets[0], Written: 8192, Verified: 8192}}}
	if err = validateEraseReceipt(&receipt, request, digest); err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	receipt.Targets[0].Verified--
	if err = validateEraseReceipt(&receipt, request, digest); err == nil {
		t.Fatal("partial readback was completion")
	}
	proof, _ := json.Marshal(EraseEvidence{Version: EraseImplementation, Request: digest, Prepared: true, Removed: true})
	if eraseEvidence(proof, request, digest, true) == nil {
		t.Fatal("removed flag replaced a receipt")
	}
}

func TestPhysicalEraseResolvesEachTargetMethodAndProvesItsExactCoverage(t *testing.T) {
	const root, data = "/dev/disk/by-id/wwn-0x5000c500a1b2c3d4", "/dev/disk/by-id/wwn-0x5000c500a1b2c3d9"
	const size = 2000398934016
	for _, test := range []struct {
		name, policy, override string
		root, data             string
	}{
		{"a target override", EraseLogicalOverwrite, EraseMetadataClear, EraseLogicalOverwrite, EraseMetadataClear},
		{"the policy default", EraseMetadataClear, "", EraseMetadataClear, EraseMetadataClear},
	} {
		t.Run(test.name, func(t *testing.T) {
			machine := server(api.MapValue(text("wwn", "0x5000c500a1b2c3d4")))
			second := api.MapValue(text("deviceName", data), text("wwn", "0x5000c500a1b2c3d9"), text("serialNumber", "DATA"), number("sizeBytes", "2000398934016"))
			if test.override != "" {
				second = second.With("method", api.StringValue(test.override))
			}
			hardware := machine.Spec().Get("hardware").With("eraseOnRemoval", api.MapValue(text("method", test.policy), field("targets", api.ListValue(
				api.MapValue(text("deviceName", root), text("wwn", "0x5000c500a1b2c3d4"), text("serialNumber", "serial-example"), number("sizeBytes", "8192")), second))))
			machine = api.NewObject(api.Machine, machine.Name(), api.Value{}, machine.Spec().With("hardware", hardware))
			install := requestOf(t, labCatalog(metalProvider(), machine, sshKeySecret(api.MapValue(field("generated", api.MapValue())))), machine.Name())
			request, err := eraseRequest(machine, install)
			if err != nil {
				t.Fatal(diagnostics.Of(err))
			}
			if request.Targets[0].DeviceName != root || request.Targets[0].Method != test.root || request.Targets[1].Method != test.data {
				t.Fatalf("resolved methods %+v", request.Targets)
			}
			canonical, _ := request.Canonical()
			removal, err := NewErase(nil).Removal(context.Background(), reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{Request: canonical}})
			verbs := map[string]string{EraseLogicalOverwrite: "logical-overwrite ", EraseMetadataClear: "metadata-clear "}
			if err != nil || !slices.Equal(removal.Impacts, []string{"boot-offline-erase " + machine.Name(), verbs[test.root] + root, verbs[test.data] + data}) {
				t.Fatalf("removal impacts %v, %v", removal.Impacts, err)
			}
			digest := strings.Repeat("a", 64)
			receipt := EraseReceipt{Version: EraseImplementation, State: "complete", OriginalAttempt: "original/1", Request: digest, UUID: "example-uuid", MACs: []string{"52:54:00:9a:1b:01"}}
			for _, target := range request.Targets {
				covered := EraseCoverageBytes(target)
				receipt.Targets = append(receipt.Targets, EraseCoverage{EraseTarget: target, Written: covered, Verified: covered})
			}
			if receipt.Targets[1].Written != 805306368 {
				t.Fatalf("metadataClear coverage of a %d-byte disk is %d", size, receipt.Targets[1].Written)
			}
			if err := validateEraseReceipt(&receipt, request, digest); err != nil {
				t.Fatal(diagnostics.Of(err))
			}
			for name, mutate := range map[string]func(*EraseCoverage){
				"whole-disk coverage for a metadata clear": func(c *EraseCoverage) { c.Written, c.Verified = size, size },
				"a short readback":                         func(c *EraseCoverage) { c.Verified-- },
				"another method":                           func(c *EraseCoverage) { c.Method = EraseLogicalOverwrite },
			} {
				changed := receipt
				changed.Targets = slices.Clone(receipt.Targets)
				mutate(&changed.Targets[1])
				if validateEraseReceipt(&changed, request, digest) == nil {
					t.Fatalf("%s proved completion", name)
				}
			}
		})
	}
}

func TestPhysicalEraseRequestBoundsALogicalOverwriteTargetTo896GiB(t *testing.T) {
	const root, data = "/dev/disk/by-id/wwn-0x5000c500a1b2c3d4", "/dev/disk/by-id/wwn-0x5000c500a1b2c3d9"
	for _, test := range []struct {
		method, size string
		refused      bool
	}{
		{EraseLogicalOverwrite, "962072674304", false},
		{EraseLogicalOverwrite, "962072678400", true},
		{EraseMetadataClear, "962072678400", false},
	} {
		machine := server(api.MapValue(text("wwn", "0x5000c500a1b2c3d4")))
		hardware := machine.Spec().Get("hardware").With("eraseOnRemoval", api.MapValue(text("method", EraseLogicalOverwrite), field("targets", api.ListValue(
			api.MapValue(text("deviceName", root), text("wwn", "0x5000c500a1b2c3d4"), text("serialNumber", "serial-example"), number("sizeBytes", "8192")),
			api.MapValue(text("deviceName", data), text("wwn", "0x5000c500a1b2c3d9"), text("serialNumber", "DATA"), number("sizeBytes", test.size), text("method", test.method))))))
		machine = api.NewObject(api.Machine, machine.Name(), api.Value{}, machine.Spec().With("hardware", hardware))
		install := requestOf(t, labCatalog(metalProvider(), machine, sshKeySecret(api.MapValue(field("generated", api.MapValue())))), machine.Name())
		_, err := eraseRequest(machine, install)
		if !test.refused {
			if err != nil {
				t.Fatal(diagnostics.Of(err))
			}
			continue
		}
		expectRefusal(t, err, "api.value")
		if !strings.Contains(diagnostics.Of(err)[0].Remediation, "metadataClear") {
			t.Fatalf("refusal = %v", diagnostics.Of(err))
		}
	}
}

// A root disk declared by serial number and size is erased through the target
// without deviceName that declares that serial number and size; such a target
// freezes before those named by path, plans as serial:<serialNumber>, and a
// root target of another size, a lone deviceName or wwn, or a root target
// named by path refuses when the request is built.
func TestPhysicalEraseIncludesARootDeclaredBySerialNumberAndSize(t *testing.T) {
	root := func(size string) api.Value {
		return api.MapValue(text("serialNumber", "ZC1A2B3C"), number("sizeBytes", size))
	}
	data := api.MapValue(text("deviceName", "/dev/disk/by-id/wwn-0x5000c500a1b2c3d9"), text("wwn", "0x5000c500a1b2c3d9"), text("serialNumber", "OTHER"), number("sizeBytes", "8192"))
	build := func(targets ...api.Value) (EraseRequest, error) {
		machine := server(api.MapValue(text("serialNumber", "ZC1A2B3C"), number("sizeBytes", "480103981056")))
		hardware := machine.Spec().Get("hardware").With("eraseOnRemoval", api.MapValue(text("method", "metadataClear"), field("targets", api.ListValue(targets...))))
		machine = api.NewObject(api.Machine, machine.Name(), api.Value{}, machine.Spec().With("hardware", hardware))
		install := requestOf(t, labCatalog(metalProvider(), machine, sshKeySecret(api.MapValue(field("generated", api.MapValue())))), machine.Name())
		return eraseRequest(machine, install)
	}
	request, err := build(data, root("480103981056"))
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if len(request.Targets) != 2 || request.Targets[0] != (EraseTarget{Method: EraseMetadataClear, SerialNumber: "ZC1A2B3C", SizeBytes: 480103981056}) || request.Targets[1].DeviceName == "" {
		t.Fatalf("targets = %+v", request.Targets)
	}
	if impacts := eraseImpacts(request); !slices.Equal(impacts, []string{"boot-offline-erase metal-01", "metadata-clear serial:ZC1A2B3C", "metadata-clear /dev/disk/by-id/wwn-0x5000c500a1b2c3d9"}) {
		t.Fatalf("impacts = %v", impacts)
	}
	canonical, err := request.Canonical()
	if err != nil || strings.Contains(string(canonical), `"deviceName":""`) || strings.Contains(string(canonical), `"wwn":""`) {
		t.Fatalf("a serial target froze an empty path or WWN: %v %s", err, canonical)
	}
	if got, err := build(data); err != nil || len(got.Targets) != 1 {
		t.Fatalf("data-only erase failed: %v", err)
	}
	for name, test := range map[string]struct {
		targets []api.Value
		message string
	}{
		"another root size": {[]api.Value{data, root("480103985152")}, "root target names another wwn, serialNumber or sizeBytes"},
		"the root serial by path": {[]api.Value{api.MapValue(text("deviceName", "/dev/disk/by-id/wwn-0x5000c500a1b2c3d4"), text("wwn", "0x5000c500a1b2c3d4"),
			text("serialNumber", "ZC1A2B3C"), number("sizeBytes", "480103981056"))}, "uses the original serial-only selector"},
		"a lone deviceName": {[]api.Value{root("480103981056"), data.Without("wwn")}, "by deviceName with its wwn, or by serialNumber and sizeBytes alone"},
		"a lone wwn":        {[]api.Value{root("480103981056"), data.Without("deviceName")}, "by deviceName with its wwn, or by serialNumber and sizeBytes alone"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := build(test.targets...)
			if err == nil || !strings.Contains(diagnostics.Of(err)[0].Message, test.message) {
				t.Fatalf("refusal = %v", diagnostics.Of(err))
			}
		})
	}
}

// A root disk declared by its wwn is the erase target naming the by-id path
// that wwn derives or, under any other native path, that wwn; either one must
// carry the root's declared size.
func TestPhysicalEraseMatchesAWWNRootByItsDerivedPathOrItsWWN(t *testing.T) {
	for name, test := range map[string]struct {
		device, wwn, size string
		refused           bool
	}{
		"the derived path":          {"/dev/disk/by-id/wwn-0x5000c500a1b2c3d4", "0x5000c500a1b2c3d4", "8192", false},
		"the derived path, resized": {"/dev/disk/by-id/wwn-0x5000c500a1b2c3d4", "0x5000c500a1b2c3d4", "12288", true},
		"another path, its wwn":     {"/dev/sda", "naa.5000C500A1B2C3D4", "12288", true},
		"another path and wwn":      {"/dev/sda", "0x5000c500a1b2c3d9", "12288", false},
	} {
		t.Run(name, func(t *testing.T) {
			machine := server(api.MapValue(text("wwn", "0x5000c500a1b2c3d4"), number("sizeBytes", "8192")))
			hardware := machine.Spec().Get("hardware").With("eraseOnRemoval", api.MapValue(text("method", "logicalOverwrite"), field("targets", api.ListValue(
				api.MapValue(text("deviceName", test.device), text("wwn", test.wwn), text("serialNumber", "ZC1A2B3C"), number("sizeBytes", test.size))))))
			machine = api.NewObject(api.Machine, machine.Name(), api.Value{}, machine.Spec().With("hardware", hardware))
			if !machineref.RootEraseSelected(machine) == (test.wwn != "0x5000c500a1b2c3d9") {
				t.Fatalf("root erase selected = %v", machineref.RootEraseSelected(machine))
			}
			install := requestOf(t, labCatalog(metalProvider(), machine, sshKeySecret(api.MapValue(field("generated", api.MapValue())))), machine.Name())
			if _, err := eraseRequest(machine, install); (err != nil) != test.refused {
				t.Fatalf("erase request error = %v, refused %v", diagnostics.Of(err), test.refused)
			}
		})
	}
}

// A root disk declared by its wwn and size is erased only through its
// target of that size.
func TestPhysicalEraseRootTargetAgreesWithTheDeclaredRootSize(t *testing.T) {
	const path = "/dev/disk/by-id/wwn-0x5000c500a1b2c3d4"
	for size, refused := range map[string]bool{"8192": false, "12288": true} {
		machine := server(api.MapValue(text("wwn", "0x5000c500a1b2c3d4"), number("sizeBytes", "8192")))
		hardware := machine.Spec().Get("hardware").With("eraseOnRemoval", api.MapValue(text("method", "logicalOverwrite"), field("targets", api.ListValue(
			api.MapValue(text("deviceName", path), text("wwn", "0x5000c500a1b2c3d4"), text("serialNumber", "ZC1A2B3C"), number("sizeBytes", size))))))
		machine = api.NewObject(api.Machine, machine.Name(), api.Value{}, machine.Spec().With("hardware", hardware))
		install := requestOf(t, labCatalog(metalProvider(), machine, sshKeySecret(api.MapValue(field("generated", api.MapValue())))), machine.Name())
		if _, err := eraseRequest(machine, install); (err != nil) != refused {
			t.Fatalf("a %s-byte root target: %v", size, diagnostics.Of(err))
		}
	}
}
