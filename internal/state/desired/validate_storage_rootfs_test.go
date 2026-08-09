package desiredstate

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

func storageRootProfileState(providerType string, diskGiB int) (v1alpha1.State, v1alpha1.StorageCluster) {
	profile := v1alpha1.MachineProfile{Name: "ceph-node", DiskGiB: diskGiB}
	provider := v1alpha1.InfraProvider{
		Metadata: v1alpha1.Metadata{Name: "virt"},
		Spec:     v1alpha1.InfraProviderSpec{Type: providerType},
	}
	switch providerType {
	case v1alpha1.ProvisionerLibvirt:
		provider.Spec.Libvirt = &v1alpha1.InfraProviderLibvirt{MachineProfiles: []v1alpha1.MachineProfile{profile}}
	case v1alpha1.ProvisionerVSphere:
		provider.Spec.VSphere = &v1alpha1.InfraProviderVSphere{MachineProfiles: []v1alpha1.MachineProfile{profile}}
	case v1alpha1.ProvisionerKubeVirt:
		provider.Spec.KubeVirt = &v1alpha1.InfraProviderKubeVirt{MachineProfiles: []v1alpha1.MachineProfile{profile}}
	case v1alpha1.ProvisionerBareMetal:
		provider.Spec.BareMetal = &v1alpha1.InfraProviderBareMetal{}
	}
	machine := v1alpha1.Machine{
		Metadata: v1alpha1.Metadata{Name: "node-0"},
		Spec: v1alpha1.MachineSpec{Substrate: v1alpha1.MachineSubstrate{
			ProviderRef: v1alpha1.LocalObjectReference{Name: "virt"},
			ProfileRef:  v1alpha1.LocalObjectReference{Name: "ceph-node"},
		}},
	}
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{{
				Name:       "node-0",
				MachineRef: v1alpha1.LocalObjectReference{Name: "node-0"},
				Roles:      []string{v1alpha1.StorageCephRoleMON},
			}},
			}}},
	}
	return v1alpha1.State{
		Machines:        []v1alpha1.Machine{machine},
		InfraProviders:  []v1alpha1.InfraProvider{provider},
		StorageClusters: []v1alpha1.StorageCluster{cluster},
	}, cluster
}

func TestValidateStorageCephRootFilesystemProfilesRejectsBelowAbsoluteFloor(t *testing.T) {
	for _, providerType := range []string{
		v1alpha1.ProvisionerLibvirt,
		v1alpha1.ProvisionerVSphere,
		v1alpha1.ProvisionerKubeVirt,
	} {
		t.Run(providerType, func(t *testing.T) {
			state, cluster := storageRootProfileState(providerType, topology.RootFilesystemFloorGiB-1)
			errs := validateStorageCephRootFilesystemProfiles(state, cluster)
			if len(errs) != 1 {
				t.Fatalf("root profile validation errors = %v, want one", errs)
			}
			for _, want := range []string{
				"StorageCluster/ceph",
				"Machine/node-0",
				"InfraProvider/virt",
				"profile \"ceph-node\"",
				"diskGiB 19",
				"absolute Ceph root-filesystem floor of 20 GiB",
				"requires 20 GiB free",
				"before apply",
			} {
				if !strings.Contains(errs[0], want) {
					t.Errorf("root profile refusal missing %q: %s", want, errs[0])
				}
			}
		})
	}
}

func TestValidateStorageCephRootFilesystemProfilesAcceptsFloorAndNonProfiledHardware(t *testing.T) {
	state, cluster := storageRootProfileState(v1alpha1.ProvisionerLibvirt, topology.RootFilesystemFloorGiB)
	if errs := validateStorageCephRootFilesystemProfiles(state, cluster); len(errs) != 0 {
		t.Fatalf("profile at the absolute floor must defer free-space truth to live preflight, got %v", errs)
	}
	state, cluster = storageRootProfileState(v1alpha1.ProvisionerBareMetal, 0)
	if errs := validateStorageCephRootFilesystemProfiles(state, cluster); len(errs) != 0 {
		t.Fatalf("bare metal has no declared root-disk capacity to validate, got %v", errs)
	}
	state, cluster = storageRootProfileState(v1alpha1.ProvisionerKubeVirt, 0)
	if errs := validateStorageCephRootFilesystemProfiles(state, cluster); len(errs) != 0 {
		t.Fatalf("an omitted KubeVirt diskGiB resolves to the adapter's %d GiB default, got %v", topology.KubeVirtDefaultDiskGiB, errs)
	}
}
