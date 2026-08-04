package workflow

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func ibmPackagesState(packages *v1alpha1.StorageCephIBMPackagesSpec) v1alpha1.State {
	return v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec: v1alpha1.StorageClusterSpec{Type: v1alpha1.StorageClusterTypeCeph, Ceph: &v1alpha1.StorageClusterCephSpec{
			Distribution: v1alpha1.StorageCephDistributionIBM,
			Release:      "9.9.1.0",
			IBM:          &v1alpha1.StorageCephIBMSpec{CallHome: v1alpha1.StorageCephIBMCallHomeDisabled, Packages: packages},
		}},
	}}}
}

func TestCephIBMPackagesIsReconcilableNotStructural(t *testing.T) {
	base := ibmPackagesState(nil)
	flipped := ibmPackagesState(&v1alpha1.StorageCephIBMPackagesSpec{
		Source:            v1alpha1.StorageCephIBMPackageSourceSubscription,
		SubscriptionRepos: []string{"Org_IBM_ibm-storage-ceph-9"},
	})

	if hashJSON(t, storageClusterDesiredHashVars(base, "ceph")) == hashJSON(t, storageClusterDesiredHashVars(flipped, "ceph")) {
		t.Fatal("an ibm packages source flip must change the desired hash so apply reconciles the repository setup")
	}
	if hashJSON(t, storageClusterStructuralHashVars(base, "ceph")) != hashJSON(t, storageClusterStructuralHashVars(flipped, "ceph")) {
		t.Fatal("ibm packages must not change the storage structural hash; the flip reconfigures host repositories, so it must not propose a cluster rebuild")
	}
	if hashJSON(t, managedMachineOSStructuralHashVars(base, "ceph")) != hashJSON(t, managedMachineOSStructuralHashVars(flipped, "ceph")) {
		t.Fatal("ibm packages must not change the managed-OS structural hash; that kind is not reconfigure-only, so a flip would propose a machine reinstall")
	}
}

func TestCephIBMPackagesOmittedWhenUnset(t *testing.T) {
	encoded := hashJSON(t, ibmPackagesState(nil))
	if strings.Contains(encoded, "packages") {
		t.Fatalf("an unset ibm packages block must not serialize; a bare key false-drifts every already-recorded fleet on the first apply after upgrade: %s", encoded)
	}
}
