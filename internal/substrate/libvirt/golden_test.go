package libvirt

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
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
// substrate_machine_protocol.py publishes for one state of the lab machine:
// the validator of that state accepts exactly those bytes, every other
// validator refuses them, and the removal gate reads them as the quiescence
// they prove.
func TestMachineEvidenceMatchesItsGoldens(t *testing.T) {
	request := machineRequest(t)
	disks := make([]DiskEvidence, 0, len(request.Disks))
	gone := make([]DiskEvidence, 0, len(request.Disks))
	for _, disk := range request.Disks {
		disks = append(disks, DiskEvidence{Name: disk.Name, Present: true, SizeGiB: disk.SizeGiB})
		gone = append(gone, DiskEvidence{Name: disk.Name})
	}
	validators := map[string]func([]byte) error{
		"presence": func(data []byte) error { return ValidateMachinePresence(data, request, evidenceDigest) },
		"absence":  func(data []byte) error { return ValidateMachineAbsence(data, evidenceDigest) },
		"partial":  func(data []byte) error { return ValidateMachinePartial(data, evidenceDigest) },
	}
	// Each case names the substrate_machine_protocol.py call it mirrors.
	for name, test := range map[string]struct {
		evidence   MachineEvidence
		accepts    string
		quiescence string
	}{
		// presence() after an apply: the domain defined, owned and shut off,
		// every disk at its frozen size, and the controller running the pinned
		// image, listening, and answering for this machine's system.
		"completed": {
			MachineEvidence{
				Answered: true, Controller: request.Controller.Image, Disks: disks, Domain: request.Domain,
				Listener: observed(true), Owned: true, Postcondition: true, Power: "Off", Request: evidenceDigest,
				State: domainOff, System: request.UUID, Unit: "active",
			},
			"presence", lifecycle.Quiescent,
		},
		// absence() once the hypervisor answers that it defines no such domain,
		// the controller is gone, nothing exists at the root disk's path and
		// nothing listens on the controller's socket.
		"removed": {
			MachineEvidence{
				Absent: true, Answered: true, Disks: gone, Listener: observed(false), Postcondition: true,
				Request: evidenceDigest,
			},
			"absence", lifecycle.Quiescent,
		},
		// An observed presence() whose hypervisor did not answer while the
		// controller still runs: the empty domain proves nothing, the unit is
		// this context's own, and the controller's power state is the one
		// answer the removal gate reads.
		"partial-silent": {
			MachineEvidence{
				Controller: request.Controller.Image, Disks: disks, Listener: observed(true), Power: "On",
				Request: evidenceDigest, Unit: "active",
			},
			"partial", lifecycle.Live,
		},
		// An observed presence() over a machine a removal left with only its
		// root disk: the hypervisor answered that no domain is defined, so the
		// disk is this context's own work part way removed.
		"partial-disks": {
			MachineEvidence{Answered: true, Disks: disks, Listener: observed(false), Request: evidenceDigest},
			"partial", lifecycle.Quiescent,
		},
		// An observed presence() with nothing of the machine left but a
		// listener on the controller's socket, which is not proved to be this
		// Machine's, so no validator reads it.
		"held-socket": {
			MachineEvidence{Answered: true, Disks: gone, Listener: observed(true), Request: evidenceDigest},
			"", lifecycle.Quiescent,
		},
	} {
		t.Run(name, func(t *testing.T) {
			canonical, err := reconciliation.Freeze(test.evidence, "machine evidence")
			if err != nil {
				t.Fatalf("encoding: %v", diagnostics.Of(err))
			}
			matchesGolden(t, "machine-evidence-"+name, canonical)
			for validator, validate := range validators {
				if err := validate(canonical); (err == nil) != (validator == test.accepts) {
					t.Errorf("the %s validator read the %s evidence as %v", validator, name, diagnostics.Of(err))
				}
			}
			if state := quiescenceOf(t, request, canonical); state != test.quiescence {
				t.Errorf("the %s evidence reads as %s to the removal gate, want %s", name, state, test.quiescence)
			}
		})
	}
}

// quiescenceOf is what the removal gate reads from one published observation
// of the lab machine.
func quiescenceOf(t *testing.T, request MachineRequest, evidence []byte) string {
	t.Helper()
	canonical, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	probe := lifecycle.Probe{Block: reconciliation.Block{
		BlockDefinition: reconciliation.BlockDefinition{ID: "machine-rhel-01", Request: canonical},
		RequestDigest:   evidenceDigest,
	}}
	runner := &fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: evidence}}
	quiescence, err := NewMachine(runner).Quiescent(context.Background(), probe)
	if err != nil {
		t.Fatal(err)
	}
	return quiescence.State
}

// Each golden is what substrate_host_protocol.py publishes for one state of
// the lab provider host, whose one network is managed, and only the validator
// of that state accepts those bytes.
func TestHostEvidenceMatchesItsGoldens(t *testing.T) {
	request := hostRequest(t)
	var completed HostEvidence
	if err := json.Unmarshal(hostEvidence(request, evidenceDigest), &completed); err != nil {
		t.Fatal(err)
	}
	if len(completed.Networks) != 1 || !completed.Networks[0].Managed {
		t.Fatalf("the lab provider host no longer has exactly one managed network: %+v", completed.Networks)
	}
	forgotten := []NetworkEvidence{{Answered: true, Managed: true, Name: completed.Networks[0].Name}}
	unread := []NetworkEvidence{{Managed: true, Name: completed.Networks[0].Name}}
	validators := map[string]func([]byte) error{
		"presence": func(data []byte) error { return ValidateHostPresence(data, request, evidenceDigest) },
		"absence":  func(data []byte) error { return ValidateHostAbsence(data, evidenceDigest) },
		"partial":  func(data []byte) error { return ValidateHostPartial(data, evidenceDigest) },
	}
	// Each case names the substrate_host_protocol.py call it mirrors.
	for name, test := range map[string]struct {
		evidence HostEvidence
		accepts  string
	}{
		// presence() after an apply: everything the request froze, and the
		// pool directory present.
		"completed": {completed, "presence"},
		// absence() once the URI answered, the storage and network drivers
		// answered that the pool and the managed network are gone, and
		// nothing exists at the pool directory's path; the hypervisor closure
		// and its daemons stay, as the removal leaves them.
		"removed": {
			HostEvidence{
				Absent: true, Directory: observed(false), Hypervisor: true, Networks: forgotten, PoolAnswered: true,
				Postcondition: true, Request: evidenceDigest, Services: completed.Services, URI: true,
			},
			"absence",
		},
		// An observed presence() over a host a removal left with only its pool
		// directory: the URI and both drivers answered for the pool and the
		// network.
		"partial-directory": {
			HostEvidence{
				Directory: observed(true), Hypervisor: true, Networks: forgotten, PoolAnswered: true,
				Request: evidenceDigest, Services: completed.Services, URI: true,
			},
			"partial",
		},
		// An observed presence() whose URI answered while the network driver
		// did not: observe_host reads the managed network as unanswered, so
		// its missing state proves nothing, and no validator accepts it.
		"network-silent": {
			HostEvidence{
				Directory: observed(false), Hypervisor: true, Networks: unread, PoolAnswered: true,
				Request: evidenceDigest, Services: completed.Services, URI: true,
			},
			"",
		},
		// An observed presence() whose URI did not answer: observe_host reads
		// no network state and no pool without an answer, so neither proves
		// anything, and nothing exists at the pool directory's path.
		"silent": {
			HostEvidence{
				Directory: observed(false), Hypervisor: true, Networks: unread, Request: evidenceDigest,
				Services: completed.Services,
			},
			"",
		},
	} {
		t.Run(name, func(t *testing.T) {
			canonical, err := reconciliation.Freeze(test.evidence, "provider host evidence")
			if err != nil {
				t.Fatalf("encoding: %v", diagnostics.Of(err))
			}
			matchesGolden(t, "host-evidence-"+name, canonical)
			for validator, validate := range validators {
				if err := validate(canonical); (err == nil) != (validator == test.accepts) {
					t.Errorf("the %s validator read the %s evidence as %v", validator, name, diagnostics.Of(err))
				}
			}
		})
	}
}
