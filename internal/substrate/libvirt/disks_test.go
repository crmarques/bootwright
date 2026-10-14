package libvirt

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/substrate"
)

func TestDataDiskSerialSeparatesOwnedVolumes(t *testing.T) {
	serial := substrate.DataDiskSerial("lab", "node-a", "data")
	if len(serial) != 20 || serial != substrate.DataDiskSerial("lab", "node-a", "data") {
		t.Fatalf("unstable disk identity %q", serial)
	}
	for _, args := range [][3]string{{"lab2", "node-a", "data"}, {"lab", "node-b", "data"}, {"lab", "node-a", "other"}} {
		if substrate.DataDiskSerial(args[0], args[1], args[2]) == serial {
			t.Fatal("distinct owned volumes share a serial")
		}
	}
}

func TestProvedDataDiskRequiresFrozenOwnedVolume(t *testing.T) {
	request := machineRequest(t)
	request.Disks = append(request.Disks, Disk{Name: "data", Target: "vdb", Serial: substrate.DataDiskSerial(testContext, request.Identity.Object, "data"), SizeGiB: 80})
	canonical, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(canonical)
	digest := hex.EncodeToString(hash[:])
	block := reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{Kind: MachineKind, Object: request.Identity.Object, Implementation: MachineImplementation, Request: canonical}, RequestDigest: digest}
	data := request.Disks[len(request.Disks)-1]
	evidence := machineEvidence(request, digest)
	if !ProvedDataDisk(block, evidence, "/dev/vdb", data.Serial, 80<<30) {
		t.Fatal("original frozen owned data volume was refused")
	}
	for name, damage := range map[string]func(*reconciliation.Block, *[]byte, *string, *string, *int64){
		"absent evidence":        func(_ *reconciliation.Block, e *[]byte, _ *string, _ *string, _ *int64) { *e = nil },
		"foreign kind":           func(b *reconciliation.Block, _ *[]byte, _ *string, _ *string, _ *int64) { b.Kind = "StorageCluster" },
		"foreign implementation": func(b *reconciliation.Block, _ *[]byte, _ *string, _ *string, _ *int64) { b.Implementation = "foreign" },
		"different digest": func(b *reconciliation.Block, _ *[]byte, _ *string, _ *string, _ *int64) {
			b.RequestDigest = "different"
		},
		"missing request":  func(b *reconciliation.Block, _ *[]byte, _ *string, _ *string, _ *int64) { b.Request = nil },
		"different path":   func(_ *reconciliation.Block, _ *[]byte, p *string, _ *string, _ *int64) { *p = "/dev/vdc" },
		"different serial": func(_ *reconciliation.Block, _ *[]byte, _ *string, s *string, _ *int64) { *s = "another-volume" },
		"missing serial":   func(_ *reconciliation.Block, _ *[]byte, _ *string, s *string, _ *int64) { *s = "" },
		"different size":   func(_ *reconciliation.Block, _ *[]byte, _ *string, _ *string, s *int64) { *s = 81 << 30 },
		"root device":      func(_ *reconciliation.Block, _ *[]byte, p *string, _ *string, _ *int64) { *p = "/dev/vda" },
		"unowned evidence": func(_ *reconciliation.Block, e *[]byte, _ *string, _ *string, _ *int64) {
			var value MachineEvidence
			if err := json.Unmarshal(*e, &value); err != nil {
				t.Fatal(err)
			}
			value.Owned = false
			*e = encode(t, value)
		},
		"missing volume": func(_ *reconciliation.Block, e *[]byte, _ *string, _ *string, _ *int64) {
			var value MachineEvidence
			if err := json.Unmarshal(*e, &value); err != nil {
				t.Fatal(err)
			}
			value.Disks = value.Disks[:1]
			*e = encode(t, value)
		},
	} {
		t.Run(name, func(t *testing.T) {
			b, e, path, serial, size := block, []byte(evidence), "/dev/vdb", data.Serial, int64(80<<30)
			damage(&b, &e, &path, &serial, &size)
			if ProvedDataDisk(b, e, path, serial, size) {
				t.Fatal("missing or mismatched original volume proof was accepted")
			}
		})
	}
	// Even a root disk with an explicit serial is never an OSD-owned volume.
	root := request
	root.Disks = []Disk{{Name: "root", Target: "vda", Serial: "root-serial", SizeGiB: 60}}
	block.Request, err = root.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	hash = sha256.Sum256(block.Request)
	block.RequestDigest = hex.EncodeToString(hash[:])
	if ProvedDataDisk(block, machineEvidence(root, block.RequestDigest), "/dev/vda", "root-serial", 60<<30) {
		t.Fatal("root volume was accepted as an OSD data volume")
	}
}
