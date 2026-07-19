package workflow

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/host/execution"
	"github.com/crmarques/bootwright/internal/host/safefs"
	"github.com/crmarques/bootwright/internal/render"
	stategraph "github.com/crmarques/bootwright/internal/state/graph"
	stateview "github.com/crmarques/bootwright/internal/state/view"
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
	Cluster        string               `json:"cluster"`
	DesiredHash    string               `json:"desiredHash"`
	StructuralHash string               `json:"structuralHash,omitempty"`
	HashSchema     int                  `json:"hashSchema,omitempty"`
	Status         ClusterInstallStatus `json:"status"`
	Phase          ClusterInstallPhase  `json:"phase"`
	RunID          string               `json:"runId,omitempty"`
	StartedAt      time.Time            `json:"startedAt,omitempty"`
	UpdatedAt      time.Time            `json:"updatedAt"`
	InstalledAt    *time.Time           `json:"installedAt,omitempty"`
}

type ClusterConnectionRecord struct {
	Cluster           string    `json:"cluster"`
	APIURL            string    `json:"apiURL,omitempty"`
	ConsoleURL        string    `json:"consoleURL,omitempty"`
	IngressBaseDomain string    `json:"ingressBaseDomain,omitempty"`
	KubeconfigPath    string    `json:"kubeconfigPath,omitempty"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

type ClusterAvailabilityChecker interface {
	Available(ctx context.Context, kubeconfigPath string) (bool, error)
}

type OCClusterAvailabilityChecker struct {
	Command string
	Runner  execution.Runner
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
	runner := c.Runner
	if runner == nil {
		runner = execution.OSRunner{}
	}
	var combined bytes.Buffer
	err := runner.Run(ctx, execution.Command{
		Name: name,
		Args: []string{
			"--kubeconfig", kubeconfigPath,
			"--request-timeout=5s",
			"get", "clusterversion", "version",
			"-o", `jsonpath={.status.conditions[?(@.type=="Available")].status}`,
		},
		Stdout: &combined,
		Stderr: &combined,
	})
	out := combined.Bytes()
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

func RemoveClusterInstallState(clustersDir, cluster string) error {
	if strings.TrimSpace(clustersDir) == "" || strings.TrimSpace(cluster) == "" {
		return nil
	}
	for _, path := range []string{
		ClusterInstallRecordPath(clustersDir, cluster),
		ClusterConnectionPath(clustersDir, cluster),
		clusterKubeconfigPath(clustersDir, cluster),
		filepath.Join(ClusterSecretsDir(clustersDir, cluster), "kubeadmin-password"),
	} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove cluster install state %s: %w", path, err)
		}
	}
	return nil
}

func RecordedProvisionedClusters(clustersDir string) ([]string, error) {
	entries, err := os.ReadDir(clustersDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("list provisioned cluster records: %w", err)
	}
	var out []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		record, found, err := LoadClusterInstallRecord(clustersDir, entry.Name())
		if err != nil {
			return nil, err
		}
		if !found || record.Status == ClusterInstallStatusDestroyed {
			continue
		}
		out = append(out, entry.Name())
	}
	sort.Strings(out)
	return out, nil
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
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("encode cluster install record: %w", err)
	}
	data = append(data, '\n')
	if err := safefs.WriteFileEnsuringDir(path, data, 0o600); err != nil {
		return fmt.Errorf("write cluster install record: %w", err)
	}
	return nil
}

func SaveClusterConnectionRecord(clustersDir string, record ClusterConnectionRecord) error {
	path := ClusterConnectionPath(clustersDir, record.Cluster)
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("encode cluster connection record: %w", err)
	}
	data = append(data, '\n')
	if err := safefs.WriteFileEnsuringDir(path, data, 0o600); err != nil {
		return fmt.Errorf("write cluster connection record: %w", err)
	}
	return nil
}

func ReconcileApplyClusterInstallState(ctx context.Context, clustersDir, contextName, secretsDir, runID string, state v1alpha1.State, tasks []ApplyTask, mode ApplyMode, ackedReinstalls []string, checker ClusterAvailabilityChecker, now time.Time) ([]ApplyTask, []string, error) {
	if checker == nil {
		checker = OCClusterAvailabilityChecker{}
	}
	acked := map[string]bool{}
	for _, name := range ackedReinstalls {
		acked[name] = true
	}
	var installedMatching []string
	out := make([]ApplyTask, len(tasks))
	copy(out, tasks)
	for _, name := range installTaskClusterNames(out) {
		if !stateHasContainerCluster(state, name) {
			continue
		}
		hash, err := clusterInstallDesiredHashForContext(contextName, state, name, secretsDir)
		if err != nil {
			return out, installedMatching, err
		}
		structuralHash, err := clusterInstallStructuralHashForContext(contextName, state, name, secretsDir)
		if err != nil {
			return out, installedMatching, err
		}
		record, found, err := LoadClusterInstallRecord(clustersDir, name)
		if err != nil {
			return out, installedMatching, err
		}
		hashMatches := !found || installInputsMatch(record, hash, structuralHash)
		if err := guardStaleAgentISOBoot(out, name, record, found, hashMatches); err != nil {
			return out, installedMatching, err
		}
		if mode == ApplyModeCreate && found && record.Status != ClusterInstallStatusDestroyed {
			return out, installedMatching, fmt.Errorf("apply --expect-new requires a greenfield environment and ContainerCluster/%s already has an install record (status %s); drop --expect-new to reconcile it, or run bootwright destroy --stage clusters --clusters %s --yes first", name, record.Status, name)
		}
		if mode == ApplyModeOverride {
			if !found || record.Status != ClusterInstallStatusInstalled {
				continue
			}
			if !installInputsMatch(record, hash, structuralHash) {
				if acked[name] {
					continue
				}
				return out, installedMatching, fmt.Errorf("ContainerCluster/%s recorded install inputs differ from current desired inputs but its reinstall was not acknowledged when this apply was confirmed; re-run bootwright apply --stage clusters --clusters %s --override --allow-destroy --yes to acknowledge the reinstall", name, name)
			}
			available, availErr := checker.Available(ctx, clusterKubeconfigPath(clustersDir, name))
			if availErr == nil && available {
				out = skipClusterInstallTasks(out, name, allClusterInstallTaskKinds(), "cluster already installed and Available=True for desired install inputs; --override rebuilds only drifted objects, not a healthy in-sync cluster", now)
				installedMatching = append(installedMatching, name)
				continue
			}
			if acked[name] {
				continue
			}
			if availErr != nil {
				return out, installedMatching, fmt.Errorf("ContainerCluster/%s was Available=True when this apply was confirmed but availability could not be verified at execution: %w; re-run bootwright apply --stage clusters --clusters %s --override --allow-destroy --yes to acknowledge the reinstall", name, availErr, name)
			}
			return out, installedMatching, fmt.Errorf("ContainerCluster/%s was Available=True when this apply was confirmed but does not report Available=True at execution; re-run bootwright apply --stage clusters --clusters %s --override --allow-destroy --yes to acknowledge the reinstall", name, name)
		}
		if found && !hashMatches && record.Status != ClusterInstallStatusDestroyed && (record.Status == ClusterInstallStatusInstalled || clusterInstallPhaseMayHaveBooted(record.Phase)) {
			return out, installedMatching, fmt.Errorf("ContainerCluster/%s already has install state for missing or different install inputs after node boot; run bootwright destroy --stage clusters --clusters %s --yes or bootwright apply --stage clusters --clusters %s --override --allow-destroy --yes after resetting target machines", name, name, name)
		}
		kubeconfigPath := clusterKubeconfigPath(clustersDir, name)
		if !found {
			if err := guardUnrecordedCluster(ctx, name, kubeconfigPath, checker); err != nil {
				return out, installedMatching, err
			}
			continue
		}
		switch record.Status {
		case ClusterInstallStatusInstalled:
			available, err := checker.Available(ctx, kubeconfigPath)
			if err != nil {
				return out, installedMatching, fmt.Errorf("ContainerCluster/%s has an installed record but availability could not be verified: %w; if the cluster is unreachable, rebuild it with bootwright apply --stage clusters --clusters %s --override --allow-destroy --yes (or bootwright destroy --stage clusters --clusters %s --yes first)", name, err, name, name)
			}
			if !available {
				return out, installedMatching, fmt.Errorf("ContainerCluster/%s has an installed record but kubeconfig does not report Available=True; to keep its data, repair the cluster to Available=True and re-run apply, or rebuild it with bootwright apply --stage clusters --clusters %s --override --allow-destroy --yes", name, name)
			}
			out = skipClusterInstallTasks(out, name, allClusterInstallTaskKinds(), "cluster already installed and Available=True for desired install inputs", now)
			installedMatching = append(installedMatching, name)
		case ClusterInstallStatusInstalling, ClusterInstallStatusFailed:
			if !hashMatches {
				continue
			}
			planned, err := resumeClusterInstallTasks(out, record, name, now)
			if err != nil {
				return out, installedMatching, err
			}
			out = planned
		}
	}
	return out, installedMatching, nil
}

func stampInstalledClusterConvergeRecords(runsDir, contextName, runID string, tasks []ApplyTask, clusters []string, now time.Time) error {
	if len(clusters) == 0 || strings.TrimSpace(runsDir) == "" {
		return nil
	}
	set := map[string]bool{}
	for _, name := range clusters {
		set[name] = true
	}
	for _, task := range tasks {
		if !set[task.Entry.Cluster] || !isClusterInstallTaskKind(task.Entry.Kind) {
			continue
		}
		if err := MarkApplyTaskConvergeSafety(runsDir, contextName, runID, task, ConvergeSafetyStatusSkipped, now); err != nil {
			return fmt.Errorf("record verified-installed cluster %s: %w", task.Entry.Cluster, err)
		}
	}
	return nil
}

type ClusterReinstall struct {
	Name       string
	Descriptor string
}

func ClusterReinstallNames(reinstalls []ClusterReinstall) []string {
	var out []string
	for _, reinstall := range reinstalls {
		out = append(out, reinstall.Name)
	}
	return out
}

func ClusterReinstallDescriptors(reinstalls []ClusterReinstall) []string {
	var out []string
	for _, reinstall := range reinstalls {
		out = append(out, reinstall.Descriptor)
	}
	return out
}

func OverrideRebuildInstalledClusters(ctx context.Context, clustersDir, contextName, secretsDir string, state v1alpha1.State, tasks []ApplyTask, checker ClusterAvailabilityChecker) []ClusterReinstall {
	if checker == nil {
		checker = OCClusterAvailabilityChecker{}
	}
	var out []ClusterReinstall
	for _, name := range installTaskClusterNames(tasks) {
		if !stateHasContainerCluster(state, name) {
			continue
		}
		hash, structuralHash, err := clusterInstallHashes(contextName, state, name, secretsDir)
		if err != nil {
			continue
		}
		record, found, err := LoadClusterInstallRecord(clustersDir, name)
		if err != nil {
			continue
		}
		if !found {
			if clusterKubeconfigExists(clustersDir, name) {
				out = append(out, ClusterReinstall{Name: name, Descriptor: fmt.Sprintf("reinstall ContainerCluster/%s (live cluster with no install record; --override reinstalls it and wipes its node disks)", name)})
			}
			continue
		}
		if record.Status != ClusterInstallStatusInstalled {
			if clusterInstallPhaseMayHaveBooted(record.Phase) {
				out = append(out, ClusterReinstall{Name: name, Descriptor: fmt.Sprintf("reinstall ContainerCluster/%s (incomplete install record at phase %q — its nodes may already have booted the installer; --override reinstalls it and wipes its node disks)", name, record.Phase)})
			}
			continue
		}
		if !installInputsMatch(record, hash, structuralHash) {
			out = append(out, ClusterReinstall{Name: name, Descriptor: fmt.Sprintf("reinstall ContainerCluster/%s (recorded install inputs differ from current desired inputs — e.g. rotated secret material or changed install config; --override reinstalls the cluster and wipes its node disks)", name)})
			continue
		}
		available, availErr := checker.Available(ctx, clusterKubeconfigPath(clustersDir, name))
		switch {
		case availErr != nil:
			out = append(out, ClusterReinstall{Name: name, Descriptor: fmt.Sprintf("reinstall ContainerCluster/%s (installed record matches desired inputs but availability could not be verified: %v — if this cluster should not be rebuilt, restore API reachability and re-run, or exclude it with --clusters)", name, availErr)})
		case !available:
			out = append(out, ClusterReinstall{Name: name, Descriptor: fmt.Sprintf("reinstall ContainerCluster/%s (installed record matches desired inputs but the cluster does not report Available=True; to keep its data, repair the cluster to Available=True and re-run plain apply — --override reinstalls it and wipes its node disks)", name)})
		}
	}
	return out
}

func OverrideReinstallInputDriftedClusters(clustersDir, contextName, secretsDir string, state v1alpha1.State, tasks []ApplyTask) []string {
	var out []string
	for _, name := range installTaskClusterNames(tasks) {
		if !stateHasContainerCluster(state, name) {
			continue
		}
		hash, structuralHash, err := clusterInstallHashes(contextName, state, name, secretsDir)
		if err != nil {
			continue
		}
		record, found, err := LoadClusterInstallRecord(clustersDir, name)
		if err != nil || !found || record.Status != ClusterInstallStatusInstalled {
			continue
		}
		if !installInputsMatch(record, hash, structuralHash) {
			out = append(out, name)
		}
	}
	return out
}

func InstalledRecordedClusters(clustersDir string, tasks []ApplyTask) []string {
	var out []string
	for _, name := range installTaskClusterNames(tasks) {
		record, found, err := LoadClusterInstallRecord(clustersDir, name)
		if err != nil || !found {
			continue
		}
		if record.Status == ClusterInstallStatusInstalled {
			out = append(out, name)
		}
	}
	return out
}

func guardStaleAgentISOBoot(tasks []ApplyTask, name string, record ClusterInstallRecord, found, hashMatches bool) error {
	if !clusterTaskKindPlanned(tasks, name, ApplyTaskKindNodeBoot) || clusterTaskKindPlanned(tasks, name, ApplyTaskKindClusterISO) {
		return nil
	}
	switch {
	case !found:
		return fmt.Errorf("ContainerCluster/%s: this scope boots nodes from the previously published agent ISO, but no install record proves an ISO was created from the current desired inputs; run bootwright apply --through base --clusters %s (or the full graph) to regenerate the ISO and boot", name, name)
	case !hashMatches:
		if clusterInstallPhaseMayHaveBooted(record.Phase) {
			return fmt.Errorf("ContainerCluster/%s: install inputs changed after nodes booted from the published agent ISO; run bootwright destroy --stage clusters --clusters %s --yes or bootwright apply --stage clusters --clusters %s --override --allow-destroy --yes after resetting target machines", name, name, name)
		}
		return fmt.Errorf("ContainerCluster/%s: install inputs changed after the published agent ISO was created; booting it would install the cluster from stale inputs while recording the current ones; run bootwright apply --through base --clusters %s (or the full graph) to regenerate the ISO first", name, name)
	case record.Status != ClusterInstallStatusInstalled && record.Phase != ClusterInstallPhaseISOCreated && !clusterInstallPhaseMayHaveBooted(record.Phase):
		return fmt.Errorf("ContainerCluster/%s has install phase %q: the agent ISO for the desired inputs was never fully created; run bootwright apply --through base --clusters %s (or the full graph) to create it", name, record.Phase, name)
	}
	return nil
}

func clusterTaskKindPlanned(tasks []ApplyTask, cluster, kind string) bool {
	for _, task := range tasks {
		if task.Entry.Cluster == cluster && task.Entry.Kind == kind {
			return true
		}
	}
	return false
}

func guardUnrecordedCluster(ctx context.Context, name, kubeconfigPath string, checker ClusterAvailabilityChecker) error {
	if _, err := os.Stat(kubeconfigPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("stat kubeconfig %s: %w", kubeconfigPath, err)
	}
	available, err := checker.Available(ctx, kubeconfigPath)
	if err != nil {
		return fmt.Errorf("ContainerCluster/%s has existing kubeconfig but availability could not be verified: %w", name, err)
	}
	if !available {
		return fmt.Errorf("ContainerCluster/%s has existing kubeconfig but does not report Available=True; refusing to regenerate installer inputs without --override", name)
	}
	return fmt.Errorf("ContainerCluster/%s has a reachable kubeconfig but no install record; bootwright cannot confirm it was installed from the current desired inputs and will not adopt it silently. Rebuild it from the desired state with bootwright apply --stage clusters --clusters %s --override --allow-destroy --yes, or restore clusters/%s/runtime/%s if the running cluster already matches", name, name, name, ClusterInstallRecordFileName)
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

func MarkClusterInstallTaskStarted(clustersDir, contextName, secretsDir, runID string, task ApplyTask, now time.Time) error {
	phase, ok := clusterInstallTaskStartPhase(task.Entry.Kind)
	if !ok || task.Entry.Cluster == "" {
		return nil
	}
	if !stateHasContainerCluster(task.State, task.Entry.Cluster) {
		return nil
	}
	hash, structuralHash, err := clusterInstallHashes(contextName, task.State, task.Entry.Cluster, secretsDir)
	if err != nil {
		return err
	}
	record, found, err := LoadClusterInstallRecord(clustersDir, task.Entry.Cluster)
	if err != nil {
		return err
	}
	if !found || record.Status == ClusterInstallStatusInstalled {
		record = ClusterInstallRecord{Cluster: task.Entry.Cluster, StartedAt: now.UTC()}
	}
	record.DesiredHash = hash
	record.StructuralHash = structuralHash
	record.HashSchema = ConvergeHashSchema
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

func MarkClusterInstallTaskSucceeded(clustersDir, contextName, secretsDir, runID string, task ApplyTask, now time.Time) error {
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
	hash, structuralHash, err := clusterInstallHashes(contextName, task.State, task.Entry.Cluster, secretsDir)
	if err != nil {
		return err
	}
	record.DesiredHash = hash
	record.StructuralHash = structuralHash
	record.HashSchema = ConvergeHashSchema
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
	if err := SaveClusterInstallRecord(clustersDir, record); err != nil {
		return err
	}
	if task.Entry.Kind == ApplyTaskKindInstallWait {
		return SaveClusterConnectionRecord(clustersDir, clusterConnectionRecord(clustersDir, task.Entry.Cluster, task.State.Environments, now))
	}
	return nil
}

func MarkClusterInstallTaskFailed(clustersDir, contextName, secretsDir, runID string, task ApplyTask, now time.Time) error {
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
	hash, structuralHash, err := clusterInstallHashes(contextName, task.State, task.Entry.Cluster, secretsDir)
	if err != nil {
		return err
	}
	record.DesiredHash = hash
	record.StructuralHash = structuralHash
	record.HashSchema = ConvergeHashSchema
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

func clusterInstallDesiredHashForContext(contextName string, state v1alpha1.State, clusterName, secretsDir string) (string, error) {
	return clusterInstallHashForContext(contextName, state, clusterName, secretsDir, false)
}

func clusterInstallStructuralHashForContext(contextName string, state v1alpha1.State, clusterName, secretsDir string) (string, error) {
	return clusterInstallHashForContext(contextName, state, clusterName, secretsDir, true)
}

func clusterInstallHashForContext(contextName string, state v1alpha1.State, clusterName, secretsDir string, projectDay2 bool) (string, error) {
	contextName = effectiveContextName(contextName)
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
		return "", fmt.Errorf("encode cluster install hash input: %w", err)
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func installInputsMatch(record ClusterInstallRecord, desiredHash, structuralHash string) bool {
	if record.HashSchema < ConvergeHashSchema {
		return false
	}
	if record.StructuralHash != "" && structuralHash != "" {
		return record.StructuralHash == structuralHash
	}
	return record.DesiredHash == desiredHash
}

func clusterInstallHashes(contextName string, state v1alpha1.State, clusterName, secretsDir string) (hash, structuralHash string, err error) {
	hash, err = clusterInstallDesiredHashForContext(contextName, state, clusterName, secretsDir)
	if err != nil {
		return "", "", err
	}
	structuralHash, err = clusterInstallStructuralHashForContext(contextName, state, clusterName, secretsDir)
	if err != nil {
		return "", "", err
	}
	return hash, structuralHash, nil
}

func clusterKubeconfigPath(clustersDir, clusterName string) string {
	return filepath.Join(ClusterSecretsDir(clustersDir, clusterName), "kubeconfig")
}

func clusterKubeconfigExists(clustersDir, clusterName string) bool {
	_, err := os.Stat(clusterKubeconfigPath(clustersDir, clusterName))
	return err == nil
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
	record.IngressBaseDomain = stateview.ClusterIngressBaseDomain(clusterName, baseDomain)
	record.APIURL = stateview.ClusterAPIURL(clusterName, baseDomain)
	record.ConsoleURL = stateview.ClusterConsoleURL(clusterName, baseDomain)
	return record
}

func ContainerInstallClusterNames(tasks []ApplyTask) []string {
	return installTaskClusterNames(tasks)
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
