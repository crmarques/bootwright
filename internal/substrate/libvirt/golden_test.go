package libvirt

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// update rewrites each golden this package compares instead of comparing it:
// ./scripts/go test ./internal/substrate/libvirt -run Golden -update
var update = flag.Bool("update", false, "rewrite testdata/*.golden from the current output")

// evidenceDigest stands for the frozen request digest, which the protocol
// plugin accepts only as 64 lowercase hexadecimal characters.
const evidenceDigest = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// matchesGolden compares canonical JSON bytes with testdata/<name>.golden,
// which holds them indented for review. Indenting is lossless, so comparing
// the indented form byte for byte compares the canonical bytes exactly.
func matchesGolden(t *testing.T, name string, canonical []byte) {
	t.Helper()
	var indented bytes.Buffer
	if err := json.Indent(&indented, canonical, "", "  "); err != nil {
		t.Fatalf("%s: the canonical bytes are not JSON: %v", name, err)
	}
	indented.WriteByte('\n')
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, indented.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v; run with -update to write it", err)
	}
	if !bytes.Equal(want, indented.Bytes()) {
		t.Errorf("%s differs (-golden +got); rerun with -update if the change is intended:\n%s%s",
			path, lineDiff(string(want), indented.String()), firstDifference(string(want), indented.String()))
	}
}

// firstDifference quotes both texts around the first byte they differ at,
// which locates a change inside one long line.
func firstDifference(want, got string) string {
	at := 0
	for at < len(want) && at < len(got) && want[at] == got[at] {
		at++
	}
	excerpt := func(text string) string { return text[max(0, at-40):min(len(text), at+40)] }
	return fmt.Sprintf("first difference at byte %d: golden %q, got %q", at, excerpt(want), excerpt(got))
}

// lineDiff lists the lines only the golden holds (-) and only the output holds
// (+), each numbered in its own text, along a longest common subsequence.
func lineDiff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	common := make([][]int, len(w)+1)
	for i := range common {
		common[i] = make([]int, len(g)+1)
	}
	for i := len(w) - 1; i >= 0; i-- {
		for j := len(g) - 1; j >= 0; j-- {
			if w[i] == g[j] {
				common[i][j] = common[i+1][j+1] + 1
			} else {
				common[i][j] = max(common[i+1][j], common[i][j+1])
			}
		}
	}
	var out strings.Builder
	for i, j := 0, 0; i < len(w) || j < len(g); {
		switch {
		case i < len(w) && j < len(g) && w[i] == g[j]:
			i, j = i+1, j+1
		case i < len(w) && (j == len(g) || common[i+1][j] >= common[i][j+1]):
			fmt.Fprintf(&out, "-%d: %s\n", i+1, w[i])
			i++
		default:
			fmt.Fprintf(&out, "+%d: %s\n", j+1, g[j])
			j++
		}
	}
	return out.String()
}

// The protocol plugin publishes evidence through a channel that encodes with
// sorted keys and no spaces (plugins/module_utils/controller_channel.py), which
// Freeze proves is also how Go encodes it. Each golden is what
// substrate_machine_protocol.py publishes for one state of the lab machine,
// and the validator of that state accepts exactly those bytes.
func TestMachineEvidenceMatchesItsGoldens(t *testing.T) {
	request := machineRequest(t)
	disks := make([]DiskEvidence, 0, len(request.Disks))
	for _, disk := range request.Disks {
		disks = append(disks, DiskEvidence{Name: disk.Name, Present: true, SizeGiB: disk.SizeGiB})
	}
	// Each case names the substrate_machine_protocol.py call it mirrors.
	for name, test := range map[string]struct {
		evidence MachineEvidence
		validate func([]byte) error
	}{
		// presence() after an apply: the domain defined, owned and shut off,
		// every disk at its frozen size, and the controller running the pinned
		// image and answering for this machine's system.
		"completed": {
			MachineEvidence{
				Answered: true, Controller: request.Controller.Image, Disks: disks, Domain: request.Domain,
				Owned: true, Postcondition: true, Power: "Off", Request: evidenceDigest, State: domainOff,
				System: request.UUID, Unit: "active",
			},
			func(data []byte) error { return ValidateMachinePresence(data, request, evidenceDigest) },
		},
		// absence() once the hypervisor answers that it defines no such domain
		// and the controller and disks are gone; it lists no disks at all.
		"removed": {
			MachineEvidence{Absent: true, Answered: true, Disks: []DiskEvidence{}, Postcondition: true, Request: evidenceDigest},
			func(data []byte) error { return ValidateMachineAbsence(data, evidenceDigest) },
		},
		// An observed presence() whose hypervisor did not answer while the
		// controller still runs: the empty domain proves nothing, the unit is
		// this context's own, and the controller's power state is the one
		// answer the removal gate reads.
		"partial-silent": {
			MachineEvidence{
				Controller: request.Controller.Image, Disks: disks, Power: "On", Request: evidenceDigest, Unit: "active",
			},
			func(data []byte) error { return ValidateMachinePartial(data, evidenceDigest) },
		},
	} {
		t.Run(name, func(t *testing.T) {
			canonical, err := reconciliation.Freeze(test.evidence, "machine evidence")
			if err != nil {
				t.Fatalf("encoding: %v", diagnostics.Of(err))
			}
			matchesGolden(t, "machine-evidence-"+name, canonical)
			if err := test.validate(canonical); err != nil {
				t.Fatalf("the %s evidence was refused: %v", name, diagnostics.Of(err))
			}
		})
	}
}
