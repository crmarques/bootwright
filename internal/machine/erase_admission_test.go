package machine

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/substrate"
)

func TestEraseAdmissionRetainsOmissionAndRequiresExplicitRootIdentity(t *testing.T) {
	machine, _ := fixture()
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
		"missing root": list(target.With("deviceName", api.StringValue("/dev/sdb"))),
		"duplicate":    list(target, target.With("deviceName", api.StringValue("/dev/sdb"))),
		"unaligned":    list(target.With("sizeBytes", api.IntegerValue("8193"))),
		"empty WWN":    list(target.With("wwn", api.StringValue(""))),
		"non ASCII":    list(target.With("serialNumber", api.StringValue("é"))),
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
