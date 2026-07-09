package converge

import (
	"context"
	"fmt"
	"io"
	"strings"

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

func DestroyDryRunSafetyReport(decision workflow.DestroySafetyDecision, override bool) *DryRunDestroySafety {
	if len(decision.Reasons) == 0 {
		return nil
	}
	return &DryRunDestroySafety{
		OverrideRequired: decision.RequiredOverride,
		Override:         override,
		Reasons:          append([]string(nil), decision.Reasons...),
	}
}

func LoadContextOwnershipRecordsWithWarnings(ownershipDir, contextName string) ([]ownership.ResourceRecord, []error, error) {
	return ownership.LoadContextWithWarnings(ownershipDir, contextName)
}

func ExecuteDestroy(cmdCtx context.Context, stdout, stderr io.Writer, ctx workspace.Context, clustersDir string, executable string, bundleDir string, playbook string, plan WorkflowPlan, artifactsBaseName string, check bool, becomePasswordFile string, dryRun bool, streamAnsible bool, label string, reporter workflow.Reporter) (workflow.RunResult, string, error) {
	logPath := workflow.DestroyLogPath(ctx.RunsDir, artifactsBaseName)
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
	opts.AcquireRunLease = true
	result, err := workflow.Run(cmdCtx, opts, runner, reporter)
	return result, logPath, err
}

func ExecuteDestroyGraph(cmdCtx context.Context, stdout, stderr io.Writer, ctx workspace.Context, clustersDir string, executable string, bundleDir string, scopeName string, clusterScope string, plan WorkflowPlan, check bool, becomePasswordFile string, streamAnsible bool, label string, reporter workflow.ApplyReporter) (render.Result, workflow.RunLedger, string, error) {
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
	prepared, err := workflow.PrepareDestroyTaskGraph(ctx.RunsDir, runOpts, tasks, workflow.ConcurrencyLimits{})
	if err != nil {
		return render.Result{}, workflow.RunLedger{}, "", err
	}
	ledger, err := workflow.RunPreparedDestroyTaskGraph(cmdCtx, stdout, stderr, ctx.RunsDir, runOpts, workflow.ApplyTarget{Name: label}, clusterScope, prepared, reporter, nil)
	return renderResult, ledger, workflow.ApplyRunLogPath(ctx.RunsDir, prepared.RunID), err
}

func ResetConvergeRecordsAfterDestroy(runsDir, clustersDir string, runScope Scope, state v1alpha1.State, storageWorkNames, partialStorageClusters []string) {
	partial := make(map[string]bool, len(partialStorageClusters))
	for _, name := range partialStorageClusters {
		partial[name] = true
	}
	resetScope := runScope
	if runScope.Name == InfraScope.Name {
		resetScope = AllScope
	}
	target := resetScope.ApplyTarget()
	if storageWorkNames != nil {
		target.StorageClusterNames = append([]string{}, storageWorkNames...)
	}
	if tasks, perr := workflow.PlanApplyTasksChecked(target, state); perr == nil {
		for _, task := range tasks {
			if isPartialStorageTask(task, partial) {
				continue
			}
			_ = workflow.RemoveApplyTaskConvergeSafety(runsDir, task)
		}
		for _, name := range workflow.ContainerInstallClusterNames(tasks) {
			_ = workflow.RemoveClusterInstallState(clustersDir, name)
		}
	}
	for _, name := range destroyStorageResetNames(state, storageWorkNames) {
		if partial[name] {
			continue
		}
		_ = workflow.RemoveStorageSubObjectsConvergeSafety(runsDir, state, name)
	}
}

func isPartialStorageTask(task workflow.ApplyTask, partial map[string]bool) bool {
	if len(partial) == 0 {
		return false
	}
	switch task.Entry.Kind {
	case workflow.ApplyTaskKindStorageInfra, workflow.ApplyTaskKindStorageCluster:
		return partial[task.Entry.Cluster]
	}
	return false
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
