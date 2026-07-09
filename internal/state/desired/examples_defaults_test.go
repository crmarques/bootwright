package desiredstate

import (
	"path/filepath"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestSNOLibvirtExampleMaterializesDefaults(t *testing.T) {
	state, err := LoadNormalizeValidate([]string{filepath.Join("..", "..", "..", "examples", "sno-libvirt-redfish")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}

	var cluster v1alpha1.ContainerCluster
	found := false
	for _, candidate := range state.ContainerClusters {
		if candidate.Metadata.Name == "sno-libvirt" {
			cluster = candidate
			found = true
		}
	}
	if !found {
		t.Fatal("ContainerCluster/sno-libvirt not loaded")
	}

	if got := cluster.Spec.Distribution.Type; got != v1alpha1.DistributionOpenShift {
		t.Fatalf("distribution.type = %q, want %q", got, v1alpha1.DistributionOpenShift)
	}

	apiInt, ok := cluster.Spec.Install.Endpoints[v1alpha1.EndpointAPIInt]
	if !ok {
		t.Fatal("api-int endpoint was not materialized from api")
	}
	if apiInt.Address != "192.168.132.20" || apiInt.Source.Type != v1alpha1.EndpointSourceExternal {
		t.Fatalf("api-int endpoint = %+v, want address 192.168.132.20 source external", apiInt)
	}

	platform := cluster.Spec.Install.Platform
	if platform.Type != v1alpha1.PlatformTypeBareMetal {
		t.Fatalf("platform.type = %q, want %q", platform.Type, v1alpha1.PlatformTypeBareMetal)
	}
	if platform.BareMetal == nil || platform.BareMetal.ProvisioningNetwork != v1alpha1.ProvisioningNetworkDisabled {
		t.Fatalf("platform.baremetal = %+v, want provisioningNetwork disabled", platform.BareMetal)
	}

	networking := cluster.Spec.Networking
	if networking == nil {
		t.Fatal("spec.networking was not materialized")
	}
	if len(networking.ClusterNetwork) != 1 ||
		networking.ClusterNetwork[0].CIDR != v1alpha1.DefaultClusterNetworkCIDR ||
		networking.ClusterNetwork[0].HostPrefix != v1alpha1.DefaultClusterNetworkHostPrefix {
		t.Fatalf("clusterNetwork = %+v, want stock default", networking.ClusterNetwork)
	}
	if len(networking.ServiceNetwork) != 1 || networking.ServiceNetwork[0] != v1alpha1.DefaultServiceNetworkCIDR {
		t.Fatalf("serviceNetwork = %+v, want stock default", networking.ServiceNetwork)
	}

	machine, ok := func() (v1alpha1.Machine, bool) {
		for _, m := range state.Machines {
			if m.Metadata.Name == "sno-libvirt-master-0" {
				return m, true
			}
		}
		return v1alpha1.Machine{}, false
	}()
	if !ok {
		t.Fatal("Machine/sno-libvirt-master-0 not loaded")
	}
	if got := machine.Spec.Network.Config.AttachmentRef.Name; got != "sno-bridge" {
		t.Fatalf("attachmentRef = %q, want sno-bridge (defaulted from networkConfigRef)", got)
	}
}
