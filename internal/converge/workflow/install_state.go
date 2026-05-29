package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/runtime/fs"
	"github.com/crmarques/bootwright/internal/state/graph"
)

const (
	ClusterInstallRecordFileName = "install-record.json"
	ClusterConnectionFileName    = "connection.json"
)

type ClusterInstallStatus string

const (
	ClusterInstallStatusInstalling ClusterInstallStatus = "installing"
	ClusterInstallStatusInstalled  ClusterInstallStatus = "installed"
	ClusterInstallStatusFailed     ClusterInstallStatus = "failed"
	ClusterInstallStatusDestroyed  ClusterInstallStatus = "destroyed"
	ClusterInstallStatusSuperseded ClusterInstallStatus = "superseded"
)

type ClusterInstallPhase string

const (
	ClusterInstallPhaseCreatingISO ClusterInstallPhase = "creating-iso"
	ClusterInstallPhaseISOCreated  ClusterInstallPhase = "iso-created"
	ClusterInstallPhaseBooting     ClusterInstallPhase = "booting"
	ClusterInstallPhaseNodesBooted ClusterInstallPhase = "nodes-booted"
	ClusterInstallPhaseWaiting     ClusterInstallPhase = "waiting"
	ClusterInstallPhaseComplete    ClusterInstallPhase = "complete"
)

type ClusterInstallRecord struct {
	Cluster     string                              `json:"cluster"`
	DesiredHash string                              `json:"desiredHash"`
	Status      ClusterInstallStatus                `json:"status"`
	Phase       ClusterInstallPhase                 `json:"phase"`
	RunID       string                              `json:"runId,omitempty"`
	StartedAt   time.Time                           `json:"startedAt,omitempty"`
	UpdatedAt   time.Time                           `json:"updatedAt"`
	InstalledAt *time.Time                          `json:"installedAt,omitempty"`
	Nodes       map[string]ClusterInstallNodeRecord `json:"nodes,omitempty"`
}

type ClusterConnectionRecord struct {
	Cluster           string    `json:"cluster"`
	APIURL            string    `json:"apiURL,omitempty"`
	ConsoleURL        string    `json:"consoleURL,omitempty"`
	IngressBaseDomain string    `json:"ingressBaseDomain,omitempty"`
	KubeconfigPath    string    `json:"kubeconfigPath,omitempty"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

type ClusterInstallNodeRecord struct {
	Booted   bool       `json:"booted"`
	BootedAt *time.Time `json:"bootedAt,omitempty"`
}

type ClusterAvailabilityChecker interface {
	Available(ctx context.Context, kubeconfigPath string) (bool, error)
}

type OCClusterAvailabilityChecker struct {
	Command string
}

func (c OCClusterAvailabilityChecker) Available(ctx context.Context, kubeconfigPath string) (bool, error) {
	if _, err := os.Stat(kubeconfigPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("stat kubeconfig %s: %w", kubeconfigPath, err)
	}
	name := strings.TrimSpace(c.Command)
	if name == "" {
		name = "oc"
	}
	out, err := exec.CommandContext(ctx, name,
		"--kubeconfig", kubeconfigPath,
		"--request-timeout=5s",
		"get", "clusterversion", "version",
		"-o", `jsonpath={.status.conditions[?(@.type=="Available")].status}`,
	).CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("probe cluster availability with %s: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)) == "True", nil
}

func ClusterRuntimeDir(clustersDir, cluster string) string {
	return filepath.Join(clustersDir, cluster, "runtime")
}

func ClusterSecretsDir(clustersDir, cluster string) string {
	return filepath.Join(clustersDir, cluster, "secrets")
}

func ClusterInstallRecordPath(clustersDir, cluster string) string {
	return filepath.Join(ClusterRuntimeDir(clustersDir, cluster), ClusterInstallRecordFileName)
}

func ClusterConnectionPath(clustersDir, cluster string) string {
	return filepath.Join(ClusterRuntimeDir(clustersDir, cluster), ClusterConnectionFileName)
}

func LoadClusterInstallRecord(clustersDir, cluster string) (ClusterInstallRecord, bool, error) {
	path := ClusterInstallRecordPath(clustersDir, cluster)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return ClusterInstallRecord{}, false, nil
	}
	if err != nil {
		return ClusterInstallRecord{}, false, fmt.Errorf("read cluster install record: %w", err)
	}
	var record ClusterInstallRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return ClusterInstallRecord{}, true, fmt.Errorf("decode cluster install record %s: %w", path, err)
	}
	return record, true, nil
}

func SaveClusterInstallRecord(clustersDir string, record ClusterInstallRecord) error {
	path := ClusterInstallRecordPath(clustersDir, record.Cluster)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create cluster install record directory: %w", err)
	}
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("chmod cluster install record directory: %w", err)
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("encode cluster install record: %w", err)
	}
	data = append(data, '\n')
	if err := safefs.AtomicWriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write cluster install record: %w", err)
	}
	return nil
}

func SaveClusterConnectionRecord(clustersDir string, record ClusterConnectionRecord) error {
	path := ClusterConnectionPath(clustersDir, record.Cluster)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create cluster connection record directory: %w", err)
	}
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("chmod cluster connection record directory: %w", err)
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("encode cluster connection record: %w", err)
	}
	data = append(data, '\n')
	if err := safefs.AtomicWriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write cluster connection record: %w", err)
	}
	return nil
}

func ReconcileApplyClusterInstallState(ctx context.Context, clustersDir, secretsDir, runID string, state v1alpha1.State, tasks []ApplyTask, override bool, checker ClusterAvailabilityChecker, now time.Time) ([]ApplyTask, error) {
	if checker == nil {
		checker = OCClusterAvailabilityChecker{}
	}
	out := make([]ApplyTask, len(tasks))
	copy(out, tasks)
	for _, name := range installTaskClusterNames(out) {
		if !stateHasContainerCluster(state, name) {
			continue
		}
		hash, err := clusterInstallDesiredHash(state, name, secretsDir)
		if err != nil {
			return out, err
		}
		record, found, err := LoadClusterInstallRecord(clustersDir, name)
		if err != nil {
			return out, err
		}
		if override {
			continue
		}
		hashMatches := !found || record.DesiredHash == hash
		if found && !hashMatches && record.Status != ClusterInstallStatusDestroyed && (record.Status == ClusterInstallStatusInstalled || clusterInstallPhaseMayHaveBooted(record.Phase)) {
			return out, fmt.Errorf("ContainerCluster/%s already has install state for missing or different install inputs after node boot; run bootwright destroy cluster --yes or bootwright apply cluster --override --yes after resetting target machines", name)
		}
		kubeconfigPath := clusterKubeconfigPath(clustersDir, name)
		if !found {
			adopted, err := adoptAvailableClusterRecord(ctx, clustersDir, name, hash, runID, kubeconfigPath, state.Environments, checker, now)
			if err != nil {
				return out, err
			}
			if adopted {
				out = skipClusterInstallTasks(out, name, allClusterInstallTaskKinds(), "cluster already installed and Available=True for desired install inputs", now)
			}
			continue
		}
		switch record.Status {
		case ClusterInstallStatusInstalled:
			available, err := checker.Available(ctx, kubeconfigPath)
			if err != nil {
				return out, fmt.Errorf("ContainerCluster/%s has an installed record but availability could not be verified: %w", name, err)
			}
			if !available {
				return out, fmt.Errorf("ContainerCluster/%s has an installed record but kubeconfig does not report Available=True; refusing to regenerate installer inputs without --override", name)
			}
			out = skipClusterInstallTasks(out, name, allClusterInstallTaskKinds(), "cluster already installed and Available=True for desired install inputs", now)
		case ClusterInstallStatusInstalling, ClusterInstallStatusFailed:
			if !hashMatches {
				continue
			}
			planned, err := resumeClusterInstallTasks(out, record, name, now)
			if err != nil {
				return out, err
			}
			out = planned
		}
	}
	return out, nil
}

func adoptAvailableClusterRecord(ctx context.Context, clustersDir, name, hash, runID, kubeconfigPath string, environments []v1alpha1.Environment, checker ClusterAvailabilityChecker, now time.Time) (bool, error) {
	if _, err := os.Stat(kubeconfigPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("stat kubeconfig %s: %w", kubeconfigPath, err)
	}
	available, err := checker.Available(ctx, kubeconfigPath)
	if err != nil {
		return false, fmt.Errorf("ContainerCluster/%s has existing kubeconfig but availability could not be verified: %w", name, err)
	}
	if !available {
		return false, fmt.Errorf("ContainerCluster/%s has existing kubeconfig but does not report Available=True; refusing to regenerate installer inputs without --override", name)
	}
	t := now.UTC()
	record := ClusterInstallRecord{
		Cluster:     name,
		DesiredHash: hash,
		Status:      ClusterInstallStatusInstalled,
		Phase:       ClusterInstallPhaseComplete,
		RunID:       runID,
		StartedAt:   t,
		UpdatedAt:   t,
		InstalledAt: &t,
	}
	if err := SaveClusterInstallRecord(clustersDir, record); err != nil {
		return false, err
	}
	if err := SaveClusterConnectionRecord(clustersDir, clusterConnectionRecord(clustersDir, name, environments, now)); err != nil {
		return false, err
	}
	return true, nil
}

func resumeClusterInstallTasks(tasks []ApplyTask, record ClusterInstallRecord, name string, now time.Time) ([]ApplyTask, error) {
	switch record.Phase {
	case ClusterInstallPhaseISOCreated:
		return skipClusterInstallTasks(tasks, name, []string{ApplyTaskKindClusterISO}, "previous install already created the agent ISO; resuming from node boot", now), nil
	case ClusterInstallPhaseNodesBooted, ClusterInstallPhaseWaiting:
		return skipClusterInstallTasks(tasks, name, []string{ApplyTaskKindClusterISO, ApplyTaskKindNodeBoot}, "previous install already booted nodes; resuming install wait", now), nil
	case "", ClusterInstallPhaseCreatingISO:
		return tasks, nil
	case ClusterInstallPhaseBooting:
		return tasks, fmt.Errorf("ContainerCluster/%s has prior install state at phase %s; node boot completion is uncertain, refusing to reboot without --override", name, record.Phase)
	case ClusterInstallPhaseComplete:
		return tasks, nil
	default:
		return tasks, fmt.Errorf("ContainerCluster/%s has unrecognized install phase %q; refusing to continue without --override", name, record.Phase)
	}
}

func MarkClusterInstallTaskStarted(clustersDir, secretsDir, runID string, task ApplyTask, now time.Time) error {
	phase, ok := clusterInstallTaskStartPhase(task.Entry.Kind)
	if !ok || task.Entry.Cluster == "" {
		return nil
	}
	if !stateHasContainerCluster(task.State, task.Entry.Cluster) {
		return nil
	}
	hash, err := clusterInstallDesiredHash(task.State, task.Entry.Cluster, secretsDir)
	if err != nil {
		return err
	}
	record, found, err := LoadClusterInstallRecord(clustersDir, task.Entry.Cluster)
	if err != nil {
		return err
	}
	if !found || record.Status == ClusterInstallStatusInstalled || record.Status == ClusterInstallStatusSuperseded {
		record = ClusterInstallRecord{Cluster: task.Entry.Cluster, StartedAt: now.UTC()}
	}
	record.DesiredHash = hash
	record.Status = ClusterInstallStatusInstalling
	record.Phase = phase
	record.RunID = runID
	record.UpdatedAt = now.UTC()
	record.InstalledAt = nil
	if err := SaveClusterInstallRecord(clustersDir, record); err != nil {
		return err
	}
	if task.Entry.Kind == ApplyTaskKindInstallWait {
		return SaveClusterConnectionRecord(clustersDir, clusterConnectionRecord(clustersDir, task.Entry.Cluster, task.State.Environments, now))
	}
	return nil
}

func MarkClusterInstallTaskSucceeded(clustersDir, secretsDir, runID string, task ApplyTask, now time.Time) error {
	phase, ok := clusterInstallTaskSuccessPhase(task.Entry.Kind)
	if !ok || task.Entry.Cluster == "" {
		return nil
	}
	if !stateHasContainerCluster(task.State, task.Entry.Cluster) {
		return nil
	}
	record, found, err := LoadClusterInstallRecord(clustersDir, task.Entry.Cluster)
	if err != nil {
		return err
	}
	if !found {
		record = ClusterInstallRecord{Cluster: task.Entry.Cluster, StartedAt: now.UTC()}
	}
	hash, err := clusterInstallDesiredHash(task.State, task.Entry.Cluster, secretsDir)
	if err != nil {
		return err
	}
	record.DesiredHash = hash
	record.RunID = runID
	record.Phase = phase
	record.UpdatedAt = now.UTC()
	if task.Entry.Kind == ApplyTaskKindInstallWait {
		record.Status = ClusterInstallStatusInstalled
		t := now.UTC()
		record.InstalledAt = &t
	} else {
		record.Status = ClusterInstallStatusInstalling
	}
	if task.Entry.Kind == ApplyTaskKindNodeBoot {
		record.Nodes = bootedClusterNodes(task.State, task.Entry.Cluster, now)
	}
	if err := SaveClusterInstallRecord(clustersDir, record); err != nil {
		return err
	}
	if task.Entry.Kind == ApplyTaskKindInstallWait {
		return SaveClusterConnectionRecord(clustersDir, clusterConnectionRecord(clustersDir, task.Entry.Cluster, task.State.Environments, now))
	}
	return nil
}

func MarkClusterInstallTaskFailed(clustersDir, secretsDir, runID string, task ApplyTask, now time.Time) error {
	phase, ok := clusterInstallTaskStartPhase(task.Entry.Kind)
	if !ok || task.Entry.Cluster == "" {
		return nil
	}
	if !stateHasContainerCluster(task.State, task.Entry.Cluster) {
		return nil
	}
	record, found, err := LoadClusterInstallRecord(clustersDir, task.Entry.Cluster)
	if err != nil {
		return err
	}
	if !found {
		record = ClusterInstallRecord{Cluster: task.Entry.Cluster, StartedAt: now.UTC()}
	}
	hash, err := clusterInstallDesiredHash(task.State, task.Entry.Cluster, secretsDir)
	if err != nil {
		return err
	}
	record.DesiredHash = hash
	record.Status = ClusterInstallStatusFailed
	record.Phase = phase
	record.RunID = runID
	record.UpdatedAt = now.UTC()
	return SaveClusterInstallRecord(clustersDir, record)
}

func clusterInstallTaskStartPhase(kind string) (ClusterInstallPhase, bool) {
	switch kind {
	case ApplyTaskKindClusterISO:
		return ClusterInstallPhaseCreatingISO, true
	case ApplyTaskKindNodeBoot:
		return ClusterInstallPhaseBooting, true
	case ApplyTaskKindInstallWait:
		return ClusterInstallPhaseWaiting, true
	default:
		return "", false
	}
}

func clusterInstallTaskSuccessPhase(kind string) (ClusterInstallPhase, bool) {
	switch kind {
	case ApplyTaskKindClusterISO:
		return ClusterInstallPhaseISOCreated, true
	case ApplyTaskKindNodeBoot:
		return ClusterInstallPhaseNodesBooted, true
	case ApplyTaskKindInstallWait:
		return ClusterInstallPhaseComplete, true
	default:
		return "", false
	}
}

func clusterInstallPhaseMayHaveBooted(phase ClusterInstallPhase) bool {
	switch phase {
	case ClusterInstallPhaseBooting, ClusterInstallPhaseNodesBooted, ClusterInstallPhaseWaiting, ClusterInstallPhaseComplete:
		return true
	default:
		return false
	}
}

func clusterInstallDesiredHash(state v1alpha1.State, clusterName, secretsDir string) (string, error) {
	clusterState := stategraph.FilterStateToClusters(state, []string{clusterName})
	if len(clusterState.ContainerClusters) != 1 {
		return "", fmt.Errorf("ContainerCluster/%s does not resolve to exactly one selected cluster", clusterName)
	}
	ocp := clusterState.ContainerClusters[0]
	installConfig, err := render.InstallerConfig(clusterState, ocp)
	if err != nil {
		return "", err
	}
	agentConfig, err := render.AgentConfig(clusterState, ocp)
	if err != nil {
		return "", err
	}
	secretInputs, err := render.InstallerSecretInputStats(clusterState, ocp, secretsDir)
	if err != nil {
		return "", err
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
		State:         clusterState,
		InstallConfig: installConfig,
		AgentConfig:   agentConfig,
		Manifests:     render.InstallerManifests(ocp, render.PlaceholderInstallerSecrets(clusterState, ocp)),
		SecretInputs:  secretInputs,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode cluster install hash input: %w", err)
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func clusterKubeconfigPath(clustersDir, clusterName string) string {
	return filepath.Join(ClusterSecretsDir(clustersDir, clusterName), "kubeconfig")
}

func clusterConnectionRecord(clustersDir, clusterName string, environments []v1alpha1.Environment, now time.Time) ClusterConnectionRecord {
	baseDomain := ""
	for _, env := range environments {
		if env.Spec.BaseDomain != "" {
			baseDomain = env.Spec.BaseDomain
			break
		}
	}
	record := ClusterConnectionRecord{
		Cluster:        clusterName,
		KubeconfigPath: clusterKubeconfigPath(clustersDir, clusterName),
		UpdatedAt:      now.UTC(),
	}
	if baseDomain == "" {
		return record
	}
	record.IngressBaseDomain = "apps." + clusterName + "." + baseDomain
	record.APIURL = "https://api." + clusterName + "." + baseDomain + ":6443"
	record.ConsoleURL = "https://console-openshift-console.apps." + clusterName + "." + baseDomain
	return record
}

func installTaskClusterNames(tasks []ApplyTask) []string {
	seen := map[string]bool{}
	for _, task := range tasks {
		if !isClusterInstallTaskKind(task.Entry.Kind) || task.Entry.Cluster == "" {
			continue
		}
		seen[task.Entry.Cluster] = true
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func stateHasContainerCluster(state v1alpha1.State, name string) bool {
	for _, cluster := range state.ContainerClusters {
		if cluster.Metadata.Name == name {
			return true
		}
	}
	return false
}

func skipClusterInstallTasks(tasks []ApplyTask, clusterName string, kinds []string, reason string, now time.Time) []ApplyTask {
	kindSet := map[string]bool{}
	for _, kind := range kinds {
		kindSet[kind] = true
	}
	out := make([]ApplyTask, len(tasks))
	copy(out, tasks)
	t := now.UTC()
	for i := range out {
		if out[i].Entry.Cluster != clusterName || !kindSet[out[i].Entry.Kind] {
			continue
		}
		out[i].Entry.Status = TaskStatusSkipped
		out[i].Entry.EndedAt = &t
		out[i].Entry.SkippedReason = reason
	}
	return out
}

func allClusterInstallTaskKinds() []string {
	return []string{ApplyTaskKindClusterISO, ApplyTaskKindNodeBoot, ApplyTaskKindInstallWait}
}

func isClusterInstallTaskKind(kind string) bool {
	switch kind {
	case ApplyTaskKindClusterISO, ApplyTaskKindNodeBoot, ApplyTaskKindInstallWait:
		return true
	default:
		return false
	}
}

func bootedClusterNodes(state v1alpha1.State, clusterName string, now time.Time) map[string]ClusterInstallNodeRecord {
	names := applyClusterMachineNames(state, clusterName)
	if len(names) == 0 {
		return nil
	}
	t := now.UTC()
	out := make(map[string]ClusterInstallNodeRecord, len(names))
	for _, name := range names {
		out[name] = ClusterInstallNodeRecord{Booted: true, BootedAt: &t}
	}
	return out
}
