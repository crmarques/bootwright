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
	"github.com/crmarques/bootwright/internal/substrate/baremetal"
)

func eraseMachine() api.Object {
	machine := server(api.MapValue(text("deviceName", "/dev/vda")))
	hardware := machine.Spec().Get("hardware").With("eraseOnRemoval", api.MapValue(text("method", "logicalOverwrite"), field("targets", api.ListValue(api.MapValue(text("deviceName", "/dev/vda"), text("wwn", "wwn-example"), text("serialNumber", "serial-example"), number("sizeBytes", "8192"))))))
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
	if _, err = eraseRequest(machine, request); err == nil {
		t.Fatal("erase omitted the approved root target")
	}
}

func TestPhysicalEraseOrdersAfterClaimAndBeforeInstallation(t *testing.T) {
	input := planInput(reconciliation.Apply)
	machine := eraseMachine()
	catalog := labCatalog(metalProvider(), machine, sshKeySecret(api.MapValue(field("generated", api.MapValue()))))
	input.State = compilation.NewState(catalog, catalog, nil)
	erase, err := NewErase(nil).WithClaims(baremetal.BlockID).WithMedia(labMedia()).Plan(context.Background(), input)
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
	dependencies := realizing(target.Requires)
	for index := range dependencies {
		if dependencies[index].Kind == "Machine" && dependencies[index].Object == machine.Name() {
			dependencies[index].ID = baremetal.BlockID(machine.Name())
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
