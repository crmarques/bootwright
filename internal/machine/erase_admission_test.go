package machine

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/substrate"
)

func TestEraseAdmissionRetainsOmissionAndRequiresExplicitRootIdentity(t *testing.T) {
	machine, _ := fixture()
	machine = rootDeclared(machine, "/dev/sda", api.MapValue())
	machine = machine.WithSpec(machine.Spec().WithPath(api.StringValue("rhel"), "os", "installProfileRef"))
	machine = machine.WithSpec(machine.Spec().WithPath(api.BoolValue(true), "substrate", "baremetal", "bmc", "tls", "verify").WithPath(api.StringValue("established"), "substrate", "baremetal", "bmc", "virtualMedia", "tls", "trust"))
	if issues := validateErase(machine, substrate.ArmBaremetal); len(issues) != 0 {
		t.Fatal(issues)
	}
	target := m("deviceName", "/dev/sda", "wwn", "naa.example", "serialNumber", "EXAMPLE-01").With("sizeBytes", api.IntegerValue("8192"))
	policy := m("method", "logicalOverwrite", "targets", list(target))
	machine = machine.WithSpec(machine.Spec().WithPath(policy, "hardware", "eraseOnRemoval"))
	if issues := validateErase(machine, substrate.ArmBaremetal); len(issues) != 0 {
		t.Fatal(issues)
	}
	for _, variant := range []string{substrate.ArmLibvirt, substrate.ArmKubeVirt, ""} {
		if issues := validateErase(machine, variant); len(issues) == 0 {
			t.Fatalf("nonphysical %q admitted erase", variant)
		}
	}
	provided := machine.WithSpec(machine.Spec().WithPath(api.BoolValue(true), "os", "provided"))
	if issues := validateErase(provided, substrate.ArmBaremetal); len(issues) == 0 {
		t.Fatal("provided machine admitted erase")
	}
	for name, targets := range map[string]api.Value{
		"duplicate": list(target, target.With("deviceName", api.StringValue("/dev/sdb"))),
		"unaligned": list(target.With("sizeBytes", api.IntegerValue("8193"))),
		"empty WWN": list(target.With("wwn", api.StringValue(""))),
		"non ASCII": list(target.With("serialNumber", api.StringValue("é"))),
	} {
		t.Run(name, func(t *testing.T) {
			changed := machine.WithSpec(machine.Spec().WithPath(targets, "hardware", "eraseOnRemoval", "targets"))
			if issues := validateErase(changed, substrate.ArmBaremetal); len(issues) == 0 {
				t.Fatal("unsafe erase declaration admitted")
			}
		})
	}
}

func TestEraseAdmissionBoundsOnlyLogicalOverwriteTargetsTo896GiB(t *testing.T) {
	machine, _ := fixture()
	machine = rootDeclared(machine, "/dev/sda", api.MapValue())
	machine = machine.WithSpec(machine.Spec().WithPath(api.StringValue("rhel"), "os", "installProfileRef"))
	machine = machine.WithSpec(machine.Spec().WithPath(api.BoolValue(true), "substrate", "baremetal", "bmc", "tls", "verify").WithPath(api.StringValue("established"), "substrate", "baremetal", "bmc", "virtualMedia", "tls", "trust"))
	root := m("deviceName", "/dev/sda", "wwn", "naa.root", "serialNumber", "ROOT").With("sizeBytes", api.IntegerValue("8192"))
	data := m("deviceName", "/dev/sdb", "wwn", "naa.data", "serialNumber", "DATA")
	bound, over := api.IntegerValue("962072674304"), api.IntegerValue("962072678400")
	for name, test := range map[string]struct {
		policy  string
		target  api.Value
		refused bool
	}{
		"a logicalOverwrite disk of exactly 896 GiB":    {"logicalOverwrite", data.With("sizeBytes", bound), false},
		"a logicalOverwrite disk past 896 GiB":          {"logicalOverwrite", data.With("sizeBytes", over), true},
		"a policy-wide metadataClear disk past 896 GiB": {"metadataClear", data.With("sizeBytes", over), false},
		"a metadataClear override past 896 GiB":         {"logicalOverwrite", data.With("sizeBytes", over).With("method", api.StringValue("metadataClear")), false},
		"a logicalOverwrite override past 896 GiB":      {"metadataClear", data.With("sizeBytes", over).With("method", api.StringValue("logicalOverwrite")), true},
	} {
		t.Run(name, func(t *testing.T) {
			changed := machine.WithSpec(machine.Spec().WithPath(m("method", test.policy, "targets", list(root, test.target)), "hardware", "eraseOnRemoval"))
			issues := validateErase(changed, substrate.ArmBaremetal)
			if !test.refused {
				if len(issues) != 0 {
					t.Fatal(issues)
				}
				return
			}
			if len(issues) != 1 || issues[0].Field != "$.spec.hardware.eraseOnRemoval.targets[1].sizeBytes" || !strings.Contains(issues[0].Remediation, "metadataClear") {
				t.Fatalf("issues = %+v", issues)
			}
		})
	}
	if MaxLogicalOverwriteBytes != 896<<30 {
		t.Fatal("the logicalOverwrite bound moved without its spec")
	}
}

// A root disk declared by serial number and size is included by the erase
// target without deviceName that declares the same serial number and size; a
// target naming only one of deviceName and wwn refuses at the target, and a
// root target whose size is not the declared root size includes nothing.
func TestEraseAdmissionIncludesARootDeclaredBySerialNumberAndSize(t *testing.T) {
	machine, _ := fixture()
	machine = rootDeclared(machine, "/dev/sda", api.MapValue())
	spec := machine.Spec().WithPath(api.StringValue("rhel"), "os", "installProfileRef").
		WithPath(api.BoolValue(true), "substrate", "baremetal", "bmc", "tls", "verify").
		WithPath(api.StringValue("established"), "substrate", "baremetal", "bmc", "virtualMedia", "tls", "trust").
		WithPath(m("serialNumber", "SYNTHETIC-ROOT-01").With("sizeBytes", api.IntegerValue("480103981056")), "substrate", "baremetal", "rootDevice")
	machine = machine.WithSpec(spec.WithPath(spec.Get("hardware", "rootDevice").Without("deviceName"), "hardware", "rootDevice"))
	root := m("serialNumber", "SYNTHETIC-ROOT-01").With("sizeBytes", api.IntegerValue("480103981056"))
	data := m("deviceName", "/dev/disk/by-id/wwn-0x5000000000000001", "wwn", "0x5000000000000001", "serialNumber", "DATA").With("sizeBytes", api.IntegerValue("8192"))
	erase := func(targets ...api.Value) []api.Issue {
		changed := machine.WithSpec(machine.Spec().WithPath(m("method", "metadataClear", "targets", list(targets...)), "hardware", "eraseOnRemoval"))
		return validateErase(changed, substrate.ArmBaremetal)
	}
	if issues := erase(data, root); len(issues) != 0 {
		t.Fatalf("a root target named by serial number and size was refused: %v", issues)
	}
	for name, test := range map[string]struct {
		targets []api.Value
		field   string
	}{
		"another root size":           {[]api.Value{data, root.With("sizeBytes", api.IntegerValue("480103985152"))}, ".targets[1].sizeBytes"},
		"the root serial with a path": {[]api.Value{root.With("deviceName", api.StringValue("/dev/disk/by-id/wwn-0x5000000000000002")).With("wwn", api.StringValue("0x5000000000000002"))}, ".targets[0]"},
		"a lone deviceName":           {[]api.Value{root, data.Without("wwn")}, ".targets[1]"},
		"a lone wwn":                  {[]api.Value{root, data.Without("deviceName")}, ".targets[1]"},
		"a repeated serial":           {[]api.Value{root, data.With("serialNumber", api.StringValue("SYNTHETIC-ROOT-01"))}, ".targets[1].serialNumber"},
	} {
		t.Run(name, func(t *testing.T) {
			issues := erase(test.targets...)
			if len(issues) != 1 || issues[0].Field != "$.spec.hardware.eraseOnRemoval"+test.field {
				t.Fatalf("issues = %+v", issues)
			}
		})
	}
	if issues := erase(data); len(issues) != 0 {
		t.Fatalf("OSD-only erasure refused: %+v", issues)
	}

}

// A root disk declared by path and size is included only by its path's target
// of that size.
func TestEraseAdmissionComparesTheDeclaredRootSize(t *testing.T) {
	machine, _ := fixture()
	machine = rootDeclared(machine, "/dev/sda", api.MapValue())
	spec := machine.Spec().WithPath(api.StringValue("rhel"), "os", "installProfileRef").
		WithPath(api.BoolValue(true), "substrate", "baremetal", "bmc", "tls", "verify").
		WithPath(api.StringValue("established"), "substrate", "baremetal", "bmc", "virtualMedia", "tls", "trust").
		WithPath(api.IntegerValue("8192"), "substrate", "baremetal", "rootDevice", "sizeBytes")
	target := m("deviceName", "/dev/sda", "wwn", "naa.example", "serialNumber", "EXAMPLE-01")
	for size, included := range map[string]bool{"8192": true, "12288": false} {
		changed := machine.WithSpec(spec.WithPath(m("method", "metadataClear", "targets", list(target.With("sizeBytes", api.IntegerValue(size)))), "hardware", "eraseOnRemoval"))
		if issues := validateErase(changed, substrate.ArmBaremetal); (len(issues) == 0) != included {
			t.Fatalf("a %s-byte root target: issues %+v", size, issues)
		}
	}
	byWWN := rootDeclared(machine.WithSpec(spec), "", m("wwn", "0x5000000000000009").With("sizeBytes", api.IntegerValue("8192")))
	for _, device := range []string{"/dev/disk/by-id/wwn-0x5000000000000009", "/dev/sdz"} {
		for size, included := range map[string]bool{"8192": true, "12288": false} {
			target := m("deviceName", device, "wwn", "naa.5000000000000009", "serialNumber", "EXAMPLE-01").With("sizeBytes", api.IntegerValue(size))
			changed := byWWN.WithSpec(byWWN.Spec().WithPath(m("method", "metadataClear", "targets", list(target)), "hardware", "eraseOnRemoval"))
			if issues := validateErase(changed, substrate.ArmBaremetal); (len(issues) == 0) != included {
				t.Fatalf("a %s-byte root target at %s for a wwn root: issues %+v", size, device, issues)
			}
		}
	}
	withSerial := rootDeclared(machine.WithSpec(spec), "", m("wwn", "0x5000000000000009", "serialNumber", "EXAMPLE-ROOT").With("sizeBytes", api.IntegerValue("8192")))
	for size, included := range map[string]bool{"8192": true, "12288": false} {
		target := m("serialNumber", "EXAMPLE-ROOT").With("sizeBytes", api.IntegerValue(size))
		changed := withSerial.WithSpec(withSerial.Spec().WithPath(m("method", "metadataClear", "targets", list(target)), "hardware", "eraseOnRemoval"))
		if issues := validateErase(changed, substrate.ArmBaremetal); (len(issues) == 0) != included || !included && issues[0].Field != "$.spec.hardware.eraseOnRemoval.targets[0].sizeBytes" {
			t.Fatalf("a %s-byte target naming a wwn root's serial number: issues %+v", size, issues)
		}
	}
}

func TestErasePolicyListsTheRootOnlyWhenItNamesIt(t *testing.T) {
	const root = "/dev/disk/by-id/wwn-0x5000000000000009"
	data := m("deviceName", "/dev/disk/by-id/wwn-0x5000000000000001", "wwn", "0x5000000000000001", "serialNumber", "DATA").With("sizeBytes", api.IntegerValue("8192"))
	rootTarget := m("deviceName", root, "wwn", "0x5000000000000009", "serialNumber", "ROOT").With("sizeBytes", api.IntegerValue("8192"))
	for name, declare := range map[string]func(api.Object) api.Object{
		"wwn":                    func(o api.Object) api.Object { return rootDeclared(o, "", m("wwn", "0x5000000000000009")) },
		"wwn under another path": func(o api.Object) api.Object { return rootDeclared(o, "", m("wwn", "0x5000000000000009")) },
		"serial": func(o api.Object) api.Object {
			return rootDeclared(o, "", m("serialNumber", "ROOT").With("sizeBytes", api.IntegerValue("8192")))
		},
		"volumeRef": func(o api.Object) api.Object {
			return rootDeclared(o, "", m("volumeRef", m("storageId", "s", "volumeId", "v")))
		},
		"bootVolume": func(o api.Object) api.Object { return rootDeclared(o, "", m("bootVolume", m())) },
	} {
		t.Run(name, func(t *testing.T) {
			machine, _ := fixture()
			machine = declare(machine)
			machine = machine.WithSpec(machine.Spec().WithPath(api.StringValue("rhel"), "os", "installProfileRef").
				WithPath(api.BoolValue(true), "substrate", "baremetal", "bmc", "tls", "verify").
				WithPath(api.StringValue("established"), "substrate", "baremetal", "bmc", "virtualMedia", "tls", "trust").
				WithPath(m("method", "metadataClear", "targets", list(data)), "hardware", "eraseOnRemoval"))
			if issues := validateErase(machine, substrate.ArmBaremetal); len(issues) != 0 {
				t.Fatalf("a policy without a root target was refused: %+v", issues)
			}
			if RootEraseSelected(machine) {
				t.Fatal("a policy without a root target selected the root")
			}
			target := rootTarget
			if name == "serial" {
				target = target.Without("deviceName").Without("wwn")
			}
			if name == "wwn under another path" {
				target = target.With("deviceName", api.StringValue("/dev/sdz")).With("wwn", api.StringValue("naa.5000000000000009"))
			}
			named := machine.WithSpec(machine.Spec().WithPath(list(data, target), "hardware", "eraseOnRemoval", "targets"))
			if issues := validateErase(named, substrate.ArmBaremetal); len(issues) != 0 {
				t.Fatalf("a policy naming its root was refused: %+v", issues)
			}
			if selected := RootEraseSelected(named); selected != (name == "wwn" || name == "wwn under another path" || name == "serial") {
				t.Fatalf("root selected %v", selected)
			}
		})
	}
}
