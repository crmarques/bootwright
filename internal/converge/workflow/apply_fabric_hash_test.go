package workflow

import (
	"encoding/json"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestOCPStructuralHashIgnoresFabricEdits(t *testing.T) {
	proj := func(s v1alpha1.State) string {
		b, err := json.Marshal(containerClusterInstallStructuralHashVars(s))
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return string(b)
	}
	withFabric := v1alpha1.State{
		ContainerClusters: []v1alpha1.ContainerCluster{{
			Metadata: v1alpha1.Metadata{Name: "ocp"},
			Spec:     v1alpha1.ContainerClusterSpec{Nodes: []v1alpha1.OCPNodeSpec{{MachineRef: v1alpha1.LocalObjectReference{Name: "n0"}}}},
		}},
		InfraProviders:  []v1alpha1.InfraProvider{{Metadata: v1alpha1.Metadata{Name: "bm"}, Spec: v1alpha1.InfraProviderSpec{Type: v1alpha1.ProvisionerBareMetal}}},
		InfraComponents: []v1alpha1.InfraComponent{{Metadata: v1alpha1.Metadata{Name: "artifacts"}}},
		NetworkConfigs:  []v1alpha1.NetworkConfig{{Metadata: v1alpha1.Metadata{Name: "net"}}},
	}
	noFabric := v1alpha1.State{
		ContainerClusters: withFabric.ContainerClusters,
	}
	if proj(withFabric) != proj(noFabric) {
		t.Fatal("shared fabric (provider/component/network) must not contribute to the OCP install structural hash")
	}

	hostAdded := v1alpha1.State{
		ContainerClusters: []v1alpha1.ContainerCluster{{
			Metadata: v1alpha1.Metadata{Name: "ocp"},
			Spec: v1alpha1.ContainerClusterSpec{Nodes: []v1alpha1.OCPNodeSpec{
				{MachineRef: v1alpha1.LocalObjectReference{Name: "n0"}},
				{MachineRef: v1alpha1.LocalObjectReference{Name: "n1"}},
			}},
		}},
	}
	if proj(hostAdded) == proj(noFabric) {
		t.Fatal("a change to the cluster's own host set MUST move the structural hash")
	}
}
