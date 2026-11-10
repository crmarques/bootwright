package installation

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const serialRootSize = "480103981056"

func serialRootMachine(targets ...api.Value) api.Object {
	machine := server(api.MapValue(text("serialNumber", "ZC1A2B3C"), number("sizeBytes", serialRootSize)))
	hardware := machine.Spec().Get("hardware").With("eraseOnRemoval", api.MapValue(text("method", "metadataClear"), field("targets", api.ListValue(targets...))))
	return api.NewObject(api.Machine, machine.Name(), api.Value{}, machine.Spec().With("hardware", hardware))
}

type rootErase struct {
	request   EraseRequest
	execution lifecycle.Execution
}

func installedRootErase(t *testing.T, machine api.Object, state reconciliation.BlockState, evidence func(Request, string) Evidence) rootErase {
	t.Helper()
	install := requestOf(t, labCatalog(metalProvider(), machine, sshKeySecret(api.MapValue(field("generated", api.MapValue())))), machine.Name())
	request, err := eraseRequest(machine, install)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	canonical, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	installed, err := install.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	definition := reconciliation.BlockDefinition{ID: BlockID(machine.Name()), Kind: Kind, Object: machine.Name(), Implementation: Implementation, ContentDigest: ContentDigest(), Request: installed}
	digest, err := reconciliation.RequestDigest(definition)
	if err != nil || digest != request.InstallDigest {
		t.Fatalf("installation digest %s, erase names %s: %v", digest, request.InstallDigest, err)
	}
	original := lifecycle.BlockEvidence{Kind: Kind, Implementation: Implementation, Object: machine.Name(), Verb: reconciliation.Apply, State: state,
		Block: reconciliation.Block{BlockDefinition: definition, RequestDigest: digest}}
	if evidence != nil {
		original.Evidence, _ = json.Marshal(evidence(install, digest))
	}
	execution := lifecycle.Execution{Operation: "remove-1", ApplyOperation: "apply-1", Attempt: 1, Material: fleetMaterial(),
		Block:                     reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{ID: EraseBlockID(machine.Name()), Request: canonical}, RequestDigest: strings.Repeat("a", 64)},
		OriginalRemovalDependents: []lifecycle.BlockEvidence{original},
		PrepareRemoval:            func(context.Context, json.RawMessage) error { return nil }}
	return rootErase{request: request, execution: execution}
}

func completedWith(wwn string) func(Request, string) Evidence {
	return func(install Request, digest string) Evidence {
		return Evidence{Address: install.Address, HostKey: "ssh-ed25519 AAAAHOST", Postcondition: true, Power: "On", Reachable: true, Request: digest,
			Network: true, NetworkReboot: true, NetworkDifferences: []string{}, Update: UpdateEvidence{State: updateNone}, RootWWN: wwn}
	}
}

func eraseRun(t *testing.T, erase rootErase) (*fakeRunner, error) {
	t.Helper()
	runner := &fakeRunner{err: refusal("lifecycle.state", "the fake runner stops here", "")}
	capability := NewErase(runner).WithIdentities(&pinReader{pinned: true, identity: machineref.HardwareIdentity{UUID: "example-system"}})
	_, err := capability.Destroy(context.Background(), erase.execution)
	return runner, err
}

// A root declared by serial number has the WWN it reported at installation
// recorded in that installation's evidence, and its erase compares it: a
// target naming that WWN, by its wwn under any path or by the by-id path an
// NAA identifier derives, is the root, which a serial-selected root's target
// names by its serial number alone, so it refuses before the worker runs. The
// worker is handed that WWN to compare with every disk a target selects. A
// root that reported no WWN keeps the serial number and size comparison, and
// an installation record that proves no completion records none.
func TestPhysicalEraseComparesTheWWNASerialRootReportedAtInstallation(t *testing.T) {
	data := func(device, wwn string) api.Value {
		return api.MapValue(text("deviceName", device), text("wwn", wwn), text("serialNumber", "DATA-01"), number("sizeBytes", "8192"))
	}
	root := api.MapValue(text("serialNumber", "ZC1A2B3C"), number("sizeBytes", serialRootSize))
	const recorded = "naa.5000c500a1b2c3d4"
	for name, test := range map[string]struct {
		target   api.Value
		state    reconciliation.BlockState
		reported string
		refused  bool
		handed   string
	}{
		"the recorded WWN by wwn":          {data("/dev/sdb", "0x5000C500A1B2C3D4"), reconciliation.BlockDone, recorded, true, ""},
		"the recorded WWN in its own form": {data("/dev/sdb", recorded), reconciliation.BlockDone, recorded, true, ""},
		"the by-id path it derives":        {data("/dev/disk/by-id/wwn-0x5000c500a1b2c3d4", "0x5000c500a1b2c3d9"), reconciliation.BlockDone, recorded, true, ""},
		"an NVMe identifier by wwn":        {data("/dev/nvme1n1", "eui.0025388b91c0a2f1"), reconciliation.BlockDone, "eui.0025388b91c0a2f1", true, ""},
		"another disk":                     {data("/dev/disk/by-id/wwn-0x5000c500a1b2c3d9", "0x5000c500a1b2c3d9"), reconciliation.BlockDone, recorded, false, recorded},
		"the root by its serial number":    {root, reconciliation.BlockDone, recorded, false, recorded},
		"a root that reported no WWN":      {data("/dev/sdb", recorded), reconciliation.BlockDone, "", false, ""},
		"an installation never completed":  {data("/dev/sdb", recorded), reconciliation.BlockFailed, recorded, false, ""},
	} {
		t.Run(name, func(t *testing.T) {
			erase := installedRootErase(t, serialRootMachine(test.target), test.state, completedWith(test.reported))
			runner, err := eraseRun(t, erase)
			if test.refused {
				if err == nil || len(runner.requests) != 0 || !strings.Contains(diagnostics.Of(err)[0].Message, "uses the original serial-only selector") {
					t.Fatalf("a target naming the root's recorded WWN reached the worker: %v", diagnostics.Of(err))
				}
				return
			}
			if len(runner.requests) != 1 {
				t.Fatalf("the erase did not reach the worker: %v", diagnostics.Of(err))
			}
			if handed := runner.requests[0].MaterialValues["rootWWNBase64"]; handed != base64.StdEncoding.EncodeToString([]byte(test.handed)) {
				t.Fatalf("the worker compares the root WWN %q, want %q", handed, test.handed)
			}
		})
	}
	erase := installedRootErase(t, serialRootMachine(root), reconciliation.BlockDone, func(install Request, digest string) Evidence {
		evidence := completedWith(recorded)(install, digest)
		evidence.Postcondition = false
		return evidence
	})
	if runner, err := eraseRun(t, erase); err == nil || len(runner.requests) != 0 {
		t.Fatal("an installation record proving no completion named a root WWN")
	}
}

// A root declared by its wwn hands the worker that wwn, so the worker
// recognises the root in any target that selects it; a target without
// deviceName naming the root's declared serial number is that root, whose
// size must be the root's.
func TestPhysicalEraseHandsTheWorkerTheDeclaredRootWWN(t *testing.T) {
	declared := func(size string) api.Object {
		machine := server(api.MapValue(text("wwn", "0x5000c500a1b2c3d4"), text("serialNumber", "ZC1A2B3C"), number("sizeBytes", "8192")))
		hardware := machine.Spec().Get("hardware").With("eraseOnRemoval", api.MapValue(text("method", "metadataClear"), field("targets", api.ListValue(
			api.MapValue(text("serialNumber", "DATA-01"), number("sizeBytes", "8192")), api.MapValue(text("serialNumber", "ZC1A2B3C"), number("sizeBytes", size))))))
		return api.NewObject(api.Machine, machine.Name(), api.Value{}, machine.Spec().With("hardware", hardware))
	}
	if !machineref.RootEraseSelected(declared("8192")) {
		t.Fatal("a target naming the root's serial number and size does not select the root")
	}
	runner, _ := eraseRun(t, installedRootErase(t, declared("8192"), reconciliation.BlockDone, nil))
	if len(runner.requests) != 1 || runner.requests[0].MaterialValues["rootWWNBase64"] != base64.StdEncoding.EncodeToString([]byte("0x5000c500a1b2c3d4")) {
		t.Fatalf("the worker was not handed the declared root WWN: %+v", runner.requests)
	}
	machine := declared("12288")
	install := requestOf(t, labCatalog(metalProvider(), machine, sshKeySecret(api.MapValue(field("generated", api.MapValue())))), machine.Name())
	if _, err := eraseRequest(machine, install); err == nil || !strings.Contains(diagnostics.Of(err)[0].Message, "root target names another wwn, serialNumber or sizeBytes") {
		t.Fatalf("a root target of another size was not refused: %v", diagnostics.Of(err))
	}
}

// A completed installation's evidence records the WWN a root declared by
// serial number reported, or none, as bounded printable text; a root declared
// any other way records none.
func TestInstallationEvidenceRecordsTheWWNOfASerialDeclaredRoot(t *testing.T) {
	digest := strings.Repeat("1", 64)
	present := func(install Request, wwn string) error {
		marker, err := MarkerFor(install, digest)
		if err != nil {
			t.Fatal(err)
		}
		evidence := completedWith(wwn)(install, digest)
		evidence.Marker, evidence.Image, evidence.Tree = string(marker), install.Private == nil, install.Tree != nil
		if install.TreeMedia != nil {
			evidence.TreeIdentity = install.TreeMedia.SHA256
		}
		data, _ := json.Marshal(evidence)
		return ValidatePresence(data, install, digest, string(marker))
	}
	serial := serialRootMachine(api.MapValue(text("serialNumber", "ZC1A2B3C"), number("sizeBytes", serialRootSize)))
	install := requestOf(t, labCatalog(metalProvider(), serial, sshKeySecret(api.MapValue(field("generated", api.MapValue())))), serial.Name())
	for _, wwn := range []string{"naa.5000c500a1b2c3d4", "eui.0025388b91c0a2f1", "t10.ATA     EXAMPLE DISK", ""} {
		if err := present(install, wwn); err != nil {
			t.Fatalf("%q: %v", wwn, diagnostics.Of(err))
		}
	}
	for _, wwn := range []string{" naa.5000c500a1b2c3d4", "naa.\x01", "naa.é", strings.Repeat("x", 257)} {
		if err := present(install, wwn); err == nil || !strings.Contains(diagnostics.Of(err)[0].Message, "malformed root disk WWN") {
			t.Fatalf("%q was accepted: %v", wwn, diagnostics.Of(err))
		}
	}
	declared := eraseMachine()
	byWWN := requestOf(t, labCatalog(metalProvider(), declared, sshKeySecret(api.MapValue(field("generated", api.MapValue())))), declared.Name())
	if err := present(byWWN, ""); err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if err := present(byWWN, "naa.5000c500a1b2c3d4"); err == nil || !strings.Contains(diagnostics.Of(err)[0].Message, "not declared by serial number") {
		t.Fatalf("a WWN root recorded a reported WWN: %v", diagnostics.Of(err))
	}
}
