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
	"github.com/crmarques/bootwright/internal/converge/remedy"
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
		record, found, err := LoadClusterInstallRecord(clustersDir, name)
		if err != nil {
			return out, installedMatching, err
		}
		invalidRecordState := false
		if found {
			if stateErr := validateClusterInstallRecordState(clustersDir, name, record); stateErr != nil {
				if mode != ApplyModeRebuild || !acked[name] {
					return out, installedMatching, stateErr
				}
				invalidRecordState = true
			}
		}
		if invalidRecordState {
			continue
		}
		hash, structuralHash, input, err := clusterInstallHashesAndInput(contextName, state, name, secretsDir)
		if err != nil {
			return out, installedMatching, err
		}
		hashMatches := !found
		rebaseline := false
		if found {
			hashMatches, rebaseline, err = clusterInstallRecordInputsMatch(clustersDir, runsDir, record, name, installWaitTask(tasks, name), hash, structuralHash, input)
			if err != nil {
				return out, installedMatching, err
			}
		}
		if err := guardStaleAgentISOBoot(out, name, record, found, hashMatches); err != nil {
			return out, installedMatching, err
		}
		if mode == ApplyModeCreate && found && record.Status != ClusterInstallStatusDestroyed {
			return out, installedMatching, &ClusterInstallStateError{
				Cluster:   name,
				Condition: ClusterInstallConditionExistingRecord,
				Status:    record.Status,
				Phase:     record.Phase,
				Message:   fmt.Sprintf("apply --mode create requires a greenfield environment and ContainerCluster/%s already has an install record (status %s); create refuses before mutation because the selected work is not greenfield", name, record.Status),
				Request:   clusterInstallRemedy(remedy.ActionReconcileSameSelection, name),
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
				return out, installedMatching, &ClusterInstallStateError{
					Cluster:   name,
					Condition: ClusterInstallConditionReinstallNotAcknowledged,
					Status:    record.Status,
					Phase:     record.Phase,
					Message:   fmt.Sprintf("ContainerCluster/%s recorded install inputs differ from current desired inputs but its reinstall was not acknowledged when this apply was confirmed; bootwright refuses before reinstalling or wiping node disks", name),
					Request:   clusterInstallRemedy(remedy.ActionRebuildSameSelection, name),
				}
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
				return out, installedMatching, &ClusterInstallStateError{
					Cluster:   name,
					Condition: ClusterInstallConditionAvailabilityChanged,
					Status:    record.Status,
					Phase:     record.Phase,
					Message:   fmt.Sprintf("ContainerCluster/%s was Available=True when this apply was confirmed but availability could not be verified at execution; bootwright refuses before reinstalling or wiping node disks and requires the destructive decision to be confirmed again", name),
					Cause:     availErr,
					Request:   clusterInstallRemedy(remedy.ActionRebuildSameSelection, name),
				}
			}
			return out, installedMatching, &ClusterInstallStateError{
				Cluster:   name,
				Condition: ClusterInstallConditionAvailabilityChanged,
				Status:    record.Status,
				Phase:     record.Phase,
				Message:   fmt.Sprintf("ContainerCluster/%s was Available=True when this apply was confirmed but does not report Available=True at execution; bootwright refuses before reinstalling or wiping node disks and requires the destructive decision to be confirmed again", name),
				Request:   clusterInstallRemedy(remedy.ActionRebuildSameSelection, name),
			}
		}
		declaredVersion := clusterInstallDeclaredVersion(state, name)
		if record.Status != ClusterInstallStatusDestroyed && record.Phase == ClusterInstallPhaseISOCreated {
			if versionErr := clusterInstallVersionMismatch(record, declaredVersion, false); versionErr != nil {
				return out, installedMatching, versionErr
			}
		}
		if found && !hashMatches && record.Status != ClusterInstallStatusDestroyed && (record.Status == ClusterInstallStatusInstalled || clusterInstallPhaseMayHaveBooted(record.Phase)) {
			return out, installedMatching, &ClusterInstallStateError{
				Cluster:   name,
				Condition: ClusterInstallConditionPostBootInputDrift,
				Status:    record.Status,
				Phase:     record.Phase,
				Message:   fmt.Sprintf("ContainerCluster/%s already has install state for missing or different install inputs after node boot; bootwright refuses to mix the new inputs with an install that may already have changed its nodes, and the target machines must be reset before a deliberate rebuild", name),
				Request:   clusterInstallRemedy(remedy.ActionRebuildCluster, name),
			}
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
				return out, installedMatching, &ClusterInstallStateError{
					Cluster:   name,
					Condition: ClusterInstallConditionAvailabilityProbeFailed,
					Status:    record.Status,
					Phase:     record.Phase,
					Message:   fmt.Sprintf("ContainerCluster/%s has an installed record but availability could not be verified; restore API reachability and the local oc command before retrying, because bootwright will not infer a destructive rebuild from a failed probe", name),
					Cause:     err,
					Request:   clusterInstallRemedy(remedy.ActionRetrySameInvocation, name),
				}
			}
			if !available {
				return out, installedMatching, &ClusterInstallStateError{
					Cluster:   name,
					Condition: ClusterInstallConditionUnavailable,
					Status:    record.Status,
					Phase:     record.Phase,
					Message:   fmt.Sprintf("ContainerCluster/%s has an installed record but kubeconfig does not report Available=True; repair the cluster until it reports Available=True before retrying, because bootwright will not infer a destructive rebuild from unavailability", name),
					Request:   clusterInstallRemedy(remedy.ActionRetrySameInvocation, name),
				}
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
		if found {
			identity := convergeSafetyRecordIdentity{
				ResourceID:   applyTaskSafetyResourceID(task),
				ResourceKind: task.Entry.Kind,
				TaskID:       task.Entry.ID,
				TaskKind:     task.Entry.Kind,
				OwnerContext: contextName,
			}
			if strings.TrimSpace(record.Owner.Manager) != "" && record.Owner.Manager != ConvergeSafetyOwner {
				return fmt.Errorf("cannot replace convergence safety evidence for %s recorded by manager %q while stamping installed cluster %s; use that manager to reconcile it, or remove the exact record only after proving it stale", identity.ResourceID, record.Owner.Manager, task.Entry.Cluster)
			}
			if err := validateConvergeSafetyRecordAuthority(record, identity); err != nil {
				return untrustedConvergenceEvidence(runsDir, identity.ResourceID, err)
			}
		}
		if found && record.HashSchema != ConvergeHashSchema {
			desiredHash, err := ApplyTaskDesiredHash(task)
			if err != nil {
				return err
			}
			class, err := classifyApplyTaskWithRecordForContext(task, runsDir, contextName, record, desiredHash)
			if err != nil {
				return err
			}
			if class != ConvergeSafetyMatch {
				return &ClusterInstallStateError{
					Cluster:   task.Entry.Cluster,
					Condition: ClusterInstallConditionLegacyConvergeEvidenceMismatch,
					Status:    ClusterInstallStatusInstalled,
					Details:   []string{record.ResourceID},
					Message:   fmt.Sprintf("ContainerCluster/%s cannot rebaseline legacy converge record %s because its exact successful-run input snapshot is missing or differs; bootwright refuses to treat unverifiable legacy evidence as a match", task.Entry.Cluster, record.ResourceID),
					Request:   clusterInstallRemedy(remedy.ActionRebuildCluster, task.Entry.Cluster),
				}
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
		if stateErr := validateClusterInstallRecordState(clustersDir, name, record); stateErr != nil {
			description := fmt.Sprintf("install record has unsupported lifecycle state status %q and phase %q — bootwright cannot prove which install steps ran", record.Status, record.Phase)
			var typed *ClusterInstallStateError
			if errors.As(stateErr, &typed) && typed.Condition == ClusterInstallConditionInvalidRecordEvidence {
				description = "install record identity or writer evidence is invalid: " + strings.Join(typed.Details, "; ")
			}
			out = append(out, ClusterReinstall{Name: name, Descriptor: fmt.Sprintf("reinstall ContainerCluster/%s (%s; --mode rebuild reinstalls it and wipes its node disks)", name, description)})
			continue
		}
		hash, structuralHash, input, err := clusterInstallHashesAndInput(contextName, state, name, secretsDir)
		if err != nil {
			continue
		}
		if record.Status != ClusterInstallStatusInstalled {
			if clusterInstallPhaseMayHaveBooted(record.Phase) {
				out = append(out, ClusterReinstall{Name: name, Descriptor: fmt.Sprintf("reinstall ContainerCluster/%s (incomplete install record at phase %q — its nodes may already have booted the installer; --mode rebuild reinstalls it and wipes its node disks)", name, record.Phase)})
			}
			continue
		}
		matches, _, err := clusterInstallRecordInputsMatch(clustersDir, runsDir, record, name, installWaitTask(tasks, name), hash, structuralHash, input)
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
		return nil, unverifiableOverrideProbeError(unverifiable, unverifiableDetails)
	}
	return out, nil
}

func unverifiableOverrideProbeError(unverifiable, details []string) error {
	return &ClusterInstallStateError{
		Clusters:  append([]string(nil), unverifiable...),
		Condition: ClusterInstallConditionAvailabilityProbeFailed,
		Details:   append([]string(nil), details...),
		Message:   fmt.Sprintf("apply --mode rebuild refuses to act on ContainerCluster(s) whose recorded install inputs match desired state but whose availability could not be probed: %s; an unprovable cluster is not a drifted one, so bootwright will not wipe its node disks on a failed probe — restore API reachability and the local oc command before retrying exactly this selected work", strings.Join(details, ", ")),
		Request:   clusterInstallRemedy(remedy.ActionRetrySameInvocation, unverifiable...),
	}
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
		matches, _, matchErr := clusterInstallRecordInputsMatch(clustersDir, runsDir, record, name, installWaitTask(tasks, name), hash, structuralHash, input)
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
		if record.Status == ClusterInstallStatusInstalled && validateClusterInstallRecordState(clustersDir, name, record) == nil {
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
		return &ClusterInstallStateError{
			Cluster:   name,
			Condition: ClusterInstallConditionMissingISORecord,
			Message:   fmt.Sprintf("ContainerCluster/%s: this scope would boot nodes from a previously published agent ISO, but no install record proves an ISO was created from the current desired inputs; bootwright refuses to boot from unproven media", name),
			Request:   clusterInstallRemedy(remedy.ActionRegenerateClusterISO, name),
		}
	case !hashMatches:
		if clusterInstallPhaseMayHaveBooted(record.Phase) {
			return &ClusterInstallStateError{
				Cluster:   name,
				Condition: ClusterInstallConditionPostBootInputDrift,
				Status:    record.Status,
				Phase:     record.Phase,
				Message:   fmt.Sprintf("ContainerCluster/%s: install inputs changed after nodes booted from the published agent ISO; bootwright refuses to mix the new inputs with nodes already changed by the old media, and the target machines must be reset before a deliberate rebuild", name),
				Request:   clusterInstallRemedy(remedy.ActionRebuildCluster, name),
			}
		}
		return &ClusterInstallStateError{
			Cluster:   name,
			Condition: ClusterInstallConditionISOInputDrift,
			Status:    record.Status,
			Phase:     record.Phase,
			Message:   fmt.Sprintf("ContainerCluster/%s: install inputs changed after the published agent ISO was created; booting it would install the cluster from stale inputs while recording the current ones, so bootwright refuses before node boot", name),
			Request:   clusterInstallRemedy(remedy.ActionRegenerateClusterISO, name),
		}
	case record.Status != ClusterInstallStatusInstalled && record.Phase != ClusterInstallPhaseISOCreated && !clusterInstallPhaseMayHaveBooted(record.Phase):
		return &ClusterInstallStateError{
			Cluster:   name,
			Condition: ClusterInstallConditionISOIncomplete,
			Status:    record.Status,
			Phase:     record.Phase,
			Message:   fmt.Sprintf("ContainerCluster/%s has install phase %q: the agent ISO for the desired inputs was never fully created, so bootwright refuses before node boot", name, record.Phase),
			Request:   clusterInstallRemedy(remedy.ActionRegenerateClusterISO, name),
		}
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
		return &ClusterInstallStateError{
			Cluster:    name,
			Condition:  ClusterInstallConditionAvailabilityProbeFailed,
			RecordPath: ClusterInstallRecordPath(clustersDir, name),
			Message:    fmt.Sprintf("ContainerCluster/%s has an existing kubeconfig but no install record, and availability could not be verified; restore API reachability and the local oc command before retrying because bootwright will neither adopt nor overwrite unknown live state", name),
			Cause:      err,
			Request:    clusterInstallRemedy(remedy.ActionRetrySameInvocation, name),
		}
	}
	if !available {
		return &ClusterInstallStateError{
			Cluster:    name,
			Condition:  ClusterInstallConditionNoInstallRecord,
			RecordPath: ClusterInstallRecordPath(clustersDir, name),
			Message:    fmt.Sprintf("ContainerCluster/%s has an existing kubeconfig but no install record and does not report Available=True; bootwright refuses to regenerate installer inputs or overwrite unknown cluster state without a deliberate rebuild", name),
			Request:    clusterInstallRemedy(remedy.ActionRebuildCluster, name),
		}
	}
	return &ClusterInstallStateError{
		Cluster:    name,
		Condition:  ClusterInstallConditionNoInstallRecord,
		RecordPath: ClusterInstallRecordPath(clustersDir, name),
		Message:    fmt.Sprintf("ContainerCluster/%s has a reachable kubeconfig but no install record at %s; bootwright cannot confirm it was installed from the current desired inputs, will not adopt it silently, and requires a deliberate rebuild", name, ClusterInstallRecordPath(clustersDir, name)),
		Request:    clusterInstallRemedy(remedy.ActionRebuildCluster, name),
	}
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
	if record.StartedAt.UTC().After(now.UTC()) {
		return &ClusterInstallResumeExpiredError{
			Cluster:    name,
			Phase:      record.Phase,
			StartedAt:  record.StartedAt.UTC(),
			ObservedAt: now.UTC(),
		}
	}
	deadline := record.StartedAt.UTC().Add(ClusterInstallResumeCeiling)
	if now.UTC().Before(deadline) {
		return nil
	}
	return &ClusterInstallResumeExpiredError{
		Cluster:    name,
		Phase:      record.Phase,
		StartedAt:  record.StartedAt.UTC(),
		ObservedAt: now.UTC(),
		Deadline:   deadline,
	}
}

func resumeClusterInstallTasks(tasks []ApplyTask, record ClusterInstallRecord, name string, now time.Time) ([]ApplyTask, error) {
	switch record.Phase {
	case ClusterInstallPhaseISOCreated:
		if err := guardPublishedAgentISOFresh(record, name, now); err != nil {
			return tasks, err
		}
		return skipClusterInstallTasks(tasks, name, []string{ApplyTaskKindClusterISO}, "previous install created a fresh agent ISO; resuming from node boot", now), nil
	case ClusterInstallPhaseNodesBooted, ClusterInstallPhaseWaitingBootstrap:
		return skipClusterInstallTasks(tasks, name, []string{ApplyTaskKindClusterISO, ApplyTaskKindNodeBoot}, "previous install already booted nodes; resuming bootstrap wait", now), nil
	case ClusterInstallPhaseBootstrapComplete, ClusterInstallPhaseWaiting:
		return skipClusterInstallTasks(tasks, name, []string{ApplyTaskKindClusterISO, ApplyTaskKindNodeBoot, ApplyTaskKindBootstrapWait}, "previous install completed bootstrap; resuming install-complete wait", now), nil
	case "", ClusterInstallPhaseCreatingISO:
		return tasks, nil
	case ClusterInstallPhaseBooting:
		return tasks, &ClusterInstallStateError{
			Cluster:   name,
			Condition: ClusterInstallConditionUncertainBoot,
			Status:    record.Status,
			Phase:     record.Phase,
			Message:   fmt.Sprintf("ContainerCluster/%s has prior install state at phase %s; node boot completion is uncertain, so bootwright refuses to reboot before any mutation", name, record.Phase),
			Request:   clusterInstallRemedy(remedy.ActionRebuildCluster, name),
		}
	default:
		return tasks, &ClusterInstallStateError{
			Cluster:   name,
			Condition: ClusterInstallConditionUnrecognizedPhase,
			Status:    record.Status,
			Phase:     record.Phase,
			Message:   fmt.Sprintf("ContainerCluster/%s has unrecognized install phase %q; bootwright cannot prove which install steps already changed the cluster and refuses before any mutation", name, record.Phase),
			Request:   clusterInstallRemedy(remedy.ActionRebuildCluster, name),
		}
	}
}

const publishedAgentISOFreshWindow = 24 * time.Hour

func guardPublishedAgentISOFresh(record ClusterInstallRecord, name string, now time.Time) error {
	publishedAt := record.UpdatedAt.UTC()
	observedAt := now.UTC()
	if !publishedAt.IsZero() && !observedAt.IsZero() && !publishedAt.After(observedAt) && observedAt.Sub(publishedAt) < publishedAgentISOFreshWindow {
		return nil
	}
	return &ClusterInstallISOAgeError{
		Cluster:     name,
		PublishedAt: publishedAt,
		ObservedAt:  observedAt,
		FreshWindow: publishedAgentISOFreshWindow,
	}
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
