package cli

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestValidateScopedApplySharedServicesFailsForInfraScope(t *testing.T) {
	state := cliStateWithSharedDNS()

	err := validateScopedApplySharedServices(state, "infra", "cluster-a")
	if err == nil {
		t.Fatal("expected shared service conflict, got nil")
	}
	if !strings.Contains(err.Error(), "--scope would narrow shared provider service") {
		t.Fatalf("error %q does not explain scoped apply conflict", err)
	}
	if !strings.Contains(err.Error(), "unscoped {cluster-b}") {
		t.Fatalf("error %q does not list unscoped consumer", err)
	}
}

func TestValidateScopedApplySharedServicesFailsForInfraAndAllSharedKinds(t *testing.T) {
	state := cliStateWithAllSharedProviderServices()
	for _, target := range []string{"infra", "all"} {
		t.Run(target, func(t *testing.T) {
			err := validateScopedApplySharedServices(state, target, "cluster-a")
			if err == nil {
				t.Fatal("expected shared service conflict, got nil")
			}
			for _, fragment := range []string{
				"artifacts InfraComponent/artifact-server",
				"loadBalancer services/default",
				"nameResolution services/default",
				"proxy services/default",
				"registry services/default",
			} {
				if !strings.Contains(err.Error(), fragment) {
					t.Fatalf("%s error %q missing %q", target, err, fragment)
				}
			}
		})
	}
}

func TestValidateScopedApplySharedServicesAllowsClusterScope(t *testing.T) {
	if err := validateScopedApplySharedServices(cliStateWithSharedDNS(), "cluster", "cluster-a"); err != nil {
		t.Fatalf("cluster apply scope should not validate provider services: %v", err)
	}
}

func TestValidateScopedApplySharedServicesAllowsAllConsumers(t *testing.T) {
	if err := validateScopedApplySharedServices(cliStateWithSharedDNS(), "infra", "cluster-a,cluster-b"); err != nil {
		t.Fatalf("infra apply with all consumers should validate: %v", err)
	}
}

func cliStateWithSharedDNS() v1alpha1.State {
	return v1alpha1.State{
		Hosts: []v1alpha1.Host{{
			Metadata: v1alpha1.Metadata{Name: "service-host"},
			Spec: v1alpha1.HostSpec{
				Addresses:    []v1alpha1.HostAddress{{Name: "ssh", Address: "10.0.0.5"}},
				SSH:          &v1alpha1.HostSSHSpec{AddressName: "ssh"},
				Capabilities: []string{v1alpha1.HostCapabilityContainerRuntime},
			},
		}},
		InfraProviders: []v1alpha1.InfraProvider{{
			Metadata: v1alpha1.Metadata{Name: "services"},
			Spec: v1alpha1.InfraProviderSpec{
				DNS: []v1alpha1.DNSCapability{{
					Name:    "default",
					Dnsmasq: &v1alpha1.DnsmasqCapability{HostRef: v1alpha1.LocalObjectReference{Name: "service-host"}},
				}},
			},
		}},
		ClusterInfras: []v1alpha1.ClusterInfra{
			cliClusterInfraWithDNS("infra-a", "machines-a"),
			cliClusterInfraWithDNS("infra-b", "machines-b"),
		},
		ContainerClusters: []v1alpha1.ContainerCluster{
			cliContainerCluster("cluster-a", "infra-a"),
			cliContainerCluster("cluster-b", "infra-b"),
		},
	}
}

func cliStateWithAllSharedProviderServices() v1alpha1.State {
	state := cliStateWithSharedDNS()
	state.Environments = []v1alpha1.Environment{{
		Metadata: v1alpha1.Metadata{Name: "env"},
		Spec: v1alpha1.EnvironmentSpec{
			ArtifactServer: &v1alpha1.EnvironmentArtifactServerSpec{
				ComponentRef: v1alpha1.LocalObjectReference{Name: "artifact-server"},
				Routes: v1alpha1.EnvironmentArtifactRoutes{
					ClusterInstall: v1alpha1.EnvironmentArtifactRoute{Endpoint: "cluster"},
				},
			},
		},
	}}
	state.InfraComponents = []v1alpha1.InfraComponent{cliArtifactServerComponent()}
	state.InfraProviders[0].Spec.LoadBalancers = []v1alpha1.LoadBalancerCapability{{
		Name:    "default",
		HAProxy: &v1alpha1.HAProxyCapability{HostRef: v1alpha1.LocalObjectReference{Name: "service-host"}},
	}}
	state.InfraProviders[0].Spec.Proxies = []v1alpha1.ProxyCapability{{
		Name:  "default",
		Squid: &v1alpha1.SquidCapability{HostRef: v1alpha1.LocalObjectReference{Name: "service-host"}},
	}}
	state.InfraProviders[0].Spec.Registries = []v1alpha1.RegistryCapability{{
		Name:           "default",
		MirrorRegistry: &v1alpha1.MirrorRegistryCapability{HostRef: v1alpha1.LocalObjectReference{Name: "service-host"}},
	}}
	for i := range state.ClusterInfras {
		state.ClusterInfras[i].Spec.Components.LoadBalancers = []v1alpha1.ClusterLoadBalancerComponent{{
			Name: "default",
			From: v1alpha1.From{Provider: "services", Name: "default"},
		}}
		state.ClusterInfras[i].Spec.Components.Proxy = &v1alpha1.ClusterComponentRef{
			From: v1alpha1.From{Provider: "services", Name: "default"},
		}
		state.ClusterInfras[i].Spec.Components.Registry = &v1alpha1.ClusterComponentRef{
			From: v1alpha1.From{Provider: "services", Name: "default"},
		}
	}
	for i := range state.ContainerClusters {
		state.ContainerClusters[i].Spec.Install.Mode = v1alpha1.InstallModeDisconnected
	}
	return state
}

func cliArtifactServerComponent() v1alpha1.InfraComponent {
	return v1alpha1.InfraComponent{
		Metadata: v1alpha1.Metadata{Name: "artifact-server"},
		Spec: v1alpha1.InfraComponentSpec{
			ArtifactServer: &v1alpha1.ArtifactServerComponent{
				HostRef: v1alpha1.LocalObjectReference{Name: "service-host"},
				Listeners: []v1alpha1.ArtifactServerListener{{
					Name:     "https",
					Protocol: v1alpha1.ArtifactServerProtocolHTTPS,
					Port:     v1alpha1.DefaultArtifactsHTTPPort,
				}},
				Endpoints: []v1alpha1.ArtifactServerEndpoint{{
					Name:        "cluster",
					Listener:    "https",
					AddressName: "ssh",
				}},
			},
		},
	}
}

func cliClusterInfraWithDNS(name, machineProvider string) v1alpha1.ClusterInfra {
	return v1alpha1.ClusterInfra{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.ClusterInfraSpec{Components: v1alpha1.ClusterComponents{
			Machines: []v1alpha1.ClusterMachineComponent{{
				Name: "master-0",
				From: v1alpha1.From{Provider: machineProvider, Name: "node-0"},
			}},
			NameResolution: &v1alpha1.ClusterNameResolutionComponent{
				From: v1alpha1.From{Provider: "services", Name: "default"},
			},
		}},
	}
}

func cliContainerCluster(name, infraName string) v1alpha1.ContainerCluster {
	return v1alpha1.ContainerCluster{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.ContainerClusterSpec{Nodes: []v1alpha1.OCPNodeSpec{{
			Hostname: "master-0",
			Role:     "master",
			MachineRef: v1alpha1.NodeMachineRef{
				ClusterInfra: infraName,
				Name:         "master-0",
			},
		}}},
	}
}
