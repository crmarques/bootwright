package render_test

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/render/inventory"
	desiredstate "github.com/crmarques/bootwright/internal/state/desired"
)

const fixtureRoot = "../state/desired/testdata/good"

func TestAllSucceedsForGoodFixtures(t *testing.T) {
	entries, err := os.ReadDir(fixtureRoot)
	if err != nil {
		t.Fatalf("read fixtures: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("no fixtures under %s", fixtureRoot)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		t.Run(name, func(t *testing.T) {
			state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, name)})
			if err != nil {
				t.Fatalf("LoadNormalizeValidate: %v", err)
			}

			renderedDir := t.TempDir()
			clustersDir := t.TempDir()
			secretsDir := t.TempDir()
			result, err := render.All(renderedDir, clustersDir, secretsDir, state)
			if err != nil {
				t.Fatalf("render.All: %v", err)
			}

			for _, path := range []string{
				result.EffectiveStatePath,
				result.LockPath,
				result.InventoryPath,
				result.VarsPath,
			} {
				assertFileMode(t, path, 0o600)
			}
			assertDirMode(t, renderedDir, 0o700)
			assertDirMode(t, filepath.Dir(result.InventoryPath), 0o700)
			assertDirMode(t, result.ArtifactsDir, 0o700)

			if len(state.ContainerClusters) == 0 {
				return
			}
			if len(result.InstallerAssets) != len(state.ContainerClusters) {
				t.Fatalf("installer assets got %d, want %d (one per cluster)",
					len(result.InstallerAssets), len(state.ContainerClusters))
			}
			for _, asset := range result.InstallerAssets {
				assertFileMode(t, asset.InstallConfigPath, 0o600)
				assertFileMode(t, asset.AgentConfigPath, 0o600)
				body, err := os.ReadFile(asset.InstallConfigPath)
				if err != nil {
					t.Fatalf("read placeholder install-config: %v", err)
				}
				if !bytes.Contains(body, []byte("bootwright-secret-ref:")) {
					t.Fatalf("placeholder install-config at %s missing the bootwright-secret-ref marker; pull secret may have been baked in",
						asset.InstallConfigPath)
				}
				for _, leaked := range []string{"-----BEGIN", "PRIVATE KEY", `"auth":`} {
					if bytes.Contains(body, []byte(leaked)) {
						t.Fatalf("placeholder install-config at %s contains real-looking secret material %q",
							asset.InstallConfigPath, leaked)
					}
				}
			}
		})
	}
}

func TestAllSucceedsForCanonicalExamples(t *testing.T) {
	examplesRoot := filepath.Join("..", "..", "examples")
	entries, err := os.ReadDir(examplesRoot)
	if err != nil {
		t.Fatalf("read examples: %v", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if name == "_wip" {
			continue
		}
		t.Run(name, func(t *testing.T) {
			state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(examplesRoot, name)})
			if err != nil {
				t.Fatalf("LoadNormalizeValidate: %v", err)
			}
			if _, err := render.All(t.TempDir(), t.TempDir(), t.TempDir(), state); err != nil {
				t.Fatalf("render.All: %v", err)
			}
		})
	}
}

func TestKubeVirtChildExampleRendersVarsGeneratedMACAndNonSecretState(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "examples", "baremetal-redfish-multidc-virtualized-odf-ceph")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	vars := inventory.VarsWithSecretsDir(state, t.TempDir())
	child := clustersByName(t, vars)["dc1-child-ocp"]
	machine := firstMachineComponent(t, child)
	kubevirt := machine["kubevirt"].(map[string]any)
	if got := kubevirt["hostClusterRef"]; got != "dc1-metal-ocp" {
		t.Fatalf("hostClusterRef = %v, want dc1-metal-ocp", got)
	}
	if got := kubevirt["namespace"]; got != "bootwright-dc1-child-ocp" {
		t.Fatalf("namespace = %v, want bootwright-dc1-child-ocp", got)
	}
	if got := kubevirt["kubeconfig"]; got != "{{ bootwright_clusters_dir }}/dc1-metal-ocp/secrets/kubeconfig" {
		t.Fatalf("kubeconfig = %v, want host cluster secret kubeconfig template", got)
	}
	interfaces := machine["interfaces"].([]any)
	iface := interfaces[0].(map[string]any)
	mac, _ := iface["macAddress"].(string)
	if !strings.HasPrefix(mac, "52:54:00:") {
		t.Fatalf("generated KubeVirt MAC = %q, want deterministic qemu prefix", mac)
	}
	agent, err := render.AgentConfig(state, containerClusterByName(t, state, "dc1-child-ocp"))
	if err != nil {
		t.Fatalf("AgentConfig: %v", err)
	}
	agentIface := agent["hosts"].([]any)[0].(map[string]any)["interfaces"].([]any)[0].(map[string]any)
	if got := agentIface["macAddress"]; got != mac {
		t.Fatalf("agent-config MAC = %v, want vars MAC %s", got, mac)
	}
	attachment := machine["networkAttachment"].(map[string]any)
	if got := attachment["kind"]; got != v1alpha1.ProvisionerKubeVirt {
		t.Fatalf("network attachment kind = %v, want kubevirt", got)
	}
	if got := attachment["kubevirt"].(map[string]any)["nad"]; got != "bootwright-dc1-child-ocp/dc1-child-net" {
		t.Fatalf("network NAD = %v, want bootwright-dc1-child-ocp/dc1-child-net", got)
	}

	result, err := render.All(t.TempDir(), t.TempDir(), t.TempDir(), state)
	if err != nil {
		t.Fatalf("render.All: %v", err)
	}
	effective, err := os.ReadFile(result.EffectiveStatePath)
	if err != nil {
		t.Fatalf("read effective state: %v", err)
	}
	if bytes.Contains(effective, []byte("apiVersion: v1\nkind: Config")) {
		t.Fatalf("effective state contains kubeconfig-looking bytes")
	}
	if !bytes.Contains(effective, []byte("hostClusterRef")) {
		t.Fatalf("effective state should preserve the non-secret hostClusterRef")
	}
}

func TestProviderMachineLabelsRenderToVars(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "005-3nodes-baremetal")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	for i := range state.Machines {
		state.Machines[i].Metadata.Labels = map[string]string{"datacenter": "dc1"}
	}

	vars := inventory.VarsWithSecretsDir(state, t.TempDir())
	cluster := vars["bootwright_clusters"].([]any)[0].(map[string]any)
	component := firstMachineComponent(t, cluster)
	if got := component["labels"].(map[string]string)["datacenter"]; got != "dc1" {
		t.Fatalf("machine label got %q, want dc1", got)
	}
	server := component["server"].(map[string]any)
	if got := server["labels"].(map[string]string)["datacenter"]; got != "dc1" {
		t.Fatalf("component server label got %q, want dc1", got)
	}
}

func TestInstallerConfigReturnsManagedProxyURLResolutionError(t *testing.T) {
	state := v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Metadata: v1alpha1.Metadata{Name: "env"},
			Spec: v1alpha1.EnvironmentSpec{
				BaseDomain: "example.test",
				ProxyFor: v1alpha1.EnvironmentProxyForSpec{
					ContainerClusterInstall: "managed",
				},
				InfraComponents: v1alpha1.EnvironmentInfraComponentsSpec{
					Proxies: []v1alpha1.EnvironmentProxyComponent{{
						Name:         "managed",
						Management:   v1alpha1.EnvironmentComponentManaged,
						ComponentRef: v1alpha1.LocalObjectReference{Name: "proxy"},
					}},
				},
			},
		}},
		Machines: []v1alpha1.Machine{{
			Metadata: v1alpha1.Metadata{Name: "controller"},
			Spec: v1alpha1.MachineSpec{
				OS: v1alpha1.MachineOSSpec{
					Provided: v1alpha1.BoolPtr(true),
				},
				Addresses: []v1alpha1.MachineAddress{{Name: "ssh", Address: "127.0.0.1"}},
				Access: v1alpha1.MachineAccess{
					SSH: &v1alpha1.MachineSSHSpec{AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"}},
				},
			},
		}, installerTestMachine("master-0")},
		InfraComponents: []v1alpha1.InfraComponent{{
			Metadata: v1alpha1.Metadata{Name: "proxy"},
			Spec: v1alpha1.InfraComponentSpec{
				Type: v1alpha1.ComponentSlotProxy,
				Proxy: &v1alpha1.ProxyComponent{
					Implementation: v1alpha1.InfraComponentTypeSquid,
					MachineRef:     v1alpha1.LocalObjectReference{Name: "controller"},
					Port:           3128,
				},
			},
		}},
		ContainerClusters: []v1alpha1.ContainerCluster{{
			Metadata: v1alpha1.Metadata{Name: "cluster"},
			Spec: v1alpha1.ContainerClusterSpec{
				Install: v1alpha1.OCPInstallSpec{
					PullSecretRef: v1alpha1.SecretRef{Name: "pull-secret"},
					NodeSSH:       v1alpha1.NodeSSHSpec{KeyPairRef: v1alpha1.SecretRef{Name: "ssh-key"}},
				},
				Nodes: []v1alpha1.OCPNodeSpec{{
					Name:       "master-0",
					Role:       v1alpha1.NodeRoleMaster,
					MachineRef: v1alpha1.LocalObjectReference{Name: "master-0"},
				}},
			},
		}},
	}

	_, err := render.InstallerConfig(state, state.ContainerClusters[0])
	if err == nil {
		t.Fatal("InstallerConfig succeeded, want managed proxy URL error")
	}
	if !strings.Contains(err.Error(), "has no routable address") {
		t.Fatalf("InstallerConfig error = %q, want routable address failure", err)
	}
}

func installerTestMachine(name string) v1alpha1.Machine {
	return v1alpha1.Machine{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.MachineSpec{
			OS: v1alpha1.MachineOSSpec{Provided: v1alpha1.BoolPtr(false)},
		},
	}
}

func vsphereTestMachine(name string) v1alpha1.Machine {
	return v1alpha1.Machine{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.MachineSpec{
			Substrate: v1alpha1.MachineSubstrate{
				ProviderRef: v1alpha1.LocalObjectReference{Name: "vsphere"},
				ProfileRef:  v1alpha1.LocalObjectReference{Name: "control-plane"},
			},
			OS: v1alpha1.MachineOSSpec{
				Provided: v1alpha1.BoolPtr(false),
			},
			Network: v1alpha1.MachineNetwork{
				Config: v1alpha1.MachineNetworkConfig{
					NetworkConfigRef: v1alpha1.LocalObjectReference{Name: "vsphere-net"},
				},
			},
		},
	}
}

func TestAllTightensLooseRenderedDirMode(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "001-sno-libvirt")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}

	renderedDir := t.TempDir()
	if err := os.Chmod(renderedDir, 0o755); err != nil {
		t.Fatalf("seed loose mode: %v", err)
	}
	secretsDir := t.TempDir()
	if _, err := render.All(renderedDir, t.TempDir(), secretsDir, state); err != nil {
		t.Fatalf("render.All: %v", err)
	}
	assertDirMode(t, renderedDir, 0o700)
}

func TestInstallerConfigDerivesManagedMirrorImageDigestSources(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "001-sno-libvirt")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	state.Secrets = append(state.Secrets, v1alpha1.Secret{
		Metadata: v1alpha1.Metadata{Name: "mirror-trust"},
		Spec: v1alpha1.SecretSpec{
			Type:   v1alpha1.SecretTypeCABundle,
			Source: v1alpha1.SecretSource{Generated: &v1alpha1.SecretGeneratedSource{CommonName: "bastion"}},
		},
	})
	state.Environments[0].Spec.Registries = &v1alpha1.EnvironmentRegistriesSpec{Mirror: &v1alpha1.EnvironmentRegistryMirrorSpec{
		TrustBundleRef: v1alpha1.SecretRef{Name: "mirror-trust"},
	}}
	state.Environments[0].Spec.InfraComponents.ArtifactServers = []v1alpha1.EnvironmentArtifactServerComponent{{
		Name:         "default",
		Management:   v1alpha1.EnvironmentComponentManaged,
		ComponentRef: v1alpha1.LocalObjectReference{Name: "artifact-server"},
	}}
	state.ContainerClusters[0].Spec.Install.Agent.BootArtifacts = v1alpha1.ArtifactServerEndpointConsumer{
		ArtifactServerEndpoint: v1alpha1.ArtifactServerEndpointRef{
			EndpointRef: v1alpha1.LocalObjectReference{Name: "cluster"},
		},
	}
	state.Environments[0].Spec.InfraComponents.Registries = []v1alpha1.EnvironmentRegistryComponent{{
		Name:         "default",
		Management:   v1alpha1.EnvironmentComponentManaged,
		ComponentRef: v1alpha1.LocalObjectReference{Name: "registry"},
	}}
	for i := range state.Machines {
		if state.Machines[i].Metadata.Name == "bastion" {
			state.Machines[i].Spec.Addresses[1].Address = "192.168.132.99"
		}
	}
	state.InfraComponents = append(state.InfraComponents,
		v1alpha1.InfraComponent{
			Metadata: v1alpha1.Metadata{Name: "artifact-server"},
			Spec: v1alpha1.InfraComponentSpec{
				Type: v1alpha1.ComponentSlotArtifactServer, ArtifactServer: &v1alpha1.ArtifactServerComponent{
					MachineRef: v1alpha1.LocalObjectReference{Name: "bastion"},
					Listeners: []v1alpha1.ArtifactServerListener{{
						Name:     "https",
						Protocol: v1alpha1.ArtifactServerProtocolHTTPS,
						Port:     9443,
					}},
					Endpoints: []v1alpha1.ArtifactServerEndpoint{{
						Name:        "cluster",
						ListenerRef: v1alpha1.LocalObjectReference{Name: "https"},
						AddressRef:  v1alpha1.LocalObjectReference{Name: "cluster-lan"},
					}},
				}},
		},
		v1alpha1.InfraComponent{
			Metadata: v1alpha1.Metadata{Name: "registry"},
			Spec: v1alpha1.InfraComponentSpec{
				Type: v1alpha1.ComponentSlotRegistry, Registry: &v1alpha1.RegistryComponent{
					Implementation: v1alpha1.InfraComponentTypeMirrorRegistry,
					MachineRef:     v1alpha1.LocalObjectReference{Name: "bastion"},
					Port:           5000,
				}},
		},
	)
	state.ContainerClusters[0].Spec.Install.Mode = v1alpha1.InstallModeDisconnected
	desiredstate.Normalize(&state)
	cfg, err := render.InstallerConfig(state, state.ContainerClusters[0])
	if err != nil {
		t.Fatalf("InstallerConfig: %v", err)
	}
	agent, err := render.AgentConfig(state, state.ContainerClusters[0])
	if err != nil {
		t.Fatalf("AgentConfig: %v", err)
	}
	if got := agent["bootArtifactsBaseURL"]; got != "https://192.168.132.99:9443/" {
		t.Fatalf("bootArtifactsBaseURL = %v, want artifact access endpoint URL", got)
	}
	sources, ok := cfg["imageDigestSources"].([]any)
	if !ok {
		t.Fatalf("imageDigestSources missing or wrong type: %v", cfg["imageDigestSources"])
	}
	wantMirror := "192.168.132.1:5000/" + v1alpha1.DefaultMirroredReleasePath
	for _, raw := range sources {
		src := raw.(map[string]any)
		if src["source"] != v1alpha1.OCPReleaseSourceQuayOCPRelease {
			continue
		}
		if src["sourcePolicy"] != v1alpha1.ImageSourcePolicyNever {
			t.Fatalf("sourcePolicy got %v, want %s", src["sourcePolicy"], v1alpha1.ImageSourcePolicyNever)
		}
		for _, mirror := range src["mirrors"].([]any) {
			if mirror == wantMirror {
				return
			}
		}
		t.Fatalf("source %s missing mirror %q: %v", v1alpha1.OCPReleaseSourceQuayOCPRelease, wantMirror, src["mirrors"])
	}
	t.Fatalf("imageDigestSources missing %s: %v", v1alpha1.OCPReleaseSourceQuayOCPRelease, sources)
}

func TestAgentConfigUsesExternalArtifactServerEndpointForDisconnectedBootArtifacts(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "001-sno-libvirt")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	state.Environments[0].Spec.InfraComponents.ArtifactServers = []v1alpha1.EnvironmentArtifactServerComponent{{
		Name:       "default",
		Management: v1alpha1.EnvironmentComponentExternal,
		Endpoints: []v1alpha1.EnvironmentArtifactServerEndpoint{{
			Name: "install",
			URL:  "https://artifacts.example.test:9443/install",
		}},
	}}
	state.ContainerClusters[0].Spec.Install.Agent.BootArtifacts = v1alpha1.ArtifactServerEndpointConsumer{
		ArtifactServerEndpoint: v1alpha1.ArtifactServerEndpointRef{
			EndpointRef: v1alpha1.LocalObjectReference{Name: "install"},
		},
	}
	state.ContainerClusters[0].Spec.Install.Mode = v1alpha1.InstallModeDisconnected
	desiredstate.Normalize(&state)

	agent, err := render.AgentConfig(state, state.ContainerClusters[0])
	if err != nil {
		t.Fatalf("AgentConfig: %v", err)
	}
	if got := agent["bootArtifactsBaseURL"]; got != "https://artifacts.example.test:9443/install/" {
		t.Fatalf("bootArtifactsBaseURL = %v, want external artifact endpoint URL", got)
	}
}

func TestInstallerConfigUsesOKDReleaseImageForDisconnectedMirror(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "001-sno-libvirt")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	state.Environments[0].Spec.Registries = &v1alpha1.EnvironmentRegistriesSpec{
		Mirror: &v1alpha1.EnvironmentRegistryMirrorSpec{
			URL:            "registry.example.test:5000",
			TrustBundleRef: v1alpha1.SecretRef{Name: "mirror-trust"},
		},
	}
	ocp := state.ContainerClusters[0]
	ocp.Spec.Distribution.Type = v1alpha1.DistributionOKD
	ocp.Spec.Distribution.Release = v1alpha1.ReleaseSpec{Image: "quay.io/okd/scos-release:4.20.0-okd-scos.13"}
	ocp.Spec.Install.Mode = v1alpha1.InstallModeDisconnected
	ocp.Spec.Install.PullSecretRef = v1alpha1.SecretRef{}

	cfg, err := render.InstallerConfig(state, ocp)
	if err != nil {
		t.Fatalf("InstallerConfig: %v", err)
	}
	sources, ok := cfg["imageDigestSources"].([]any)
	if !ok {
		t.Fatalf("imageDigestSources missing or wrong type: %v", cfg["imageDigestSources"])
	}
	if len(sources) != 1 {
		t.Fatalf("imageDigestSources len = %d, want 1: %v", len(sources), sources)
	}
	src := sources[0].(map[string]any)
	if src["source"] != "quay.io/okd/scos-release" {
		t.Fatalf("source = %v, want OKD release source", src["source"])
	}
}

func TestInstallerConfigRendersPlatformNoneForSingleNode(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "001-sno-libvirt")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}

	cfg, err := render.InstallerConfig(state, state.ContainerClusters[0])
	if err != nil {
		t.Fatalf("InstallerConfig: %v", err)
	}
	platform := cfg["platform"].(map[string]any)
	if _, ok := platform["none"].(map[string]any); !ok {
		t.Fatalf("single-node install-config platform got %v, want none", platform)
	}
	if _, ok := platform["baremetal"]; ok {
		t.Fatalf("single-node install-config rendered baremetal platform: %v", platform)
	}
}

func TestInstallerConfigRendersProvisioningNetworkForBareMetal(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "003-3nodes-libvirt")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	gotState := state.ContainerClusters[0].Spec.Install.Platform.BareMetal.ProvisioningNetwork
	if gotState != v1alpha1.ProvisioningNetworkDisabled {
		t.Fatalf("desired-state provisioningNetwork got %q, want %q", gotState, v1alpha1.ProvisioningNetworkDisabled)
	}

	cfg, err := render.InstallerConfig(state, state.ContainerClusters[0])
	if err != nil {
		t.Fatalf("InstallerConfig: %v", err)
	}
	platform := cfg["platform"].(map[string]any)
	baremetal := platform["baremetal"].(map[string]any)
	if got := baremetal["provisioningNetwork"]; got != "Disabled" {
		t.Errorf("install-config provisioningNetwork got %v, want Disabled", got)
	}
}

func TestAgentConfigRendersLibvirtGeneratedInterface(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "001-sno-libvirt")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	agent, err := render.AgentConfig(state, state.ContainerClusters[0])
	if err != nil {
		t.Fatalf("AgentConfig: %v", err)
	}
	hosts := agent["hosts"].([]any)
	host := hosts[0].(map[string]any)
	interfaces := host["interfaces"].([]any)
	if len(interfaces) != 1 {
		t.Fatalf("agent host interfaces got %d, want 1: %v", len(interfaces), interfaces)
	}
	iface := interfaces[0].(map[string]any)
	if got := iface["name"]; got != "primary" {
		t.Errorf("agent host interface name got %v, want primary", got)
	}
	if got := iface["macAddress"]; got != "52:54:00:16:3c:f8" {
		t.Errorf("agent host interface macAddress got %v, want deterministic libvirt MAC", got)
	}
	networkConfig := host["networkConfig"].(map[string]any)
	nmInterfaces := networkConfig["interfaces"].([]any)
	nmPrimary := nmInterfaces[0].(map[string]any)
	if got := nmPrimary["mac-address"]; got != "52:54:00:16:3c:f8" {
		t.Errorf("networkConfig primary mac-address got %v, want deterministic libvirt MAC", got)
	}

	vars := inventory.Vars(state)
	cluster := vars["bootwright_clusters"].([]any)[0].(map[string]any)
	machine := firstMachineComponent(t, cluster)
	machineInterfaces := machine["interfaces"].([]any)
	machinePrimary := machineInterfaces[0].(map[string]any)
	if got := machinePrimary["macAddress"]; got != "52:54:00:16:3c:f8" {
		t.Errorf("vars machine interface macAddress got %v, want deterministic libvirt MAC", got)
	}
}

func TestAgentConfigRendersInfraComponentNTPSources(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "001-sno-libvirt")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	state.Environments[0].Spec.InfraComponents.NTP = []v1alpha1.EnvironmentNTPComponent{
		{Name: "external", Management: v1alpha1.EnvironmentComponentExternal, Address: "ntp.example.test"},
		{Name: "managed", Management: v1alpha1.EnvironmentComponentManaged, ComponentRef: v1alpha1.LocalObjectReference{Name: "ntp-server"}, EndpointRef: v1alpha1.LocalObjectReference{Name: "cluster"}},
		{Name: "duplicate", Management: v1alpha1.EnvironmentComponentExternal, Address: "192.168.132.1"},
	}

	agent, err := render.AgentConfig(state, state.ContainerClusters[0])
	if err != nil {
		t.Fatalf("AgentConfig: %v", err)
	}
	want := []any{"ntp.example.test", "192.168.132.1"}
	if got := agent["additionalNTPSources"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("additionalNTPSources got %v, want %v", got, want)
	}

	vars := inventory.Vars(state)
	env := vars["bootwright_environment"].(map[string]any)
	if _, ok := env["ntp"]; ok {
		t.Fatalf("bootwright_environment rendered top-level ntp: %v", env["ntp"])
	}
	infra := env["infraComponents"].(map[string]any)
	entries := infra["ntp"].([]any)
	if len(entries) != 3 {
		t.Fatalf("infraComponents.ntp got %v", entries)
	}
	first := entries[0].(map[string]any)
	if first["name"] != "external" || first["management"] != v1alpha1.EnvironmentComponentExternal || first["address"] != "ntp.example.test" {
		t.Fatalf("external ntp source vars got %v", first)
	}
	if got := vars["bootwright_resolved_ntp_sources"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("bootwright_resolved_ntp_sources got %v, want %v", got, want)
	}
}

func TestAgentConfigRendersProviderRootDeviceHints(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "005-3nodes-baremetal")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	agent, err := render.AgentConfig(state, state.ContainerClusters[0])
	if err != nil {
		t.Fatalf("AgentConfig: %v", err)
	}
	hosts := agent["hosts"].([]any)
	host := hosts[0].(map[string]any)
	hints := host["rootDeviceHints"].(map[string]any)
	if got := hints["deviceName"]; got != "/dev/sda" {
		t.Fatalf("rootDeviceHints.deviceName got %v, want /dev/sda", got)
	}

	vars := inventory.Vars(state)
	cluster := vars["bootwright_clusters"].([]any)[0].(map[string]any)
	machine := firstMachineComponent(t, cluster)
	machineHints := machine["rootDeviceHints"].(map[string]any)
	if got := machineHints["deviceName"]; got != "/dev/sda" {
		t.Fatalf("vars rootDeviceHints.deviceName got %v, want /dev/sda", got)
	}
}

func TestInstallerConfigRendersVSphereProviderPlatform(t *testing.T) {
	state := v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Spec: v1alpha1.EnvironmentSpec{BaseDomain: "example.test"},
		}},
		NetworkConfigs: []v1alpha1.NetworkConfig{{
			Metadata: v1alpha1.Metadata{Name: "vsphere-net"},
			Spec: v1alpha1.NetworkConfigSpec{
				MachineNetwork: []v1alpha1.MachineNetworkCIDR{{CIDR: "192.168.133.0/24"}},
			},
		}},
		InfraProviders: []v1alpha1.InfraProvider{{
			Metadata: v1alpha1.Metadata{Name: "vsphere"},
			Spec: v1alpha1.InfraProviderSpec{
				Type: v1alpha1.ProvisionerVSphere,
				VSphere: &v1alpha1.InfraProviderVSphere{
					VCenters: []v1alpha1.VSphereVCenter{{
						Server:         "vcenter.example.test",
						Port:           443,
						Datacenters:    []string{"dc1"},
						CredentialsRef: v1alpha1.SecretRef{Name: "vcenter-credentials"},
					}},
					FailureDomains: []v1alpha1.VSphereFailureDomain{{
						Name:   "dc1-zone-a",
						Region: "dc1",
						Zone:   "zone-a",
						Server: "vcenter.example.test",
						Topology: v1alpha1.VSphereFailureTopology{
							Datacenter:     "dc1",
							ComputeCluster: "/dc1/host/cluster1",
							Datastore:      "/dc1/datastore/datastore1",
							Folder:         "/dc1/vm/bootwright",
							ResourcePool:   "/dc1/host/cluster1/Resources/bootwright",
							Networks:       []string{"VM_Network_1"},
						},
					}},
					NodeNetworking: &v1alpha1.VSphereNodeNetworking{
						External: &v1alpha1.VSphereNetworkSubnet{NetworkSubnetCIDR: []string{"192.168.133.0/24"}},
					},
					MachineProfiles: []v1alpha1.MachineProfile{{
						Name:             "control-plane",
						FailureDomainRef: v1alpha1.LocalObjectReference{Name: "dc1-zone-a"},
					}},
				},
			},
		}},
		Machines: []v1alpha1.Machine{
			vsphereTestMachine("master-0"),
			vsphereTestMachine("master-1"),
			vsphereTestMachine("master-2"),
		},
	}
	ocp := v1alpha1.ContainerCluster{
		Metadata: v1alpha1.Metadata{Name: "ocp"},
		Spec: v1alpha1.ContainerClusterSpec{
			Install: v1alpha1.OCPInstallSpec{
				Platform: v1alpha1.InstallPlatform{Type: v1alpha1.PlatformTypeVSphere},
				Endpoints: map[string]v1alpha1.Endpoint{
					v1alpha1.EndpointAPI:     {Address: "192.168.133.10"},
					v1alpha1.EndpointAPIInt:  {Address: "192.168.133.10"},
					v1alpha1.EndpointIngress: {Address: "192.168.133.11"},
				},
			},
			Nodes: []v1alpha1.OCPNodeSpec{{
				Name:       "master-0",
				Role:       v1alpha1.NodeRoleMaster,
				MachineRef: v1alpha1.LocalObjectReference{Name: "master-0"},
			}, {
				Name:       "master-1",
				Role:       v1alpha1.NodeRoleMaster,
				MachineRef: v1alpha1.LocalObjectReference{Name: "master-1"},
			}, {
				Name:       "master-2",
				Role:       v1alpha1.NodeRoleMaster,
				MachineRef: v1alpha1.LocalObjectReference{Name: "master-2"},
			}},
		},
	}

	cfg, err := render.InstallerConfig(state, ocp)
	if err != nil {
		t.Fatalf("InstallerConfig: %v", err)
	}
	platform := cfg["platform"].(map[string]any)
	vsphere := platform["vsphere"].(map[string]any)
	if len(vsphere["vcenters"].([]any)) != 1 {
		t.Fatalf("vcenters not rendered: %v", vsphere)
	}
	if len(vsphere["failureDomains"].([]any)) != 1 {
		t.Fatalf("failureDomains not rendered: %v", vsphere)
	}
	if _, ok := vsphere["nodeNetworking"].(map[string]any); !ok {
		t.Fatalf("nodeNetworking not rendered: %v", vsphere)
	}
}

func TestRenderCoreSharedOutputsStayInStep(t *testing.T) {
	t.Setenv("HOME", "/bootwright-golden/home")
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "001-sno-libvirt")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}

	allDir := t.TempDir()
	if _, err := render.All(allDir, t.TempDir(), "/bootwright-golden/secrets", state); err != nil {
		t.Fatalf("render.All: %v", err)
	}
	toolDir := t.TempDir()
	if _, err := render.ToolInputsPortable(toolDir, state); err != nil {
		t.Fatalf("render.ToolInputsPortable: %v", err)
	}

	for _, name := range []string{"effective-state.yaml", "bootwright.lock.yaml"} {
		fromAll, err := os.ReadFile(filepath.Join(allDir, name))
		if err != nil {
			t.Fatalf("read All %s: %v", name, err)
		}
		fromTool, err := os.ReadFile(filepath.Join(toolDir, name))
		if err != nil {
			t.Fatalf("read ToolInputs %s: %v", name, err)
		}
		if !bytes.Equal(fromAll, fromTool) {
			t.Fatalf("%s drifted between render.All and the tool-input render", name)
		}
	}
}
