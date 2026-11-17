package physicalerase

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const (
	relaxedRoot = "/dev/disk/by-id/wwn-0x5000c500a1b2c3d4"
	relaxedData = "/dev/disk/by-id/wwn-0x5000c500a1b2c3d9"
)

func relaxableMachine(method, dataMethod string) api.Object {
	machine := server(api.MapValue(text("wwn", "0x5000c500a1b2c3d4")))
	hardware := machine.Spec().Get("hardware").With("eraseOnRemoval", api.MapValue(text("method", method), field("targets", api.ListValue(
		api.MapValue(text("deviceName", relaxedRoot), text("wwn", "0x5000c500a1b2c3d4"), text("serialNumber", "serial-example"), number("sizeBytes", "8192")),
		api.MapValue(text("deviceName", relaxedData), text("wwn", "0x5000c500a1b2c3d9"), text("serialNumber", "DATA"), number("sizeBytes", "2000398934016"), text("method", dataMethod))))))
	return api.NewObject(api.Machine, machine.Name(), api.Value{}, machine.Spec().With("hardware", hardware))
}

func relaxableErase(t *testing.T, machine api.Object) (EraseRequest, reconciliation.Block, lifecycle.BlockEvidence) {
	t.Helper()
	install := requestOf(t, labCatalog(metalProvider(), machine, sshKeySecret(api.MapValue(field("generated", api.MapValue())))), machine.Name())
	request, err := eraseRequest(machine, install)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	installed, err := install.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	erased, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	block := func(implementation, id, content string, data []byte) reconciliation.Block {
		definition := reconciliation.BlockDefinition{ID: id, Kind: installation.Kind, Object: machine.Name(), Implementation: implementation, ContentDigest: content, Request: data}
		digest, err := reconciliation.RequestDigest(definition)
		if err != nil {
			t.Fatal(err)
		}
		return reconciliation.Block{BlockDefinition: definition, RequestDigest: digest}
	}
	original := lifecycle.BlockEvidence{Kind: installation.Kind, Implementation: installation.Implementation, Object: machine.Name(), Verb: reconciliation.Apply, State: reconciliation.BlockDone,
		Block: block(installation.Implementation, installation.BlockID(machine.Name()), installation.ContentDigest(), installed)}
	return request, block(EraseImplementation, EraseBlockID(machine.Name()), EraseContentDigest(), erased), original
}

func relaxedExecution(block reconciliation.Block, relaxation string, original lifecycle.BlockEvidence) lifecycle.Execution {
	block.Relaxation = relaxation
	return lifecycle.Execution{Operation: "remove-1", ApplyOperation: "apply-1", Attempt: 1, Block: block, Material: fleetMaterial(),
		OriginalRemovalDependents: []lifecycle.BlockEvidence{original}}
}

func waivedEvidence(digest string) json.RawMessage {
	data, _ := json.Marshal(EraseEvidence{Version: EraseImplementation, Request: digest, Prepared: true, Removed: true, Retained: true})
	return data
}

func coverageReceipt(targets []EraseTarget, digest string) EraseReceipt {
	receipt := EraseReceipt{Version: EraseImplementation, State: "complete", OriginalAttempt: "remove-1/" + EraseBlockID("metal-01") + "/1", Request: digest, UUID: "example-system", MACs: []string{"52:54:00:9a:1b:01"}}
	for _, target := range targets {
		covered := EraseCoverageBytes(target)
		receipt.Targets = append(receipt.Targets, EraseCoverage{EraseTarget: target, Written: covered, Verified: covered})
	}
	return receipt
}

func receiptEvidence(receipt EraseReceipt) json.RawMessage {
	data, _ := json.Marshal(EraseEvidence{Version: EraseImplementation, Request: receipt.Request, Prepared: true, Removed: true, Receipt: &receipt})
	return data
}

func TestARelaxedEraseStatesWhatItStillDoesAndWhatItLeaves(t *testing.T) {
	_, block, _ := relaxableErase(t, relaxableMachine(EraseLogicalOverwrite, EraseMetadataClear))
	capability := New(nil)
	if !capability.RelaxesMachine(block) {
		t.Fatal("the erase block does not offer a relaxation for its Machine")
	}
	declared, err := capability.Removal(context.Background(), block)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	cleared, err := capability.Relax(context.Background(), block, lifecycle.EraseMetadataClear)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if cleared.Description != "overwrite and read back only the metadata regions of the declared physical disks of metal-01" || !slices.Equal(cleared.Impacts, []string{
		"boot-offline-erase metal-01", "relax-erase metal-01 (--authorize erase=metal-01:metadataClear)", "enable-bmc-media-verification metal-01 (BMC-wide; retained)",
		"metadata-clear " + relaxedRoot + " (relaxed from logical-overwrite)", "metadata-clear " + relaxedData,
	}) || !slices.Equal(cleared.Consumes, []string{reconciliation.AuthorizationDataLoss}) {
		t.Fatalf("the metadataClear relaxation is %+v", cleared)
	}
	retained, err := capability.Relax(context.Background(), block, lifecycle.EraseRetain)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if retained.Description != "retain the declared physical disks of metal-01 without erasing them; power and media state unproved" || !slices.Equal(retained.Impacts, []string{
		"retain-disks metal-01 (erase waived; data, installed OS and Ceph host state remain)",
		"unproved-power-and-media metal-01 (power and media state unproved; an earlier removal's erase worker may still be running)",
	}) || len(retained.Groups) != 0 {
		t.Fatalf("the retain relaxation is %+v", retained)
	}
	for _, method := range []string{lifecycle.EraseMetadataClear, lifecycle.EraseRetain} {
		again, err := capability.Relax(context.Background(), block, method)
		first := map[string]lifecycle.Removal{lifecycle.EraseMetadataClear: cleared, lifecycle.EraseRetain: retained}[method]
		if err != nil || !reflect.DeepEqual(again, first) {
			t.Fatalf("a repeated %s relaxation planned %+v, then %+v (%v)", method, first, again, err)
		}
	}
	if reflect.DeepEqual(declared, cleared) || !slices.Equal(declared.Impacts, eraseImpacts(mustDecodeErase(t, block))) {
		t.Fatalf("the declared removal is %+v", declared)
	}
}

func mustDecodeErase(t *testing.T, block reconciliation.Block) EraseRequest {
	t.Helper()
	request, err := DecodeEraseRequest(block.Request)
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func TestARelaxedEraseFitsThePlanBoundsForTheLongestMachineName(t *testing.T) {
	request, block, _ := relaxableErase(t, relaxableMachine(EraseLogicalOverwrite, EraseMetadataClear))
	request.Install.Identity.Object = strings.Repeat("m", 63)
	for _, relaxation := range []string{"", lifecycle.EraseMetadataClear, lifecycle.EraseRetain} {
		removal := eraseRemoval(request, relaxation)
		definition := block.BlockDefinition
		definition.ID, definition.Object, definition.Description, definition.Impacts, definition.Consumes, definition.Relaxation =
			"long", request.Install.Identity.Object, removal.Description, removal.Impacts, removal.Consumes, relaxation
		definition.Stage = reconciliation.StageMachines
		if _, err := reconciliation.NewPlan(reconciliation.Destroy, []reconciliation.BlockDefinition{definition}); err != nil {
			t.Fatalf("the %q removal of a 63-character Machine does not plan: %v", relaxation, diagnostics.Of(err))
		}
	}
}

func TestAnEraseRelaxationThatChangesNothingOrIsNoRelaxationRefuses(t *testing.T) {
	_, cleared, _ := relaxableErase(t, relaxableMachine(EraseMetadataClear, EraseMetadataClear))
	_, declared, _ := relaxableErase(t, relaxableMachine(EraseLogicalOverwrite, EraseMetadataClear))
	foreign := declared
	foreign.Object = "metal-02"
	renamed := declared
	renamed.ID = "another-erase"
	for name, test := range map[string]struct {
		block   reconciliation.Block
		method  string
		message string
		remedy  string
	}{
		"metadataClear over a metadata-only policy": {cleared, lifecycle.EraseMetadataClear,
			"--authorize erase=metal-01:metadataClear names Machine/metal-01, whose spec.hardware.eraseOnRemoval already clears only metadata on every target, so the relaxation changes nothing",
			"repeat bootwright destroy --context lab without --authorize erase=metal-01:metadataClear, or name erase=metal-01:retain to waive the erase"},
		"the declared method": {declared, EraseLogicalOverwrite, "names logicalOverwrite, and an erase relaxation is metadataClear or retain",
			"repeat bootwright destroy --context lab without --authorize erase=metal-01:logicalOverwrite"},
		"an unknown method": {declared, "shred", "names shred", "without --authorize erase=metal-01:shred"},
		"another Machine":   {foreign, lifecycle.EraseRetain, "names a block that is not the removal erase of Machine/metal-02", "repeat the command without --authorize erase=metal-02:retain"},
		"another block":     {renamed, lifecycle.EraseRetain, "names a block that is not the removal erase", ""},
		"a block of another kind": {func() reconciliation.Block {
			other := declared
			other.Implementation = installation.Implementation
			return other
		}(), lifecycle.EraseRetain, "is not the removal erase", ""},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := New(nil).Relax(context.Background(), test.block, test.method)
			expectRefusal(t, err, "lifecycle.authorization")
			reported := diagnostics.Of(err)[0]
			if !strings.Contains(reported.Message, test.message) || !strings.Contains(reported.Remediation, test.remedy) {
				t.Fatalf("the refusal is %q, remedy %q", reported.Message, reported.Remediation)
			}
		})
	}
	if New(nil).RelaxesMachine(foreign) || New(nil).RelaxesMachine(renamed) {
		t.Fatal("a block not named for its Machine's erase offered a relaxation")
	}
	if _, err := New(nil).Relax(context.Background(), cleared, lifecycle.EraseRetain); err != nil {
		t.Fatalf("a metadata-only policy refused to be waived: %v", diagnostics.Of(err))
	}
}

func TestAWaivedEraseWithdrawsWithoutTheControllerAndRecordsRetention(t *testing.T) {
	_, block, original := relaxableErase(t, relaxableMachine(EraseLogicalOverwrite, EraseMetadataClear))
	execution := relaxedExecution(block, lifecycle.EraseRetain, original)
	runner := &fakeRunner{result: lifecycle.RunResult{Outcome: reconciliation.OutcomeChanged, Evidence: waivedEvidence(block.RequestDigest)}}
	capability := New(runner)
	result, err := capability.Destroy(context.Background(), execution)
	if err != nil || result.Outcome != reconciliation.OutcomeChanged || !waivedRetention(result.Evidence, block.RequestDigest) {
		t.Fatalf("the waived erase returned %+v (%v)", result, diagnostics.Of(err))
	}
	if len(runner.requests) != 1 {
		t.Fatalf("the waived erase made %d runs", len(runner.requests))
	}
	sent := runner.requests[0]
	want := map[string]string{"relaxationBase64": base64.StdEncoding.EncodeToString([]byte("retain")), "originalAttempt": "remove-1/" + block.ID + "/1", "applyOperation": "apply-1"}
	if sent.Operation != "destroy" || !reflect.DeepEqual(sent.MaterialValues, want) || sent.Prepare != nil || sent.Deadline != withdrawDeadline {
		t.Fatalf("the waived erase asked the adapter %s with %v (prepare %t, deadline %s)", sent.Operation, sent.MaterialValues, sent.Prepare != nil, sent.Deadline)
	}
	for _, material := range sent.Materials {
		if slices.Contains([]string{"controllerUser", "controllerPassword", "controllerCA", "fleetIdentity", "artifactCertificate"}, material.Variable) {
			t.Fatalf("the waived erase handed the adapter %s", material.Variable)
		}
	}
	runner.result.Evidence = receiptEvidence(coverageReceipt(mustDecodeErase(t, block).Targets, block.RequestDigest))
	if result, err := capability.Destroy(context.Background(), execution); err == nil || result.Outcome != reconciliation.OutcomeUnknown {
		t.Fatalf("a waived erase accepted an erase receipt: %+v", result)
	}
	for evidence, effect := range map[string]reconciliation.EffectState{
		string(waivedEvidence(block.RequestDigest)):                                                               reconciliation.EffectCompleted,
		`{"version":"physical-erase-v1","request":"` + block.RequestDigest + `","prepared":true,"removed":false}`: reconciliation.EffectPartial,
		`{"version":"physical-erase-v1","request":"another","prepared":true,"removed":true,"retained":true}`:      reconciliation.EffectUnknown,
	} {
		runner.result.Evidence = json.RawMessage(evidence)
		observed, err := capability.ObserveRemoval(context.Background(), execution)
		if err != nil || observed.Effect != effect || runner.requests[len(runner.requests)-1].Operation != "observe" {
			t.Fatalf("observing %s reported %s (%v)", evidence, observed.Effect, err)
		}
	}
}

func TestAMetadataClearReceiptProvesOnlyTheRelaxedRemoval(t *testing.T) {
	request, block, original := relaxableErase(t, relaxableMachine(EraseLogicalOverwrite, EraseMetadataClear))
	execution := relaxedExecution(block, lifecycle.EraseMetadataClear, original)
	execution.PrepareRemoval = func(context.Context, json.RawMessage) error { return nil }
	relaxed := coverageReceipt(effectiveTargets(request, lifecycle.EraseMetadataClear), block.RequestDigest)
	declared := coverageReceipt(request.Targets, block.RequestDigest)
	runner := &fakeRunner{result: lifecycle.RunResult{Outcome: reconciliation.OutcomeChanged, Evidence: receiptEvidence(relaxed)}}
	capability := New(runner).WithIdentities(&pinReader{pinned: true, identity: machineref.HardwareIdentity{UUID: "example-system"}})
	if _, err := capability.Destroy(context.Background(), execution); err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	sent := runner.requests[0]
	if sent.MaterialValues["relaxationBase64"] != base64.StdEncoding.EncodeToString([]byte("metadataClear")) || string(sent.Canonical) != string(block.Request) {
		t.Fatalf("the relaxed erase sent %v over another request", sent.MaterialValues)
	}
	if relaxed.Targets[0].Written != EraseCoverageBytes(EraseTarget{Method: EraseMetadataClear, SizeBytes: 8192}) || relaxed.Targets[0].Method != EraseMetadataClear {
		t.Fatalf("the relaxed root coverage is %+v", relaxed.Targets[0])
	}
	runner.result.Evidence = receiptEvidence(declared)
	if result, err := capability.Destroy(context.Background(), execution); err == nil || result.Outcome == reconciliation.OutcomeChanged {
		t.Fatal("a logicalOverwrite receipt proved a removal relaxed to metadataClear")
	}
	unrelaxed := relaxedExecution(block, "", original)
	unrelaxed.PrepareRemoval = execution.PrepareRemoval
	runner.result.Evidence = receiptEvidence(relaxed)
	if _, err := capability.Destroy(context.Background(), unrelaxed); err == nil {
		t.Fatal("a metadataClear receipt proved the declared logicalOverwrite removal")
	}
	if _, set := runner.requests[len(runner.requests)-1].MaterialValues["relaxationBase64"]; set {
		t.Fatal("an unrelaxed erase sent a relaxation")
	}
	unknown := relaxedExecution(block, "shred", original)
	if _, err := capability.Destroy(context.Background(), unknown); err == nil {
		t.Fatal("an unknown frozen relaxation reached the adapter")
	}
}

func removedErase(block reconciliation.Block, relaxation string, evidence json.RawMessage) lifecycle.BlockEvidence {
	block.Relaxation = relaxation
	return lifecycle.BlockEvidence{Kind: installation.Kind, Object: block.Object, Implementation: EraseImplementation, Verb: reconciliation.Destroy, State: reconciliation.BlockDone, Block: block, Evidence: evidence}
}

func TestPhysicalEraseProofReadsAWaivedRetentionAsTheRequestsTargets(t *testing.T) {
	request, block, _ := relaxableErase(t, relaxableMachine(EraseLogicalOverwrite, EraseMetadataClear))
	receipt, recognized, err := PhysicalEraseProof(block, removedErase(block, lifecycle.EraseRetain, waivedEvidence(block.RequestDigest)), "example-system")
	if err != nil || !recognized || !slices.Equal(receipt.MACs, []string{"52:54:00:9a:1b:01"}) || len(receipt.Targets) != 2 {
		t.Fatalf("the waived retention proved %+v, %t (%v)", receipt, recognized, err)
	}
	for index, target := range receipt.Targets {
		if target.EraseTarget != request.Targets[index] || target.Written != 0 || target.Verified != 0 {
			t.Fatalf("the waived retention claimed coverage %+v", target)
		}
	}
	relaxed := coverageReceipt(effectiveTargets(request, lifecycle.EraseMetadataClear), block.RequestDigest)
	if _, recognized, err := PhysicalEraseProof(block, removedErase(block, lifecycle.EraseMetadataClear, receiptEvidence(relaxed)), "example-system"); err != nil || !recognized {
		t.Fatalf("a relaxed metadataClear receipt was refused: %v", diagnostics.Of(err))
	}
	for name, removed := range map[string]lifecycle.BlockEvidence{
		"an unrelaxed retention":                removedErase(block, "", waivedEvidence(block.RequestDigest)),
		"a waiver holding a receipt":            removedErase(block, lifecycle.EraseRetain, receiptEvidence(relaxed)),
		"a waiver of another request":           removedErase(block, lifecycle.EraseRetain, waivedEvidence(strings.Repeat("b", 64))),
		"a declared receipt under a relaxation": removedErase(block, lifecycle.EraseMetadataClear, receiptEvidence(coverageReceipt(request.Targets, block.RequestDigest))),
	} {
		if _, _, err := PhysicalEraseProof(block, removed, "example-system"); err == nil {
			t.Fatalf("%s proved a physical erase", name)
		}
	}
}

func TestAWaivedRetentionIsNeverAnUnstartedInstallation(t *testing.T) {
	install, policy := originalPhysicalErase(t)
	policy.State = reconciliation.BlockDone
	policy.Evidence, _ = json.Marshal(EraseEvidence{Version: EraseImplementation, Request: policy.Block.RequestDigest, Prepared: true})
	original := []lifecycle.BlockEvidence{install, policy}
	for relaxation, unstarted := range map[string]bool{"": true, lifecycle.EraseMetadataClear: true, lifecycle.EraseRetain: false} {
		removed := removedErase(policy.Block, relaxation, waivedEvidence(policy.Block.RequestDigest))
		ok, err := PhysicalEraseUnstarted(original, []lifecycle.RemovalEvidence{{Applied: policy, Removed: removed}}, install.Object)
		if err != nil || ok != unstarted {
			t.Fatalf("a %q retention read as unstarted %t (%v)", relaxation, ok, err)
		}
	}
}
