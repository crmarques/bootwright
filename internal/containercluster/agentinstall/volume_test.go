package agentinstall

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate"
)

func volumeCatalog() api.Catalog {
	objects := physicalCatalog().Objects()
	for i, o := range objects {
		if o.Kind() == api.Machine && o.Name() == "metal-01" {
			objects[i] = o.WithSpec(o.Spec().WithPath(api.MapValue().With("bootVolume", api.MapValue()), "substrate", "baremetal", "rootDevice"))
		}
	}
	return api.NewCatalog(objects)
}

func TestLogicalRootVolumeRequestsGoldenAndRuntimeBinding(t *testing.T) {
	media, install, _ := onlyRequests(t, volumeCatalog())
	if len(media.RootVolumes) != 1 || install.Nodes[0].Hardware.RootSelector != "volume" {
		t.Fatal("logical-volume selection was lost")
	}
	for name, req := range map[string]interface{ Canonical() ([]byte, error) }{"request-media-physical-volume": media, "request-install-physical-volume": install} {
		data, err := req.Canonical()
		if err != nil {
			t.Fatal(err)
		}
		matchesGolden(t, name, data)
	}
	proof := substrate.RootVolumeProof{StorageID: "RAID", VolumeID: "OS", WWN: "0x5000000000000042", SizeBytes: 8192}
	pins := &pinsByName{pins: map[string]machine.HardwareIdentity{"metal-01": {UUID: "uuid-1", RootVolume: proof}}}
	values, err := NewMedia(nil, nil).WithIdentities(pins).volumeValues(lifecycle.Execution{}, media)
	if err != nil {
		t.Fatal(err)
	}
	data, err := base64.StdEncoding.DecodeString(values["agentConfigBase64"])
	if err != nil {
		t.Fatal(err)
	}
	var native map[string]any
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	hosts := native["hosts"].([]any)
	hints := hosts[0].(map[string]any)["rootDeviceHints"].(map[string]any)
	if hints["wwn"] != proof.WWN || len(hints) != 1 {
		t.Fatalf("native hints = %v", hints)
	}
	before, _ := json.Marshal(media.AgentConfig)
	if strings.Contains(string(before), proof.WWN) {
		t.Fatal("runtime binding mutated frozen config")
	}
	roots, _ := base64.StdEncoding.DecodeString(values["rootVolumesBase64"])
	if !strings.Contains(string(roots), proof.WWN) {
		t.Fatal("receipt lost resolved identity")
	}
	pins.pins = map[string]machine.HardwareIdentity{}
	if _, err := NewMedia(nil, nil).WithIdentities(pins).volumeValues(lifecycle.Execution{}, media); err == nil {
		t.Fatal("media built without durable volume proof")
	}
}
