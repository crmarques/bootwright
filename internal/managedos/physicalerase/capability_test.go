package physicalerase

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate"
	"github.com/crmarques/bootwright/internal/substrate/baremetal"
)

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
	matchesGolden(t, "erase-request-physical", canonical, false)
	if _, err := DecodeEraseRequest(canonical); err != nil {
		t.Fatal(err)
	}
	receipt := eraseFixtureReceipt(request)
	proof, _ := json.Marshal(EraseEvidence{Version: EraseImplementation, Request: receipt.Request, Prepared: true, Removed: true, Receipt: &receipt})
	matchesGolden(t, "evidence-physical-erase", proof, false)
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
	capability := New(runner).WithIdentities(&pinReader{pinned: true, identity: machineref.HardwareIdentity{UUID: receipt.UUID}})
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
	capability := New(runner)
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
	machine = erasing(server(api.MapValue(text("wwn", "0x5000c500a1b2c3d4"), text("serialNumber", "serial-example"), number("sizeBytes", "8192"))), "", dataDisk("OTHER", "8192", "0x5000c500a1b2c3d9", EraseLogicalOverwrite))
	if dataOnly, err := eraseRequest(machine, request); err != nil || len(dataOnly.Targets) != 1 || dataOnly.Targets[0].SerialNumber != "OTHER" {
		t.Fatalf("data-only erase failed: %v", err)
	}
}

// The installation's %pre and the erase worker prove the same root disk, so a
// frozen erase root target naming another wwn, serial number or size than the
// installation's declared root identity refuses; the WWN compares without case
// or its 0x or naa. prefix, as the worker does.
func TestPhysicalEraseRootTargetAgreesWithTheDeclaredRootIdentity(t *testing.T) {
	const path = "/dev/disk/by-id/wwn-0x5000c500a1b2c3d4"
	machine := erasing(server(api.MapValue(text("wwn", "0x5000c500a1b2c3d4"), text("serialNumber", "ZC1A2B3C"), number("sizeBytes", "8192"))), EraseLogicalOverwrite)
	install := requestOf(t, labCatalog(metalProvider(), machine, sshKeySecret(api.MapValue(field("generated", api.MapValue())))), machine.Name())
	request, err := eraseRequest(machine, install)
	if err != nil || len(request.Targets) != 1 || request.Targets[0] != (EraseTarget{DeviceName: path, Method: EraseLogicalOverwrite, WWN: "0x5000c500a1b2c3d4", SerialNumber: "ZC1A2B3C", SizeBytes: 8192}) {
		t.Fatalf("the derived root target = %+v, %v", request.Targets, diagnostics.Of(err))
	}
	other := EraseTarget{DeviceName: "/dev/disk/by-id/wwn-0x5000c500a1b2c3d9", Method: EraseLogicalOverwrite, WWN: "0x5000c500a1b2c3d9", SerialNumber: "OTHER", SizeBytes: 8192}
	for name, test := range map[string]struct {
		wwn, serial string
		size        int64
		refused     bool
	}{
		"the same identity":               {"0x5000c500a1b2c3d4", "ZC1A2B3C", 8192, false},
		"the same WWN in naa form":        {"naa.5000C500A1B2C3D4", "ZC1A2B3C", 8192, false},
		"another WWN":                     {"0x5000c500a1b2c3d5", "ZC1A2B3C", 8192, true},
		"another serial number":           {"0x5000c500a1b2c3d4", "ZC1A2B3D", 8192, true},
		"a serial number in another case": {"0x5000c500a1b2c3d4", "zc1a2b3c", 8192, true},
		"another size":                    {"0x5000c500a1b2c3d4", "ZC1A2B3C", 12288, true},
	} {
		t.Run(name, func(t *testing.T) {
			targets := []EraseTarget{{DeviceName: path, Method: EraseLogicalOverwrite, WWN: test.wwn, SerialNumber: test.serial, SizeBytes: test.size}, other}
			err := eraseRootRules(targets, install.Target.Hardware.RootDevice, install.Target.Hardware, "")
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
	erase, err := New(nil).WithMedia(labMedia()).Plan(context.Background(), input)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	install, err := installation.New(nil).WithMedia(labMedia()).Plan(context.Background(), input)
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
	dependencies := realizing(slices.DeleteFunc(slices.Clone(target.Requires), func(reference reconciliation.ObjectRef) bool { return reference.Family == installation.EraseFamily }))
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
	claim, erasure, installed := slices.Index(ids, baremetal.BlockID(machine.Name())), slices.Index(ids, EraseBlockID(machine.Name())), slices.Index(ids, installation.BlockID(machine.Name()))
	frozen, err := DecodeEraseRequest(plan.Blocks[erasure].Request)
	if err != nil || frozen.InstallDigest != plan.Blocks[installed].RequestDigest {
		t.Fatalf("erase policy did not freeze the actual installation digest: %v", err)
	}
	if !(claim < erasure && erasure < installed) {
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
	if !(slices.Index(ids, installation.BlockID(machine.Name())) < slices.Index(ids, EraseBlockID(machine.Name())) && slices.Index(ids, EraseBlockID(machine.Name())) < slices.Index(ids, baremetal.BlockID(machine.Name()))) {
		t.Fatalf("unsafe destroy ordering %v", ids)
	}
	removal, err := New(nil).Removal(context.Background(), plan.Blocks[erasure])
	if err != nil || !slices.Contains(removal.Consumes, reconciliation.AuthorizationDataLoss) {
		t.Fatal("erase did not require data-loss")
	}
	if len(removal.Groups) != len(frozen.Targets) || removal.Groups[0].ID != "erase-1" || removal.Groups[0].Description != "zero and read back "+eraseTargetName(frozen.Targets[0]) {
		t.Fatalf("erase removal groups %+v, want one per frozen target", removal.Groups)
	}
	definition := plan.Blocks[erasure].BlockDefinition
	definition.Description, definition.Impacts, definition.Groups, definition.Consumes = removal.Description, removal.Impacts, removal.Groups, removal.Consumes
	definition.Requires, definition.Dependencies, definition.Prerequisites = nil, nil, nil
	if _, err := reconciliation.NewPlan(reconciliation.Destroy, []reconciliation.BlockDefinition{definition}); err != nil {
		t.Fatalf("a removal plan of the declared groups is refused: %v", diagnostics.Of(err))
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
	policies := []installation.RetirementPolicy{retiringPolicy{"first-retirement"}, retiringPolicy{"second-retirement"}}
	erase, err := New(nil).WithMedia(labMedia()).WithRetirementPolicies(policies...).Plan(context.Background(), input)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	install, err := installation.New(nil).WithMedia(labMedia()).WithRetirementPolicies(policies...).Plan(context.Background(), input)
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
		name       string
		root, data string
	}{
		{"distinct methods", EraseLogicalOverwrite, EraseMetadataClear},
		{"one method", EraseMetadataClear, EraseMetadataClear},
	} {
		t.Run(test.name, func(t *testing.T) {
			machine := erasing(server(api.MapValue(text("wwn", "0x5000c500a1b2c3d4"), text("serialNumber", "serial-example"), number("sizeBytes", "8192"))), test.root,
				dataDisk("DATA", "2000398934016", "0x5000c500a1b2c3d9", test.data))
			install := requestOf(t, labCatalog(metalProvider(), machine, sshKeySecret(api.MapValue(field("generated", api.MapValue())))), machine.Name())
			request, err := eraseRequest(machine, install)
			if err != nil {
				t.Fatal(diagnostics.Of(err))
			}
			if request.Targets[0].DeviceName != root || request.Targets[0].Method != test.root || request.Targets[1].Method != test.data {
				t.Fatalf("resolved methods %+v", request.Targets)
			}
			canonical, _ := request.Canonical()
			removal, err := New(nil).Removal(context.Background(), reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{Request: canonical}})
			verbs := map[string]string{EraseLogicalOverwrite: "logical-overwrite ", EraseMetadataClear: "metadata-clear "}
			if err != nil || !slices.Equal(removal.Impacts, []string{"boot-offline-erase " + machine.Name(), "enable-bmc-media-verification " + machine.Name() + " (BMC-wide; retained)", verbs[test.root] + root, verbs[test.data] + data}) {
				t.Fatalf("removal impacts %v, %v", removal.Impacts, err)
			}
			labels := map[string]string{EraseLogicalOverwrite: "zero and read back ", EraseMetadataClear: "clear the metadata of "}
			groups := []reconciliation.Group{{ID: "erase-1", Description: labels[test.root] + root, Machines: []string{machine.Name()}},
				{ID: "erase-2", Description: labels[test.data] + data, Machines: []string{machine.Name()}}}
			if !reflect.DeepEqual(removal.Groups, groups) {
				t.Fatalf("removal groups %+v, want %+v", removal.Groups, groups)
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
	for _, test := range []struct {
		method, size string
		refused      bool
	}{
		{EraseLogicalOverwrite, "962072674304", false},
		{EraseLogicalOverwrite, "962072678400", true},
		{EraseMetadataClear, "962072678400", false},
	} {
		machine := erasing(server(api.MapValue(text("wwn", "0x5000c500a1b2c3d4"), text("serialNumber", "serial-example"), number("sizeBytes", "8192"))), EraseLogicalOverwrite,
			dataDisk("DATA", test.size, "0x5000c500a1b2c3d9", test.method))
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

// A root disk declared by serial number and size is erased through a target
// without deviceName that carries that serial number and size; such a target
// freezes before those named by path and plans as serial:<serialNumber>. A data
// disk repeating the root's serial number by path, or naming a wwn that derives
// no path, refuses when the request is built.
func TestPhysicalEraseIncludesARootDeclaredBySerialNumberAndSize(t *testing.T) {
	data := dataDisk("OTHER", "8192", "0x5000c500a1b2c3d9", EraseMetadataClear)
	build := func(root string, data ...api.Value) (EraseRequest, error) {
		machine := erasing(server(api.MapValue(text("serialNumber", "ZC1A2B3C"), number("sizeBytes", "480103981056"))), root, data...)
		install := requestOf(t, labCatalog(metalProvider(), machine, sshKeySecret(api.MapValue(field("generated", api.MapValue())))), machine.Name())
		return eraseRequest(machine, install)
	}
	request, err := build(EraseMetadataClear, data)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if len(request.Targets) != 2 || request.Targets[0] != (EraseTarget{Method: EraseMetadataClear, SerialNumber: "ZC1A2B3C", SizeBytes: 480103981056}) || request.Targets[1].DeviceName == "" {
		t.Fatalf("targets = %+v", request.Targets)
	}
	if impacts := eraseImpacts(request); !slices.Equal(impacts, []string{"boot-offline-erase metal-01", "enable-bmc-media-verification metal-01 (BMC-wide; retained)", "metadata-clear serial:ZC1A2B3C", "metadata-clear /dev/disk/by-id/wwn-0x5000c500a1b2c3d9"}) {
		t.Fatalf("impacts = %v", impacts)
	}
	canonical, err := request.Canonical()
	if err != nil || strings.Contains(string(canonical), `"deviceName":""`) || strings.Contains(string(canonical), `"wwn":""`) {
		t.Fatalf("a serial target froze an empty path or WWN: %v %s", err, canonical)
	}
	if got, err := build("", data); err != nil || len(got.Targets) != 1 {
		t.Fatalf("data-only erase failed: %v", err)
	}
	for name, test := range map[string]struct {
		root    string
		data    api.Value
		message string
	}{
		"the root serial by path": {"", dataDisk("ZC1A2B3C", "480103981056", "0x5000c500a1b2c3d4", EraseMetadataClear), "uses the original serial-only selector"},
		"a wwn deriving no path":  {EraseMetadataClear, dataDisk("OTHER", "8192", "naa.5000c500a1b2c3d9", EraseMetadataClear), "by the path its wwn derives, or by serialNumber and sizeBytes alone"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := build(test.root, test.data)
			if err == nil || !strings.Contains(diagnostics.Of(err)[0].Message, test.message) {
				t.Fatalf("refusal = %v", diagnostics.Of(err))
			}
		})
	}
}

// A root disk declared by its wwn is the erase target naming the by-id path
// that wwn derives; a data disk naming the root's wwn is that root and must
// carry its declared serial number and size.
func TestPhysicalEraseMatchesAWWNRootByItsDerivedPathOrItsWWN(t *testing.T) {
	root := api.MapValue(text("wwn", "0x5000c500a1b2c3d4"), text("serialNumber", "ZC1A2B3C"), number("sizeBytes", "8192"))
	for name, test := range map[string]struct {
		root    string
		data    []api.Value
		refused bool
	}{
		"the derived path":           {EraseLogicalOverwrite, nil, false},
		"a data disk of its wwn":     {"", []api.Value{dataDisk("OTHER", "12288", "0x5000c500a1b2c3d4", EraseLogicalOverwrite)}, true},
		"a data disk of another wwn": {"", []api.Value{dataDisk("OTHER", "12288", "0x5000c500a1b2c3d9", EraseLogicalOverwrite)}, false},
	} {
		t.Run(name, func(t *testing.T) {
			machine := erasing(server(root), test.root, test.data...)
			if machineref.RootEraseSelected(machine) != (test.root != "") {
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
	machine := erasing(server(api.MapValue(text("wwn", "0x5000c500a1b2c3d4"), text("serialNumber", "ZC1A2B3C"), number("sizeBytes", "8192"))), EraseLogicalOverwrite)
	install := requestOf(t, labCatalog(metalProvider(), machine, sshKeySecret(api.MapValue(field("generated", api.MapValue())))), machine.Name())
	for size, refused := range map[int64]bool{8192: false, 12288: true} {
		target := EraseTarget{DeviceName: path, Method: EraseLogicalOverwrite, WWN: "0x5000c500a1b2c3d4", SerialNumber: "ZC1A2B3C", SizeBytes: size}
		if err := eraseRootRules([]EraseTarget{target}, install.Target.Hardware.RootDevice, install.Target.Hardware, ""); (err != nil) != refused {
			t.Fatalf("a %d-byte root target: %v", size, diagnostics.Of(err))
		}
	}
}

func TestPhysicalEraseFreshRemovalBootsUnderItsOwnLineageAndRefusesASupersededReceipt(t *testing.T) {
	request, execution := eraseDestroyExecution(t)
	runner := &fakeRunner{}
	capability := New(runner).WithIdentities(&pinReader{pinned: true, identity: machineref.HardwareIdentity{UUID: "example-system"}})
	superseded := eraseFixtureReceipt(request)
	superseded.Request = execution.Block.RequestDigest
	superseded.OriginalAttempt = "remove-1/" + execution.Block.ID + "/1"
	proof, _ := json.Marshal(EraseEvidence{Version: EraseImplementation, Request: execution.Block.RequestDigest, Prepared: true, Removed: true, Receipt: &superseded})
	runner.result = lifecycle.RunResult{Outcome: "changed", Evidence: proof}
	_, err := capability.Destroy(context.Background(), execution)
	if found := diagnostics.Of(err); len(found) != 1 || !strings.Contains(found[0].Message, "another removal attempt lineage") ||
		!strings.HasPrefix(found[0].Remediation, "run bootwright destroy --context lab again") || strings.Contains(found[0].Remediation, "continue the original") {
		t.Fatalf("a superseded removal's receipt = %v, want the lineage refusal naming the destroy to run", found)
	}
	if got, want := runner.requests[0].MaterialValues["originalAttempt"], "remove-2/"+execution.Block.ID+"/1"; got != want {
		t.Fatalf("the fresh removal's original attempt = %q, want %q", got, want)
	}
}
