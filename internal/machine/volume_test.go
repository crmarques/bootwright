package machine

import (
	"strings"
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

func TestPhysicalAgentRootAdmitsEveryRootForm(t *testing.T) {
	declared, catalog := fixture()
	declared = declared.WithSpec(declared.Spec().WithPath(api.BoolValue(true), "substrate", "baremetal", "bmc", "tls", "verify"))
	original, normalization := Normalize(declared, replaced(catalog, declared))
	if len(normalization) != 0 {
		t.Fatal(normalization)
	}
	const path = "/dev/disk/by-id/wwn-0x5000c500a1b2c3d4"
	volume := m("storageId", "RAID-Integrated-1", "volumeId", "Disk_Virtual_0")
	for _, test := range []struct {
		name, device string
		physical     api.Value
	}{
		{"wwn", "", m("wwn", "0x5000c500a1b2c3d4")},
		{"serial and size", "", m("serialNumber", "SYNTHETIC-ROOT-01").With("sizeBytes", api.IntegerValue("8192"))},
		{"wwn path", path, m()},
		{"wwn path beside its wwn", path, m("wwn", "0x5000c500a1b2c3d4")},
		{"volumeRef", "", m("volumeRef", volume)},
		{"declared bootVolume", "", m("bootVolume", m())},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := rootDeclared(original, test.device, test.physical)
			if reason, remedy := AgentRootDeviceRefusal(candidate); reason != "" {
				t.Fatalf("%s: %s", reason, remedy)
			}
			if issues := Validate(candidate, replaced(catalog, candidate)); len(issues) != 0 {
				t.Fatal(issues)
			}
		})
	}
	t.Run("defaulted bootVolume", func(t *testing.T) {
		omitted := rootDeclared(declared, "", m())
		normalized, issues := Normalize(omitted, replaced(catalog, omitted))
		if len(issues) != 0 {
			t.Fatal(issues)
		}
		if hints := substrate.MachineRootDeviceHints(normalized); hints.VolumeRef == nil || !hints.VolumeRef.Firmware {
			t.Fatalf("hints %+v", hints)
		}
		if reason, remedy := AgentRootDeviceRefusal(normalized); reason != "" {
			t.Fatalf("%s: %s", reason, remedy)
		}
		if issues := Validate(normalized, replaced(catalog, normalized)); len(issues) != 0 {
			t.Fatal(issues)
		}
	})
}

func TestPhysicalAgentRootRefusalsNameEveryForm(t *testing.T) {
	original, _ := fixture()
	for name, candidate := range map[string]api.Object{
		"kernel name":              rootDeclared(original, "/dev/sda", m()),
		"wwn path beside another":  rootDeclared(original, "/dev/disk/by-id/wwn-0x5000c500a1b2c3d4", m("wwn", "0x5000c500a1b2c3d5")),
		"uppercase wwn path":       rootDeclared(original, "/dev/disk/by-id/wwn-0x5000C500A1B2C3D4", m()),
		"no selector":              rootDeclared(original, "", m()),
		"bootVolume beside a wwn":  rootDeclared(original, "", m("bootVolume", m(), "wwn", "0x5000c500a1b2c3d4")),
		"volumeRef beside its wwn": rootDeclared(original, "", m("volumeRef", m("storageId", "s", "volumeId", "v"), "wwn", "0x5000c500a1b2c3d4")),
	} {
		t.Run(name, func(t *testing.T) {
			reason, remedy := AgentRootDeviceRefusal(candidate)
			if reason == "" || !strings.Contains(reason+remedy, "volumeRef") || !strings.Contains(reason+remedy, "bootVolume") {
				t.Fatalf("reason %q remedy %q", reason, remedy)
			}
		})
	}
}

func TestLogicalVolumeRootRefusalsNameTheirOwnField(t *testing.T) {
	original, _ := fixture()
	for field, physical := range map[string]api.Value{
		"$.spec.substrate.baremetal.rootDevice.bootVolume": m("bootVolume", m()),
		"$.spec.substrate.baremetal.rootDevice.volumeRef":  m("volumeRef", m("storageId", "s", "volumeId", "v")),
	} {
		candidate := rootDeclared(original, "", physical)
		issues := validateVolumeRoot(candidate, substrate.ArmBaremetal)
		if len(issues) != 1 || issues[0].Field != field || issues[0].Message != "logical-volume discovery requires verified BMC TLS" || !strings.Contains(issues[0].Remediation, "bmc.tls.verify") {
			t.Fatalf("%s: %+v", field, issues)
		}
		alone := rootDeclared(original, "", physical.With("wwn", api.StringValue("0x5000c500a1b2c3d4")))
		if issues := validateVolumeRoot(alone, substrate.ArmBaremetal); len(issues) != 1 || issues[0].Field != field {
			t.Fatalf("%s beside a wwn: %+v", field, issues)
		}
	}
}

func TestInstalledPhysicalRootRefusalNamesEveryForm(t *testing.T) {
	original, _ := fixture()
	installed := rootDeclared(original, "", m())
	installed = installed.WithSpec(installed.Spec().WithPath(api.StringValue("rhel"), "os", "installProfileRef").WithPath(api.StringValue("host-key"), "os", "install", "hostKeyRef"))
	issues := validateBaremetal(installed)
	if len(issues) != 1 || issues[0].Field != "$.spec.hardware.rootDevice" ||
		!strings.Contains(issues[0].Remediation, "rootDevice.volumeRef") || !strings.Contains(issues[0].Remediation, "bootVolume") || !strings.Contains(issues[0].Remediation, "serialNumber and sizeBytes") {
		t.Fatalf("issues %+v", issues)
	}
}
