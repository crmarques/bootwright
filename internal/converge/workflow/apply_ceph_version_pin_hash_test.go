package workflow

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func versionPinState(packageVersion, imageVersion string) v1alpha1.State {
	ceph := &v1alpha1.StorageClusterCephSpec{
		Distribution:   v1alpha1.StorageCephDistributionIBM,
		Release:        "9.9.1.0",
		PackageVersion: packageVersion,
	}
	if imageVersion != "" {
		ceph.Image = &v1alpha1.StorageCephImageSpec{Version: imageVersion}
	}
	return v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec:     v1alpha1.StorageClusterSpec{Type: v1alpha1.StorageClusterTypeCeph, Ceph: ceph},
	}}}
}

func hashJSON(t *testing.T, state v1alpha1.State) string {
	t.Helper()
	b, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

func TestCephPackageVersionIsReconcilableNotStructural(t *testing.T) {
	base := versionPinState("", "")
	bumped := versionPinState("19.2.1-245.el9cp", "")

	if hashJSON(t, storageClusterDesiredHashVars(base, "ceph")) == hashJSON(t, storageClusterDesiredHashVars(bumped, "ceph")) {
		t.Fatal("a packageVersion pin must change the desired hash so apply reconciles the cephadm build")
	}
	if hashJSON(t, storageClusterStructuralHashVars(base, "ceph")) != hashJSON(t, storageClusterStructuralHashVars(bumped, "ceph")) {
		t.Fatal("packageVersion must not change the storage structural hash; it pins the host cephadm RPM, not the daemons, so a bump must not propose a cluster rebuild")
	}
	if hashJSON(t, managedMachineOSStructuralHashVars(base, "ceph")) != hashJSON(t, managedMachineOSStructuralHashVars(bumped, "ceph")) {
		t.Fatal("packageVersion must not change the managed-OS structural hash; that kind is not reconfigure-only, so a bump would propose a machine reinstall")
	}
}

func TestCephadmAnsiblePackageVersionIsReconcilableNotStructural(t *testing.T) {
	base := versionPinState("", "")
	pinned := versionPinState("", "")
	pinned.StorageClusters[0].Spec.Ceph.Cephadm.Ansible = &v1alpha1.StorageCephadmAnsible{PackageVersion: "17.3.2-9.el14"}

	if hashJSON(t, storageClusterDesiredHashVars(base, "ceph")) == hashJSON(t, storageClusterDesiredHashVars(pinned, "ceph")) {
		t.Fatal("a cephadm-ansible package pin must change the desired hash so apply reconciles the provider artifact")
	}
	if hashJSON(t, storageClusterStructuralHashVars(base, "ceph")) != hashJSON(t, storageClusterStructuralHashVars(pinned, "ceph")) {
		t.Fatal("a cephadm-ansible package pin must not propose a Ceph cluster rebuild")
	}
	if hashJSON(t, managedMachineOSStructuralHashVars(base, "ceph")) != hashJSON(t, managedMachineOSStructuralHashVars(pinned, "ceph")) {
		t.Fatal("a cephadm-ansible package pin must not propose a managed-machine reinstall")
	}
}

func TestCephImageVersionIsStructural(t *testing.T) {
	base := versionPinState("", "")
	pinned := versionPinState("", "9.9.1.0-123")
	if hashJSON(t, storageClusterStructuralHashVars(base, "ceph")) == hashJSON(t, storageClusterStructuralHashVars(pinned, "ceph")) {
		t.Fatal("the daemon image version must be structural drift, matching release and image base")
	}
}

func TestCephVersionPinsOmittedWhenUnset(t *testing.T) {
	encoded := hashJSON(t, versionPinState("", ""))
	for _, key := range []string{"packageVersion", "ansible", "image"} {
		if strings.Contains(encoded, key) {
			t.Fatalf("an unset %s must not serialize; a bare tag false-drifts every already-recorded fleet on the first apply after upgrade: %s", key, encoded)
		}
	}
}
