package converge

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/ansible"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/ownership"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/workspace"
)

func DestroyStageScope(stage string) (Scope, error) {
	switch strings.TrimSpace(stage) {
	case "":
		return AllScope, nil
	case "infra":
		return InfraScope, nil
	case "clusters":
		return ClustersScope, nil
	default:
		return Scope{}, fmt.Errorf("--stage must be one of %s (sub-phases %s are apply-only)",
			strings.Join(DestroyStageNames(), ", "), strings.Join(SubPhaseStageNames(), ", "))
	}
}

func DestroyIsFullScope(scope Scope) bool {
	return scope.Name == AllScope.Name
}

func DestroyStageCommandLabel(stage, defaultLabel string) string {
	if strings.TrimSpace(stage) == "" {
		return defaultLabel
	}
	return strings.TrimSpace(stage) + " destroy"
}

func DestroyDryRunSafetyReport(decision workflow.DestroySafetyDecision, authorized bool) *DryRunDestroySafety {
	if len(decision.Reasons) == 0 {
		return nil
	}
	return &DryRunDestroySafety{
		AuthorizationRequired: decision.RequiresAuthorization,
		Authorized:            authorized,
		Reasons:               append([]string(nil), decision.Reasons...),
	}
}

func LoadContextOwnershipRecordsWithWarnings(ownershipDir, contextName string) ([]ownership.ResourceRecord, []error, error) {
	return ownership.LoadContextWithWarnings(ownershipDir, contextName)
}

func ExecuteDestroy(cmdCtx context.Context, stdout, stderr io.Writer, ctx workspace.Context, clustersDir string, executable string, bundleDir string, playbook string, plan WorkflowPlan, artifactsBaseName string, check bool, becomePasswordFile string, dryRun bool, streamAnsible bool, label string, reporter workflow.Reporter, runLease *workflow.CommandRunLease, recordRunLedger bool, invocationArgs []string) (workflow.RunResult, string, error) {
	logPath := workflow.DestroyLogPath(ctx.RunsDir, artifactsBaseName)
	artifactServerDestroy := playbook == InfraDestroyArtifactServerPlaybook
	var artifactRecords []ownership.ResourceRecord
	var artifactOwnerHosts []string
	if artifactServerDestroy && !dryRun && !plan.NoRemoteWork {
		if runLease == nil {
			return workflow.RunResult{}, logPath, fmt.Errorf("artifact-server destroy requires a caller-owned mutating run lease through completion proof and convergence-evidence cleanup")
		}
		var warnings []error
		var err error
		artifactRecords, warnings, err = LoadContextOwnershipRecordsWithWarnings(ctx.OwnershipDir, ctx.Name)
		if err != nil {
			return workflow.RunResult{}, logPath, fmt.Errorf("load artifact-server ownership evidence: %w", err)
		}
		if len(warnings) > 0 {
			parts := make([]string, 0, len(warnings))
			for _, warning := range warnings {
				parts = append(parts, warning.Error())
			}
			return workflow.RunResult{}, logPath, fmt.Errorf("cannot prove artifact-server ownership because ownership evidence was unreadable: %s", strings.Join(parts, "; "))
		}
		artifactOwnerHosts, err = artifactServerOwnerHosts(artifactRecords, ctx.Name, plan.ExtraVarPairs)
		if err != nil {
			return workflow.RunResult{}, logPath, err
		}
	}
	runner := ansible.CommandRunner{}
	if streamAnsible {
		runner = ansible.CommandRunner{Stdout: stdout, Stderr: stderr}
	}
	opts := runOptionsForContext(ctx, clustersDir, executable, plan.State)
	opts.BundleDir = bundleDir
	opts.Playbook = playbook
	opts.Limit = plan.Limit
	opts.ExtraVarPairs = plan.ExtraVarPairs
	opts.ArtifactsBaseName = artifactsBaseName
	opts.OutputLogPath = logPath
	opts.Check = check
	opts.AskBecomePass = plan.AskBecomePass && becomePasswordFile == ""
	opts.BecomePasswordFile = becomePasswordFile
	opts.UseControllingTTY = UseControllingTTYForWorkflow(plan.Selected, plan.AskBecomePass && becomePasswordFile == "")
	opts.DryRun = dryRun
	opts.Label = label
	opts.AcquireRunLease = runLease == nil
	opts.RecordRunLedger = recordRunLedger
	opts.RunLease = runLease
	opts.InvocationArgs = append([]string(nil), invocationArgs...)
	opts.ClassifyUnreachable = artifactServerDestroy
	if artifactServerDestroy && !dryRun && !plan.NoRemoteWork {
		expectedHosts := append([]string(nil), render.HostGroupMembersWithOwnershipRecords(plan.State, artifactRecords)[plan.Limit]...)
		sort.Strings(expectedHosts)
		opts.PostRunFinalizer = func(result workflow.RunResult) error {
			proofPath := filepath.Join(result.Render.ArtifactsDir, artifactsBaseName, ansible.RunResultName)
			if err := workflow.RequireDestroyCompletionEvidence(proofPath, "destroy.infra-artifact-server", expectedHosts); err != nil {
				return err
			}
			if err := runLease.RequireOwned(); err != nil {
				return err
			}
			return resetArtifactServerConvergenceEvidence(ctx.RunsDir, artifactOwnerHosts)
		}
	}
	result, err := workflow.Run(cmdCtx, opts, runner, reporter)
	return result, logPath, err
}

func resetArtifactServerConvergenceEvidence(runsDir string, hosts []string) error {
	for _, host := range hosts {
		task := workflow.ApplyTask{Entry: workflow.TaskLedgerEntry{
			ID:   "infra-component." + host,
			Kind: workflow.ApplyTaskKindInfraComponentServices,
		}}
		if err := workflow.RemoveApplyTaskConvergeSafety(runsDir, task); err != nil {
			return fmt.Errorf("remove artifact-server convergence evidence for host %s: %w", host, err)
		}
	}
	return nil
}

func artifactServerOwnerHosts(records []ownership.ResourceRecord, contextName string, extraVars []string) ([]string, error) {
	selected, constrained, err := artifactServerReclaimSelection(extraVars)
	if err != nil {
		return nil, err
	}
	found := map[string]bool{}
	hosts := map[string]bool{}
	for _, record := range records {
		if record.Kind != ownershipInfraComponentKind || record.Labels["bootwright.kind"] != artifactServerRecordKindLabel || record.IsReference() {
			continue
		}
		if constrained && !selected[record.Name] {
			continue
		}
		provider := strings.TrimSpace(record.Labels["bootwright.provider"])
		component := strings.TrimSpace(record.Labels["bootwright.name"])
		if record.APIVersion != "bootwright.io/ownership/v1alpha1" || record.Owner != ownership.Owner || record.EffectiveRole() != ownership.RoleOwner || record.Context != contextName || strings.TrimSpace(record.Host) == "" || strings.TrimSpace(record.Provider) == "" || record.Provider != provider || component == "" || record.Name != provider+"-"+component || record.Attributes["componentKind"] != artifactServerRecordKindLabel {
			return nil, fmt.Errorf("refusing artifact-server destroy because owner record %s does not have exact current-context API, owner, role, host, provider, component, and artifact identity", record.Name)
		}
		found[record.Name] = true
		hosts[record.Host] = true
	}
	if constrained {
		for name := range selected {
			if !found[name] {
				return nil, fmt.Errorf("refusing artifact-server reclaim because selected owner record %s is missing or does not have exact artifact identity", name)
			}
		}
	}
	out := make([]string, 0, len(hosts))
	for host := range hosts {
		out = append(out, host)
	}
	sort.Strings(out)
	return out, nil
}

func artifactServerReclaimSelection(extraVars []string) (map[string]bool, bool, error) {
	selected := map[string]bool{}
	constrained := false
	for _, pair := range extraVars {
		name, value, ok := strings.Cut(pair, "=")
		if !ok || name != InfraComponentReclaimExtraVar {
			continue
		}
		if constrained {
			return nil, false, fmt.Errorf("artifact-server reclaim selection was specified more than once")
		}
		constrained = true
		for _, item := range strings.Split(value, ",") {
			item = strings.TrimSpace(item)
			if item == "" || selected[item] {
				return nil, false, fmt.Errorf("artifact-server reclaim selection contains an empty or duplicate owner record identity")
			}
			selected[item] = true
		}
	}
	return selected, constrained, nil
}

func ExecuteDestroyGraph(cmdCtx context.Context, stdout, stderr io.Writer, ctx workspace.Context, clustersDir string, executable string, bundleDir string, scopeName string, clusterScope string, plan WorkflowPlan, check bool, becomePasswordFile string, streamAnsible bool, label string, reporter workflow.ApplyReporter, runLease *workflow.CommandRunLease, invocationArgs []string) (render.Result, workflow.RunLedger, string, error) {
	if runLease != nil {
		bound, release, err := runLease.BindContext(cmdCtx)
		if err != nil {
			return render.Result{}, workflow.RunLedger{}, "", err
		}
		defer release()
		cmdCtx = bound
	}
	renderResult, err := workflow.RenderOnly(ctx.RenderedDir, clustersDir, ctx.SecretsDir, plan.State)
	if err != nil {
		return render.Result{}, workflow.RunLedger{}, "", err
	}
	tasks, err := workflow.PlanDestroyTasks(scopeName, plan.State, plan.Limit, plan.ExtraVarPairs, plan.StorageWorkNames)
	if err != nil {
		return render.Result{}, workflow.RunLedger{}, "", err
	}
	runOpts := runOptionsForContext(ctx, clustersDir, executable, plan.State)
	runOpts.BundleDir = bundleDir
	runOpts.Check = check
	runOpts.AskBecomePass = plan.AskBecomePass && becomePasswordFile == ""
	runOpts.BecomePasswordFile = becomePasswordFile
	runOpts.StreamAnsible = streamAnsible
	runOpts.SelectedMachines = plan.SelectedMachines
	runOpts.RunLease = runLease
	runOpts.InvocationArgs = append([]string(nil), invocationArgs...)
	prepared, err := workflow.PrepareDestroyTaskGraph(ctx.RunsDir, runOpts, tasks, workflow.ConcurrencyLimits{})
	if err != nil {
		return render.Result{}, workflow.RunLedger{}, "", err
	}
	ledger, err := workflow.RunPreparedDestroyTaskGraph(cmdCtx, stdout, stderr, ctx.RunsDir, runOpts, workflow.ApplyTarget{Name: label}, clusterScope, prepared, reporter, nil)
	return renderResult, ledger, workflow.ApplyRunLogPath(ctx.RunsDir, prepared.RunID), err
}

func ResetConvergeRecordsAfterDestroy(runsDir, clustersDir, contextName string, runScope Scope, state v1alpha1.State, storageWorkNames, partialStorageClusters []string, fabricHosts map[string]bool, succeededDestroyKinds workflow.DestroyOutcome, destroyRunID string, purgeHistory, skipUnreachable bool) []error {
	var problems []error
	var purgedClusters, resetClusters []string
	partial := make(map[string]bool, len(partialStorageClusters))
	for _, name := range partialStorageClusters {
		partial[name] = true
	}
	include := destroyKindIncluded(succeededDestroyKinds)
	purgeProven := !skipUnreachable
	resetScope := runScope
	if runScope.Name == InfraScope.Name {
		resetScope = AllScope
	}
	target := resetScope.ApplyTarget()
	if storageWorkNames != nil {
		target.StorageClusterNames = append([]string{}, storageWorkNames...)
	}
	target.FabricHosts = fabricHosts
	if tasks, perr := workflow.PlanApplyTasksChecked(target, state); perr != nil {
		problems = append(problems, fmt.Errorf("plan converge-record reset: %w", perr))
	} else {
		for _, task := range tasks {
			if isPartialStorageTask(task, partial) {
				continue
			}
			if !include(destroyKindForApplyTaskKind(task.Entry.Kind), task.Entry.Cluster) {
				continue
			}
			if err := workflow.RemoveApplyTaskConvergeSafety(runsDir, task); err != nil {
				problems = append(problems, fmt.Errorf("remove converge record for %s: %w", task.Entry.ID, err))
			}
		}
		for _, name := range workflow.ContainerInstallClusterNames(tasks) {
			if !include(workflow.DestroyTaskKindContainerCluster, name) {
				continue
			}
			if purgeHistory && purgeProven {
				purgedClusters = append(purgedClusters, name)
				continue
			}
			if err := workflow.RemoveClusterInstallState(clustersDir, contextName, name); err != nil {
				problems = append(problems, fmt.Errorf("remove install record for ContainerCluster/%s: %w", name, err))
			}
			resetClusters = append(resetClusters, name)
		}
		if ScopeTearsMachineLayer(runScope) {
			for _, name := range workflow.MachineSubstrateClusters(tasks) {
				if !include(workflow.DestroyTaskKindMachineInfra, name) {
					continue
				}
				if substrateReleaseConfirmed(name, runScope, state, storageWorkNames, partial, succeededDestroyKinds, skipUnreachable) {
					if err := workflow.MarkSubstrateReleased(runsDir, name, time.Now()); err != nil {
						problems = append(problems, fmt.Errorf("record substrate release for %s: %w", name, err))
					}
				}
				if partial[name] {
					continue
				}
				if purgeHistory && purgeProven {
					purgedClusters = append(purgedClusters, name)
					continue
				}
				resetClusters = append(resetClusters, name)
			}
		}
	}
	for _, name := range destroyStorageResetNames(state, storageWorkNames) {
		if partial[name] || !include(workflow.DestroyTaskKindStorageCluster, name) {
			continue
		}
		if err := workflow.RemoveStorageSubObjectsConvergeSafety(runsDir, state, name); err != nil {
			problems = append(problems, fmt.Errorf("remove storage sub-object records for StorageCluster/%s: %w", name, err))
		}
		if err := workflow.RemoveStorageClusterCapturedSecrets(clustersDir, contextName, name); err != nil {
			problems = append(problems, fmt.Errorf("remove captured secrets for StorageCluster/%s: %w", name, err))
		}
		if purgeHistory {
			purgedClusters = append(purgedClusters, name)
			continue
		}
		resetClusters = append(resetClusters, name)
	}
	if DestroyIsFullScope(runScope) && storageWorkNames == nil && len(partial) == 0 && workflow.DestroyOutcomeFullySucceeded(succeededDestroyKinds) {
		if err := workflow.RemoveAllConvergeSafetyRecords(runsDir); err != nil {
			problems = append(problems, fmt.Errorf("remove remaining converge records: %w", err))
		}
	}
	if purgeHistory {
		keepMachineState := !ScopeTearsMachineLayer(runScope)
		for _, name := range uniqueDestroyedNames(purgedClusters) {
			if err := purgeClusterRuntimeDir(clustersDir, name, keepMachineState); err != nil {
				problems = append(problems, fmt.Errorf("purge history for cluster %s: %w", name, err))
			}
		}
		if purgeProven && DestroyIsFullScope(runScope) && storageWorkNames == nil && len(partial) == 0 && workflow.DestroyOutcomeFullySucceeded(succeededDestroyKinds) {
			if err := purgeAllRunHistory(runsDir, destroyRunID); err != nil {
				problems = append(problems, fmt.Errorf("purge run history: %w", err))
			}
		} else if len(purgedClusters) > 0 {
			if err := purgeRunHistoryForComponents(runsDir, purgedClusters, nil, destroyRunID); err != nil {
				problems = append(problems, fmt.Errorf("purge run history: %w", err))
			}
		}
	}
	problems = append(problems, pruneDestroyedClusterStateDirs(clustersDir, resetClusters)...)
	return problems
}

func pruneDestroyedClusterStateDirs(clustersDir string, clusters []string) []error {
	var problems []error
	for _, name := range uniqueDestroyedNames(clusters) {
		if err := pruneEmptyClusterStateDirs(clustersDir, name); err != nil {
			problems = append(problems, fmt.Errorf("prune emptied state directories for cluster %s: %w", name, err))
		}
	}
	return problems
}

func uniqueDestroyedNames(names []string) []string {
	seen := make(map[string]bool, len(names))
	out := make([]string, 0, len(names))
	for _, name := range names {
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

func substrateReleaseConfirmed(cluster string, runScope Scope, state v1alpha1.State, storageWorkNames []string, partial map[string]bool, succeededDestroyKinds workflow.DestroyOutcome, skipUnreachable bool) bool {
	if partial[cluster] {
		return false
	}
	storageTornDown := succeededDestroyKinds.Covers(workflow.DestroyTaskKindStorageCluster, cluster)
	if !skipUnreachable {
		return storageTornDown || !succeededDestroyKinds.Attempted(workflow.DestroyTaskKindStorageCluster, cluster)
	}
	if !workflow.DestroyScopeCoversStorage(runScope.Name) || !storageTornDown {
		return false
	}
	for _, name := range destroyStorageResetNames(state, storageWorkNames) {
		if name == cluster {
			return true
		}
	}
	return false
}

func DestroyStorageScopeNames(state v1alpha1.State, storageWorkNames []string) []string {
	return destroyStorageResetNames(state, storageWorkNames)
}

func destroyStorageResetNames(state v1alpha1.State, storageWorkNames []string) []string {
	if storageWorkNames != nil {
		return storageWorkNames
	}
	names := make([]string, 0, len(state.StorageClusters))
	for _, cluster := range state.StorageClusters {
		names = append(names, cluster.Metadata.Name)
	}
	return names
}
