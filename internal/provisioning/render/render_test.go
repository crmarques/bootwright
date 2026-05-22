package render_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/provisioning/render"
)

// fixtureRoot is the relative path from internal/provisioning/render to
// the canonical good fixtures already used by the desiredstate validator
// tests. Sharing the fixtures avoids fixture drift and ensures the
// render pipeline stays in step with what the validator accepts.
const fixtureRoot = "../../desiredstate/testdata/good"

// TestAllSucceedsForGoodFixtures runs render.All over each good fixture
// and asserts the renderer produces all four canonical artifacts
// (effective-state, lock, inventory, vars) plus per-cluster install
// assets, with the file modes the rest of the pipeline relies on.
//
// This is a smoke test rather than a golden test — Go map iteration is
// nondeterministic and YAML marshal layout shifts between go.yaml.in
// versions, so byte equality would be brittle. We instead assert
// structural invariants the downstream Ansible layers depend on.
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

			stateDir := t.TempDir()
			secretsDir := t.TempDir() // empty — All() must not consult it
			result, err := render.All(stateDir, secretsDir, state)
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
			// The state directory and every subdir under it hold
			// rendered artifacts that contain (or will contain after
			// ResolveInstaller) secret material. Mode 0700 is the
			// security boundary the comment on render.go:55-58 calls
			// out — keep these assertions as the invariant.
			assertDirMode(t, stateDir, 0o700)
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
				// Placeholder install-config carries placeholders
				// only, without leaking secret material.
				// Placeholder markers MUST be present; real-looking
				// secret material (PEM keys, base64-style "auth"
				// values) MUST NOT.
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

// TestAllTightensLooseStateDirMode verifies render.All chmods a
// pre-existing state directory that was created with looser permissions
// back down to 0700. The Chmod-after-MkdirAll sequence in render.go is
// the only thing that guards against a state dir created by a user
// umask of 0022 (which leaves 0755); removing that Chmod would silently
// expose secret material to other local users. This test fails fast if
// the Chmod call disappears or its mode constant drifts.
func TestAllTightensLooseStateDirMode(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "001-sno-libvirt")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}

	stateDir := t.TempDir()
	if err := os.Chmod(stateDir, 0o755); err != nil {
		t.Fatalf("seed loose mode: %v", err)
	}
	secretsDir := t.TempDir()
	if _, err := render.All(stateDir, secretsDir, state); err != nil {
		t.Fatalf("render.All: %v", err)
	}
	assertDirMode(t, stateDir, 0o700)
}

// TestAllIsStableAcrossRuns asserts render.All produces byte-identical
// inventory/vars on a second invocation against the same state. This
// is the floor for downstream determinism — Ansible artifact dirs and
// the GitOps publish target both diff these files between runs.
func TestAllIsStableAcrossRuns(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "001-sno-libvirt")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}

	first := t.TempDir()
	second := t.TempDir()
	secretsDir := t.TempDir()
	a, err := render.All(first, secretsDir, state)
	if err != nil {
		t.Fatalf("first All: %v", err)
	}
	b, err := render.All(second, secretsDir, state)
	if err != nil {
		t.Fatalf("second All: %v", err)
	}

	for _, pair := range []struct {
		label string
		left  string
		right string
	}{
		{"inventory", a.InventoryPath, b.InventoryPath},
		{"vars", a.VarsPath, b.VarsPath},
	} {
		if !sameFile(t, pair.left, pair.right) {
			t.Fatalf("%s output differs between runs:\n  %s\n  %s", pair.label, pair.left, pair.right)
		}
	}
}

func TestInstallerConfigDerivesManagedMirrorImageDigestSources(t *testing.T) {
	srcRoot := filepath.Join(fixtureRoot, "001-sno-libvirt")
	dir := t.TempDir()
	for _, name := range []string{"environment.yaml", "hosts.yaml", "networks.yaml", "provider.yaml", "cluster.yaml"} {
		body, err := os.ReadFile(filepath.Join(srcRoot, name))
		if err != nil {
			t.Fatalf("read fixture %s: %v", name, err)
		}
		text := string(body)
		switch name {
		case "environment.yaml":
			text = strings.Replace(text, "    proxy-credentials:\n      generated:\n        credentials:\n          username: proxy\n", "    proxy-credentials:\n      generated:\n        credentials:\n          username: proxy\n    mirror-trust:\n      generated:\n        selfSignedCertificate:\n          commonName: lab-host\n", 1)
			text = strings.Replace(text, "  proxy:\n", "  registries:\n    mirror:\n      trustBundleRef:\n        name: mirror-trust\n  proxy:\n", 1)
		case "hosts.yaml":
			text = strings.Replace(text, "    - name: cluster-lan\n      address: 192.168.132.1\n", "    - name: cluster-lan\n      address: 192.168.132.99\n", 1)
		case "provider.yaml":
			text = strings.Replace(text, "        hostRef:\n          name: lab-host\n        routes:", "        hostRef:\n          name: lab-host\n        port: 9443\n        routes:", 1)
			needle := "  dns:\n    - name: default\n      dnsmasq:\n        hostRef:\n          name: lab-host\n"
			text = strings.Replace(text, needle, needle+"\n  registries:\n    - name: default\n      mirrorRegistry:\n        hostRef:\n          name: lab-host\n", 1)
		case "cluster.yaml":
			text = strings.Replace(text, "    mode: connected\n", "    mode: disconnected\n", 1)
			needle := "      additionalIngressHosts:\n        - console-openshift-console.apps.sno-libvirt.bootwright.test\n        - oauth-openshift.apps.sno-libvirt.bootwright.test\n"
			text = strings.Replace(text, needle, needle+"\n    registry:\n      from:\n        provider: lab-libvirt-provider\n        name: default\n      port: 5000\n      bindAddress: 0.0.0.0\n", 1)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatalf("write fixture %s: %v", name, err)
		}
	}

	state, err := desiredstate.LoadNormalizeValidate([]string{dir})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	cfg, err := render.InstallerConfig(state, state.ContainerClusters[0])
	if err != nil {
		t.Fatalf("InstallerConfig: %v", err)
	}
	agent, err := render.AgentConfig(state, state.ContainerClusters[0])
	if err != nil {
		t.Fatalf("AgentConfig: %v", err)
	}
	if got := agent["bootArtifactsBaseURL"]; got != "https://192.168.132.99:9443/" {
		t.Fatalf("bootArtifactsBaseURL = %v, want route-derived URL", got)
	}
	sources, ok := cfg["imageDigestSources"].([]any)
	if !ok {
		t.Fatalf("imageDigestSources missing or wrong type: %v", cfg["imageDigestSources"])
	}
	// lab-host SSH address resolves to localhost; ClusterFacingHostAddress
	// substitutes the gateway of the cluster's api endpoint network
	// (sno-bridge → 192.168.132.1) so the mirror URL is reachable from
	// cluster guests.
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
	ocp.Spec.Install.ImageDigestSources = nil

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
	gotState := state.ClusterInfras[0].Spec.Platform.BareMetal.ProvisioningNetwork
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

	vars := render.Vars(state)
	cluster := vars["bootwright_clusters"].([]any)[0].(map[string]any)
	machine := firstMachineComponent(t, cluster)
	machineInterfaces := machine["interfaces"].([]any)
	machinePrimary := machineInterfaces[0].(map[string]any)
	if got := machinePrimary["macAddress"]; got != "52:54:00:16:3c:f8" {
		t.Errorf("vars machine interface macAddress got %v, want deterministic libvirt MAC", got)
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

	vars := render.Vars(state)
	cluster := vars["bootwright_clusters"].([]any)[0].(map[string]any)
	machine := firstMachineComponent(t, cluster)
	machineHints := machine["rootDeviceHints"].(map[string]any)
	if got := machineHints["deviceName"]; got != "/dev/sda" {
		t.Fatalf("vars rootDeviceHints.deviceName got %v, want /dev/sda", got)
	}
}

func TestInstallerConfigRendersVSphereProviderPlatform(t *testing.T) {
	state := v1alpha1.State{
		NetworkConfigs: []v1alpha1.NetworkConfig{{
			Metadata: v1alpha1.Metadata{Name: "vsphere-net"},
			Spec: v1alpha1.NetworkConfigSpec{
				MachineNetwork: []v1alpha1.MachineNetworkCIDR{{CIDR: "192.168.133.0/24"}},
			},
		}},
		InfraProviders: []v1alpha1.InfraProvider{{
			Metadata: v1alpha1.Metadata{Name: "vsphere"},
			Spec: v1alpha1.InfraProviderSpec{
				MachineProfiles: []v1alpha1.MachineProfileCapability{{
					Name: "control-plane",
					VSphere: &v1alpha1.MachineProfileVSphereProvisioner{
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
					},
				}},
			},
		}},
		ClusterInfras: []v1alpha1.ClusterInfra{{
			Metadata: v1alpha1.Metadata{Name: "infra"},
			Spec: v1alpha1.ClusterInfraSpec{
				Platform: v1alpha1.ClusterInfraPlatform{Type: v1alpha1.PlatformTypeVSphere},
				Endpoints: map[string]v1alpha1.Endpoint{
					v1alpha1.EndpointAPI:     {ExternalVIP: "192.168.133.10"},
					v1alpha1.EndpointAPIInt:  {ExternalVIP: "192.168.133.10"},
					v1alpha1.EndpointIngress: {ExternalVIP: "192.168.133.11"},
				},
				Components: v1alpha1.ClusterComponents{Machines: []v1alpha1.ClusterMachineComponent{{
					Name:          "master-0",
					From:          v1alpha1.From{Provider: "vsphere", Profile: "control-plane"},
					NetworkConfig: v1alpha1.ClusterMachineNetworkConfig{Ref: v1alpha1.LocalObjectReference{Name: "vsphere-net"}},
				}, {
					Name:          "master-1",
					From:          v1alpha1.From{Provider: "vsphere", Profile: "control-plane"},
					NetworkConfig: v1alpha1.ClusterMachineNetworkConfig{Ref: v1alpha1.LocalObjectReference{Name: "vsphere-net"}},
				}, {
					Name:          "master-2",
					From:          v1alpha1.From{Provider: "vsphere", Profile: "control-plane"},
					NetworkConfig: v1alpha1.ClusterMachineNetworkConfig{Ref: v1alpha1.LocalObjectReference{Name: "vsphere-net"}},
				}}},
			},
		}},
	}
	ocp := v1alpha1.ContainerCluster{
		Metadata: v1alpha1.Metadata{Name: "ocp"},
		Spec: v1alpha1.ContainerClusterSpec{
			Install: v1alpha1.OCPInstallSpec{BaseDomain: "example.test"},
			Nodes: []v1alpha1.OCPNodeSpec{{
				Hostname:   "master-0",
				Role:       v1alpha1.NodeRoleMaster,
				MachineRef: v1alpha1.NodeMachineRef{ClusterInfra: "infra", Name: "master-0"},
			}, {
				Hostname:   "master-1",
				Role:       v1alpha1.NodeRoleMaster,
				MachineRef: v1alpha1.NodeMachineRef{ClusterInfra: "infra", Name: "master-1"},
			}, {
				Hostname:   "master-2",
				Role:       v1alpha1.NodeRoleMaster,
				MachineRef: v1alpha1.NodeMachineRef{ClusterInfra: "infra", Name: "master-2"},
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
