package desiredstate

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

// TestMachineProfilesRejectForeignProviderFields covers F64: template and
// failureDomainRef drive only the vSphere adapter, and dataDisks are
// provisioned only by the libvirt adapter. Authoring them on another provider
// must be rejected at validation rather than silently ignored (the
// MachinePoolSpec precedent).
func TestMachineProfilesRejectForeignProviderFields(t *testing.T) {
	profile := v1alpha1.MachineProfile{
		Name:             "p",
		Template:         "rhcos",
		FailureDomainRef: v1alpha1.LocalObjectReference{Name: "dc1-zone-a"},
		DataDisks:        []v1alpha1.MachineProfileDisk{{Name: "data", SizeGiB: 10}},
	}
	cases := []struct {
		providerType string
		want         []string
		wantAbsent   []string
	}{
		{
			providerType: v1alpha1.ProvisionerLibvirt,
			want: []string{
				".template is not supported when type=libvirt; only the vsphere adapter clones machines from a template",
				".failureDomainRef is not supported when type=libvirt; failure domains exist only on vsphere providers",
			},
			wantAbsent: []string{".dataDisks is not supported"},
		},
		{
			providerType: v1alpha1.ProvisionerKubeVirt,
			want: []string{
				".template is not supported when type=kubevirt",
				".failureDomainRef is not supported when type=kubevirt",
				".dataDisks is not supported when type=kubevirt; only the libvirt adapter provisions data disks",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.providerType, func(t *testing.T) {
			prefix := "InfraProvider/p spec." + tc.providerType + ".machineProfiles"
			errs := validateMachineProfiles(prefix, tc.providerType, []v1alpha1.MachineProfile{profile}, nil)
			joined := strings.Join(errs, "\n")
			for _, want := range tc.want {
				if !strings.Contains(joined, want) {
					t.Fatalf("missing %q in %v", want, errs)
				}
			}
			for _, absent := range tc.wantAbsent {
				if strings.Contains(joined, absent) {
					t.Fatalf("unexpected %q in %v", absent, errs)
				}
			}
		})
	}
}

// TestVSphereMachineProfileFields covers F24/F18/F34/F64 on the vSphere arm:
// template plus a resolving failureDomainRef are accepted, a dangling
// failureDomainRef gets the standard dangling-reference error, and dataDisks
// are rejected because the vSphere adapter does not provision them.
func TestVSphereMachineProfileFields(t *testing.T) {
	prefix := "InfraProvider/v spec.vsphere.machineProfiles"
	failureDomains := map[string]bool{"dc1-zone-a": true}
	valid := v1alpha1.MachineProfile{
		Name:             "control-plane",
		Template:         "rhcos",
		FailureDomainRef: v1alpha1.LocalObjectReference{Name: "dc1-zone-a"},
	}
	if errs := validateMachineProfiles(prefix, v1alpha1.ProvisionerVSphere, []v1alpha1.MachineProfile{valid}, failureDomains); len(errs) != 0 {
		t.Fatalf("valid vSphere profile should be accepted, got %v", errs)
	}

	dangling := valid
	dangling.FailureDomainRef = v1alpha1.LocalObjectReference{Name: "dc1-zone-b"}
	errs := validateMachineProfiles(prefix, v1alpha1.ProvisionerVSphere, []v1alpha1.MachineProfile{dangling}, failureDomains)
	want := `.failureDomainRef "dc1-zone-b" does not match any failureDomains[].name`
	if !strings.Contains(strings.Join(errs, "\n"), want) {
		t.Fatalf("missing %q in %v", want, errs)
	}

	withDisks := valid
	withDisks.DataDisks = []v1alpha1.MachineProfileDisk{{Name: "data", SizeGiB: 10}}
	errs = validateMachineProfiles(prefix, v1alpha1.ProvisionerVSphere, []v1alpha1.MachineProfile{withDisks}, failureDomains)
	want = ".dataDisks is not supported when type=vsphere"
	if !strings.Contains(strings.Join(errs, "\n"), want) {
		t.Fatalf("missing %q in %v", want, errs)
	}
}
