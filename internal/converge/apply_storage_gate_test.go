package converge

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func managedCephCluster(name string) v1alpha1.StorageCluster {
	return v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.StorageClusterSpec{
			Type:       v1alpha1.StorageClusterTypeCeph,
			Management: v1alpha1.StorageClusterManagementManaged,
			Ceph:       &v1alpha1.StorageClusterCephSpec{Distribution: v1alpha1.StorageCephDistributionOSS},
		},
	}
}

func externalCephCluster(name string) v1alpha1.StorageCluster {
	return v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.StorageClusterSpec{
			Type:       v1alpha1.StorageClusterTypeCeph,
			Management: v1alpha1.StorageClusterManagementExternal,
			Ceph:       &v1alpha1.StorageClusterCephSpec{Distribution: v1alpha1.StorageCephDistributionOSS},
		},
	}
}

func TestOverrideStorageDeviceGateApplies(t *testing.T) {
	managed := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{managedCephCluster("ceph1")}}
	external := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{externalCephCluster("ext1")}}

	if !OverrideStorageDeviceGateApplies(false, nil, managed) {
		t.Fatal("unscoped run with a managed Ceph cluster must arm the device gate")
	}
	if OverrideStorageDeviceGateApplies(false, nil, external) {
		t.Fatal("an external-only estate must not arm the device gate")
	}
	if OverrideStorageDeviceGateApplies(true, map[string]bool{}, managed) {
		t.Fatal("a container-only selection carrying a DF-referenced managed cluster it cannot touch must not arm the gate")
	}
	if !OverrideStorageDeviceGateApplies(true, map[string]bool{"ceph1": true}, managed) {
		t.Fatal("a selection naming the managed storage root must arm the gate")
	}
	if OverrideStorageDeviceGateApplies(true, map[string]bool{"ext1": true}, external) {
		t.Fatal("a selection naming an external storage root must not arm the gate")
	}
}
