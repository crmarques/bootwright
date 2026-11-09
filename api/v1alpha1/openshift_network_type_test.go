package v1alpha1

import (
	"slices"
	"testing"
)

func TestNetworkTypeAdmitsOVNKubernetesAlone(t *testing.T) {
	networking, ok := openShiftClusterSchema().Field("networking")
	if !ok {
		t.Fatal("missing networking")
	}
	networkType, ok := networking.Shape.Field("networkType")
	if !ok || networkType.Required || networkType.Default.Present() {
		t.Fatal("networkType stays optional, leaving the installer default when absent")
	}
	if !slices.Equal(networkType.Shape.Enums, []string{"OVNKubernetes"}) {
		t.Fatalf("networkType enumerates %v; every networking rule reads OVN-Kubernetes", networkType.Shape.Enums)
	}
}
