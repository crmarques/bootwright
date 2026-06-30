package desiredstate

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

// TestMachineProfilesRejectForeignProviderFields covers F64: template and
// failureDomainRef drive only the vSphere adapter, and dataDisks are
// provisioned only by the libvirt and vsphere adapters. Authoring them on
// another provider must be rejected at validation rather than silently
// ignored (the MachinePoolSpec precedent).
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
				".dataDisks is not supported when type=kubevirt; only the libvirt and vsphere adapters provision data disks",
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
// failureDomainRef gets the standard dangling-reference error, dataDisks are
// accepted because the vSphere adapter provisions them, and an empty
// failureDomainRef is rejected only when several failure domains are
// declared (a single one resolves implicitly).
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
	if errs := validateMachineProfiles(prefix, v1alpha1.ProvisionerVSphere, []v1alpha1.MachineProfile{withDisks}, failureDomains); len(errs) != 0 {
		t.Fatalf("vSphere profile with dataDisks should be accepted, got %v", errs)
	}

	badDisk := valid
	badDisk.DataDisks = []v1alpha1.MachineProfileDisk{{Name: "", SizeGiB: 0}}
	errs = validateMachineProfiles(prefix, v1alpha1.ProvisionerVSphere, []v1alpha1.MachineProfile{badDisk}, failureDomains)
	joined := strings.Join(errs, "\n")
	for _, want := range []string{".dataDisks[0].name is required", ".dataDisks[0].sizeGiB must be greater than zero"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %v", want, errs)
		}
	}

	noRef := valid
	noRef.FailureDomainRef = v1alpha1.LocalObjectReference{}
	if errs := validateMachineProfiles(prefix, v1alpha1.ProvisionerVSphere, []v1alpha1.MachineProfile{noRef}, failureDomains); len(errs) != 0 {
		t.Fatalf("empty failureDomainRef with a single failure domain should be accepted, got %v", errs)
	}
	multiFD := map[string]bool{"dc1-zone-a": true, "dc1-zone-b": true}
	errs = validateMachineProfiles(prefix, v1alpha1.ProvisionerVSphere, []v1alpha1.MachineProfile{noRef}, multiFD)
	want = ".failureDomainRef is required when the provider declares multiple failureDomains"
	if !strings.Contains(strings.Join(errs, "\n"), want) {
		t.Fatalf("missing %q in %v", want, errs)
	}
}

// TestVSphereISOStagingValidation covers the isoStaging override block: an
// authored block must carry at least one override, and either field alone is
// enough because the other keeps its default.
func TestVSphereISOStagingValidation(t *testing.T) {
	base := v1alpha1.InfraProviderVSphere{
		VCenters: []v1alpha1.VSphereVCenter{{
			Server:         "vc.example.com",
			Datacenters:    []string{"dc1"},
			CredentialsRef: v1alpha1.SecretRef{Name: "vcenter-credentials"},
		}},
		FailureDomains: []v1alpha1.VSphereFailureDomain{{
			Name:   "dc1-zone-a",
			Region: "dc1",
			Zone:   "a",
			Server: "vc.example.com",
			Topology: v1alpha1.VSphereFailureTopology{
				Datacenter:     "dc1",
				ComputeCluster: "compute",
				Datastore:      "datastore1",
				Networks:       []string{"lab-portgroup"},
			},
		}},
	}
	if errs := validateProviderVSphere("spec.vsphere", &base); len(errs) != 0 {
		t.Fatalf("baseline vSphere spec should be accepted, got %v", errs)
	}

	empty := base
	empty.ISOStaging = &v1alpha1.VSphereISOStaging{}
	errs := validateProviderVSphere("spec.vsphere", &empty)
	want := "spec.vsphere.isoStaging must set at least one of {datastore, folder}"
	if !strings.Contains(strings.Join(errs, "\n"), want) {
		t.Fatalf("missing %q in %v", want, errs)
	}

	for _, staging := range []v1alpha1.VSphereISOStaging{{Datastore: "iso-datastore"}, {Folder: "isos"}} {
		withStaging := base
		withStaging.ISOStaging = &staging
		if errs := validateProviderVSphere("spec.vsphere", &withStaging); len(errs) != 0 {
			t.Fatalf("isoStaging %+v should be accepted, got %v", staging, errs)
		}
	}

	// A duplicate failure-domain name would break the single-domain
	// implicit failureDomainRef resolution, which counts declared entries.
	duplicated := base
	duplicated.FailureDomains = append(append([]v1alpha1.VSphereFailureDomain(nil), base.FailureDomains...), base.FailureDomains[0])
	errs = validateProviderVSphere("spec.vsphere", &duplicated)
	want = `.failureDomains[1].name "dc1-zone-a" is duplicated`
	if !strings.Contains(strings.Join(errs, "\n"), want) {
		t.Fatalf("missing %q in %v", want, errs)
	}
}

// TestBareMetalProviderBMCDefaults covers the provider-default BMC arm: a
// cert-only default (no credentialsRef) is accepted (the require is relaxed),
// and a virtualMedia.tls block must obey the same invariants as the per-machine
// one, reported against the provider path.
func TestBareMetalProviderBMCDefaults(t *testing.T) {
	mk := func(bmc *v1alpha1.BMCDefaults) v1alpha1.InfraProvider {
		return v1alpha1.InfraProvider{
			Metadata: v1alpha1.Metadata{Name: "bm"},
			Spec: v1alpha1.InfraProviderSpec{
				Type:      v1alpha1.ProvisionerBareMetal,
				BareMetal: &v1alpha1.InfraProviderBareMetal{Defaults: v1alpha1.BareMetalDefaultsSpec{BMC: bmc}},
			},
		}
	}
	noMachines := map[string]v1alpha1.Machine{}
	noClusters := map[string]v1alpha1.ContainerCluster{}

	verifyFalse := false
	certOnly := validateProviderSpec(mk(&v1alpha1.BMCDefaults{
		TLS:          &v1alpha1.BMCTLS{Verify: &verifyFalse},
		VirtualMedia: &v1alpha1.BMCVirtualMedia{TLS: &v1alpha1.BMCVirtualMediaTLS{ImportServerCertificate: true}},
	}), noMachines, noClusters)
	for _, bad := range []string{"defaults.bmc.credentialsRef", "virtualMedia.tls"} {
		if strings.Contains(strings.Join(certOnly, "\n"), bad) {
			t.Fatalf("cert-only provider BMC default must not error on %q: %v", bad, certOnly)
		}
	}

	badRemove := validateProviderSpec(mk(&v1alpha1.BMCDefaults{
		VirtualMedia: &v1alpha1.BMCVirtualMedia{TLS: &v1alpha1.BMCVirtualMediaTLS{RemoveServerCertificateAfterBoot: true}},
	}), noMachines, noClusters)
	want := "spec.bareMetal.defaults.bmc.virtualMedia.tls.removeServerCertificateAfterBoot requires importServerCertificate"
	if !strings.Contains(strings.Join(badRemove, "\n"), want) {
		t.Fatalf("missing %q in %v", want, badRemove)
	}
}
