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
		// No stage selected: tear down the whole context. The full destroy
		// runs the clusters teardown then the infra teardown as one task
		// graph; AllScope has no single DestroyPlaybook, so the granular task
		// chain (PlanDestroyTasks "all") is the only execution path for it.
		return AllScope, nil
	case "infra":
		return InfraScope, nil
	case "clusters":
		return ClustersScope, nil
	default:
		// Educate users who learned apply's wider vocabulary: the sub-phases are
		// apply-only because a sub-phase has no single destroy playbook.
		return Scope{}, fmt.Errorf("--stage must be one of %s (sub-phases %s are apply-only)",
			strings.Join(DestroyStageNames(), ", "), strings.Join(SubPhaseStageNames(), ", "))
	}
}

// DestroyIsFullScope reports whether a resolved destroy scope is the
// whole-context teardown (stage omitted). Full destroy has no single playbook
// and always runs through the granular task graph, so the CLI special-cases it
// for execution and dry-run rendering.
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

// LoadContextOwnershipRecords loads the resource records destroy acts on.
// Destroy tears down resources recorded in the context ownership store even
// when the desired state no longer references them. Load those records so the
// plan's no-remote-work decision (which gates the confirmation prompt and the
// become-password prompt) counts the same hosts workflow.Run will act on.
//
// Records live under a per-context ownership dir, but drop any record
// explicitly stamped with a different context as defense in depth: a
// destroy must never tear down resources recorded for another Bootwright
// context that share a host or a misconfigured context directory.
func LoadContextOwnershipRecords(ownershipDir, contextName string) ([]ownership.ResourceRecord, error) {
	records, err := ownership.LoadResources(ownershipDir)
	if err != nil {
		return nil, err
	}
	return ownership.FilterByContext(records, contextName), nil
}

// ExecuteDestroy assembles the destroy run options and runs the workflow.
// The become password file is captured by the CLI's credential prompt and
// passed in; the reporter is the CLI's progress surface behind the
// workflow.Reporter interface.
func ExecuteDestroy(cmdCtx context.Context, stdout, stderr io.Writer, ctx workspace.Context, clustersDir string, executable string, bundleDir string, playbook string, plan WorkflowPlan, artifactsBaseName string, check bool, becomePasswordFile string, dryRun bool, streamAnsible bool, label string, reporter workflow.Reporter) (workflow.RunResult, string, error) {
	logPath := workflow.DestroyLogPath(ctx.RunsDir, artifactsBaseName)
	// Route ansible output to the log only so the terminal shows the progress
	// view, not a raw teardown stream; --stream-ansible tees it back to the
	// terminal. On failure the runner reads this log to summarize the failure.
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
	// Destroy mutates outside the apply scheduler; hold the run lease for the
	// teardown so it is mutually exclusive with a concurrent apply.
	opts.AcquireRunLease = true
	result, err := workflow.Run(cmdCtx, opts, runner, reporter)
	return result, logPath, err
}

// ExecuteDestroyGraph runs a scoped destroy as an apply-style task graph so it
// shows granular per-step progress and routes ansible output to per-task logs.
// It renders once for the artifact paths, decomposes the scope into the destroy
// task chain (reusing the run's limit and extra-vars), and runs it through the
// shared scheduler. It returns the render result, the run ledger, and the run
// log path. Post-destroy record resets remain the CLI's responsibility.
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
	// Record the cluster selection on the run ledger, symmetric with apply
	// (ExecuteApply passes its clusterScope here); the stage itself is already
	// carried by the run label in ApplyTarget.Name.
	ledger, err := workflow.RunPreparedDestroyTaskGraph(cmdCtx, stdout, stderr, ctx.RunsDir, runOpts, workflow.ApplyTarget{Name: label}, clusterScope, prepared, reporter, nil)
	return renderResult, ledger, workflow.ApplyRunLogPath(ctx.RunsDir, prepared.RunID), err
}

// ResetConvergeRecordsAfterDestroy resets convergence records for what a
// successful destroy tore down so a later apply re-sees those objects as
// missing: the default reconcile creates rather than skips a gone object as
// already-applied, and --expect-new no longer refuses it. Best-effort — a
// cleanup miss must not fail an otherwise-successful destroy. state is
// render-inclusive, so the storage work set (the directly-selected storage
// roots; nil for an unscoped destroy) gates the storage reset exactly as it
// gates the teardown — a render-reference StorageCluster the destroy did not
// tear down keeps its records.
func ResetConvergeRecordsAfterDestroy(runsDir, clustersDir string, runScope Scope, state v1alpha1.State, storageWorkNames []string) {
	resetScope := runScope
	if runScope.Name == InfraScope.Name {
		resetScope = AllScope
	}
	target := resetScope.ApplyTarget()
	if storageWorkNames != nil {
		// Mirror apply's applyTarget.StorageClusterNames so the planned reset
		// tasks cover only the storage clusters actually torn down, never a
		// render-reference cluster pulled into state for attachment rendering.
		target.StorageClusterNames = append([]string{}, storageWorkNames...)
	}
	if tasks, perr := workflow.PlanApplyTasksChecked(target, state); perr == nil {
		for _, task := range tasks {
			_ = workflow.RemoveApplyTaskConvergeSafety(runsDir, task)
		}
		// The ansible cluster destroy runs on the OCP nodes and cannot remove the
		// controller-side install record, connection record, and kubeconfig. Remove
		// them here so the next apply does not refuse a destroyed container cluster
		// at the install-state reconcile (surviving kubeconfig / installed record).
		for _, name := range workflow.ContainerInstallClusterNames(tasks) {
			_ = workflow.RemoveClusterInstallState(clustersDir, name)
		}
	}
	// Storage sub-objects (pools/filesystems/gateways/exports) have no
	// backing task; a destroyed cluster rm-cluster --zap-osds removes them
	// all, so reset their records too or a later apply would mis-report a
	// gone pool as match/drift instead of recreating it.
	for _, name := range destroyStorageResetNames(state, storageWorkNames) {
		_ = workflow.RemoveStorageSubObjectsConvergeSafety(runsDir, state, name)
	}
}

// destroyStorageResetNames returns the StorageCluster names whose convergence
// records a destroy should reset: the directly-selected storage roots when a
// --clusters selection narrowed storage (storageWorkNames non-nil, possibly
// empty), else every StorageCluster the render-inclusive state carries.
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
