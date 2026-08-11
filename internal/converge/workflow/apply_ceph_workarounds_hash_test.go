package workflow

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestCephadmWorkaroundsDoNotChangeStructuralHashes(t *testing.T) {
	base := versionPinState("", "")
	authored := versionPinState("", "")
	authored.StorageClusters[0].Spec.Ceph.Cephadm.Workarounds = []string{
		v1alpha1.StorageCephadmWorkaroundMgmtGatewaySpecDependencyRecording,
	}
	if hashJSON(t, storageClusterStructuralHashVars(base, "ceph")) != hashJSON(t, storageClusterStructuralHashVars(authored, "ceph")) {
		t.Fatal("cephadm workaround metadata must not change the storage structural hash; selecting it validates an authored safe shape and must not propose a cluster rebuild")
	}
	if hashJSON(t, managedMachineOSStructuralHashVars(base, "ceph")) != hashJSON(t, managedMachineOSStructuralHashVars(authored, "ceph")) {
		t.Fatal("cephadm workaround metadata must not change the managed-OS structural hash; it reaches no machine installation and must not propose an OS reinstall")
	}
}
