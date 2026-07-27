package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
	stategraph "github.com/crmarques/bootwright/internal/state/graph"
)

func installHashPinState() v1alpha1.State {
	return v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Metadata: v1alpha1.Metadata{Name: "env"},
			Spec:     v1alpha1.EnvironmentSpec{Domains: v1alpha1.EnvironmentDomainsSpec{Base: "example.test"}},
		}},
		ContainerClusters: []v1alpha1.ContainerCluster{{
			Metadata: v1alpha1.Metadata{Name: "pinned"},
			Spec: v1alpha1.ContainerClusterSpec{
				Nodes: []v1alpha1.OCPNodeSpec{{
					Name:       "n1",
					Role:       v1alpha1.NodeRoleMaster,
					MachineRef: v1alpha1.LocalObjectReference{Name: "m1"},
					Labels:     map[string]string{"day2": "true"},
				}},
			},
		}},
		Machines: []v1alpha1.Machine{{
			Metadata: v1alpha1.Metadata{Name: "m1"},
			Spec: v1alpha1.MachineSpec{
				OS:        v1alpha1.MachineOSSpec{Provided: v1alpha1.BoolPtr(true)},
				Addresses: []v1alpha1.MachineAddress{{Name: "ssh", Address: "10.0.0.11"}},
			},
		}},
	}
}

func TestClusterInstallHashesPinnedPayload(t *testing.T) {
	state := installHashPinState()
	desired, structural, err := clusterInstallHashes("test", state, "pinned", "")
	if err != nil {
		t.Fatalf("clusterInstallHashes: %v", err)
	}
	const (
		pinnedDesired    = "sha256:567d751e4c02b5229251c751a14a09d4a12d04ac2daa83832e57068c618fb5bf"
		pinnedStructural = "sha256:c547d9a0f49e761a9b9855d9dee7bedaff62bddc54a8363e21f2ee8c5bba89bb"
	)
	if desired != pinnedDesired {
		t.Fatalf("cluster install desired hash moved: got %s want %s; the payload is persisted in every cluster install record, so a reordering invalidates install-inputs matching, --expect-new and the --converge-drifted reinstall guard on every installed fleet", desired, pinnedDesired)
	}
	if structural != pinnedStructural {
		t.Fatalf("cluster install structural hash moved: got %s want %s; the payload is persisted in every cluster install record, so a reordering invalidates install-inputs matching, --expect-new and the --converge-drifted reinstall guard on every installed fleet", structural, pinnedStructural)
	}
}

func legacyClusterInstallHashForContext(contextName string, state v1alpha1.State, clusterName, secretsDir string, projectDay2 bool) (string, error) {
	contextName = effectiveContextName(contextName)
	clusterState := stategraph.FilterStateToClusters(state, []string{clusterName})
	ocp := clusterState.ContainerClusters[0]
	installConfig, err := render.InstallerConfig(clusterState, ocp)
	if err != nil {
		return "", err
	}
	agentConfig, err := render.AgentConfig(clusterState, ocp)
	if err != nil {
		return "", err
	}
	secretInputs, err := render.InstallerSecretInputStatsForContext(contextName, clusterState, ocp, secretsDir)
	if err != nil {
		return "", err
	}
	embedState := hashScopedState(clusterState)
	if projectDay2 {
		embedState = containerClusterInstallStructuralHashVars(clusterState)
	}
	payload := struct {
		APIVersion    string                            `json:"apiVersion"`
		Cluster       string                            `json:"cluster"`
		State         v1alpha1.State                    `json:"state"`
		InstallConfig map[string]any                    `json:"installConfig"`
		AgentConfig   map[string]any                    `json:"agentConfig"`
		Manifests     []render.InstallerManifest        `json:"manifests"`
		SecretInputs  []render.InstallerSecretInputStat `json:"secretInputs"`
	}{
		APIVersion:    v1alpha1.APIVersion,
		Cluster:       clusterName,
		State:         embedState,
		InstallConfig: installConfig,
		AgentConfig:   agentConfig,
		Manifests:     render.InstallerManifests(ocp, render.PlaceholderInstallerSecrets(clusterState, ocp)),
		SecretInputs:  secretInputs,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func TestClusterInstallHashesMatchLegacyPayloadOnFixture(t *testing.T) {
	state := loadWorkflowFixtureState(t, "001-sno-libvirt")
	const cluster = "sno-libvirt"
	dir := t.TempDir()
	secretsDir := writeWorkflowInstallerSecrets(t, dir)

	desired, structural, err := clusterInstallHashes("test", state, cluster, secretsDir)
	if err != nil {
		t.Fatalf("clusterInstallHashes: %v", err)
	}
	wantDesired, err := legacyClusterInstallHashForContext("test", state, cluster, secretsDir, false)
	if err != nil {
		t.Fatalf("legacy desired hash: %v", err)
	}
	wantStructural, err := legacyClusterInstallHashForContext("test", state, cluster, secretsDir, true)
	if err != nil {
		t.Fatalf("legacy structural hash: %v", err)
	}
	if desired != wantDesired {
		t.Fatalf("cluster install desired hash payload changed: got %s want %s (recorded in every install record)", desired, wantDesired)
	}
	if structural != wantStructural {
		t.Fatalf("cluster install structural hash payload changed: got %s want %s (recorded in every install record)", structural, wantStructural)
	}
	if desired == structural {
		t.Fatal("desired and structural install hashes must differ; the day-2 projection is not being applied")
	}
}

func TestClusterInstallHashInputMemoIsKeyedOnTheFilteredState(t *testing.T) {
	state := loadWorkflowFixtureState(t, "001-sno-libvirt")
	const cluster = "sno-libvirt"
	dir := t.TempDir()
	secretsDir := writeWorkflowInstallerSecrets(t, dir)

	first, err := clusterInstallDesiredHashForContext("test", state, cluster, secretsDir)
	if err != nil {
		t.Fatalf("first desired hash: %v", err)
	}
	scoped := stategraph.FilterStateToClusters(state, []string{cluster})
	fromScoped, err := clusterInstallDesiredHashForContext("test", scoped, cluster, secretsDir)
	if err != nil {
		t.Fatalf("scoped desired hash: %v", err)
	}
	if fromScoped != first {
		t.Fatalf("the install hash must be keyed on the filtered cluster state, not the caller's state: full=%s scoped=%s", first, fromScoped)
	}

	edited := state
	edited.ContainerClusters = append([]v1alpha1.ContainerCluster(nil), state.ContainerClusters...)
	for i := range edited.ContainerClusters {
		if edited.ContainerClusters[i].Metadata.Name != cluster {
			continue
		}
		nodes := append([]v1alpha1.OCPNodeSpec(nil), edited.ContainerClusters[i].Spec.Nodes...)
		nodes[0].Labels = map[string]string{"memo": "edited"}
		edited.ContainerClusters[i].Spec.Nodes = nodes
	}
	changed, err := clusterInstallDesiredHashForContext("test", edited, cluster, secretsDir)
	if err != nil {
		t.Fatalf("edited desired hash: %v", err)
	}
	if changed == first {
		t.Fatal("a desired-state edit reused the memoised install hash inputs; drift would go unnoticed")
	}
	back, err := clusterInstallDesiredHashForContext("test", state, cluster, secretsDir)
	if err != nil {
		t.Fatalf("re-computed desired hash: %v", err)
	}
	if back != first {
		t.Fatalf("recomputing the original state returned %s, want %s", back, first)
	}
}
