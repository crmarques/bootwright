package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/remedy"
	"github.com/crmarques/bootwright/internal/render"
	stategraph "github.com/crmarques/bootwright/internal/state/graph"
)

func clusterInstallDesiredHashForContext(contextName string, state v1alpha1.State, clusterName, secretsDir string) (string, error) {
	return clusterInstallHashForContext(contextName, state, clusterName, secretsDir, false)
}

type clusterInstallHashInputs struct {
	clusterState    v1alpha1.State
	ocp             v1alpha1.ContainerCluster
	installConfig   map[string]any
	agentConfig     map[string]any
	manifests       []render.InstallerManifest
	scopedState     v1alpha1.State
	structuralState v1alpha1.State
}

var clusterInstallHashInputCache = struct {
	mu      sync.Mutex
	entries map[string]*clusterInstallHashInputs
}{}

func clusterInstallHashInputCacheKey(contextName, clusterName, secretsDir string) string {
	return contextName + "\x00" + clusterName + "\x00" + secretsDir
}

func clusterInstallHashInputsFor(contextName string, state v1alpha1.State, clusterName, secretsDir string) (*clusterInstallHashInputs, error) {
	clusterState := stategraph.FilterStateToClusters(state, []string{clusterName})
	if len(clusterState.ContainerClusters) != 1 {
		return nil, fmt.Errorf("ContainerCluster/%s does not resolve to exactly one selected cluster", clusterName)
	}
	key := clusterInstallHashInputCacheKey(contextName, clusterName, secretsDir)
	clusterInstallHashInputCache.mu.Lock()
	defer clusterInstallHashInputCache.mu.Unlock()
	if cached := clusterInstallHashInputCache.entries[key]; cached != nil && reflect.DeepEqual(cached.clusterState, clusterState) {
		return cached, nil
	}
	ocp := clusterState.ContainerClusters[0]
	installConfig, err := render.InstallerConfig(clusterState, ocp)
	if err != nil {
		return nil, err
	}
	agentConfig, err := render.AgentConfig(clusterState, ocp)
	if err != nil {
		return nil, err
	}
	inputs := &clusterInstallHashInputs{
		clusterState:    clusterState,
		ocp:             ocp,
		installConfig:   installConfig,
		agentConfig:     agentConfig,
		manifests:       render.InstallerManifests(ocp, render.PlaceholderInstallerSecrets(clusterState, ocp)),
		scopedState:     hashScopedState(clusterState),
		structuralState: containerClusterInstallStructuralHashVars(clusterState),
	}
	if clusterInstallHashInputCache.entries == nil {
		clusterInstallHashInputCache.entries = map[string]*clusterInstallHashInputs{}
	}
	clusterInstallHashInputCache.entries[key] = inputs
	return inputs, nil
}

func finishClusterInstallHash(clusterName string, inputs *clusterInstallHashInputs, secretInputs []render.InstallerSecretInputStat, projectDay2 bool) (string, error) {
	data, err := clusterInstallHashInput(clusterName, inputs, secretInputs, projectDay2)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func clusterInstallHashInput(clusterName string, inputs *clusterInstallHashInputs, secretInputs []render.InstallerSecretInputStat, projectDay2 bool) ([]byte, error) {
	embedState := inputs.scopedState
	if projectDay2 {
		embedState = inputs.structuralState
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
		InstallConfig: inputs.installConfig,
		AgentConfig:   inputs.agentConfig,
		Manifests:     inputs.manifests,
		SecretInputs:  secretInputs,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode cluster install hash input: %w", err)
	}
	return data, nil
}

func clusterInstallHashForContext(contextName string, state v1alpha1.State, clusterName, secretsDir string, projectDay2 bool) (string, error) {
	contextName = effectiveContextName(contextName)
	inputs, secretInputs, err := clusterInstallHashParts(contextName, state, clusterName, secretsDir)
	if err != nil {
		return "", err
	}
	return finishClusterInstallHash(clusterName, inputs, secretInputs, projectDay2)
}

func clusterInstallHashParts(contextName string, state v1alpha1.State, clusterName, secretsDir string) (*clusterInstallHashInputs, []render.InstallerSecretInputStat, error) {
	inputs, err := clusterInstallHashInputsFor(contextName, state, clusterName, secretsDir)
	if err != nil {
		return nil, nil, err
	}
	secretInputs, err := render.InstallerSecretInputStatsForContext(contextName, inputs.clusterState, inputs.ocp, secretsDir)
	if err != nil {
		return nil, nil, err
	}
	return inputs, secretInputs, nil
}

func installInputsMatch(record ClusterInstallRecord, _ string, structuralHash string) bool {
	if record.HashSchema != ConvergeHashSchema {
		return false
	}
	if record.StructuralHash == "" || structuralHash == "" {
		return false
	}
	return record.StructuralHash == structuralHash
}

func clusterInstallHashes(contextName string, state v1alpha1.State, clusterName, secretsDir string) (hash, structuralHash string, err error) {
	hash, structuralHash, _, err = clusterInstallHashesAndInput(contextName, state, clusterName, secretsDir)
	return hash, structuralHash, err
}

func clusterInstallHashesAndInput(contextName string, state v1alpha1.State, clusterName, secretsDir string) (hash, structuralHash string, input []byte, err error) {
	contextName = effectiveContextName(contextName)
	inputs, secretInputs, err := clusterInstallHashParts(contextName, state, clusterName, secretsDir)
	if err != nil {
		return "", "", nil, err
	}
	input, err = clusterInstallHashInput(clusterName, inputs, secretInputs, false)
	if err != nil {
		return "", "", nil, err
	}
	sum := sha256.Sum256(input)
	hash = "sha256:" + hex.EncodeToString(sum[:])
	structuralHash, err = finishClusterInstallHash(clusterName, inputs, secretInputs, true)
	if err != nil {
		return "", "", nil, err
	}
	return hash, structuralHash, input, nil
}

func clusterInstallSnapshotResourceID(cluster string) string {
	return ObjectKindContainerCluster + "Install/" + cluster
}

func installWaitTask(tasks []ApplyTask, cluster string) *ApplyTask {
	for i := range tasks {
		if tasks[i].Entry.Cluster == cluster && tasks[i].Entry.Kind == ApplyTaskKindInstallWait {
			return &tasks[i]
		}
	}
	return nil
}

func clusterInstallRecordInputsMatch(clustersDir, runsDir string, record ClusterInstallRecord, cluster string, task *ApplyTask, desiredHash, structuralHash string, input []byte) (bool, bool, error) {
	if err := validateClusterInstallRecordState(clustersDir, cluster, record); err != nil {
		return false, false, err
	}
	if record.HashSchema == ConvergeHashSchema {
		return installInputsMatch(record, desiredHash, structuralHash), false, nil
	}
	if record.HashSchema != ConvergeHashSchema-1 || record.Status != ClusterInstallStatusInstalled || task == nil {
		return false, false, nil
	}
	matched, err := successfulInputSnapshotMatches(runsDir, record.RunID, clusterInstallSnapshotResourceID(cluster), task.Entry.ID, task.Entry.Kind, TaskStatusOK, record.HashSchema, input)
	if err != nil {
		return false, false, &ClusterInstallStateError{
			Cluster:   cluster,
			Condition: ClusterInstallConditionLegacyInstallEvidenceUnreadable,
			Status:    record.Status,
			Phase:     record.Phase,
			Message:   fmt.Sprintf("cannot verify legacy install inputs for ContainerCluster/%s because its immutable successful-run evidence is missing or unreadable; bootwright refuses to infer that the installed cluster matches desired state", cluster),
			Cause:     err,
			Request:   clusterInstallRemedy(remedy.ActionRebuildCluster, cluster),
		}
	}
	return matched, matched, nil
}

func rebaselineClusterInstallRecord(clustersDir string, record *ClusterInstallRecord, desiredHash, structuralHash string, now time.Time) error {
	record.DesiredHash = desiredHash
	record.StructuralHash = structuralHash
	record.HashSchema = ConvergeHashSchema
	record.UpdatedAt = now.UTC()
	return SaveClusterInstallRecord(clustersDir, *record)
}

func ComputeClusterInstallHashes(contextName string, state v1alpha1.State, clusterName, secretsDir string) (string, string, error) {
	return clusterInstallHashes(contextName, state, clusterName, secretsDir)
}
