package desiredstate

import (
	"fmt"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestNormalizeDerivesSatelliteContentBaseURL(t *testing.T) {
	state := v1alpha1.State{Entitlements: []v1alpha1.Entitlement{{
		Metadata: v1alpha1.Metadata{Name: "rhel"},
		Spec: v1alpha1.EntitlementSpec{
			Type: v1alpha1.EntitlementTypeRedHatRHEL,
			RHSM: &v1alpha1.EntitlementRHSM{
				OrganizationRef:  v1alpha1.SecretRef{Name: "rhel-org"},
				ActivationKeyRef: v1alpha1.SecretRef{Name: "rhel-key"},
				Satellite: &v1alpha1.EntitlementRHSMSatellite{
					Hostname: "  satellite.corp.example.com  ",
				},
			},
		},
	}}}
	Normalize(&state)
	sat := state.Entitlements[0].Spec.RHSM.Satellite
	if sat.Hostname != "satellite.corp.example.com" {
		t.Fatalf("hostname not trimmed: %q", sat.Hostname)
	}
	if sat.ContentBaseURL != "https://satellite.corp.example.com/pulp/content" {
		t.Fatalf("contentBaseURL = %q, want derived default", sat.ContentBaseURL)
	}

	// An explicit contentBaseURL is preserved.
	state.Entitlements[0].Spec.RHSM.Satellite = &v1alpha1.EntitlementRHSMSatellite{
		Hostname:       "capsule.corp.example.com",
		ContentBaseURL: "https://capsule.corp.example.com/custom/content",
	}
	Normalize(&state)
	if got := state.Entitlements[0].Spec.RHSM.Satellite.ContentBaseURL; got != "https://capsule.corp.example.com/custom/content" {
		t.Fatalf("explicit contentBaseURL overwritten: %q", got)
	}
}

func TestNormalizeDefaultsClusterInstallSecretRefs(t *testing.T) {
	state := v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Metadata: v1alpha1.Metadata{Name: "env"},
			Spec: v1alpha1.EnvironmentSpec{
				BaseDomain: "example.test",
			},
		}},
		ContainerClusters: []v1alpha1.ContainerCluster{{
			Metadata: v1alpha1.Metadata{Name: "cluster-a"},
		}},
	}

	Normalize(&state)

	install := state.ContainerClusters[0].Spec.Install
	if got := install.PullSecretRef.Name; got != v1alpha1.DefaultPullSecretName {
		t.Fatalf("PullSecretRef.Name = %q, want %q", got, v1alpha1.DefaultPullSecretName)
	}
	wantSSHKey := v1alpha1.ClusterAdminSSHKeyName("cluster-a")
	if got := install.NodeSSH.KeyPairRef.Name; got != wantSSHKey {
		t.Fatalf("NodeSSH.KeyPairRef.Name = %q, want %q", got, wantSSHKey)
	}
	defaulted := state.ContainerClusters[0].DefaultedRefs
	if !defaulted.PullSecretRef || !defaulted.NodeSSH {
		t.Fatalf("DefaultedRefs = %+v, want PullSecretRef and NodeSSH marked defaulted", defaulted)
	}
}

func TestNormalizeUsesEnvironmentInstallDefaults(t *testing.T) {
	state := v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Metadata: v1alpha1.Metadata{Name: "env"},
			Spec: v1alpha1.EnvironmentSpec{
				BaseDomain: "example.test",
				Defaults: v1alpha1.EnvironmentDefaultsSpec{
					Install: v1alpha1.EnvironmentInstallDefaultsSpec{
						PullSecretRef: v1alpha1.SecretRef{Name: "custom-pull"},
						NodeSSH: v1alpha1.NodeSSHSpec{
							PublicKeyRef:  v1alpha1.SecretRef{Name: "cluster-public"},
							PrivateKeyRef: v1alpha1.SecretRef{Name: "cluster-private"},
						},
					},
				},
			},
		}},
		ContainerClusters: []v1alpha1.ContainerCluster{{
			Metadata: v1alpha1.Metadata{Name: "cluster-a"},
		}},
	}

	Normalize(&state)

	install := state.ContainerClusters[0].Spec.Install
	if got := install.PullSecretRef.Name; got != "custom-pull" {
		t.Fatalf("PullSecretRef.Name = %q, want custom-pull", got)
	}
	if got := install.NodeSSH.PublicKeyRef.Name; got != "cluster-public" {
		t.Fatalf("NodeSSH.PublicKeyRef.Name = %q, want cluster-public", got)
	}
	if got := install.NodeSSH.PrivateKeyRef.Name; got != "cluster-private" {
		t.Fatalf("NodeSSH.PrivateKeyRef.Name = %q, want cluster-private", got)
	}
	// Environment-defaults copies are normalize-injected too: the cluster
	// author never wrote them, so they carry the defaulted markers.
	defaulted := state.ContainerClusters[0].DefaultedRefs
	if !defaulted.PullSecretRef || !defaulted.NodeSSH {
		t.Fatalf("DefaultedRefs = %+v, want PullSecretRef and NodeSSH marked defaulted", defaulted)
	}
}

func TestNormalizeDoesNotDefaultMachineSSHUser(t *testing.T) {
	state := v1alpha1.State{
		Machines: []v1alpha1.Machine{
			{
				Metadata: v1alpha1.Metadata{Name: "omitted"},
				Spec: v1alpha1.MachineSpec{
					OS: v1alpha1.MachineOSSpec{
						Provided: v1alpha1.BoolPtr(true),
					},
					Access: v1alpha1.MachineAccess{
						SSH: &v1alpha1.MachineSSHSpec{AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"}},
					},
				},
			},
			{
				Metadata: v1alpha1.Metadata{Name: "explicit"},
				Spec: v1alpha1.MachineSpec{
					OS: v1alpha1.MachineOSSpec{
						Provided: v1alpha1.BoolPtr(true),
					},
					Access: v1alpha1.MachineAccess{
						SSH: &v1alpha1.MachineSSHSpec{AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"}, User: "root"},
					},
				},
			},
			{
				Metadata: v1alpha1.Metadata{Name: "no-ssh"},
			},
		},
	}

	Normalize(&state)

	// An omitted user is left empty so rendered artifacts are deterministic
	// across operators; the connection layer falls back to the (root) user
	// running ansible-playbook, matching the kickstart's root default.
	if got := state.Machines[0].Spec.Access.SSH.User; got != "" {
		t.Fatalf("omitted Machine SSH user = %q, want empty (not process-derived)", got)
	}
	if got := state.Machines[1].Spec.Access.SSH.User; got != "root" {
		t.Fatalf("explicit Machine SSH user = %q, want root", got)
	}
}

func TestNormalizeDefaultsAPIIntEndpointFromAPI(t *testing.T) {
	state := v1alpha1.State{
		ContainerClusters: []v1alpha1.ContainerCluster{{
			Metadata: v1alpha1.Metadata{Name: "cluster-a"},
			Spec: v1alpha1.ContainerClusterSpec{
				Install: v1alpha1.OCPInstallSpec{
					Endpoints: map[string]v1alpha1.Endpoint{
						v1alpha1.EndpointAPI: {
							Address: "192.0.2.10",
							Source:  v1alpha1.EndpointSource{Type: v1alpha1.EndpointSourceExternal},
						},
						v1alpha1.EndpointIngress: {
							Address: "192.0.2.11",
							Source:  v1alpha1.EndpointSource{Type: v1alpha1.EndpointSourceExternal},
						},
					},
				},
			},
		}},
	}

	Normalize(&state)

	apiInt, ok := state.ContainerClusters[0].Spec.Install.Endpoints[v1alpha1.EndpointAPIInt]
	if !ok {
		t.Fatal("api-int endpoint was not defaulted from api")
	}
	if apiInt.Address != "192.0.2.10" {
		t.Fatalf("api-int address = %q, want 192.0.2.10", apiInt.Address)
	}
	if apiInt.Source.Type != v1alpha1.EndpointSourceExternal {
		t.Fatalf("api-int source.type = %q, want %q", apiInt.Source.Type, v1alpha1.EndpointSourceExternal)
	}
}

func TestNormalizeKeepsAuthoredAPIIntEndpoint(t *testing.T) {
	state := v1alpha1.State{
		ContainerClusters: []v1alpha1.ContainerCluster{{
			Metadata: v1alpha1.Metadata{Name: "cluster-a"},
			Spec: v1alpha1.ContainerClusterSpec{
				Install: v1alpha1.OCPInstallSpec{
					Endpoints: map[string]v1alpha1.Endpoint{
						v1alpha1.EndpointAPI: {
							Address: "192.0.2.10",
							Source:  v1alpha1.EndpointSource{Type: v1alpha1.EndpointSourceExternal},
						},
						v1alpha1.EndpointAPIInt: {
							Address: "192.0.2.12",
							Source:  v1alpha1.EndpointSource{Type: v1alpha1.EndpointSourceExternal},
						},
					},
				},
			},
		}},
	}

	Normalize(&state)

	apiInt := state.ContainerClusters[0].Spec.Install.Endpoints[v1alpha1.EndpointAPIInt]
	if apiInt.Address != "192.0.2.12" {
		t.Fatalf("authored api-int address = %q, want 192.0.2.12", apiInt.Address)
	}
}

func TestNormalizeDefaultsClusterAndServiceNetworks(t *testing.T) {
	state := v1alpha1.State{
		ContainerClusters: []v1alpha1.ContainerCluster{{
			Metadata: v1alpha1.Metadata{Name: "cluster-a"},
		}},
	}

	Normalize(&state)

	networking := state.ContainerClusters[0].Spec.Networking
	if networking == nil {
		t.Fatal("spec.networking was not defaulted")
	}
	if len(networking.ClusterNetwork) != 1 ||
		networking.ClusterNetwork[0].CIDR != v1alpha1.DefaultClusterNetworkCIDR ||
		networking.ClusterNetwork[0].HostPrefix != v1alpha1.DefaultClusterNetworkHostPrefix {
		t.Fatalf("clusterNetwork = %+v, want stock default", networking.ClusterNetwork)
	}
	if len(networking.ServiceNetwork) != 1 || networking.ServiceNetwork[0] != v1alpha1.DefaultServiceNetworkCIDR {
		t.Fatalf("serviceNetwork = %+v, want stock default", networking.ServiceNetwork)
	}
}

func TestNormalizeKeepsAuthoredNetworking(t *testing.T) {
	state := v1alpha1.State{
		ContainerClusters: []v1alpha1.ContainerCluster{{
			Metadata: v1alpha1.Metadata{Name: "cluster-a"},
			Spec: v1alpha1.ContainerClusterSpec{
				Networking: &v1alpha1.OCPNetworkingSpec{
					ClusterNetwork: []v1alpha1.ContainerClusterNetworkCIDR{{CIDR: "10.132.0.0/14", HostPrefix: 23}},
					ServiceNetwork: []string{"172.31.0.0/16"},
				},
			},
		}},
	}

	Normalize(&state)

	networking := state.ContainerClusters[0].Spec.Networking
	if networking.ClusterNetwork[0].CIDR != "10.132.0.0/14" {
		t.Fatalf("authored clusterNetwork cidr = %q, want 10.132.0.0/14", networking.ClusterNetwork[0].CIDR)
	}
	if networking.ServiceNetwork[0] != "172.31.0.0/16" {
		t.Fatalf("authored serviceNetwork = %q, want 172.31.0.0/16", networking.ServiceNetwork[0])
	}
}

func TestNormalizeDefaultsMachineAttachmentRef(t *testing.T) {
	state := v1alpha1.State{
		Machines: []v1alpha1.Machine{
			{
				Metadata: v1alpha1.Metadata{Name: "defaulted"},
				Spec: v1alpha1.MachineSpec{
					Substrate: v1alpha1.MachineSubstrate{
						ProviderRef: v1alpha1.LocalObjectReference{Name: "rack"},
					},
					Network: v1alpha1.MachineNetwork{
						Config: v1alpha1.MachineNetworkConfig{
							NetworkConfigRef: v1alpha1.LocalObjectReference{Name: "cluster-net"},
						},
					},
				},
			},
			{
				Metadata: v1alpha1.Metadata{Name: "authored"},
				Spec: v1alpha1.MachineSpec{
					Substrate: v1alpha1.MachineSubstrate{
						ProviderRef: v1alpha1.LocalObjectReference{Name: "rack"},
					},
					Network: v1alpha1.MachineNetwork{
						Config: v1alpha1.MachineNetworkConfig{
							NetworkConfigRef: v1alpha1.LocalObjectReference{Name: "cluster-net"},
							AttachmentRef:    v1alpha1.LocalObjectReference{Name: "other-attachment"},
						},
					},
				},
			},
			{
				Metadata: v1alpha1.Metadata{Name: "no-provider"},
				Spec: v1alpha1.MachineSpec{
					Network: v1alpha1.MachineNetwork{
						Config: v1alpha1.MachineNetworkConfig{
							NetworkConfigRef: v1alpha1.LocalObjectReference{Name: "cluster-net"},
						},
					},
				},
			},
		},
	}

	Normalize(&state)

	if got := state.Machines[0].Spec.Network.Config.AttachmentRef.Name; got != "cluster-net" {
		t.Fatalf("defaulted attachmentRef = %q, want cluster-net", got)
	}
	if !state.Machines[0].DefaultedRefs.AttachmentRef {
		t.Fatalf("DefaultedRefs = %+v, want AttachmentRef marked defaulted", state.Machines[0].DefaultedRefs)
	}
	if got := state.Machines[1].Spec.Network.Config.AttachmentRef.Name; got != "other-attachment" {
		t.Fatalf("authored attachmentRef = %q, want other-attachment", got)
	}
	if state.Machines[1].DefaultedRefs.AttachmentRef {
		t.Fatalf("DefaultedRefs = %+v, want authored attachmentRef left unmarked", state.Machines[1].DefaultedRefs)
	}
	if got := state.Machines[2].Spec.Network.Config.AttachmentRef.Name; got != "" {
		t.Fatalf("provider-less machine attachmentRef = %q, want empty", got)
	}
	if state.Machines[2].DefaultedRefs.AttachmentRef {
		t.Fatalf("DefaultedRefs = %+v, want provider-less machine left unmarked", state.Machines[2].DefaultedRefs)
	}
}

func TestNormalizeDefaultsDistributionType(t *testing.T) {
	state := v1alpha1.State{
		ContainerClusters: []v1alpha1.ContainerCluster{
			{Metadata: v1alpha1.Metadata{Name: "omitted"}},
			{
				Metadata: v1alpha1.Metadata{Name: "authored"},
				Spec: v1alpha1.ContainerClusterSpec{
					Distribution: v1alpha1.DistributionSpec{Type: v1alpha1.DistributionOKD},
				},
			},
		},
	}

	Normalize(&state)

	if got := state.ContainerClusters[0].Spec.Distribution.Type; got != v1alpha1.DistributionOpenShift {
		t.Fatalf("omitted distribution.type = %q, want %q", got, v1alpha1.DistributionOpenShift)
	}
	if got := state.ContainerClusters[1].Spec.Distribution.Type; got != v1alpha1.DistributionOKD {
		t.Fatalf("authored distribution.type = %q, want %q", got, v1alpha1.DistributionOKD)
	}
}

func platformDerivationState(providerTypes ...string) v1alpha1.State {
	state := v1alpha1.State{}
	cluster := v1alpha1.ContainerCluster{Metadata: v1alpha1.Metadata{Name: "cluster-a"}}
	for i, providerType := range providerTypes {
		providerName := fmt.Sprintf("provider-%d", i)
		machineName := fmt.Sprintf("machine-%d", i)
		state.InfraProviders = append(state.InfraProviders, v1alpha1.InfraProvider{
			Metadata: v1alpha1.Metadata{Name: providerName},
			Spec:     v1alpha1.InfraProviderSpec{Type: providerType},
		})
		state.Machines = append(state.Machines, v1alpha1.Machine{
			Metadata: v1alpha1.Metadata{Name: machineName},
			Spec: v1alpha1.MachineSpec{
				Substrate: v1alpha1.MachineSubstrate{
					ProviderRef: v1alpha1.LocalObjectReference{Name: providerName},
				},
				OS: v1alpha1.MachineOSSpec{Provided: v1alpha1.BoolPtr(false)},
			},
		})
		cluster.Spec.Hosts = append(cluster.Spec.Hosts, v1alpha1.OCPHostSpec{
			Hostname:   fmt.Sprintf("master-%d", i),
			Role:       "master",
			MachineRef: v1alpha1.LocalObjectReference{Name: machineName},
		})
	}
	state.ContainerClusters = []v1alpha1.ContainerCluster{cluster}
	return state
}

func TestNormalizeDerivesInstallPlatformFromProviderType(t *testing.T) {
	for _, providerType := range []string{v1alpha1.ProvisionerLibvirt, v1alpha1.ProvisionerBareMetal} {
		t.Run(providerType, func(t *testing.T) {
			state := platformDerivationState(providerType)

			Normalize(&state)

			platform := state.ContainerClusters[0].Spec.Install.Platform
			if platform.Type != v1alpha1.PlatformTypeBareMetal {
				t.Fatalf("derived platform.type = %q, want %q", platform.Type, v1alpha1.PlatformTypeBareMetal)
			}
			if platform.BareMetal == nil || platform.BareMetal.ProvisioningNetwork != v1alpha1.ProvisioningNetworkDisabled {
				t.Fatalf("derived platform.baremetal = %+v, want provisioningNetwork disabled", platform.BareMetal)
			}
		})
	}

	t.Run(v1alpha1.ProvisionerKubeVirt, func(t *testing.T) {
		state := platformDerivationState(v1alpha1.ProvisionerKubeVirt)

		Normalize(&state)

		platform := state.ContainerClusters[0].Spec.Install.Platform
		if platform.Type != v1alpha1.PlatformTypeNone {
			t.Fatalf("derived platform.type = %q, want %q", platform.Type, v1alpha1.PlatformTypeNone)
		}
		if platform.BareMetal != nil {
			t.Fatalf("derived platform.baremetal = %+v, want nil", platform.BareMetal)
		}
	})
}

func TestNormalizeKeepsAuthoredInstallPlatform(t *testing.T) {
	state := platformDerivationState(v1alpha1.ProvisionerLibvirt)
	state.ContainerClusters[0].Spec.Install.Platform = v1alpha1.InstallPlatform{Type: v1alpha1.PlatformTypeNone}

	Normalize(&state)

	platform := state.ContainerClusters[0].Spec.Install.Platform
	if platform.Type != v1alpha1.PlatformTypeNone {
		t.Fatalf("authored platform.type = %q, want %q", platform.Type, v1alpha1.PlatformTypeNone)
	}
	if platform.BareMetal != nil {
		t.Fatalf("authored platform.baremetal = %+v, want nil", platform.BareMetal)
	}
}

func TestNormalizeLeavesAmbiguousInstallPlatformForValidation(t *testing.T) {
	state := platformDerivationState(v1alpha1.ProvisionerLibvirt, v1alpha1.ProvisionerBareMetal)

	Normalize(&state)

	platform := state.ContainerClusters[0].Spec.Install.Platform
	if !installPlatformOmitted(platform) {
		t.Fatalf("ambiguous provider types must not derive a platform, got %+v", platform)
	}

	errs := validateClusterPlatformWithDerivation(state, state.ContainerClusters[0])
	if len(errs) != 1 {
		t.Fatalf("validation errors = %v, want exactly one conflict diagnostic", errs)
	}
	want := `ContainerCluster/cluster-a spec.install.platform cannot be derived: spec.hosts bind machines across multiple provider types (InfraProvider/provider-0 (libvirt), InfraProvider/provider-1 (baremetal)); set spec.install.platform.type explicitly`
	if errs[0] != want {
		t.Fatalf("conflict diagnostic = %q, want %q", errs[0], want)
	}
}

func TestNormalizeDefaultsSecretStorageAndSSHKeyPairType(t *testing.T) {
	state := v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Metadata: v1alpha1.Metadata{Name: "env"},
			Spec: v1alpha1.EnvironmentSpec{
				BaseDomain: "example.test",
			},
		}},
		Secrets: []v1alpha1.Secret{{
			Metadata: v1alpha1.Metadata{Name: "cluster-a-cluster-admin-ssh-key"},
			Spec: v1alpha1.SecretSpec{
				Type:   v1alpha1.SecretTypeSSHKeyPair,
				Source: v1alpha1.SecretSource{Generated: &v1alpha1.SecretGeneratedSource{}},
			},
		}},
	}

	Normalize(&state)

	env := state.Environments[0]
	if got := env.Spec.SecretStorage.Mode; got != v1alpha1.SecretStorageModeSource {
		t.Fatalf("SecretStorage.Mode = %q, want %q", got, v1alpha1.SecretStorageModeSource)
	}
	if got := state.Secrets[0].Spec.Source.Generated.KeyType; got != v1alpha1.SSHKeyPairTypeEd25519 {
		t.Fatalf("SSHKeyPair keyType = %q, want %q", got, v1alpha1.SSHKeyPairTypeEd25519)
	}
}

func bareMetalProvider(name string) v1alpha1.InfraProvider {
	return v1alpha1.InfraProvider{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.InfraProviderSpec{
			Type:      v1alpha1.ProvisionerBareMetal,
			BareMetal: &v1alpha1.InfraProviderBareMetal{},
		},
	}
}

func bareMetalMachine(name, provider string) v1alpha1.Machine {
	return v1alpha1.Machine{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.MachineSpec{
			Substrate: v1alpha1.MachineSubstrate{
				ProviderRef: v1alpha1.LocalObjectReference{Name: provider},
			},
			Hardware: v1alpha1.MachineHardware{
				NICs: []v1alpha1.MachineNIC{{Name: "primary", MACAddress: "52:54:00:00:00:01"}},
				Boot: v1alpha1.MachineHardwareBoot{NICRef: v1alpha1.LocalObjectReference{Name: "primary"}},
				Management: v1alpha1.MachineHardwareManagement{
					BMC: v1alpha1.BMCSpec{Address: "redfish-virtualmedia+https://bmc.example.test/redfish/v1/Systems/1"},
				},
			},
			OS: v1alpha1.MachineOSSpec{Provided: v1alpha1.BoolPtr(false)},
		},
	}
}

func profiledMachine(name, provider, providerType string) v1alpha1.Machine {
	machine := v1alpha1.Machine{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.MachineSpec{
			Substrate: v1alpha1.MachineSubstrate{
				ProviderRef: v1alpha1.LocalObjectReference{Name: provider},
			},
			OS: v1alpha1.MachineOSSpec{Provided: v1alpha1.BoolPtr(false)},
		},
	}
	if providerType == v1alpha1.ProvisionerLibvirt {
		machine.Spec.Substrate.ProfileRef = v1alpha1.LocalObjectReference{Name: "worker"}
	}
	return machine
}

func containerClusterWithMachine(name, machine string) v1alpha1.ContainerCluster {
	return v1alpha1.ContainerCluster{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.ContainerClusterSpec{
			Hosts: []v1alpha1.OCPHostSpec{{
				Hostname:   "master-0",
				Role:       "master",
				MachineRef: v1alpha1.LocalObjectReference{Name: machine},
			}},
		},
	}
}

func TestNormalizeDefaultsInfraComponentProxy(t *testing.T) {
	state := v1alpha1.State{
		InfraComponents: []v1alpha1.InfraComponent{{
			Metadata: v1alpha1.Metadata{Name: "proxy"},
			Spec: v1alpha1.InfraComponentSpec{
				Type: v1alpha1.ComponentSlotProxy,
				Proxy: &v1alpha1.ProxyComponent{
					Implementation: v1alpha1.InfraComponentTypeSquid,
					MachineRef:     v1alpha1.LocalObjectReference{Name: "services-host"},
				},
			},
		}},
	}

	Normalize(&state)

	proxy := state.InfraComponents[0].Spec.Proxy
	if got := proxy.Port; got != v1alpha1.DefaultSquidPort {
		t.Fatalf("proxy port = %d, want %d", got, v1alpha1.DefaultSquidPort)
	}
	if got := proxy.BindAddress; got != v1alpha1.DefaultServiceBindAddress {
		t.Fatalf("proxy bindAddress = %q, want %q", got, v1alpha1.DefaultServiceBindAddress)
	}
}

func TestNormalizeDefaultsArtifactServerListener(t *testing.T) {
	state := v1alpha1.State{
		InfraComponents: []v1alpha1.InfraComponent{{
			Spec: v1alpha1.InfraComponentSpec{
				Type: v1alpha1.ComponentSlotArtifactServer,
				ArtifactServer: &v1alpha1.ArtifactServerComponent{
					MachineRef: v1alpha1.LocalObjectReference{Name: "services-host"},
				},
			},
		}},
	}

	Normalize(&state)

	listeners := state.InfraComponents[0].Spec.ArtifactServer.Listeners
	if len(listeners) != 1 {
		t.Fatalf("artifact server listeners = %d, want 1", len(listeners))
	}
	if got := listeners[0].Protocol; got != v1alpha1.ArtifactServerProtocolHTTPS {
		t.Fatalf("artifact server listener protocol = %q, want %q", got, v1alpha1.ArtifactServerProtocolHTTPS)
	}
	if got := listeners[0].Port; got != v1alpha1.DefaultArtifactsHTTPPort {
		t.Fatalf("artifact server listener port = %d, want %d", got, v1alpha1.DefaultArtifactsHTTPPort)
	}
}

func TestNormalizeDefaultsBMCEmulationPorts(t *testing.T) {
	state := v1alpha1.State{
		InfraProviders: []v1alpha1.InfraProvider{{
			Spec: v1alpha1.InfraProviderSpec{
				Type: v1alpha1.ProvisionerLibvirt,
				Libvirt: &v1alpha1.InfraProviderLibvirt{
					BMCEmulationDefaults: &v1alpha1.BMCEmulationDefaults{},
				},
			},
		}},
	}

	Normalize(&state)

	d := state.InfraProviders[0].Spec.Libvirt.BMCEmulationDefaults
	if got := d.Port; got != v1alpha1.DefaultBMCEmulationStartPort {
		t.Fatalf("BMC emulator port = %d, want %d", got, v1alpha1.DefaultBMCEmulationStartPort)
	}
	if got := d.VMediaPort; got != v1alpha1.DefaultBMCEmulationStartPort+1 {
		t.Fatalf("BMC emulator vMediaPort = %d, want %d", got, v1alpha1.DefaultBMCEmulationStartPort+1)
	}
}
