package clusteraccess

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestValidateScopedApplySharedServicesFailsForInfraScope(t *testing.T) {
	state := cliStateWithSharedDNS()

	err := ValidateScopedApplySharedServices(state, "infra", "cluster-a")
	if err == nil {
		t.Fatal("expected shared service conflict, got nil")
	}
	if !strings.Contains(err.Error(), "--clusters would narrow shared machine service") {
		t.Fatalf("error %q does not explain scoped apply conflict", err)
	}
	if !strings.Contains(err.Error(), "unscoped {cluster-b}") {
		t.Fatalf("error %q does not list unscoped consumer", err)
	}
}

func TestValidateScopedApplySharedServicesFailsForInfraClustersAndAllSharedKinds(t *testing.T) {
	state := cliStateWithAllSharedMachineServices()
	for _, target := range []string{"infra", "all"} {
		t.Run(target, func(t *testing.T) {
			err := ValidateScopedApplySharedServices(state, target, "cluster-a")
			if err == nil {
				t.Fatal("expected shared service conflict, got nil")
			}
			for _, fragment := range []string{
				"artifactServer InfraComponent/artifact-server",
				"loadBalancer InfraComponent/load-balancer",
				"nameResolution InfraComponent/name-resolution",
				"ntp InfraComponent/ntp-server",
				"proxy InfraComponent/proxy",
				"registry InfraComponent/registry",
			} {
				if !strings.Contains(err.Error(), fragment) {
					t.Fatalf("%s error %q missing %q", target, err, fragment)
				}
			}
		})
	}
}

func TestValidateScopedApplySharedServicesAllowsContainerClusterScope(t *testing.T) {
	if err := ValidateScopedApplySharedServices(cliStateWithSharedDNS(), "container-cluster", "cluster-a"); err != nil {
		t.Fatalf("container-cluster apply scope should not validate machine services: %v", err)
	}
}

func TestValidateScopedApplySharedServicesAllowsAllConsumers(t *testing.T) {
	if err := ValidateScopedApplySharedServices(cliStateWithSharedDNS(), "infra", "cluster-a,cluster-b"); err != nil {
		t.Fatalf("infra apply with all consumers should validate: %v", err)
	}
}

func cliStateWithSharedDNS() v1alpha1.State {
	return v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Metadata: v1alpha1.Metadata{Name: "env"},
			Spec: v1alpha1.EnvironmentSpec{
				BaseDomain: "example.test",
				InfraComponents: v1alpha1.EnvironmentInfraComponentsSpec{
					NameResolution: []v1alpha1.EnvironmentNameResolutionComponent{{
						Name:         "default",
						Management:   v1alpha1.EnvironmentComponentManaged,
						ComponentRef: v1alpha1.LocalObjectReference{Name: "name-resolution"},
					}},
				},
			},
		}},
		Machines: []v1alpha1.Machine{
			cliServiceMachine(),
			cliRawMachine("cluster-a-master-0", "machines-a", "net-a"),
			cliRawMachine("cluster-b-master-0", "machines-b", "net-b"),
		},
		NetworkConfigs: []v1alpha1.NetworkConfig{
			cliNetworkConfig("net-a"),
			cliNetworkConfig("net-b"),
		},
		InfraProviders: []v1alpha1.InfraProvider{
			cliBareMetalProvider("machines-a"),
			cliBareMetalProvider("machines-b"),
		},
		InfraComponents: []v1alpha1.InfraComponent{cliNameResolutionComponent()},
		ContainerClusters: []v1alpha1.ContainerCluster{
			cliContainerCluster("cluster-a", "cluster-a-master-0"),
			cliContainerCluster("cluster-b", "cluster-b-master-0"),
		},
	}
}

func cliStateWithAllSharedMachineServices() v1alpha1.State {
	state := cliStateWithSharedDNS()
	state.Environments[0].Spec.ProxyFor = v1alpha1.EnvironmentProxyForSpec{
		Bootwright:              "default",
		ContainerClusterInstall: "default",
	}
	state.Environments[0].Spec.InfraComponents.Proxies = []v1alpha1.EnvironmentProxyComponent{{
		Name:         "default",
		Management:   v1alpha1.EnvironmentComponentManaged,
		ComponentRef: v1alpha1.LocalObjectReference{Name: "proxy"},
	}}
	state.Environments[0].Spec.InfraComponents.ArtifactServers = []v1alpha1.EnvironmentArtifactServerComponent{{
		Name:         "default",
		Management:   v1alpha1.EnvironmentComponentManaged,
		ComponentRef: v1alpha1.LocalObjectReference{Name: "artifact-server"},
	}}
	for i := range state.ContainerClusters {
		state.ContainerClusters[i].Spec.Install.ArtifactAccess = v1alpha1.ClusterArtifactAccess{
			ServerRef: v1alpha1.LocalObjectReference{Name: "default"},
			ContainerClusterInstall: v1alpha1.ClusterArtifactEndpointRef{
				EndpointRef: v1alpha1.LocalObjectReference{Name: "cluster"},
			},
		}
	}
	state.Environments[0].Spec.InfraComponents.Registries = []v1alpha1.EnvironmentRegistryComponent{{
		Name:         "default",
		Management:   v1alpha1.EnvironmentComponentManaged,
		ComponentRef: v1alpha1.LocalObjectReference{Name: "registry"},
	}}
	state.Environments[0].Spec.InfraComponents.NTP = []v1alpha1.EnvironmentNTPComponent{{
		Name:         "default",
		Management:   v1alpha1.EnvironmentComponentManaged,
		ComponentRef: v1alpha1.LocalObjectReference{Name: "ntp-server"},
	}}
	state.InfraComponents = append(state.InfraComponents,
		cliLoadBalancerComponent(),
		cliProxyComponent(),
		cliNTPComponent(),
		cliRegistryComponent(),
		cliArtifactServerComponent(),
	)
	for i := range state.ContainerClusters {
		state.ContainerClusters[i].Spec.Install.Endpoints = map[string]v1alpha1.Endpoint{
			v1alpha1.EndpointAPI: {
				Source: v1alpha1.EndpointSource{
					Type:           v1alpha1.EndpointSourceInfraComponent,
					ComponentRef:   v1alpha1.LocalObjectReference{Name: "load-balancer"},
					BindAddressRef: "api",
				},
			},
		}
	}
	for i := range state.ContainerClusters {
		state.ContainerClusters[i].Spec.Install.Mode = v1alpha1.InstallModeDisconnected
	}
	return state
}

func cliNetworkConfig(name string) v1alpha1.NetworkConfig {
	return v1alpha1.NetworkConfig{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.NetworkConfigSpec{
			NameResolutionRefs: []string{"default"},
		},
	}
}

func cliServiceMachine() v1alpha1.Machine {
	return v1alpha1.Machine{
		Metadata: v1alpha1.Metadata{Name: "service-host"},
		Spec: v1alpha1.MachineSpec{
			Capabilities: []string{
				v1alpha1.MachineCapabilityContainerRuntime,
				v1alpha1.MachineCapabilityArtifactServer,
				v1alpha1.MachineCapabilityLoadBalancer,
				v1alpha1.MachineCapabilityNameResolution,
				v1alpha1.MachineCapabilityNTP,
				v1alpha1.MachineCapabilityProxy,
				v1alpha1.MachineCapabilityRegistry,
			},
			OS: v1alpha1.MachineOSSpec{
				Provided: v1alpha1.BoolPtr(true),
			},
			Addresses: []v1alpha1.MachineAddress{{Name: "ssh", Address: "10.0.0.5"}},
			Access: v1alpha1.MachineAccess{
				SSH: &v1alpha1.MachineSSHSpec{AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"}},
			},
		},
	}
}

func cliRawMachine(name, providerName, networkName string) v1alpha1.Machine {
	return v1alpha1.Machine{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.MachineSpec{
			Capabilities: []string{v1alpha1.MachineCapabilityOpenShiftNode},
			Substrate: v1alpha1.MachineSubstrate{
				ProviderRef: v1alpha1.LocalObjectReference{Name: providerName},
			},
			Hardware: v1alpha1.MachineHardware{
				NICs: []v1alpha1.MachineNIC{{Name: "primary", MACAddress: "52:54:00:00:00:01"}},
				Boot: v1alpha1.MachineHardwareBoot{NICRef: v1alpha1.LocalObjectReference{Name: "primary"}},
			},
			OS: v1alpha1.MachineOSSpec{
				Provided: v1alpha1.BoolPtr(false),
			},
			Network: v1alpha1.MachineNetwork{
				Config: v1alpha1.MachineNetworkConfig{
					NetworkConfigRef: v1alpha1.LocalObjectReference{Name: networkName},
				},
			},
		},
	}
}

func cliBareMetalProvider(name string) v1alpha1.InfraProvider {
	return v1alpha1.InfraProvider{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.InfraProviderSpec{
			Type:      v1alpha1.ProvisionerBareMetal,
			BareMetal: &v1alpha1.InfraProviderBareMetal{},
		},
	}
}

func cliArtifactServerComponent() v1alpha1.InfraComponent {
	return v1alpha1.InfraComponent{
		Metadata: v1alpha1.Metadata{Name: "artifact-server"},
		Spec: v1alpha1.InfraComponentSpec{
			Type: v1alpha1.ComponentSlotArtifactServer,
			ArtifactServer: &v1alpha1.ArtifactServerComponent{
				MachineRef: v1alpha1.LocalObjectReference{Name: "service-host"},
				Listeners: []v1alpha1.ArtifactServerListener{{
					Name:     "https",
					Protocol: v1alpha1.ArtifactServerProtocolHTTPS,
					Port:     v1alpha1.DefaultArtifactsHTTPPort,
				}},
				Endpoints: []v1alpha1.ArtifactServerEndpoint{{
					Name:        "cluster",
					ListenerRef: "https",
					AddressRef:  "ssh",
				}},
			},
		},
	}
}

func cliLoadBalancerComponent() v1alpha1.InfraComponent {
	return v1alpha1.InfraComponent{
		Metadata: v1alpha1.Metadata{Name: "load-balancer"},
		Spec: v1alpha1.InfraComponentSpec{
			Type: v1alpha1.ComponentSlotLoadBalancer, LoadBalancer: &v1alpha1.LoadBalancerComponent{
				Implementation: v1alpha1.InfraComponentTypeHAProxy,
				MachineRef:     v1alpha1.LocalObjectReference{Name: "service-host"},
				BindAddresses: []v1alpha1.LoadBalancerBindAddress{{
					Name:    "api",
					Address: "10.0.0.10",
				}},
			}},
	}
}

func cliNameResolutionComponent() v1alpha1.InfraComponent {
	return v1alpha1.InfraComponent{
		Metadata: v1alpha1.Metadata{Name: "name-resolution"},
		Spec: v1alpha1.InfraComponentSpec{
			Type: v1alpha1.ComponentSlotNameResolution, NameResolution: &v1alpha1.NameResolutionComponent{
				Implementation: v1alpha1.InfraComponentTypeDnsmasq,
				MachineRef:     v1alpha1.LocalObjectReference{Name: "service-host"},
				BindAddress:    "10.0.0.5",
				Port:           v1alpha1.DefaultDNSPort,
			}},
	}
}

func cliNTPComponent() v1alpha1.InfraComponent {
	return v1alpha1.InfraComponent{
		Metadata: v1alpha1.Metadata{Name: "ntp-server"},
		Spec: v1alpha1.InfraComponentSpec{
			Type: v1alpha1.ComponentSlotNTP, NTP: &v1alpha1.NTPComponent{
				Implementation: v1alpha1.InfraComponentTypeChrony,
				MachineRef:     v1alpha1.LocalObjectReference{Name: "service-host"},
				BindAddress:    "10.0.0.5",
				Port:           v1alpha1.DefaultNTPPort,
			}},
	}
}

func cliProxyComponent() v1alpha1.InfraComponent {
	return v1alpha1.InfraComponent{
		Metadata: v1alpha1.Metadata{Name: "proxy"},
		Spec: v1alpha1.InfraComponentSpec{
			Type: v1alpha1.ComponentSlotProxy, Proxy: &v1alpha1.ProxyComponent{
				Implementation: v1alpha1.InfraComponentTypeSquid,
				MachineRef:     v1alpha1.LocalObjectReference{Name: "service-host"},
				Port:           v1alpha1.DefaultSquidPort,
			}},
	}
}

func cliRegistryComponent() v1alpha1.InfraComponent {
	return v1alpha1.InfraComponent{
		Metadata: v1alpha1.Metadata{Name: "registry"},
		Spec: v1alpha1.InfraComponentSpec{
			Type: v1alpha1.ComponentSlotRegistry, Registry: &v1alpha1.RegistryComponent{
				Implementation: v1alpha1.InfraComponentTypeMirrorRegistry,
				MachineRef:     v1alpha1.LocalObjectReference{Name: "service-host"},
				Port:           v1alpha1.DefaultMirrorRegistryPort,
			}},
	}
}

func cliContainerCluster(name, machineName string) v1alpha1.ContainerCluster {
	return v1alpha1.ContainerCluster{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.ContainerClusterSpec{
			Hosts: []v1alpha1.OCPHostSpec{{
				Hostname:   "master-0",
				Role:       "master",
				MachineRef: v1alpha1.LocalObjectReference{Name: machineName},
			}},
		},
	}
}
