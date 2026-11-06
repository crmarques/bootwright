package installation

import (
	"encoding/json"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate"
)

type volumePins struct{ pin machine.HardwareIdentity }

func (p volumePins) PinnedIdentity(string, []lifecycle.BlockEvidence) (machine.HardwareIdentity, bool, error) {
	return p.pin, p.pin.Present(), nil
}

func TestLogicalRootVolumeKickstartGoldenAndMissingProofRefusal(t *testing.T) {
	input := physicalInstallation()
	input.RootDevice = ""
	input.RootSerial = ""
	input.RootWWN = ""
	input.RootSizeBytes = 0
	input.RootVolume = true
	rendered, err := RenderKickstart(input)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rendered, "bootwright_volume_count") || !strings.Contains(rendered, VolumeWWNToken) || !strings.Contains(rendered, VolumeSizeToken) {
		t.Fatal("installer lost unique NAA and exact-capacity proof")
	}
	data, _ := json.Marshal(map[string]string{"kickstart": rendered})
	matchesGolden(t, "kickstart-physical-volume", data)
	request := Request{Identity: Identity{Object: "server"}, Target: Target{Physical: true, Hardware: &Hardware{RootVolumeRef: &substrate.VolumeRef{Firmware: true}}}}
	if _, err := New(nil).WithIdentities(volumePins{}).pinValues(lifecycle.Execution{}, request); err == nil {
		t.Fatal("unproved target accepted")
	}
	proof := substrate.RootVolumeProof{StorageID: "RAID", VolumeID: "OS", WWN: "0x5000000000000042", SizeBytes: 8192}
	values, err := New(nil).WithIdentities(volumePins{machine.HardwareIdentity{UUID: "uuid-1", RootVolume: proof}}).pinValues(lifecycle.Execution{}, request)
	if err != nil || values["rootVolumeWWN"] != "5000000000000042" || values["rootVolumeSize"] != "8192" {
		t.Fatalf("values %v error %v", values, err)
	}
	request.Target.Hardware.ClearOnInstall = []ClearTarget{{WWN: proof.WWN}}
	if _, err := New(nil).WithIdentities(volumePins{machine.HardwareIdentity{UUID: "uuid-1", RootVolume: proof}}).pinValues(lifecycle.Execution{}, request); err == nil {
		t.Fatal("root volume admitted as data-clear target")
	}
}

func TestLogicalVolumeSelectorsReachFrozenRequestGolden(t *testing.T) {
	for name, selector := range map[string]api.Value{
		"boot-volume": api.MapValue().With("bootVolume", api.MapValue()),
		"volume-ref":  api.MapValue().With("volumeRef", api.MapValue().With("storageId", api.StringValue("RAID")).With("volumeId", api.StringValue("OS"))),
	} {
		machine := server(api.MapValue())
		machine = machine.WithSpec(machine.Spec().WithPath(selector, "substrate", "baremetal", "rootDevice"))
		request := requestOf(t, labCatalog(metalProvider(), machine, sshKeySecret(api.MapValue(field("generated", api.MapValue())))), machine.Name())
		data, err := request.Canonical()
		if err != nil {
			t.Fatal(err)
		}
		matchesGolden(t, "request-physical-"+name, data)
		decoded, err := DecodeRequest(data)
		if err != nil || decoded.Target.Hardware.RootVolumeRef == nil || !strings.Contains(decoded.Kickstart, VolumeWWNToken) {
			t.Fatalf("volume request lost its binding: %v", err)
		}
	}
}
