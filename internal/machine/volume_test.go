package machine

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/substrate"
)

func TestOmittedPhysicalRootNormalizesToPersistentRedfishBootVolume(t *testing.T) {
	o, c := fixture()
	o = o.WithSpec(o.Spec().WithPath(api.MapValue(), "substrate", "baremetal", "rootDevice").WithPath(api.BoolValue(true), "substrate", "baremetal", "bmc", "tls", "verify"))
	normalized, issues := Normalize(o, c)
	if len(issues) != 0 {
		t.Fatal(issues)
	}
	hints := substrate.MachineRootDeviceHints(normalized)
	if hints.VolumeRef == nil || !hints.VolumeRef.Firmware || len(hints.Names()) != 1 {
		t.Fatalf("hints %+v", hints)
	}
	if issues := validateVolumeRoot(normalized, substrate.ArmBaremetal); len(issues) != 0 {
		t.Fatal(issues)
	}
	if reason, _ := AgentRootDeviceRefusal(normalized); reason != "" {
		t.Fatal(reason)
	}
	for _, bad := range []api.Value{api.StringValue("http://bmc.example.test/redfish/v1/Systems/1"), api.StringValue("https://bmc.example.test/redfish/v1/Systems/1")} {
		changed := normalized.WithSpec(normalized.Spec().WithPath(bad, "substrate", "baremetal", "bmc", "address").WithPath(api.BoolValue(false), "substrate", "baremetal", "bmc", "tls", "verify"))
		if issues := validateVolumeRoot(changed, substrate.ArmBaremetal); len(issues) == 0 {
			t.Fatal("unverified discovery admitted")
		}
	}
}
