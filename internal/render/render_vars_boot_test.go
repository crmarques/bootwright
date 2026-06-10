package render_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/state/desired"
)

func TestComponentPinsIncludeManagedDNSImage(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "001-sno-libvirt")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	pins := map[string]render.ComponentPin{}
	for _, pin := range render.ComponentPins(state) {
		pins[pin.Name] = pin
	}
	pin, ok := pins[v1alpha1.InfraComponentTypeDnsmasq]
	if !ok {
		t.Fatalf("dnsmasq pin missing from managed DNS state: %v", pins)
	}
	if pin.Version != "2.92_p2" {
		t.Fatalf("dnsmasq version got %q, want 2.92_p2", pin.Version)
	}
}

func TestVarsProjectResolvedComponentImages(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "001-sno-libvirt")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	state.Environments[0].Spec.ComponentImages = map[string]map[string]v1alpha1.ComponentImageSpec{
		v1alpha1.ComponentSlotNameResolution: {
			v1alpha1.InfraComponentTypeDnsmasq: {Public: "registry.example/dnsmasq:2.92_p2"},
		},
	}

	vars := render.Vars(state)
	cluster := vars["bootwright_clusters"].([]any)[0].(map[string]any)
	dns := componentByKind(t, cluster, v1alpha1.ComponentSlotNameResolution)
	if got := dns["image"]; got != "registry.example/dnsmasq:2.92_p2" {
		t.Fatalf("nameResolution image got %v", got)
	}
	lb := componentByKind(t, cluster, v1alpha1.ComponentSlotLoadBalancer)
	if got := lb["image"]; got != "docker.io/library/haproxy:3.3.10" {
		t.Fatalf("loadBalancer image got %v", got)
	}
}

func TestVarsProjectNodeSSHPrivateKeyPath(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "001-sno-libvirt")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	sourceDir := t.TempDir()
	state.Environments[0].SourcePath = filepath.Join(sourceDir, "environment.yaml")
	keyName := v1alpha1.ClusterAdminSSHKeyName("sno-libvirt")
	state.Environments[0].Spec.Secrets[keyName] = v1alpha1.EnvironmentSecretSpec{
		File: "keys/admin.pub",
	}
	vars := render.VarsWithSecretsDir(state, t.TempDir())
	cluster := vars["bootwright_clusters"].([]any)[0].(map[string]any)
	want := filepath.Join(sourceDir, "keys", "admin")
	if got := cluster["nodeSSHPrivateKeyPath"]; got != want {
		t.Fatalf("nodeSSHPrivateKeyPath got %v, want %s", got, want)
	}
}

func TestVarsProjectGeneratedNodeSSHPrivateKeyPath(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "001-sno-libvirt")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	keyName := v1alpha1.ClusterAdminSSHKeyName("sno-libvirt")
	state.Environments[0].Spec.Secrets[keyName] = v1alpha1.EnvironmentSecretSpec{
		Generated: &v1alpha1.EnvironmentSecretGenerated{
			SSHKeyPair: &v1alpha1.GeneratedSSHKeyPairSpec{Type: v1alpha1.SSHKeyPairTypeEd25519},
		},
	}
	secretsDir := t.TempDir()
	vars := render.VarsWithSecretsDir(state, secretsDir)
	cluster := vars["bootwright_clusters"].([]any)[0].(map[string]any)
	want := filepath.Join(secretsDir, keyName)
	if got := cluster["nodeSSHPrivateKeyPath"]; got != want {
		t.Fatalf("nodeSSHPrivateKeyPath got %v, want %s", got, want)
	}
}

func TestVarsProjectSplitNodeSSHPrivateKeyPath(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "001-sno-libvirt")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	state.ContainerClusters[0].Spec.Install.NodeSSH = v1alpha1.NodeSSHSpec{
		PublicKeyRef:  v1alpha1.SecretRef{Name: "cluster-admin-public"},
		PrivateKeyRef: v1alpha1.SecretRef{Name: "cluster-admin-private"},
	}
	state.Environments[0].Spec.Secrets["cluster-admin-public"] = v1alpha1.EnvironmentSecretSpec{File: "keys/admin.pub"}
	state.Environments[0].Spec.Secrets["cluster-admin-private"] = v1alpha1.EnvironmentSecretSpec{File: "keys/admin"}
	sourceDir := t.TempDir()
	state.Environments[0].SourcePath = filepath.Join(sourceDir, "environment.yaml")
	vars := render.VarsWithSecretsDir(state, t.TempDir())
	cluster := vars["bootwright_clusters"].([]any)[0].(map[string]any)
	want := filepath.Join(sourceDir, "keys", "admin")
	if got := cluster["nodeSSHPrivateKeyPath"]; got != want {
		t.Fatalf("nodeSSHPrivateKeyPath got %v, want %s", got, want)
	}
}

// TestMachineBootBlockProjectsSubstrateBlind pins the boot_redfish
// contract: every Redfish-driven machine carries a fully-resolved
// boot.{redfish,agentIso} tuple so the role does NOT branch on
// bmcRole or look up provider components at runtime. Covers both
// substrate arms: libvirt-emulated Redfish (001) and bare-metal vendor BMC
// (002) — because the leak that prompted this contract was
// emulator-specific staging logic surfacing on the bare-metal path.
func TestMachineBootBlockProjectsSubstrateBlind(t *testing.T) {
	cases := []struct {
		fixture       string
		wantBaseURL   string
		wantSystemID  string
		wantCredRef   string
		wantValidate  bool
		wantStageHost string
		wantStagePath string
		wantFetchURL  string
	}{
		{
			fixture:       "001-sno-libvirt",
			wantBaseURL:   "http://localhost:8000",
			wantSystemID:  "7ec5d09a-2be9-5fc1-8505-0dea5887aa8a",
			wantCredRef:   "bmc-credentials",
			wantValidate:  false,
			wantStageHost: "bastion",
			wantStagePath: "/var/lib/libvirt/images/bootwright/{{ bootwright_provider_state_dir | dirname | basename }}/bmc/lab-libvirt-provider/vmedia/__BOOTWRIGHT_AGENT_ISO_PUBLISH_TOKEN__/agent-sno-libvirt.iso",
			wantFetchURL:  "http://127.0.0.1:8001/__BOOTWRIGHT_AGENT_ISO_PUBLISH_TOKEN__/agent-sno-libvirt.iso",
		},
		{
			fixture:       "002-sno-emul-baremetal",
			wantBaseURL:   "REPLACE_WITH_REAL_BMC_URL_FOR_master-0",
			wantSystemID:  "",
			wantCredRef:   "bmc-credentials",
			wantValidate:  false,
			wantStageHost: "services-host",
			wantStagePath: "{{ bootwright_managed_services_dir }}/artifact-server/public/__BOOTWRIGHT_AGENT_ISO_PUBLISH_TOKEN__/agent-sno-emul-baremetal.iso",
			wantFetchURL:  "https://192.168.132.1:8443/__BOOTWRIGHT_AGENT_ISO_PUBLISH_TOKEN__/agent-sno-emul-baremetal.iso",
		},
	}

	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, tc.fixture)})
			if err != nil {
				t.Fatalf("LoadNormalizeValidate: %v", err)
			}
			vars := render.Vars(state)
			clusters, ok := vars["bootwright_clusters"].([]any)
			if !ok || len(clusters) == 0 {
				t.Fatalf("vars missing bootwright_clusters: %v", vars["bootwright_clusters"])
			}
			cluster := clusters[0].(map[string]any)
			machine := firstMachineComponent(t, cluster)
			boot, ok := machine["boot"].(map[string]any)
			if !ok {
				t.Fatalf("machine missing boot block: %v", machine)
			}
			redfish, ok := boot["redfish"].(map[string]any)
			if !ok {
				t.Fatalf("boot missing redfish: %v", boot)
			}
			readiness, ok := boot["readiness"].(map[string]any)
			if !ok {
				t.Fatalf("boot missing readiness: %v", boot)
			}
			if got := readiness["type"]; got != "ssh" {
				t.Errorf("readiness.type got %v, want ssh", got)
			}
			ssh, ok := readiness["ssh"].(map[string]any)
			if !ok {
				t.Fatalf("readiness missing ssh: %v", readiness)
			}
			if got := ssh["user"]; got != "core" {
				t.Errorf("readiness.ssh.user got %v, want core", got)
			}
			if got := ssh["port"]; got != 22 {
				t.Errorf("readiness.ssh.port got %v, want 22", got)
			}
			if got := redfish["baseUrl"]; got != tc.wantBaseURL {
				t.Errorf("redfish.baseUrl got %v, want %s", got, tc.wantBaseURL)
			}
			if got := redfish["systemId"]; got != tc.wantSystemID {
				t.Errorf("redfish.systemId got %v, want %s", got, tc.wantSystemID)
			}
			if got := redfish["credentialsRef"]; got != tc.wantCredRef {
				t.Errorf("redfish.credentialsRef got %v, want %s", got, tc.wantCredRef)
			}
			if got := redfish["validateCerts"]; got != tc.wantValidate {
				t.Errorf("redfish.validateCerts got %v, want %v", got, tc.wantValidate)
			}
			if got := machine["bootApplyRole"]; got != "bootwright.core.container_cluster_boot_redfish" {
				t.Errorf("bootApplyRole got %v, want bootwright.core.container_cluster_boot_redfish", got)
			}
			iso, ok := boot["agentIso"].(map[string]any)
			if !ok {
				t.Fatalf("boot missing agentIso: %v", boot)
			}
			if got := iso["stageHost"]; got != tc.wantStageHost {
				t.Errorf("agentIso.stageHost got %v, want %q", got, tc.wantStageHost)
			}
			if got := iso["stagePath"]; got != tc.wantStagePath {
				t.Errorf("agentIso.stagePath got %v, want %q", got, tc.wantStagePath)
			}
			if got := iso["fetchUrl"]; got != tc.wantFetchURL {
				t.Errorf("agentIso.fetchUrl got %v, want %q", got, tc.wantFetchURL)
			}
		})
	}
}

func TestEmulatedLibvirtBootProjectsMediaBackend(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "001-sno-libvirt")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	vars := render.Vars(state)
	cluster := vars["bootwright_clusters"].([]any)[0].(map[string]any)
	machine := firstMachineComponent(t, cluster)
	boot := machine["boot"].(map[string]any)
	media := boot["media"].(map[string]any)
	libvirt, ok := media["libvirt"].(map[string]any)
	if !ok {
		t.Fatalf("boot missing media.libvirt control: %v", boot)
	}
	wants := map[string]any{
		"machineRef": "bastion",
		"uri":        "qemu:///system",
		"domain":     "sno-libvirt-master-0",
	}
	for k, want := range wants {
		if got := libvirt[k]; got != want {
			t.Errorf("boot.media.libvirt.%s got %v, want %v", k, got, want)
		}
	}
	if got := machine["mediaPrepareRole"]; got != "bootwright.core.container_cluster_media_libvirt" {
		t.Fatalf("mediaPrepareRole got %v, want bootwright.core.container_cluster_media_libvirt", got)
	}
	redfish := boot["redfish"].(map[string]any)
	if got := redfish["setBootSource"]; got != false {
		t.Fatalf("setBootSource got %v, want false for media backend", got)
	}
}

func TestBareMetalBootDoesNotProjectMediaBackend(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "002-sno-emul-baremetal")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	vars := render.Vars(state)
	cluster := vars["bootwright_clusters"].([]any)[0].(map[string]any)
	machine := firstMachineComponent(t, cluster)
	boot := machine["boot"].(map[string]any)
	if _, ok := boot["media"]; ok {
		t.Fatalf("bare-metal boot unexpectedly has media backend: %v", boot)
	}
	if _, ok := machine["mediaPrepareRole"]; ok {
		t.Fatalf("bare-metal machine unexpectedly has mediaPrepareRole: %v", machine)
	}
	redfish := boot["redfish"].(map[string]any)
	if got := redfish["setBootSource"]; got != true {
		t.Fatalf("setBootSource got %v, want true for real BMC", got)
	}
}

func TestRendererUsesContainerClusterNameWhenInfraNameDiffers(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "001-sno-libvirt")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	ocp := state.ContainerClusters[0]
	cfg, err := render.InstallerConfig(state, ocp)
	if err != nil {
		t.Fatalf("InstallerConfig: %v", err)
	}
	metadata := cfg["metadata"].(map[string]any)
	if got := metadata["name"]; got != "sno-libvirt" {
		t.Errorf("install-config metadata.name got %v, want sno-libvirt", got)
	}
	if got := cfg["baseDomain"]; got != "bootwright.test" {
		t.Errorf("install-config baseDomain got %v, want bootwright.test", got)
	}
	agent, err := render.AgentConfig(state, ocp)
	if err != nil {
		t.Fatalf("AgentConfig: %v", err)
	}
	agentMetadata := agent["metadata"].(map[string]any)
	if got := agentMetadata["name"]; got != "sno-libvirt" {
		t.Errorf("agent-config metadata.name got %v, want sno-libvirt", got)
	}

	vars := render.Vars(state)
	cluster := vars["bootwright_clusters"].([]any)[0].(map[string]any)
	if got := cluster["name"]; got != "sno-libvirt" {
		t.Errorf("vars cluster.name got %v, want sno-libvirt", got)
	}
	machine := firstMachineComponent(t, cluster)
	if got := machine["clusterName"]; got != "sno-libvirt" {
		t.Errorf("machine clusterName got %v, want sno-libvirt", got)
	}
	boot := machine["boot"].(map[string]any)
	redfish := boot["redfish"].(map[string]any)
	if got := redfish["systemId"]; got != "7ec5d09a-2be9-5fc1-8505-0dea5887aa8a" {
		t.Errorf("redfish.systemId got %v, want ContainerCluster-derived UUID", got)
	}
	iso := boot["agentIso"].(map[string]any)
	if got := iso["stagePath"]; !strings.Contains(got.(string), "agent-sno-libvirt.iso") {
		t.Errorf("agentIso.stagePath got %v, want ContainerCluster-based ISO name", got)
	}
	lb := componentByKind(t, cluster, v1alpha1.ComponentSlotLoadBalancer)
	if got := lb["clusterName"]; got != "sno-libvirt" {
		t.Errorf("loadBalancer clusterName got %v, want sno-libvirt", got)
	}
}

func TestBareMetalArtifactPathUsesContainerClusterName(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "002-sno-emul-baremetal")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	vars := render.Vars(state)
	cluster := vars["bootwright_clusters"].([]any)[0].(map[string]any)
	machine := firstMachineComponent(t, cluster)
	boot := machine["boot"].(map[string]any)
	iso := boot["agentIso"].(map[string]any)
	wantStagePath := "{{ bootwright_managed_services_dir }}/artifact-server/public/__BOOTWRIGHT_AGENT_ISO_PUBLISH_TOKEN__/agent-sno-emul-baremetal.iso"
	if got := iso["stagePath"]; got != wantStagePath {
		t.Errorf("agentIso.stagePath got %v, want %q", got, wantStagePath)
	}
	if got := iso["fetchUrl"]; got != "https://192.168.132.1:8443/__BOOTWRIGHT_AGENT_ISO_PUBLISH_TOKEN__/agent-sno-emul-baremetal.iso" {
		t.Errorf("agentIso.fetchUrl got %v, want ContainerCluster-based ISO name", got)
	}
}

func TestAgentISOPublishTargetsDeduplicateClusterISO(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "005-3nodes-baremetal")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	vars := render.Vars(state)
	cluster := vars["bootwright_clusters"].([]any)[0].(map[string]any)
	targets := cluster["agentIsoPublishTargets"].([]any)
	if len(targets) != 1 {
		t.Fatalf("agentIsoPublishTargets = %v, want one shared target", targets)
	}
	target := targets[0].(map[string]any)
	wants := map[string]any{
		"stageHost":         "bastion",
		"stagePath":         "{{ bootwright_managed_services_dir }}/artifact-server/public/__BOOTWRIGHT_AGENT_ISO_PUBLISH_TOKEN__/agent-3-nodes-ocp-baremetal.iso",
		"fetchUrl":          "https://192.168.140.5:8443/__BOOTWRIGHT_AGENT_ISO_PUBLISH_TOKEN__/agent-3-nodes-ocp-baremetal.iso",
		"requiresHTTPS":     true,
		"requiresByteRange": true,
	}
	for k, want := range wants {
		if got := target[k]; got != want {
			t.Errorf("agentIsoPublishTargets[0].%s got %v, want %v", k, got, want)
		}
	}
}

func TestInstallerAssetsAreClusterScopedForMultipleClusters(t *testing.T) {
	state := twoClusterBareMetalPublicationState(t)
	clustersDir := "/clusters"

	assets := render.InstallerAssets(clustersDir, state)
	if len(assets) != 2 {
		t.Fatalf("installer assets got %d, want 2", len(assets))
	}

	seenDirs := map[string]bool{}
	for _, asset := range assets {
		wantDir := filepath.Join(clustersDir, asset.ClusterName, "rendered", "installer")
		wantWorkDir := filepath.Join(clustersDir, asset.ClusterName, "runtime", "installer")
		wantSecretsDir := filepath.Join(clustersDir, asset.ClusterName, "secrets")
		if asset.Dir != wantDir {
			t.Errorf("%s Dir got %q, want %q", asset.ClusterName, asset.Dir, wantDir)
		}
		if asset.WorkDir != wantWorkDir {
			t.Errorf("%s WorkDir got %q, want %q", asset.ClusterName, asset.WorkDir, wantWorkDir)
		}
		if asset.ClusterSecretsDir != wantSecretsDir {
			t.Errorf("%s ClusterSecretsDir got %q, want %q", asset.ClusterName, asset.ClusterSecretsDir, wantSecretsDir)
		}
		if asset.InstallConfigPath != filepath.Join(wantDir, "install-config.yaml") {
			t.Errorf("%s install-config path got %q", asset.ClusterName, asset.InstallConfigPath)
		}
		if asset.EffectiveInstallConfigPath != filepath.Join(wantWorkDir, "install-config.yaml") {
			t.Errorf("%s effective install-config path got %q", asset.ClusterName, asset.EffectiveInstallConfigPath)
		}
		if seenDirs[asset.Dir] || seenDirs[asset.WorkDir] {
			t.Fatalf("cluster %s reused installer directory: %+v", asset.ClusterName, assets)
		}
		seenDirs[asset.Dir] = true
		seenDirs[asset.WorkDir] = true
	}
}

func TestBareMetalMultiClusterPublicationUsesUniqueClusterISOsOnSharedHTTPRoot(t *testing.T) {
	state := twoClusterBareMetalPublicationState(t)
	vars := render.Vars(state)
	clusters := clustersByName(t, vars)

	roots := map[string]bool{}
	isoNames := map[string]bool{}
	for _, clusterName := range []string{"sno-emul-baremetal", "sno-emul-baremetal-b"} {
		cluster := clusters[clusterName]
		targets := cluster["agentIsoPublishTargets"].([]any)
		if len(targets) != 1 {
			t.Fatalf("%s publish targets = %v, want one", clusterName, targets)
		}
		target := targets[0].(map[string]any)
		machine := firstMachineComponent(t, cluster)
		iso := machine["boot"].(map[string]any)["agentIso"].(map[string]any)
		if iso["stagePath"] != target["stagePath"] || iso["fetchUrl"] != target["fetchUrl"] {
			t.Fatalf("%s machine boot ISO target does not match publish target: machine=%v target=%v", clusterName, iso, target)
		}

		wantISO := "agent-" + clusterName + ".iso"
		stagePath := target["stagePath"].(string)
		fetchURL := target["fetchUrl"].(string)
		if !strings.HasSuffix(stagePath, "/"+wantISO) {
			t.Errorf("%s stagePath got %q, want suffix %q", clusterName, stagePath, wantISO)
		}
		if !strings.HasSuffix(fetchURL, "/"+wantISO) {
			t.Errorf("%s fetchUrl got %q, want suffix %q", clusterName, fetchURL, wantISO)
		}
		if isoNames[wantISO] {
			t.Fatalf("ISO basename %q was reused", wantISO)
		}
		isoNames[wantISO] = true

		root := strings.Split(stagePath, "/__BOOTWRIGHT_AGENT_ISO_PUBLISH_TOKEN__/")[0]
		roots[root] = true
		if root != "{{ bootwright_managed_services_dir }}/artifact-server/public" {
			t.Errorf("%s publish root got %q", clusterName, root)
		}
	}
	if len(roots) != 1 {
		t.Fatalf("clusters should share one artifact HTTP service root, got %v", roots)
	}
}

func TestVarsUseSafeAgentISOPublishTokenPlaceholder(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "005-3nodes-baremetal")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	body := fmt.Sprint(render.Vars(state)["bootwright_clusters"])
	if strings.Contains(body, "{{ bootwright_agent_iso_publish_token }}") {
		t.Fatalf("bootwright_clusters must not contain an undefined Ansible token expression: %s", body)
	}
	if !strings.Contains(body, "__BOOTWRIGHT_AGENT_ISO_PUBLISH_TOKEN__") {
		t.Fatalf("bootwright_clusters missing publish token placeholder: %s", body)
	}
}

func TestBareMetalArtifactFetchURLUsesArtifactServerListenerPort(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "002-sno-emul-baremetal")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	setArtifactHTTPPort(&state, 9443)

	vars := render.Vars(state)
	cluster := vars["bootwright_clusters"].([]any)[0].(map[string]any)
	machine := firstMachineComponent(t, cluster)
	boot := machine["boot"].(map[string]any)
	iso := boot["agentIso"].(map[string]any)
	if got := iso["fetchUrl"]; got != "https://192.168.132.1:9443/__BOOTWRIGHT_AGENT_ISO_PUBLISH_TOKEN__/agent-sno-emul-baremetal.iso" {
		t.Errorf("agentIso.fetchUrl got %v, want configured artifact HTTP port", got)
	}

	services := vars["bootwright_infra_component_services"].([]any)
	service := firstProviderServiceByKind(t, services, v1alpha1.ComponentSlotArtifactServer)
	if got := service["port"]; got != 9443 {
		t.Errorf("artifact service port got %v, want 9443", got)
	}
	tls := service["tls"].(map[string]any)
	if got := tls["commonName"]; got != "192.168.132.1" {
		t.Errorf("artifact service tls.commonName got %v, want endpoint host", got)
	}
	if got := tls["ipAddresses"]; !containsAnyString(got.([]any), "192.168.132.1") {
		t.Errorf("artifact service tls.ipAddresses got %v, want endpoint host", got)
	}
}

func TestBareMetalArtifactFetchURLUsesSelectedArtifactEndpoint(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "002-sno-emul-baremetal")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	for i := range state.Machines {
		if state.Machines[i].Metadata.Name != "services-host" {
			continue
		}
		state.Machines[i].Spec.Addresses = append(state.Machines[i].Spec.Addresses, v1alpha1.MachineAddress{
			Name:    "cluster-lan",
			Address: "192.168.132.9",
		})
	}
	state.InfraComponents[0].Spec.ArtifactServer.Endpoints = append(state.InfraComponents[0].Spec.ArtifactServer.Endpoints, v1alpha1.ArtifactServerEndpoint{
		Name:           "cluster",
		Listener:       "https",
		MachineAddress: "cluster-lan",
	})
	state.ContainerClusters[0].Spec.Install.ArtifactAccess = v1alpha1.ClusterArtifactAccess{}
	state.Environments[0].Spec.Defaults.ArtifactAccess = v1alpha1.ClusterArtifactAccess{
		ServerRef: v1alpha1.LocalObjectReference{Name: "default"},
		RedfishVirtualMedia: v1alpha1.ClusterArtifactEndpointRef{
			EndpointRef: v1alpha1.LocalObjectReference{Name: "cluster"},
		},
	}
	desiredstate.Normalize(&state)

	vars := render.Vars(state)
	cluster := vars["bootwright_clusters"].([]any)[0].(map[string]any)
	machine := firstMachineComponent(t, cluster)
	boot := machine["boot"].(map[string]any)
	iso := boot["agentIso"].(map[string]any)
	if got := iso["fetchUrl"]; got != "https://192.168.132.9:8443/__BOOTWRIGHT_AGENT_ISO_PUBLISH_TOKEN__/agent-sno-emul-baremetal.iso" {
		t.Errorf("agentIso.fetchUrl got %v, want selected endpoint address", got)
	}
}

func TestBareMetalArtifactFetchURLUsesExternalArtifactEndpoint(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "002-sno-emul-baremetal")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	state.Environments[0].Spec.InfraComponents.ArtifactServers = []v1alpha1.EnvironmentArtifactServerComponent{{
		Name: "default",
		Type: v1alpha1.EnvironmentComponentExternal,
		Endpoints: []v1alpha1.EnvironmentArtifactServerEndpoint{{
			Name: "bmc",
			URL:  "https://artifacts.example.test:9443/vmedia",
		}},
	}}
	state.ContainerClusters[0].Spec.Install.ArtifactAccess.RedfishVirtualMedia.EndpointRef.Name = "bmc"

	vars := render.Vars(state)
	cluster := vars["bootwright_clusters"].([]any)[0].(map[string]any)
	machine := firstMachineComponent(t, cluster)
	boot := machine["boot"].(map[string]any)
	iso := boot["agentIso"].(map[string]any)
	if got := iso["stageHost"]; got != "" {
		t.Errorf("agentIso.stageHost got %v, want empty for external artifact endpoint", got)
	}
	if got := iso["stagePath"]; got != "" {
		t.Errorf("agentIso.stagePath got %v, want empty for external artifact endpoint", got)
	}
	if got := iso["fetchUrl"]; got != "https://artifacts.example.test:9443/vmedia/__BOOTWRIGHT_AGENT_ISO_PUBLISH_TOKEN__/agent-sno-emul-baremetal.iso" {
		t.Errorf("agentIso.fetchUrl got %v, want external artifact endpoint URL", got)
	}
	targets := cluster["agentIsoPublishTargets"].([]any)
	if len(targets) != 0 {
		t.Fatalf("agentIsoPublishTargets = %v, want no managed publish target for external artifact endpoint", targets)
	}
}

// TestMachineEmulatedBMCProjection pins the substrate-blind block the
// bmc_emulated role consumes. The renderer is the single source of
// truth for the emulator's listen port / vmedia port / bind address;
// without this pin, an accidental drift between Go and the role's
// default(8000)-style fallbacks would surface only at apply time as a
// "BMC reaches a different port than agentIso.fetchUrl" mismatch.
func TestMachineEmulatedBMCProjection(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "001-sno-libvirt")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	vars := render.Vars(state)
	cluster := vars["bootwright_clusters"].([]any)[0].(map[string]any)
	machine := firstMachineComponent(t, cluster)

	be, ok := machine["bmcEmulated"].(map[string]any)
	if !ok {
		t.Fatalf("machine missing bmcEmulated block: %v", machine)
	}
	wants := map[string]any{
		"protocol":          "redfish",
		"libvirtURI":        "qemu:///system",
		"bindAddress":       "0.0.0.0",
		"port":              8000,
		"vmediaPort":        8001,
		"credentialsRef":    "bmc-credentials",
		"sushyToolsVersion": "2.2.0",
	}
	for k, want := range wants {
		if got := be[k]; got != want {
			t.Errorf("bmcEmulated.%s got %v, want %v", k, got, want)
		}
	}

	// boot.agentIso.fetchUrl must use the same vmediaPort the role
	// will stand up. This pins the cross-projection invariant.
	boot := machine["boot"].(map[string]any)
	iso := boot["agentIso"].(map[string]any)
	fetchURL := iso["fetchUrl"].(string)
	if !strings.Contains(fetchURL, fmt.Sprintf(":%d/", be["vmediaPort"])) {
		t.Errorf("agentIso.fetchUrl %q must include vmediaPort=%v", fetchURL, be["vmediaPort"])
	}
}

func TestProviderServicesProjectRoleContracts(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "001-sno-libvirt")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	vars := render.Vars(state)
	services := vars["bootwright_provider_services"].([]any)
	var bmc map[string]any
	for _, raw := range services {
		entry := raw.(map[string]any)
		if entry["kind"] == v1alpha1.ProviderServiceKindBMC {
			bmc = entry
			break
		}
	}
	if bmc == nil {
		t.Fatalf("provider services missing bmc entry: %v", services)
	}
	wants := map[string]any{
		"applyRole":        "bootwright.core.provider_service_bmc_emulated",
		"destroyRole":      "bootwright.core.provider_service_bmc_emulated",
		"providerName":     "lab-libvirt-provider",
		"machineRef":       "bastion",
		"configConsistent": true,
	}
	for k, want := range wants {
		if got := bmc[k]; got != want {
			t.Errorf("bmc.%s got %v, want %v", k, got, want)
		}
	}
	be := bmc["bmcEmulated"].(map[string]any)
	if got := be["libvirtURI"]; got != "qemu:///system" {
		t.Fatalf("bmcEmulated.libvirtURI got %v", got)
	}
	setups := vars["bootwright_provider_machine_setups"].([]any)
	if len(setups) != 1 {
		t.Fatalf("provider host setup entries = %v", setups)
	}
	setup := setups[0].(map[string]any)
	if got := setup["applyRole"]; got != "bootwright.core.machine_setup_libvirt" {
		t.Fatalf("host setup applyRole got %v", got)
	}
}

func TestProviderServicesAggregateSharedManagedServices(t *testing.T) {
	state := twoClusterLibvirtProviderServicesState(t)
	vars := render.Vars(state)
	providerServices := vars["bootwright_provider_services"].([]any)
	infraComponentServices := vars["bootwright_infra_component_services"].([]any)
	providerCounts := providerServiceKindCounts(providerServices)
	infraComponentCounts := providerServiceKindCounts(infraComponentServices)
	wants := map[string]int{
		v1alpha1.ComponentSlotLoadBalancer:   1,
		v1alpha1.ComponentSlotProxy:          1,
		v1alpha1.ComponentSlotNameResolution: 1,
		v1alpha1.ComponentSlotNTP:            1,
		v1alpha1.ComponentSlotRegistry:       1,
	}
	for kind, want := range wants {
		if got := infraComponentCounts[kind]; got != want {
			t.Fatalf("infra component service %s count got %d, want %d: %v", kind, got, want, infraComponentServices)
		}
	}
	if got := providerCounts[v1alpha1.ProviderServiceKindBMC]; got != 1 {
		t.Fatalf("provider BMC service count got %d, want 1: %v", got, providerServices)
	}
	lb := firstProviderServiceByKind(t, infraComponentServices, v1alpha1.ComponentSlotLoadBalancer)
	if _, ok := lb["clusterName"]; ok {
		t.Fatalf("aggregated load balancer must not carry one top-level clusterName: %v", lb)
	}
	frontends := lb["frontends"].([]any)
	seenCluster := map[string]bool{}
	for _, raw := range frontends {
		frontend := raw.(map[string]any)
		seenCluster[frontend["clusterName"].(string)] = true
	}
	if !seenCluster["sno-libvirt"] || !seenCluster["sno-libvirt-b"] {
		t.Fatalf("aggregated frontends did not retain per-frontend cluster ownership: %v", frontends)
	}
	dns := firstProviderServiceByKind(t, infraComponentServices, v1alpha1.ComponentSlotNameResolution)
	if hosts := dns["additionalIngressHosts"].([]string); len(hosts) != 3 {
		t.Fatalf("additionalIngressHosts got %v, want union from both clusters", hosts)
	}
	ntp := firstProviderServiceByKind(t, infraComponentServices, v1alpha1.ComponentSlotNTP)
	if got := ntp["applyRole"]; got != "bootwright.core.infra_component_ntp_chrony" {
		t.Fatalf("ntp applyRole got %v", got)
	}
	if got := ntp["allowedNetworks"].([]string); strings.Join(got, ",") != "192.168.132.0/24" {
		t.Fatalf("ntp allowedNetworks got %v", got)
	}
	registry := firstProviderServiceByKind(t, infraComponentServices, v1alpha1.ComponentSlotRegistry)
	if got := registry["consumingClusters"].([]string); strings.Join(got, ",") != "sno-libvirt,sno-libvirt-b" {
		t.Fatalf("registry consumingClusters got %v", got)
	}
	bmc := firstProviderServiceByKind(t, providerServices, v1alpha1.ProviderServiceKindBMC)
	if got := bmc["consumingClusters"].([]string); strings.Join(got, ",") != "sno-libvirt,sno-libvirt-b" {
		t.Fatalf("bmc consumingClusters got %v", got)
	}
}

func TestProviderServicesAggregateSharedArtifactServer(t *testing.T) {
	state := twoClusterBareMetalPublicationState(t)
	vars := render.Vars(state)
	services := vars["bootwright_infra_component_services"].([]any)
	if got := providerServiceKindCounts(services)[v1alpha1.ComponentSlotArtifactServer]; got != 1 {
		t.Fatalf("artifact infra component service count got %d, want 1: %v", got, services)
	}
	service := firstProviderServiceByKind(t, services, v1alpha1.ComponentSlotArtifactServer)
	if got := service["consumingClusters"].([]string); strings.Join(got, ",") != "sno-emul-baremetal,sno-emul-baremetal-b" {
		t.Fatalf("artifact consumingClusters got %v", got)
	}
	for k, want := range map[string]any{
		"providerName": v1alpha1.KindInfraComponent,
		"name":         "artifact-server",
		"machineRef":   "services-host",
		"realisation":  "http",
	} {
		if got := service[k]; got != want {
			t.Fatalf("artifact service %s got %v, want %v", k, got, want)
		}
	}
	if _, ok := service["clusterName"]; ok {
		t.Fatalf("shared artifact service must not carry one top-level clusterName: %v", service)
	}
}

func TestBareMetalCorporateFixtureDoesNotRenderManagedProxyOrDNS(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "005-3nodes-baremetal")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	vars := render.Vars(state)
	cluster := vars["bootwright_clusters"].([]any)[0].(map[string]any)
	components := cluster["components"].([]any)
	for _, raw := range components {
		component := raw.(map[string]any)
		switch component["kind"] {
		case v1alpha1.ComponentSlotProxy, v1alpha1.ComponentSlotNameResolution, v1alpha1.ComponentSlotLoadBalancer, v1alpha1.ComponentSlotRegistry:
			t.Fatalf("corporate bare-metal fixture rendered managed component %v", component)
		}
	}

	services := vars["bootwright_infra_component_services"].([]any)
	if len(services) != 1 {
		t.Fatalf("infra component services = %v, want only artifact publication", services)
	}
	service := services[0].(map[string]any)
	if got := service["kind"]; got != v1alpha1.ComponentSlotArtifactServer {
		t.Fatalf("infra component service kind got %v, want %s", got, v1alpha1.ComponentSlotArtifactServer)
	}
	if got := service["machineRef"]; got != "bastion" {
		t.Fatalf("artifact service machineRef got %v, want bastion", got)
	}
	if got := service["port"]; got != v1alpha1.DefaultArtifactsHTTPPort {
		t.Fatalf("artifact service port got %v, want %d", got, v1alpha1.DefaultArtifactsHTTPPort)
	}

	proxyVars, ok := vars["bootwright_proxy"].(map[string]any)
	if !ok {
		t.Fatalf("external proxy vars missing from corporate fixture: %v", vars["bootwright_proxy"])
	}
	if got := proxyVars["http"]; got != "http://proxy.bootwright.test:3128" {
		t.Fatalf("proxy http got %v", got)
	}
}

func TestProxyForControlsRuntimeAndInstallerRendering(t *testing.T) {
	cases := []struct {
		name                    string
		bootwright              bool
		containerClusterInstall bool
		wantRuntimeProxy        bool
		wantInstallerProxy      bool
	}{
		{name: "both", bootwright: true, containerClusterInstall: true, wantRuntimeProxy: true, wantInstallerProxy: true},
		{name: "bootwright-only", bootwright: true, containerClusterInstall: false, wantRuntimeProxy: true},
		{name: "installer-only", bootwright: false, containerClusterInstall: true, wantInstallerProxy: true},
		{name: "neither", bootwright: false, containerClusterInstall: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "005-3nodes-baremetal")})
			if err != nil {
				t.Fatalf("LoadNormalizeValidate: %v", err)
			}
			if tc.bootwright {
				state.Environments[0].Spec.ProxyFor.Bootwright = "default"
			} else {
				state.Environments[0].Spec.ProxyFor.Bootwright = v1alpha1.EnvironmentComponentNone
			}
			if tc.containerClusterInstall {
				state.Environments[0].Spec.ProxyFor.ContainerClusterInstall = "default"
			} else {
				state.Environments[0].Spec.ProxyFor.ContainerClusterInstall = v1alpha1.EnvironmentComponentNone
			}

			vars := render.Vars(state)
			_, gotRuntimeProxy := vars["bootwright_proxy"]
			if gotRuntimeProxy != tc.wantRuntimeProxy {
				t.Fatalf("bootwright_proxy presence got %v, want %v", gotRuntimeProxy, tc.wantRuntimeProxy)
			}
			cfg, err := render.InstallerConfig(state, state.ContainerClusters[0])
			if err != nil {
				t.Fatalf("InstallerConfig: %v", err)
			}
			_, gotInstallerProxy := cfg["proxy"]
			if gotInstallerProxy != tc.wantInstallerProxy {
				t.Fatalf("install-config proxy presence got %v, want %v", gotInstallerProxy, tc.wantInstallerProxy)
			}
		})
	}
}

func TestManagedClusterInstallProxyRendersNoProxy(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "001-sno-libvirt")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	cfg, err := render.InstallerConfig(state, state.ContainerClusters[0])
	if err != nil {
		t.Fatalf("InstallerConfig: %v", err)
	}
	proxyBlock, ok := cfg["proxy"].(map[string]any)
	if !ok {
		t.Fatalf("install-config missing proxy block: %v", cfg["proxy"])
	}
	for _, key := range []string{"httpProxy", "httpsProxy"} {
		if got := proxyBlock[key]; got != "http://192.168.132.1:3128" {
			t.Fatalf("proxy.%s got %v, want managed proxy URL", key, got)
		}
	}
	noProxy, ok := proxyBlock["noProxy"].(string)
	if !ok {
		t.Fatalf("proxy.noProxy got %T, want string", proxyBlock["noProxy"])
	}
	for _, want := range []string{
		".bootwright.test",
		".sno-libvirt.bootwright.test",
		"192.168.132.0/24",
		"192.168.132.1",
		"192.168.132.10",
		"192.168.132.11",
		"10.128.0.0/14",
		"172.30.0.0/16",
	} {
		if !commaListContains(noProxy, want) {
			t.Fatalf("proxy.noProxy missing %q: %s", want, noProxy)
		}
	}
}

// TestMachineEmulatedBMCAbsentForBareMetal pins the negative half of
// the projection: from.name baremetal machines never carry a
// bmcEmulated block — they speak a vendor BMC, not an emulated one,
// and the role never runs against them. Keeping the absence pinned
// catches a renderer regression that would project the block onto
// every machine indiscriminately.
func TestMachineEmulatedBMCAbsentForBareMetal(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "002-sno-emul-baremetal")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	vars := render.Vars(state)
	cluster := vars["bootwright_clusters"].([]any)[0].(map[string]any)
	machine := firstMachineComponent(t, cluster)
	if _, ok := machine["bmcEmulated"]; ok {
		t.Fatalf("bare-metal machine must not carry bmcEmulated: %v", machine)
	}
}

// TestLoadBalancerFrontendAttachmentLibvirt pins the per-frontend
// attachment block network_vips consumes. The role no longer scans
// `bootwright_current_cluster.networks` for a libvirt bridge; the
// renderer projects attachment.{kind, libvirt.{bridge,prefix}} on
// every frontend so a non-libvirt cluster with a managed LB no-ops
// cleanly instead of silently skipping. This pins the libvirt arm
// (001 fixture: managed haProxy + libvirt connectivity).
func TestLoadBalancerFrontendAttachmentLibvirt(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "001-sno-libvirt")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	vars := render.Vars(state)
	cluster := vars["bootwright_clusters"].([]any)[0].(map[string]any)

	var lb map[string]any
	for _, c := range cluster["components"].([]any) {
		entry := c.(map[string]any)
		if entry["kind"] == v1alpha1.ComponentSlotLoadBalancer {
			lb = entry
			break
		}
	}
	if lb == nil {
		t.Fatal("001 fixture must declare a loadBalancer component")
	}
	frontends, ok := lb["frontends"].([]any)
	if !ok || len(frontends) == 0 {
		t.Fatalf("loadBalancer component missing frontends: %v", lb)
	}
	for _, raw := range frontends {
		f := raw.(map[string]any)
		att, ok := f["attachment"].(map[string]any)
		if !ok {
			t.Errorf("frontend %v missing attachment block", f["name"])
			continue
		}
		if got := att["kind"]; got != v1alpha1.ProvisionerLibvirt {
			t.Errorf("frontend %v attachment.kind got %v, want %s", f["name"], got, v1alpha1.ProvisionerLibvirt)
		}
		libvirt, ok := att["libvirt"].(map[string]any)
		if !ok {
			t.Errorf("frontend %v attachment.libvirt missing", f["name"])
			continue
		}
		if got := libvirt["bridge"]; got != "vbr-cb-sno" {
			t.Errorf("frontend %v attachment.libvirt.bridge got %v, want vbr-cb-sno", f["name"], got)
		}
		if got := libvirt["prefix"]; got != 24 {
			t.Errorf("frontend %v attachment.libvirt.prefix got %v, want 24", f["name"], got)
		}
	}
}

func twoClusterLibvirtProviderServicesState(t *testing.T) v1alpha1.State {
	t.Helper()
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "001-sno-libvirt")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	state.Environments[0].Spec.InfraComponents.Registries = []v1alpha1.EnvironmentRegistryComponent{{
		Name:         "default",
		Type:         v1alpha1.EnvironmentComponentManaged,
		ComponentRef: v1alpha1.LocalObjectReference{Name: "registry"},
	}}
	state.InfraComponents = append(state.InfraComponents, v1alpha1.InfraComponent{
		Metadata: v1alpha1.Metadata{Name: "registry"},
		Spec: v1alpha1.InfraComponentSpec{
			Type: v1alpha1.ComponentSlotRegistry, Registry: &v1alpha1.RegistryComponent{
				Implementation: v1alpha1.InfraComponentTypeMirrorRegistry,
				MachineRef:     v1alpha1.LocalObjectReference{Name: "bastion"},
				Port:           v1alpha1.DefaultMirrorRegistryPort,
			}},
	})
	state.ContainerClusters[0].Spec.Install.Mode = v1alpha1.InstallModeDisconnected
	state.Environments[0].Spec.InfraComponents.NameResolution[0].AdditionalIngressHosts = []string{"console-openshift-console.apps.sno-libvirt-b.bootwright.test"}
	machine := machineByName(t, state, state.ContainerClusters[0].Spec.Nodes[0].MachineRef.Name)
	machine.Metadata.Name = "sno-libvirt-b-master-0"
	machine.Spec.Addresses = append([]v1alpha1.MachineAddress(nil), machine.Spec.Addresses...)
	for i := range machine.Spec.Addresses {
		if machine.Spec.Addresses[i].Name == "ip" {
			machine.Spec.Addresses[i].Address = "192.168.132.30"
		}
	}
	ocp := state.ContainerClusters[0]
	ocp.Metadata.Name = "sno-libvirt-b"
	ocp.Spec.Install.Mode = v1alpha1.InstallModeDisconnected
	ocp.Spec.Nodes = append([]v1alpha1.OCPNodeSpec(nil), ocp.Spec.Nodes...)
	for i := range ocp.Spec.Nodes {
		ocp.Spec.Nodes[i].MachineRef.Name = machine.Metadata.Name
	}
	state.Machines = append(state.Machines, machine)
	state.ContainerClusters = append(state.ContainerClusters, ocp)
	return state
}

func twoClusterBareMetalPublicationState(t *testing.T) v1alpha1.State {
	t.Helper()
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "002-sno-emul-baremetal")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	machine := state.Machines[0]
	machine.Metadata.Name = "sno-emul-baremetal-b-master-0"
	ocp := state.ContainerClusters[0]
	ocp.Metadata.Name = "sno-emul-baremetal-b"
	ocp.Spec.Nodes = append([]v1alpha1.OCPNodeSpec(nil), ocp.Spec.Nodes...)
	for i := range ocp.Spec.Nodes {
		ocp.Spec.Nodes[i].MachineRef.Name = machine.Metadata.Name
	}
	state.Machines = append(state.Machines, machine)
	state.ContainerClusters = append(state.ContainerClusters, ocp)
	return state
}

func providerServiceKindCounts(services []any) map[string]int {
	counts := map[string]int{}
	for _, raw := range services {
		service := raw.(map[string]any)
		counts[service["kind"].(string)]++
	}
	return counts
}

func clustersByName(t *testing.T, vars map[string]any) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	for _, raw := range vars["bootwright_clusters"].([]any) {
		cluster := raw.(map[string]any)
		out[cluster["name"].(string)] = cluster
	}
	return out
}

func commaListContains(list, want string) bool {
	for _, item := range strings.Split(list, ",") {
		if strings.TrimSpace(item) == want {
			return true
		}
	}
	return false
}
