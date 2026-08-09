package workflow

import (
	"bytes"
	"context"
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
	"github.com/crmarques/bootwright/internal/secrets"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

const (
	ClusterInstallRecordFileName    = "install-record.json"
	ClusterConnectionFileName       = "connection.json"
	ClusterInstallerVersionFileName = "agent-iso-installer-version"
	ClusterInstallResumeCeiling     = 3 * time.Hour
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
	ClusterInstallPhaseCreatingISO       ClusterInstallPhase = "creating-iso"
	ClusterInstallPhaseISOCreated        ClusterInstallPhase = "iso-created"
	ClusterInstallPhaseBooting           ClusterInstallPhase = "booting"
	ClusterInstallPhaseNodesBooted       ClusterInstallPhase = "nodes-booted"
	ClusterInstallPhaseWaitingBootstrap  ClusterInstallPhase = "waiting-bootstrap"
	ClusterInstallPhaseBootstrapComplete ClusterInstallPhase = "bootstrap-complete"
	ClusterInstallPhaseWaiting           ClusterInstallPhase = "waiting"
	ClusterInstallPhaseComplete          ClusterInstallPhase = "complete"
)

type ClusterInstallRemedyAction string

const (
	ClusterInstallRemedyReconcile         ClusterInstallRemedyAction = "reconcile"
	ClusterInstallRemedyRegenerateISO     ClusterInstallRemedyAction = "regenerate-iso"
	ClusterInstallRemedyDestroyAndReapply ClusterInstallRemedyAction = "destroy-and-reapply"
	ClusterInstallRemedyFutureRebuild     ClusterInstallRemedyAction = "future-rebuild"
)

type ClusterInstallRemedy struct {
	Action  ClusterInstallRemedyAction
	Cluster string
}

type ClusterInstallRemedialError interface {
	error
	ClusterInstallRemedy() ClusterInstallRemedy
}

type ClusterInstallStateError struct {
	Message string
	Remedy  ClusterInstallRemedy
}

func (e *ClusterInstallStateError) Error() string {
	return e.Message
}

func (e *ClusterInstallStateError) ClusterInstallRemedy() ClusterInstallRemedy {
	return e.Remedy
}

type ClusterInstallResumeExpiredError struct {
	Cluster   string
	Phase     ClusterInstallPhase
	StartedAt time.Time
	Deadline  time.Time
}

func (e *ClusterInstallResumeExpiredError) Error() string {
	if e.StartedAt.IsZero() {
		return fmt.Sprintf("ContainerCluster/%s has incomplete post-boot install state at phase %q with no recorded start time; bootwright cannot prove that another automatic wait is still inside the %s resume ceiling and refuses before any mutation", e.Cluster, e.Phase, ClusterInstallResumeCeiling)
	}
	return fmt.Sprintf("ContainerCluster/%s has incomplete post-boot install state at phase %q from %s; its automatic resume ceiling ended at %s after %s, so bootwright refuses another wait before any mutation", e.Cluster, e.Phase, e.StartedAt.UTC().Format(time.RFC3339), e.Deadline.UTC().Format(time.RFC3339), ClusterInstallResumeCeiling)
}

func (e *ClusterInstallResumeExpiredError) ClusterInstallRemedy() ClusterInstallRemedy {
	return ClusterInstallRemedy{Action: ClusterInstallRemedyDestroyAndReapply, Cluster: e.Cluster}
}

type ClusterInstallVersionError struct {
	Cluster            string
	Phase              ClusterInstallPhase
	InstallerVersion   string
	DeclaredVersion    string
	NodesMayHaveBooted bool
	InstallCompleted   bool
}

func (e *ClusterInstallVersionError) Error() string {
	recorded := strings.TrimSpace(e.InstallerVersion)
	if recorded == "" {
		recorded = "<missing>"
	}
	declared := strings.TrimSpace(e.DeclaredVersion)
	if declared == "" {
		declared = "<undeclared>"
	}
	switch {
	case e.InstallCompleted:
		return fmt.Sprintf("ContainerCluster/%s finished installing and its successful install evidence was retained, but the agent ISO installer version is %s while desired state declares %s; bootwright leaves this run nonzero so the release skew is not treated as normal and requires a deliberate future rebuild", e.Cluster, recorded, declared)
	case e.NodesMayHaveBooted:
		return fmt.Sprintf("ContainerCluster/%s is already past node boot at phase %q, but the agent ISO installer version is %s while desired state declares %s; bootwright will finish the already-started install before requiring a deliberate future rebuild", e.Cluster, e.Phase, recorded, declared)
	default:
		return fmt.Sprintf("ContainerCluster/%s has not booted any node, but its recorded agent ISO installer version is %s while desired state declares %s; bootwright refuses to boot and requires deliberate ISO regeneration", e.Cluster, recorded, declared)
	}
}

func (e *ClusterInstallVersionError) ClusterInstallRemedy() ClusterInstallRemedy {
	action := ClusterInstallRemedyRegenerateISO
	if e.NodesMayHaveBooted || e.InstallCompleted {
		action = ClusterInstallRemedyFutureRebuild
	}
	return ClusterInstallRemedy{Action: action, Cluster: e.Cluster}
}

type ClusterInstallRecord struct {
	Cluster          string               `json:"cluster"`
	DesiredHash      string               `json:"desiredHash"`
	StructuralHash   string               `json:"structuralHash,omitempty"`
	HashSchema       int                  `json:"hashSchema,omitempty"`
	Status           ClusterInstallStatus `json:"status"`
	Phase            ClusterInstallPhase  `json:"phase"`
	RunID            string               `json:"runId,omitempty"`
	InstallerVersion string               `json:"installerVersion,omitempty"`
	StartedAt        time.Time            `json:"startedAt,omitempty"`
	UpdatedAt        time.Time            `json:"updatedAt"`
	InstalledAt      *time.Time           `json:"installedAt,omitempty"`
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

func ClusterProviderStateDir(clustersDir, cluster string) string {
	return filepath.Join(ClusterRuntimeDir(clustersDir, cluster), "provider-state")
}

func ClusterInstallRecordPath(clustersDir, cluster string) string {
	return filepath.Join(ClusterRuntimeDir(clustersDir, cluster), ClusterInstallRecordFileName)
}

func ClusterInstallerVersionPath(clustersDir, cluster string) string {
	return filepath.Join(ClusterRuntimeDir(clustersDir, cluster), "installer", ClusterInstallerVersionFileName)
}

func ClusterConnectionPath(clustersDir, cluster string) string {
	return filepath.Join(ClusterRuntimeDir(clustersDir, cluster), ClusterConnectionFileName)
}

func RemoveClusterInstallState(clustersDir, contextName, cluster string) error {
	if strings.TrimSpace(clustersDir) == "" || strings.TrimSpace(cluster) == "" {
		return nil
	}
	for _, path := range []string{
		ClusterInstallRecordPath(clustersDir, cluster),
		ClusterConnectionPath(clustersDir, cluster),
	} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove cluster install state %s: %w", path, err)
		}
	}
	return removeCapturedClusterSecrets(clustersDir, contextName, cluster, "kubeconfig", "kubeadmin-password")
}

func RemoveStorageClusterCapturedSecrets(clustersDir, contextName, cluster string) error {
	return removeCapturedClusterSecrets(clustersDir, contextName, cluster, "dashboard-password")
}

func removeCapturedClusterSecrets(clustersDir, contextName, cluster string, names ...string) error {
	if strings.TrimSpace(clustersDir) == "" || strings.TrimSpace(cluster) == "" {
		return nil
	}
	store := secret.NewContextStore(effectiveContextName(contextName), ClusterSecretsDir(clustersDir, cluster))
	for _, name := range names {
		if err := store.Delete(secret.MaterialKey{Name: name, Role: secret.MaterialPrimary}); err != nil {
			return fmt.Errorf("remove %s for cluster %s: %w", name, cluster, err)
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

func LoadClusterInstallerVersion(clustersDir, cluster string) (string, error) {
	path := ClusterInstallerVersionPath(clustersDir, cluster)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read agent ISO installer version %s: %w", path, err)
	}
	version := strings.TrimSpace(string(data))
	if version == "" {
		return "", fmt.Errorf("read agent ISO installer version %s: file is empty", path)
	}
	return version, nil
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

func ReconcileApplyClusterInstallState(ctx context.Context, clustersDir, runsDir, contextName, secretsDir, runID string, state v1alpha1.State, tasks []ApplyTask, mode ApplyMode, ackedReinstalls []string, checker ClusterAvailabilityChecker, now time.Time) ([]ApplyTask, []string, error) {
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
		hash, structuralHash, input, err := clusterInstallHashesAndInput(contextName, state, name, secretsDir)
		if err != nil {
			return out, installedMatching, err
		}
		record, found, err := LoadClusterInstallRecord(clustersDir, name)
		if err != nil {
			return out, installedMatching, err
		}
		hashMatches := !found
		rebaseline := false
		if found {
			hashMatches, rebaseline, err = clusterInstallRecordInputsMatch(runsDir, record, name, installWaitTask(tasks, name), hash, structuralHash, input)
			if err != nil {
				return out, installedMatching, err
			}
		}
		if err := guardStaleAgentISOBoot(out, name, record, found, hashMatches); err != nil {
			return out, installedMatching, err
		}
		if mode == ApplyModeCreate && found && record.Status != ClusterInstallStatusDestroyed {
			return out, installedMatching, &ClusterInstallStateError{
				Message: fmt.Sprintf("apply --mode create requires a greenfield environment and ContainerCluster/%s already has an install record (status %s); reconcile the same selected work set instead, or deliberately destroy that cluster before retrying create", name, record.Status),
				Remedy:  ClusterInstallRemedy{Action: ClusterInstallRemedyReconcile, Cluster: name},
			}
		}
		if mode == ApplyModeRebuild {
			if !found || record.Status != ClusterInstallStatusInstalled {
				continue
			}
			if !hashMatches {
				if acked[name] {
					continue
				}
				return out, installedMatching, fmt.Errorf("ContainerCluster/%s recorded install inputs differ from current desired inputs but its reinstall was not acknowledged when this apply was confirmed; re-run bootwright apply --stage clusters --clusters %s --mode rebuild --authorize data-loss --yes to acknowledge the reinstall", name, name)
			}
			if versionErr := clusterInstallVersionMismatch(record, clusterInstallDeclaredVersion(state, name), true); versionErr != nil {
				if acked[name] {
					continue
				}
				return out, installedMatching, versionErr
			}
			var available bool
			availErr := withMaterializedClusterKubeconfig(contextName, clustersDir, name, func(kubeconfigPath string) error {
				var err error
				available, err = checker.Available(ctx, kubeconfigPath)
				return err
			})
			if availErr == nil && available {
				if rebaseline {
					if err := rebaselineClusterInstallRecord(clustersDir, &record, hash, structuralHash, now); err != nil {
						return out, installedMatching, err
					}
				}
				out = skipClusterInstallTasks(out, name, allClusterInstallTaskKinds(), "cluster already installed and Available=True for desired install inputs; --mode rebuild rebuilds only drifted objects, not a healthy in-sync cluster", now)
				installedMatching = append(installedMatching, name)
				continue
			}
			if acked[name] {
				continue
			}
			if availErr != nil {
				return out, installedMatching, fmt.Errorf("ContainerCluster/%s was Available=True when this apply was confirmed but availability could not be verified at execution: %w; re-run bootwright apply --stage clusters --clusters %s --mode rebuild --authorize data-loss --yes to acknowledge the reinstall", name, availErr, name)
			}
			return out, installedMatching, fmt.Errorf("ContainerCluster/%s was Available=True when this apply was confirmed but does not report Available=True at execution; re-run bootwright apply --stage clusters --clusters %s --mode rebuild --authorize data-loss --yes to acknowledge the reinstall", name, name)
		}
		declaredVersion := clusterInstallDeclaredVersion(state, name)
		if record.Status != ClusterInstallStatusDestroyed && record.Phase == ClusterInstallPhaseISOCreated {
			if versionErr := clusterInstallVersionMismatch(record, declaredVersion, false); versionErr != nil {
				return out, installedMatching, versionErr
			}
		}
		if found && !hashMatches && record.Status != ClusterInstallStatusDestroyed && (record.Status == ClusterInstallStatusInstalled || clusterInstallPhaseMayHaveBooted(record.Phase)) {
			return out, installedMatching, fmt.Errorf("ContainerCluster/%s already has install state for missing or different install inputs after node boot; run bootwright destroy --stage clusters --clusters %s --yes or bootwright apply --stage clusters --clusters %s --mode rebuild --authorize data-loss --yes after resetting target machines", name, name, name)
		}
		if !found {
			if err := guardUnrecordedCluster(ctx, contextName, clustersDir, name, checker); err != nil {
				return out, installedMatching, err
			}
			continue
		}
		switch record.Status {
		case ClusterInstallStatusInstalled:
			var available bool
			err := withMaterializedClusterKubeconfig(contextName, clustersDir, name, func(kubeconfigPath string) error {
				var availErr error
				available, availErr = checker.Available(ctx, kubeconfigPath)
				return availErr
			})
			if err != nil {
				return out, installedMatching, fmt.Errorf("ContainerCluster/%s has an installed record but availability could not be verified: %w; if the cluster is unreachable, rebuild it with bootwright apply --stage clusters --clusters %s --mode rebuild --authorize data-loss --yes (or bootwright destroy --stage clusters --clusters %s --yes first)", name, err, name, name)
			}
			if !available {
				return out, installedMatching, fmt.Errorf("ContainerCluster/%s has an installed record but kubeconfig does not report Available=True; to keep its data, repair the cluster to Available=True and re-run apply, or rebuild it with bootwright apply --stage clusters --clusters %s --mode rebuild --authorize data-loss --yes", name, name)
			}
			if versionErr := clusterInstallVersionMismatch(record, declaredVersion, true); versionErr != nil {
				return out, installedMatching, versionErr
			}
			if rebaseline {
				if err := rebaselineClusterInstallRecord(clustersDir, &record, hash, structuralHash, now); err != nil {
					return out, installedMatching, err
				}
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
			if err := guardClusterInstallResumeCeiling(record, name, now); err != nil {
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
		record, found, err := LoadConvergeSafetyRecord(runsDir, applyTaskSafetyResourceID(task))
		if err != nil {
			return err
		}
		if found && record.HashSchema != ConvergeHashSchema {
			desiredHash, err := ApplyTaskDesiredHash(task)
			if err != nil {
				return err
			}
			class, err := classifyApplyTaskWithRecord(task, runsDir, record, desiredHash)
			if err != nil {
				return err
			}
			if class != ConvergeSafetyMatch {
				return fmt.Errorf("ContainerCluster/%s cannot rebaseline legacy converge record %s because its exact successful-run input snapshot is missing or differs; rebuild the cluster to establish current safety evidence", task.Entry.Cluster, record.ResourceID)
			}
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

func OverrideRebuildInstalledClusters(ctx context.Context, clustersDir, runsDir, contextName, secretsDir string, state v1alpha1.State, tasks []ApplyTask, checker ClusterAvailabilityChecker) ([]ClusterReinstall, error) {
	if checker == nil {
		checker = OCClusterAvailabilityChecker{}
	}
	var out []ClusterReinstall
	var unverifiable, unverifiableDetails []string
	for _, name := range installTaskClusterNames(tasks) {
		if !stateHasContainerCluster(state, name) {
			continue
		}
		hash, structuralHash, input, err := clusterInstallHashesAndInput(contextName, state, name, secretsDir)
		if err != nil {
			continue
		}
		record, found, err := LoadClusterInstallRecord(clustersDir, name)
		if err != nil {
			continue
		}
		if !found {
			if clusterKubeconfigExists(clustersDir, name) {
				out = append(out, ClusterReinstall{Name: name, Descriptor: fmt.Sprintf("reinstall ContainerCluster/%s (live cluster with no install record; --mode rebuild reinstalls it and wipes its node disks)", name)})
			}
			continue
		}
		if record.Status != ClusterInstallStatusInstalled {
			if clusterInstallPhaseMayHaveBooted(record.Phase) {
				out = append(out, ClusterReinstall{Name: name, Descriptor: fmt.Sprintf("reinstall ContainerCluster/%s (incomplete install record at phase %q — its nodes may already have booted the installer; --mode rebuild reinstalls it and wipes its node disks)", name, record.Phase)})
			}
			continue
		}
		matches, _, err := clusterInstallRecordInputsMatch(runsDir, record, name, installWaitTask(tasks, name), hash, structuralHash, input)
		if err != nil {
			return nil, err
		}
		if !matches {
			out = append(out, ClusterReinstall{Name: name, Descriptor: fmt.Sprintf("reinstall ContainerCluster/%s (recorded install inputs differ from current desired inputs — e.g. rotated secret material or changed install config; --mode rebuild reinstalls the cluster and wipes its node disks)", name)})
			continue
		}
		if clusterInstallVersionMismatch(record, clusterInstallDeclaredVersion(state, name), true) != nil {
			out = append(out, ClusterReinstall{Name: name, Descriptor: fmt.Sprintf("reinstall ContainerCluster/%s (the recorded agent ISO installer version does not match the desired release; --mode rebuild reinstalls the cluster and wipes its node disks)", name)})
			continue
		}
		var available bool
		availErr := withMaterializedClusterKubeconfig(contextName, clustersDir, name, func(kubeconfigPath string) error {
			var err error
			available, err = checker.Available(ctx, kubeconfigPath)
			return err
		})
		switch {
		case availErr != nil:
			unverifiable = append(unverifiable, name)
			unverifiableDetails = append(unverifiableDetails, fmt.Sprintf("%s/%s (%v)", ObjectKindContainerCluster, name, availErr))
		case !available:
			out = append(out, ClusterReinstall{Name: name, Descriptor: fmt.Sprintf("reinstall ContainerCluster/%s (installed record matches desired inputs but the cluster does not report Available=True; to keep its data, repair the cluster to Available=True and re-run plain apply — --mode rebuild reinstalls it and wipes its node disks)", name)})
		}
	}
	if len(unverifiable) > 0 {
		return nil, unverifiableOverrideProbeError(unverifiable, unverifiableDetails, installTaskClusterNames(tasks))
	}
	return out, nil
}

func unverifiableOverrideProbeError(unverifiable, details, planned []string) error {
	blocked := map[string]bool{}
	for _, name := range unverifiable {
		blocked[name] = true
	}
	var remaining []string
	for _, name := range planned {
		if !blocked[name] {
			remaining = append(remaining, name)
		}
	}
	exclude := "use --mode reconcile so apply reconciles without a rebuild"
	if len(remaining) > 0 {
		exclude = "exclude it with bootwright apply --clusters " + strings.Join(remaining, ",") + " --mode rebuild"
	}
	return fmt.Errorf("apply --mode rebuild refuses to act on ContainerCluster(s) whose recorded install inputs match desired state but whose availability could not be probed: %s; an unprovable cluster is not a drifted one, so bootwright will not wipe its node disks on a failed probe — restore API reachability (and oc on PATH) and re-run, %s, or tear it down deliberately with bootwright destroy --clusters %s --yes and re-apply to rebuild it",
		strings.Join(details, ", "), exclude, strings.Join(unverifiable, ","))
}

func OverrideReinstallInputDriftedClusters(clustersDir, runsDir, contextName, secretsDir string, state v1alpha1.State, tasks []ApplyTask) []string {
	var out []string
	for _, name := range installTaskClusterNames(tasks) {
		if !stateHasContainerCluster(state, name) {
			continue
		}
		hash, structuralHash, input, err := clusterInstallHashesAndInput(contextName, state, name, secretsDir)
		if err != nil {
			continue
		}
		record, found, err := LoadClusterInstallRecord(clustersDir, name)
		if err != nil || !found || record.Status != ClusterInstallStatusInstalled {
			continue
		}
		matches, _, matchErr := clusterInstallRecordInputsMatch(runsDir, record, name, installWaitTask(tasks, name), hash, structuralHash, input)
		if matchErr != nil || !matches || clusterInstallVersionMismatch(record, clusterInstallDeclaredVersion(state, name), true) != nil {
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
			return fmt.Errorf("ContainerCluster/%s: install inputs changed after nodes booted from the published agent ISO; run bootwright destroy --stage clusters --clusters %s --yes or bootwright apply --stage clusters --clusters %s --mode rebuild --authorize data-loss --yes after resetting target machines", name, name, name)
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

func guardUnrecordedCluster(ctx context.Context, contextName, clustersDir, name string, checker ClusterAvailabilityChecker) error {
	kubeconfigPath := clusterKubeconfigPath(clustersDir, name)
	if _, err := os.Stat(kubeconfigPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("stat kubeconfig %s: %w", kubeconfigPath, err)
	}
	var available bool
	err := withMaterializedClusterKubeconfig(contextName, clustersDir, name, func(materializedPath string) error {
		var availErr error
		available, availErr = checker.Available(ctx, materializedPath)
		return availErr
	})
	if err != nil {
		return fmt.Errorf("ContainerCluster/%s has existing kubeconfig but availability could not be verified: %w", name, err)
	}
	if !available {
		return fmt.Errorf("ContainerCluster/%s has existing kubeconfig but does not report Available=True; refusing to regenerate installer inputs without --mode rebuild", name)
	}
	return fmt.Errorf("ContainerCluster/%s has a reachable kubeconfig but no install record; bootwright cannot confirm it was installed from the current desired inputs and will not adopt it silently. Rebuild it from the desired state with bootwright apply --stage clusters --clusters %s --mode rebuild --authorize data-loss --yes, or restore clusters/%s/runtime/%s if the running cluster already matches", name, name, name, ClusterInstallRecordFileName)
}

func clusterInstallDeclaredVersion(state v1alpha1.State, name string) string {
	for _, cluster := range state.ContainerClusters {
		if cluster.Metadata.Name == name {
			return strings.TrimSpace(cluster.Spec.Distribution.Release.Version)
		}
	}
	return ""
}

func clusterInstallVersionMismatch(record ClusterInstallRecord, declaredVersion string, complete bool) error {
	installerVersion := strings.TrimSpace(record.InstallerVersion)
	declaredVersion = strings.TrimSpace(declaredVersion)
	if declaredVersion == "" || installerVersion == declaredVersion {
		return nil
	}
	return &ClusterInstallVersionError{
		Cluster:            record.Cluster,
		Phase:              record.Phase,
		InstallerVersion:   installerVersion,
		DeclaredVersion:    declaredVersion,
		NodesMayHaveBooted: clusterInstallPhaseMayHaveBooted(record.Phase),
		InstallCompleted:   complete,
	}
}

func guardClusterInstallResumeCeiling(record ClusterInstallRecord, name string, now time.Time) error {
	if !clusterInstallPhaseMayHaveBooted(record.Phase) || record.Phase == ClusterInstallPhaseComplete {
		return nil
	}
	if record.StartedAt.IsZero() {
		return &ClusterInstallResumeExpiredError{Cluster: name, Phase: record.Phase}
	}
	deadline := record.StartedAt.UTC().Add(ClusterInstallResumeCeiling)
	if now.UTC().Before(deadline) {
		return nil
	}
	return &ClusterInstallResumeExpiredError{
		Cluster:   name,
		Phase:     record.Phase,
		StartedAt: record.StartedAt.UTC(),
		Deadline:  deadline,
	}
}

func resumeClusterInstallTasks(tasks []ApplyTask, record ClusterInstallRecord, name string, now time.Time) ([]ApplyTask, error) {
	switch record.Phase {
	case ClusterInstallPhaseISOCreated:
		return skipClusterInstallTasks(tasks, name, []string{ApplyTaskKindClusterISO}, "previous install already created the agent ISO; resuming from node boot"+publishedAgentISOAgeNote(record, name, now), now), nil
	case ClusterInstallPhaseNodesBooted, ClusterInstallPhaseWaitingBootstrap:
		return skipClusterInstallTasks(tasks, name, []string{ApplyTaskKindClusterISO, ApplyTaskKindNodeBoot}, "previous install already booted nodes; resuming bootstrap wait", now), nil
	case ClusterInstallPhaseBootstrapComplete, ClusterInstallPhaseWaiting:
		return skipClusterInstallTasks(tasks, name, []string{ApplyTaskKindClusterISO, ApplyTaskKindNodeBoot, ApplyTaskKindBootstrapWait}, "previous install completed bootstrap; resuming install-complete wait", now), nil
	case "", ClusterInstallPhaseCreatingISO:
		return tasks, nil
	case ClusterInstallPhaseBooting:
		return tasks, &ClusterInstallStateError{
			Message: fmt.Sprintf("ContainerCluster/%s has prior install state at phase %s; node boot completion is uncertain, so bootwright refuses to reboot before any mutation", name, record.Phase),
			Remedy:  ClusterInstallRemedy{Action: ClusterInstallRemedyFutureRebuild, Cluster: name},
		}
	case ClusterInstallPhaseComplete:
		return tasks, nil
	default:
		return tasks, &ClusterInstallStateError{
			Message: fmt.Sprintf("ContainerCluster/%s has unrecognized install phase %q; bootwright cannot prove which install steps already changed the cluster and refuses before any mutation", name, record.Phase),
			Remedy:  ClusterInstallRemedy{Action: ClusterInstallRemedyFutureRebuild, Cluster: name},
		}
	}
}

const publishedAgentISOFreshWindow = 24 * time.Hour

func publishedAgentISOAgeNote(record ClusterInstallRecord, name string, now time.Time) string {
	if record.UpdatedAt.IsZero() {
		return ""
	}
	age := now.Sub(record.UpdatedAt)
	if age < publishedAgentISOFreshWindow {
		return ""
	}
	return fmt.Sprintf(
		". That ISO was published at least %dh ago; an agent ISO carries bootstrap certificates minted when it was created, so booting a stale one fails during bootstrap with certificate errors that read like a network fault. Regenerate it with bootwright apply --through base --clusters %s",
		int(age.Hours()), name)
}

func clusterKubeconfigPath(clustersDir, clusterName string) string {
	return filepath.Join(ClusterSecretsDir(clustersDir, clusterName), "kubeconfig")
}

func clusterKubeconfigExists(clustersDir, clusterName string) bool {
	_, err := os.Stat(clusterKubeconfigPath(clustersDir, clusterName))
	return err == nil
}

func withMaterializedClusterKubeconfig(contextName, clustersDir, cluster string, fn func(kubeconfigPath string) error) error {
	runtimeDir := ClusterRuntimeDir(clustersDir, cluster)
	if err := safefs.EnsureDir(runtimeDir, 0o700); err != nil {
		return fmt.Errorf("create cluster runtime dir for %s: %w", cluster, err)
	}
	store := secret.NewContextStore(effectiveContextName(contextName), ClusterSecretsDir(clustersDir, cluster))
	if err := store.WithMaterialized(secret.MaterialKey{Name: "kubeconfig", Role: secret.MaterialPrimary}, runtimeDir, fn); err != nil {
		return fmt.Errorf("materialize kubeconfig for cluster %s: %w", cluster, err)
	}
	return nil
}

func clusterConnectionRecord(clustersDir, clusterName string, environments []v1alpha1.Environment, now time.Time) ClusterConnectionRecord {
	baseDomain := ""
	for _, env := range environments {
		if domain := env.Spec.Domains.ContainerClustersDomain(); domain != "" {
			baseDomain = domain
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
	return []string{ApplyTaskKindClusterInstall, ApplyTaskKindClusterISO, ApplyTaskKindNodeBoot, ApplyTaskKindBootstrapWait, ApplyTaskKindInstallWait}
}

func isClusterInstallTaskKind(kind string) bool {
	switch kind {
	case ApplyTaskKindClusterInstall, ApplyTaskKindClusterISO, ApplyTaskKindNodeBoot, ApplyTaskKindBootstrapWait, ApplyTaskKindInstallWait:
		return true
	default:
		return false
	}
}
