package machine

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/substrate"
)

func TestOmittedPhysicalRootNormalizesToPersistentRedfishBootVolume(t *testing.T) {
	o, c := fixture()
	o = verifiedBMC(o.WithSpec(o.Spec().WithPath(api.MapValue(), "hardware", "rootDisk")))
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
	if reason, _ := AgentRootDeviceRefusal(c, normalized); reason != "" {
		t.Fatal(reason)
	}
	for _, bad := range []api.Value{api.StringValue("http://bmc.example.test/redfish/v1/Systems/1"), api.StringValue("https://bmc.example.test/redfish/v1/Systems/1")} {
		changed := normalized.WithSpec(normalized.Spec().WithPath(bad, "hardware", "bmc", "address").WithPath(api.StringValue("none"), "hardware", "bmc", "tls", "verification"))
		if issues := validateVolumeRoot(changed, substrate.ArmBaremetal); len(issues) == 0 {
			t.Fatal("unverified discovery admitted")
		}
	}
}

func verifiedBMC(o api.Object) api.Object {
	return o.WithSpec(o.Spec().WithPath(o.Spec().Get("hardware", "bmc").Without("tls"), "hardware", "bmc"))
}

func agentFixture(t *testing.T) (declared, normalized api.Object, catalog api.Catalog) {
	declared, catalog = fixture()
	declared = verifiedBMC(declared)
	normalized, normalization := Normalize(declared, replaced(catalog, declared))
	if len(normalization) != 0 {
		t.Fatal(normalization)
	}
	return declared, normalized, catalog
}

func TestPhysicalAgentRootAdmitsEveryRootForm(t *testing.T) {
	declared, original, catalog := agentFixture(t)
	volume := m("storageId", "RAID-Integrated-1", "volumeId", "Disk_Virtual_0")
	for _, test := range []struct {
		name     string
		physical api.Value
	}{
		{"wwn", m("wwn", "0x5000c500a1b2c3d4")},
		{"32-digit wwn", m("wwn", "0x6000c500a1b2c3d4e5f60718293a4b5c")},
		{"serial and size", m("serialNumber", "SYNTHETIC-ROOT-01").With("sizeBytes", api.IntegerValue("8192"))},
		{"wwn beside serial and size", m("wwn", "0x5000c500a1b2c3d4", "serialNumber", "SYNTHETIC-ROOT-01").With("sizeBytes", api.IntegerValue("8192"))},
		{"volumeRef", m("volumeRef", volume)},
		{"declared bootVolume", m("bootVolume", m())},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := rootDeclared(original, "", test.physical)
			if reason, remedy := AgentRootDeviceRefusal(catalog, candidate); reason != "" {
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
		if reason, remedy := AgentRootDeviceRefusal(catalog, normalized); reason != "" {
			t.Fatalf("%s: %s", reason, remedy)
		}
		if issues := Validate(normalized, replaced(catalog, normalized)); len(issues) != 0 {
			t.Fatal(issues)
		}
	})
}

func TestPhysicalAgentRootRefusalsNameEveryForm(t *testing.T) {
	original, catalog := fixture()
	for name, candidate := range map[string]api.Object{
		"kernel name":              rootDeclared(original, "/dev/sda", m()),
		"uppercase wwn path":       rootDeclared(original, "/dev/disk/by-id/wwn-0x5000C500A1B2C3D4", m()),
		"uppercase wwn":            rootDeclared(original, "", m("wwn", "0x5000C500A1B2C3D4")),
		"wwn without its 0x":       rootDeclared(original, "", m("wwn", "5000c500a1b2c3d4")),
		"no selector":              rootDeclared(original, "", m()),
		"bootVolume beside a wwn":  rootDeclared(original, "", m("bootVolume", m(), "wwn", "0x5000c500a1b2c3d4")),
		"volumeRef beside its wwn": rootDeclared(original, "", m("volumeRef", m("storageId", "s", "volumeId", "v"), "wwn", "0x5000c500a1b2c3d4")),
	} {
		t.Run(name, func(t *testing.T) {
			reason, remedy := AgentRootDeviceRefusal(catalog, candidate)
			if reason == "" || !strings.Contains(reason+remedy, "volumeRef") || !strings.Contains(reason+remedy, "bootVolume") {
				t.Fatalf("reason %q remedy %q", reason, remedy)
			}
		})
	}
}

// A bare-metal Machine an installer writes names its root disk's WWN only as
// hardware.rootDisk.wwn, so any hardware.rootDisk.hints.deviceName
// refuses at its path, for a Bootwright installation and an agent node alike;
// a by-id WWN path's remedy names the wwn it spells.
func TestAPhysicalRootDeviceNameRefusesNamingTheWWN(t *testing.T) {
	_, agent, catalog := agentFixture(t)
	installed, installedCatalog := installedFixture()
	const path = "/dev/disk/by-id/wwn-0x5000c500a1b2c3d4"
	for name, test := range map[string]struct {
		device   string
		physical api.Value
		remedy   string
	}{
		"a WWN path":            {path, m(), "with spec.hardware.rootDisk.wwn: 0x5000c500a1b2c3d4, the WWN " + path + " names"},
		"a WWN path beside wwn": {path, m("wwn", "0x5000c500a1b2c3d4"), "with spec.hardware.rootDisk.wwn: 0x5000c500a1b2c3d4"},
		"a kernel name":         {"/dev/sda", m(), "set spec.hardware.rootDisk.wwn to the disk's WWN"},
		"a by-path name":        {"/dev/disk/by-path/pci-0000:00:1f.2-ata-1", m(), "or remove every root-disk field to default to bootVolume"},
		"a 32-digit WWN path":   {"/dev/disk/by-id/wwn-0x6000c500a1b2c3d4e5f60718293a4b5c", m(), "wwn: 0x6000c500a1b2c3d4e5f60718293a4b5c"},
	} {
		for consumer, base := range map[string]struct {
			machine api.Object
			catalog api.Catalog
		}{"an agent node": {agent, catalog}, "a Bootwright installation": {installed, installedCatalog}} {
			t.Run(name+" on "+consumer, func(t *testing.T) {
				candidate := rootDeclared(base.machine, test.device, test.physical)
				issues := Validate(candidate, replaced(base.catalog, candidate))
				if len(issues) != 1 || issues[0].Code != "api.invariant" || issues[0].Field != "$.spec.hardware.rootDisk.hints.deviceName" ||
					!strings.Contains(issues[0].Message, "spec.hardware.rootDisk.wwn") || !strings.Contains(issues[0].Remediation, test.remedy) {
					t.Fatalf("issues %+v", issues)
				}
			})
		}
	}
	provided := rootDeclared(agent, path, m("wwn", "0x5000c500a1b2c3d4"))
	provided = provided.WithSpec(provided.Spec().With("os", m("provided", m())))
	if reason, _ := PhysicalRootDeviceNameRefusal(catalog, provided); reason != "" {
		t.Fatalf("an OS-ready Machine's root deviceName, which only keeps it out of Ceph OSDs, refused: %s", reason)
	}
}

// One Machine naming its root by wwn alone validates as a Bootwright
// installation and as an agent node, so the same root declaration serves
// either installer (audit 004 API-38).
func TestOneWWNRootValidatesForEitherInstaller(t *testing.T) {
	installed, installedCatalog := installedFixture()
	_, agent, catalog := agentFixture(t)
	agent = rootDeclared(agent, "", installed.Spec().Get("hardware", "rootDisk"))
	if !installed.Spec().Has("hardware", "rootDisk", "wwn") || installed.Spec().Has("hardware", "rootDisk", "hints", "deviceName") {
		t.Fatalf("the installed fixture does not name its root by wwn alone: %v", installed.Spec().Get("hardware", "rootDisk"))
	}
	if issues := Validate(installed, installedCatalog); len(issues) != 0 {
		t.Fatalf("a Bootwright installation refused a wwn root: %v", issues)
	}
	if issues := Validate(agent, replaced(catalog, agent)); len(issues) != 0 {
		t.Fatalf("an agent node refused a wwn root: %v", issues)
	}
	if got := substrate.RootDevicePath(installedCatalog, installed); got != "/dev/disk/by-id/wwn-0x5000c500a1b2c3d4" {
		t.Fatalf("root device path = %q", got)
	}
}

func TestLogicalVolumeRootRefusalsNameTheirOwnField(t *testing.T) {
	original, _ := fixture()
	for field, physical := range map[string]api.Value{
		"$.spec.hardware.rootDisk.bootVolume": m("bootVolume", m()),
		"$.spec.hardware.rootDisk.volumeRef":  m("volumeRef", m("storageId", "s", "volumeId", "v")),
	} {
		candidate := rootDeclared(original, "", physical)
		issues := validateVolumeRoot(candidate, substrate.ArmBaremetal)
		if len(issues) != 1 || issues[0].Field != field || issues[0].Message != "logical-volume discovery requires verified BMC TLS" || !strings.Contains(issues[0].Remediation, "bmc.tls.verification") {
			t.Fatalf("%s: %+v", field, issues)
		}
		alone := rootDeclared(original, "", physical.With("wwn", api.StringValue("0x5000c500a1b2c3d4")))
		if issues := validateVolumeRoot(alone, substrate.ArmBaremetal); len(issues) != 1 || issues[0].Field != field {
			t.Fatalf("%s beside a wwn: %+v", field, issues)
		}
	}
}

func TestInstalledPhysicalRootRefusalNamesEveryForm(t *testing.T) {
	original, catalog := fixture()
	installed := rootDeclared(original, "", m())
	installed = installed.WithSpec(installed.Spec().With("os", m("install", m("profileRef", "rhel", "hostKeyRef", "host-key"))))
	issues := validateBaremetal(catalog, installed)
	if len(issues) != 1 || issues[0].Field != "$.spec.hardware.rootDisk" || !strings.Contains(issues[0].Remediation, "rootDisk.wwn") ||
		!strings.Contains(issues[0].Remediation, "its volumeRef") || !strings.Contains(issues[0].Remediation, "bootVolume") || !strings.Contains(issues[0].Remediation, "serialNumber and sizeBytes") {
		t.Fatalf("issues %+v", issues)
	}
}

func TestAVSphereMachineWithAManualMACIsNotBareMetalToTheRootRefusals(t *testing.T) {
	provider := object(api.InfraProvider, "vcenter", m("vsphere", m()))
	nics := list(m("name", "eth0", "macAddress", "00:50:56:00:00:01"))
	for _, os := range []api.Value{m("install", m("profileRef", "rhel")), m("clusterInstalled", m())} {
		vm := object(api.Machine, "vm-1", m("hardware", m("providerRef", "vcenter", "nics", nics, "rootDisk", m("hints", m("deviceName", "/dev/sda"))), "os", os))
		catalog := api.NewCatalog([]api.Object{provider, vm})
		if substrate.MachineArm(catalog, vm) != substrate.ArmVSphere {
			t.Fatal("the fixture is no vSphere Machine")
		}
		if reason, _ := PhysicalRootDeviceNameRefusal(catalog, vm); reason != "" {
			t.Errorf("a vSphere Machine's root deviceName refused as bare metal: %s", reason)
		}
		if reason, _ := AgentRootDeviceRefusal(catalog, vm); reason != "" {
			t.Errorf("a vSphere Machine's root refused as an agent bare-metal root: %s", reason)
		}
	}
	metal := object(api.InfraProvider, "vcenter", m("baremetal", m()))
	vm := object(api.Machine, "vm-1", m("hardware", m("providerRef", "vcenter", "nics", nics, "rootDisk", m("hints", m("deviceName", "/dev/sda"))), "os", m("clusterInstalled", m())))
	if reason, _ := PhysicalRootDeviceNameRefusal(api.NewCatalog([]api.Object{metal, vm}), vm); reason == "" {
		t.Fatal("a bare-metal Machine's root deviceName was admitted")
	}
}
