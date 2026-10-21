package ceph

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate/libvirt"
)

func originalDiskProof(t *testing.T, host Host) lifecycle.BlockEvidence {
	t.Helper()
	disk := host.Devices[0]
	request := libvirt.MachineRequest{
		Version: "machine-libvirt-v7", Identity: libvirt.Identity{Context: "original", Object: host.Machine},
		Domain: "owned-" + host.Machine, UUID: "12345678-1234-5678-1234-123456789abc",
		Controller: libvirt.Controller{Image: "registry.example.test/emulator@sha256:" + strings.Repeat("a", 64)},
		Disks:      []libvirt.Disk{{Name: "root", Target: "vda", SizeGiB: 60}, {Name: "data", Target: "vdb", Serial: disk.Serial, SizeGiB: int(disk.SizeBytes >> 30)}},
	}
	if disk.Path == "/dev/sda" {
		request.Disks[1].Bus, request.Disks[1].Media, request.Disks[1].RotationRate, request.Disks[1].Target = "scsi", "ssd", 1, "sda"
	}
	if disk.Rotational {
		request.Disks[1].Media, request.Disks[1].RotationRate = "hdd", 7200
	}
	canonical, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(canonical)
	digest := hex.EncodeToString(hash[:])
	diskEvidence := libvirt.DiskEvidence{Name: "data", Present: true, SizeGiB: int(disk.SizeBytes >> 30)}
	if disk.Path == "/dev/sda" {
		diskEvidence.Bus, diskEvidence.Target, diskEvidence.RotationRate = "scsi", "sda", 1
	}
	if disk.Rotational {
		diskEvidence.RotationRate = 7200
	}
	evidence, err := json.Marshal(libvirt.MachineEvidence{
		Request: digest, Domain: request.Domain, Owned: true, Postcondition: true,
		Controller: request.Controller.Image, System: request.UUID, Unit: "active", Power: "On",
		Disks: []libvirt.DiskEvidence{{Name: "root", Present: true, SizeGiB: 60}, diskEvidence},
	})
	if err != nil {
		t.Fatal(err)
	}
	return lifecycle.BlockEvidence{
		Kind: libvirt.MachineKind, Object: host.Machine, Implementation: libvirt.MachineImplementation,
		Verb: reconciliation.Apply, State: reconciliation.BlockDone, Evidence: evidence,
		Block: reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{Kind: libvirt.MachineKind, Object: host.Machine, Implementation: libvirt.MachineImplementation, Request: canonical}, RequestDigest: digest},
	}
}

func diskProofExecution(t *testing.T, request Request) lifecycle.Execution {
	t.Helper()
	e := lifecycle.Execution{ApplyOperation: "original", Operation: "later", Context: "renamed"}
	for _, host := range request.Hosts {
		e.ApplyProved = append(e.ApplyProved, originalDiskProof(t, host))
	}
	return e
}

func TestOSDDiskCustodyRequiresUniqueOriginalCompletedApply(t *testing.T) {
	r := coreRequest()
	if err := proveDisks(diskProofExecution(t, r), r, dependencyProofs{}); err != nil {
		t.Fatalf("valid original proof after context rename refused: %v", err)
	}
	for name, damage := range map[string]func(*lifecycle.Execution, *Request){
		"absent proof":             func(e *lifecycle.Execution, _ *Request) { e.ApplyProved = nil },
		"missing host proof":       func(e *lifecycle.Execution, _ *Request) { e.ApplyProved = e.ApplyProved[1:] },
		"foreign machine":          func(e *lifecycle.Execution, _ *Request) { e.ApplyProved[0].Object = "another-machine" },
		"foreign implementation":   func(e *lifecycle.Execution, _ *Request) { e.ApplyProved[0].Implementation = "foreign" },
		"destroy proof":            func(e *lifecycle.Execution, _ *Request) { e.ApplyProved[0].Verb = reconciliation.Destroy },
		"unfinished apply":         func(e *lifecycle.Execution, _ *Request) { e.ApplyProved[0].State = reconciliation.BlockRunning },
		"different frozen digest":  func(e *lifecycle.Execution, _ *Request) { e.ApplyProved[0].Block.RequestDigest = "different" },
		"missing frozen request":   func(e *lifecycle.Execution, _ *Request) { e.ApplyProved[0].Block.Request = nil },
		"root device":              func(_ *lifecycle.Execution, r *Request) { r.Hosts[0].Devices[0].Path = "/dev/vda" },
		"different path":           func(_ *lifecycle.Execution, r *Request) { r.Hosts[0].Devices[0].Path = "/dev/vdc" },
		"different serial":         func(_ *lifecycle.Execution, r *Request) { r.Hosts[0].Devices[0].Serial = "another-volume" },
		"missing serial":           func(_ *lifecycle.Execution, r *Request) { r.Hosts[0].Devices[0].Serial = "" },
		"different size":           func(_ *lifecycle.Execution, r *Request) { r.Hosts[0].Devices[0].SizeBytes++ },
		"duplicate original proof": func(e *lifecycle.Execution, _ *Request) { e.ApplyProved = append(e.ApplyProved, e.ApplyProved[0]) },
		"current proof cannot replace original": func(e *lifecycle.Execution, _ *Request) {
			e.Proved = append([]lifecycle.BlockEvidence(nil), e.ApplyProved...)
			e.ApplyProved = append([]lifecycle.BlockEvidence(nil), e.ApplyProved...)
			e.ApplyProved[0].Block.RequestDigest = "different"
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := coreRequest()
			e := diskProofExecution(t, r)
			damage(&e, &r)
			if err := proveDisks(e, r, dependencyProofs{}); err == nil {
				t.Fatal("missing, foreign or ambiguous original disk proof accepted")
			}
		})
	}
}

func TestOSDDiskCustodyAcceptsCurrentApplyDependencyEvidence(t *testing.T) {
	r := coreRequest()
	e := diskProofExecution(t, r)
	e.Proved, e.ApplyProved = e.ApplyProved, nil
	if err := proveDisks(e, r, dependencyProofs{}); err != nil {
		t.Fatalf("current apply's completed dependencies refused: %v", err)
	}
}

func TestMissingDependencyProofReadersRefuse(t *testing.T) {
	r := coreRequest()
	if err := proveDisks(diskProofExecution(t, r), r, nil); err == nil {
		t.Fatal("missing disk reader accepted")
	}
	host := r.Hosts[0]
	host.HostKeyRef = ""
	if _, err := provedHostKey(host, lifecycle.Execution{}, nil); err == nil {
		t.Fatal("missing host-key reader accepted")
	}
}

func TestOSDDiskCustodyBindsRotationalIdentity(t *testing.T) {
	r := coreRequest()
	for i := range r.Hosts {
		r.Hosts[i].Devices[0].Path = "/dev/sda"
		r.Hosts[i].Devices[0].Rotational = true
	}
	e := diskProofExecution(t, r)
	if err := proveDisks(e, r, dependencyProofs{}); err != nil {
		t.Fatal(err)
	}
	r.Hosts[0].Devices[0].Rotational = false
	if proveDisks(e, r, dependencyProofs{}) == nil {
		t.Fatal("rotational identity changed after original apply")
	}
}
