package inventory

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestStorageClusterInstallProjectsPerInterfaceNetworkAttachments(t *testing.T) {
	attachments := []v1alpha1.MachineInterfaceAttachment{
		{Interface: "primary", AttachmentRef: v1alpha1.LocalObjectReference{Name: "machine"}},
		{Interface: "ceph-public", AttachmentRef: v1alpha1.LocalObjectReference{Name: "ceph-public"}},
	}
	state := v1alpha1.State{
		Machines: []v1alpha1.Machine{{
			Metadata: v1alpha1.Metadata{Name: "ceph-vm"},
			Spec: v1alpha1.MachineSpec{
				Substrate: v1alpha1.MachineSubstrate{ProviderRef: v1alpha1.LocalObjectReference{Name: "kubevirt"}},
				Network: v1alpha1.MachineNetwork{Config: v1alpha1.MachineNetworkConfig{
					NetworkConfigRef:     v1alpha1.LocalObjectReference{Name: "ceph-vm-net"},
					InterfaceAttachments: attachments,
				}},
			},
		}},
	}
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{{
				Name: "node-01", MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-vm"},
			}}},
		}},
	}
	install, ok := storageClusterInstall(state, cluster)
	if !ok || len(install.NetworkBindings) != 1 {
		t.Fatalf("storageClusterInstall = %+v, %v", install, ok)
	}
	binding := install.NetworkBindings[0]
	if len(binding.InterfaceAttachments) != 2 || binding.InterfaceAttachments[1].AttachmentRef.Name != "ceph-public" {
		t.Fatalf("projected interface attachments = %+v, want both bindings", binding.InterfaceAttachments)
	}
}
