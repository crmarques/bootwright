package installation

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/machine/diskselection"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type DiscoveryInventory struct {
	Machine string
	Disks   []diskselection.Disk
}

type DiscoverResult struct {
	Operation   *lifecycle.OperationResult
	Inventories []DiscoveryInventory
}

func RecordedInventory(name string, records []lifecycle.BlockEvidence) (DiscoveryInventory, bool, error) {
	out := DiscoveryInventory{Machine: name}
	found := false
	for _, record := range records {
		if record.Implementation != DiscoveryImplementation || record.Object != name || record.Verb != reconciliation.Apply || record.State != reconciliation.BlockDone {
			continue
		}
		request, err := DecodeDiscoveryRequest(record.Block.Request)
		if err != nil {
			return out, false, err
		}
		e, report, err := discoveryEvidence(record.Evidence, record.Block.RequestDigest)
		if err != nil {
			return out, false, err
		}
		if !e.Completed || !e.Cleaned || report.Validate() != nil || report.Binding.Context != request.Identity.Context || report.Binding.Machine != name || report.Binding.RequestDigest != record.Block.RequestDigest || report.Binding.Operation != record.Operation || report.Binding.MediaSHA256 != request.BootMedia.SHA256 || report.Binding.ToolDigest != record.Block.ContentDigest || !slices.Equal(report.Binding.MACs, discoveryMACs(request)) {
			return out, false, refusal("lifecycle.state", "the recorded disk inventory has no complete matching provenance", "")
		}
		if found {
			return out, false, refusal("lifecycle.state", "multiple owned disk inventories compete for one Machine", "")
		}
		out.Disks = slices.Clone(report.Disks)
		found = true
	}
	return out, found, nil
}

func (c Capability) proveDiscoveredRoot(e lifecycle.Execution, r Request) error {
	if r.DiscoveryProof == nil {
		return nil
	}
	refused := func() error {
		return refusal("lifecycle.state", "the frozen root has no matching completed inventory dependency", "restore the original discovery proof before continuing installation")
	}
	dependencies := e.ApplyProved
	if len(dependencies) == 0 {
		dependencies = e.Proved
	}
	if count := len(slices.DeleteFunc(slices.Clone(dependencies), func(record lifecycle.BlockEvidence) bool {
		return record.Implementation != DiscoveryImplementation || record.Object != r.Identity.Object
	})); count != 1 {
		return refused()
	}
	for _, record := range dependencies {
		if record.Implementation != DiscoveryImplementation || record.Object != r.Identity.Object || record.State != reconciliation.BlockDone || record.Verb != reconciliation.Apply {
			continue
		}
		source, err := DecodeDiscoveryRequest(record.Block.Request)
		if err != nil {
			return err
		}
		evidence, report, err := discoveryEvidence(record.Evidence, record.Block.RequestDigest)
		if err != nil {
			return err
		}
		if !evidence.Completed || !evidence.Cleaned || report.Validate() != nil {
			return refused()
		}
		b := report.Binding
		if b.Incarnation != e.Incarnation || b.Context != e.Context || b.Context != r.Identity.Context || b.Context != source.Identity.Context || b.Machine != r.Identity.Object || b.Operation != record.Operation || b.RequestDigest != record.Block.RequestDigest || b.MediaSHA256 != source.BootMedia.SHA256 || b.ToolDigest != record.Block.ContentDigest || !slices.Equal(b.MACs, discoveryMACs(source)) {
			return refused()
		}
		wire, err := report.Canonical()
		if err != nil {
			return err
		}
		hash := sha256.Sum256(wire)
		if hex.EncodeToString(hash[:]) != r.DiscoveryProof.ReportDigest {
			return refused()
		}
		if c.identities == nil {
			return refused()
		}
		pin, found, err := c.identities.PinnedIdentity(r.Identity.Object, dependencies)
		if err != nil {
			return err
		}
		if !found || strings.ToLower(strings.TrimSpace(pin.UUID)) != b.UUID {
			return refused()
		}
		count := 0
		for _, disk := range report.Disks {
			wanted := r.DiscoveryProof.Selected
			if disk.Whole && disk.Fixed && disk.Writable && !disk.Hidden && disk.SizeBytes == wanted.SizeBytes && (wanted.WWN == "" || disk.WWN == wanted.WWN) && (wanted.SerialNumber == "" || disk.SerialNumber == wanted.SerialNumber) {
				count++
			}
		}
		if count != 1 {
			return refused()
		}
		return nil
	}
	return refused()
}
