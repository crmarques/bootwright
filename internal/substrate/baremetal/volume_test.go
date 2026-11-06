package baremetal

import (
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/substrate"
)

func TestLogicalRootVolumeProofGoldenAndSelectionLinkage(t *testing.T) {
	proof := substrate.RootVolumeProof{StorageID: "RAID", VolumeID: "OS", WWN: "0x5000000000000042", SizeBytes: 8192}
	for _, ref := range []substrate.VolumeRef{{StorageID: "RAID", VolumeID: "OS"}, {Firmware: true}} {
		request := fixtureRequest(t)
		request.RootVolumeRef = &ref
		data, err := request.Canonical()
		if err != nil {
			t.Fatal(diagnostics.Of(err))
		}
		if _, err := DecodeRequest(data); err != nil {
			t.Fatal(diagnostics.Of(err))
		}
		if !ref.Firmware {
			matchesGolden(t, "request-logical-root-volume", data, false)
		}
		evidence := Evidence{Postcondition: true, Addresses: request.Addresses(), Power: "Off", UUID: "uuid-1", Request: "digest", RootVolume: &proof}
		data = encode(t, evidence)
		if err := ValidatePresence(data, request, "digest"); err != nil {
			t.Fatal(err)
		}
		identity, err := ProvedIdentity(data)
		if err != nil || identity.RootVolume != proof {
			t.Fatalf("volume proof lost: %+v %v", identity, err)
		}
		if !ref.Firmware {
			matchesGolden(t, "evidence-logical-root-volume", data, false)
		}
		changed := proof
		changed.VolumeID = "OTHER"
		evidence.RootVolume = &changed
		if !ref.Firmware && ValidatePresence(encode(t, evidence), request, "digest") == nil {
			t.Fatal("another volume proved")
		}
		evidence.RootVolume = nil
		if ValidatePresence(encode(t, evidence), request, "digest") == nil {
			t.Fatal("missing root-volume proof accepted")
		}
	}
}
