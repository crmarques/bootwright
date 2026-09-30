package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate/baremetal"
	"github.com/crmarques/bootwright/internal/substrate/libvirt"
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
