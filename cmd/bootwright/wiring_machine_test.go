package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/machine"
	machineaccess "github.com/crmarques/bootwright/internal/machine/access"
	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate/baremetal"
	"github.com/crmarques/bootwright/internal/substrate/libvirt"
	"github.com/crmarques/bootwright/internal/trust/enrollment"
)

// provedEvidence is the bare-metal proof the capability's own golden freezes,
// compact as the engine hands it over.
func provedEvidence(t *testing.T) []byte {
	t.Helper()
	golden, err := os.ReadFile(filepath.Join("..", "..", "internal", "substrate", "baremetal", "testdata", "evidence-proved.golden"))
	if err != nil {
		t.Fatal(err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, golden); err != nil {
		t.Fatal(err)
	}
	return compact.Bytes()
}

// An installation attempt is handed the evidence of every block it depends
// on, another Machine's proof among them, so the pin it reads is only the
// proof of the exact Machine it installs, decoded by the bare-metal capability
// that wrote it; a libvirt realization of the same name proves no identity.
func TestTheInstallationsReadTheBareMetalPinOfTheirOwnMachine(t *testing.T) {
	proof := provedEvidence(t)
	another := bytes.ReplaceAll(bytes.ReplaceAll(proof,
		[]byte("4c4c4544-0042-3510-8052-b4c04f4d4e31"), []byte("4c4c4544-0042-3510-8052-b4c04f4d4e32")),
		[]byte("CZJ2440ABC"), []byte("CZJ2440ABD"))
	done := func(object, implementation string, evidence []byte) lifecycle.BlockEvidence {
		return lifecycle.BlockEvidence{
			Kind: "Machine", Object: object, Implementation: implementation,
			Verb: reconciliation.Apply, State: reconciliation.BlockDone, Evidence: evidence,
		}
	}
	proved := []lifecycle.BlockEvidence{
		done("metal-02", baremetal.Implementation, another),
		done("metal-01", libvirt.MachineImplementation, []byte(`{"domain":"lab-metal-01"}`)),
		{Kind: "ArtifactServer", Object: "metal-01", Verb: reconciliation.Apply, State: reconciliation.BlockDone, Evidence: another},
		done("metal-01", baremetal.Implementation, proof),
	}
	for name, test := range map[string]struct {
		machine string
		want    machine.HardwareIdentity
		pinned  bool
	}{
		"its own proof":       {"metal-01", machine.HardwareIdentity{UUID: "4c4c4544-0042-3510-8052-b4c04f4d4e31", Serial: "CZJ2440ABC"}, true},
		"another's proof":     {"metal-02", machine.HardwareIdentity{UUID: "4c4c4544-0042-3510-8052-b4c04f4d4e32", Serial: "CZJ2440ABD"}, true},
		"no proof of its own": {"metal-03", machine.HardwareIdentity{}, false},
	} {
		t.Run(name, func(t *testing.T) {
			identity, pinned, err := provedIdentities{}.PinnedIdentity(test.machine, proved)
			if err != nil || pinned != test.pinned || identity != test.want {
				t.Fatalf("pin of %s = %+v, %t (%v), want %+v", test.machine, identity, pinned, err, test.want)
			}
		})
	}
	if _, _, err := (provedIdentities{}).PinnedIdentity("metal-04", []lifecycle.BlockEvidence{
		done("metal-04", baremetal.Implementation, []byte(strings.Replace(string(proof), `"postcondition":true`, `"postcondition":false`, 1))),
	}); err == nil {
		t.Fatal("a proof that proves nothing was read as a pin")
	}
}

// An emulated controller is an effect of the libvirt machine block, so power
// reads that block of the current plan and never the installation that follows
// it on the same Machine, whatever order the evidence lists them in, nor a
// bare-metal claim, which creates no controller.
func TestPowerAsksTheMachineBlockNotTheInstallation(t *testing.T) {
	block := func(implementation string, verb reconciliation.Verb, state reconciliation.BlockState) lifecycle.BlockEvidence {
		return lifecycle.BlockEvidence{Kind: "Machine", Object: "rhel-01", Implementation: implementation, Verb: verb, State: state}
	}
	for name, test := range map[string]struct {
		published []lifecycle.BlockEvidence
		want      machine.OwnershipState
		found     bool
	}{
		"installation failed": {[]lifecycle.BlockEvidence{
			block(installation.Implementation, reconciliation.Apply, reconciliation.BlockFailed),
			block(libvirt.MachineImplementation, reconciliation.Apply, reconciliation.BlockDone),
		}, machine.OwnershipState{Verb: "apply", State: "done"}, true},
		"installation unproved": {[]lifecycle.BlockEvidence{
			block(installation.Implementation, reconciliation.Apply, reconciliation.BlockUnknown),
			block(libvirt.MachineImplementation, reconciliation.Apply, reconciliation.BlockDone),
		}, machine.OwnershipState{Verb: "apply", State: "done"}, true},
		"installation running": {[]lifecycle.BlockEvidence{
			block(installation.Implementation, reconciliation.Apply, reconciliation.BlockRunning),
			block(libvirt.MachineImplementation, reconciliation.Apply, reconciliation.BlockDone),
		}, machine.OwnershipState{Verb: "apply", State: "done"}, true},
		"machine block failed": {[]lifecycle.BlockEvidence{
			block(libvirt.MachineImplementation, reconciliation.Apply, reconciliation.BlockFailed),
		}, machine.OwnershipState{Verb: "apply", State: "failed"}, true},
		"removal pending": {[]lifecycle.BlockEvidence{
			block(installation.Implementation, reconciliation.Destroy, reconciliation.BlockDone),
			block(libvirt.MachineImplementation, reconciliation.Destroy, reconciliation.BlockPending),
		}, machine.OwnershipState{Verb: "destroy", State: "pending"}, true},
		"nothing planned":       {nil, machine.OwnershipState{}, false},
		"only the installation": {[]lifecycle.BlockEvidence{block(installation.Implementation, reconciliation.Apply, reconciliation.BlockDone)}, machine.OwnershipState{}, false},
		"only a bare-metal claim": {[]lifecycle.BlockEvidence{block(baremetal.Implementation, reconciliation.Apply, reconciliation.BlockDone)},
			machine.OwnershipState{}, false},
	} {
		t.Run(name, func(t *testing.T) {
			state, found := realizationOf(test.published)
			if state != test.want || found != test.found {
				t.Fatalf("realization = %+v, %t, want %+v, %t", state, found, test.want, test.found)
			}
		})
	}
}

// An interactive process shows the host-key plan on its own standard output
// before the prompt; one without an output binds no presenter, and enrollment
// then refuses to ask.
func TestTrustEnrollmentPresentsOnTheProcessStandardOutput(t *testing.T) {
	var out bytes.Buffer
	options := trustOptions(machineDependencies{Streams: machineaccess.Streams{Out: &out}}, nil)
	if options.Presenter == nil {
		t.Fatal("an interactive process binds no trust plan presenter")
	}
	report := enrollment.Report{Context: "lab", Pending: 1, Hosts: []enrollment.HostReport{
		{Machine: "node-a", Address: "192.0.2.10", Port: 22, Action: enrollment.ActionAdd, KeyType: "ssh-ed25519", Fingerprint: "SHA256:aaa"},
	}}
	if err := options.Presenter.PresentTrustPlan(context.Background(), report); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "Host-key trust plan for context lab: 1 machine(s) checked, 1 pending\n") ||
		!strings.Contains(out.String(), "SHA256:aaa") {
		t.Fatalf("standard output = %q", out.String())
	}
	if trustOptions(machineDependencies{}, nil).Presenter != nil {
		t.Fatal("a process without standard output bound a presenter")
	}
}
