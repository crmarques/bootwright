package converge

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/bundle"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/workspace"
)

// CheckApplyOverrideDestroyProtection fails closed when apply --override would
// rebuild destroy-protected resources.
//
// apply --override authorizes Bootwright-owned destructive rebuilds
// (managed-OS VM reinstall, owned-Ceph wipe-and-rebuild). On a
// destroy-protected Environment that destruction must cross the destroy
// authorization boundary instead of slipping in through apply, so fail
// closed before any mutation and direct the operator to destroy first.
// Dry-run/plan still previews the override plan.
func CheckApplyOverrideDestroyProtection(state v1alpha1.State) error {
	if protected := workflow.ProtectedEnvironments(state); len(protected) > 0 {
		return fmt.Errorf("apply --override would rebuild destroy-protected resources (%s); run `bootwright destroy --override` for the affected scope, then re-apply", strings.Join(protected, ", "))
	}
	return nil
}

// PlanScopedApply stamps the apply safety mode onto the plan's extra vars,
// builds the apply target (widening the storage selection to the scoped
// state's transitive closure when a --clusters selection is active), plans
// the checked task graph, and resolves concurrency limits. The returned
// dryRunTasks carry dry-run cluster log annotations for plan/JSON output.
func PlanScopedApply(runScope Scope, plan *WorkflowPlan, mode workflow.ApplyMode, storageSelectionActive bool, limits workflow.ConcurrencyLimits, clustersDir string) (workflow.ApplyTarget, []workflow.ApplyTask, workflow.ConcurrencyLimits, []workflow.ApplyTask, error) {
	// The explicit safety mode drives both the Go object preflight and the
	// per-role Ansible gate (create/continue/override). It replaces the legacy
	// bootwright_install_override boolean.
	plan.ExtraVarPairs = append(plan.ExtraVarPairs, "bootwright_apply_mode="+string(mode))
	applyTarget := runScope.ApplyTarget()
	if storageSelectionActive {
		// plan.State already includes the transitive closure of storage clusters
		// required by the selected container clusters' data-foundation
		// attachments (FilterStateToClusters -> filterStorageToClusters).
		// Schedule provision for all of them; selecting only the literal
		// --clusters storage names would skip a transitively-required managed
		// StorageCluster, failing the attachment task after nodes have booted.
		names := make([]string, 0, len(plan.State.StorageClusters))
		for _, sc := range plan.State.StorageClusters {
			names = append(names, sc.Metadata.Name)
		}
		applyTarget.StorageClusterNames = names
	}
	tasks, err := workflow.PlanApplyTasksChecked(applyTarget, plan.State)
	if err != nil {
		return workflow.ApplyTarget{}, nil, workflow.ConcurrencyLimits{}, nil, err
	}
	limits = workflow.ResolveApplyConcurrencyLimits(limits, tasks)
	dryRunTasks := workflow.AnnotateApplyTaskClusterLogPaths(clustersDir, "dry-run", tasks)
	return applyTarget, tasks, limits, dryRunTasks, nil
}

// ApplyModePreflight classifies every selected object against the recorded
// convergence state and enforces the apply-mode contract before any
// mutation (the default reconcile refuses drift/foreign; --expect-new
// refuses pre-existing objects; --override refuses only foreign). Per-role
// Ansible gates enforce the same contract against live state.
func ApplyModePreflight(mode workflow.ApplyMode, tasks []workflow.ApplyTask, runsDir string) ([]workflow.ObjectClassification, error) {
	objects, err := workflow.ClassifyApplyObjects(tasks, runsDir)
	if err != nil {
		return nil, err
	}
	if err := workflow.EvaluateApplyModePreflight(mode, objects); err != nil {
		return nil, err
	}
	return objects, nil
}

// BuildApplyRunOptions assembles the workflow run options for a scoped apply.
// The become password file is captured by the CLI's credential prompt and
// passed in.
func BuildApplyRunOptions(ctx workspace.Context, clustersDir string, executable string, runScope Scope, plan WorkflowPlan, check bool, becomePasswordFile string, dryRun bool, label string, mode workflow.ApplyMode, streamAnsible bool) workflow.RunOptions {
	opts := runOptionsForContext(ctx, clustersDir, executable, plan.State)
	opts.Playbook = runScope.ApplyPlaybook
	opts.Limit = plan.Limit
	opts.Forks = workflow.AnsibleForksForLimit(plan.State, plan.Limit)
	opts.ExtraVarPairs = plan.ExtraVarPairs
	opts.ArtifactsBaseName = runScope.ArtifactsBaseName
	opts.Check = check
	opts.AskBecomePass = plan.AskBecomePass && becomePasswordFile == ""
	opts.BecomePasswordFile = becomePasswordFile
	opts.UseControllingTTY = UseControllingTTYForWorkflow(plan.Selected, plan.AskBecomePass && becomePasswordFile == "")
	opts.DryRun = dryRun
	opts.ResolveInstaller = plan.TargetsClusters
	opts.Label = label
	opts.ApplyMode = mode
	opts.StreamAnsible = streamAnsible
	return opts
}

// ApplyRunReporter is the progress surface ExecuteApply drives. The CLI's
// workflow reporter implements it; converge stays free of presentation.
type ApplyRunReporter interface {
	RenderStart()
	ResolveInstallerStart()
	BundleStart()
	BundleReady(result bundle.AnsibleBundleResult)
}

// ExecuteApply renders inputs, prepares the task graph, snapshots the run
// input, resolves installer secrets when needed, refreshes the Ansible
// bundle, and runs the prepared apply task graph. It returns the render
// result, the (possibly refreshed) bundle result, and the run ledger; the
// CLI maps the ledger state to exit codes and prints results.
func ExecuteApply(cmdCtx context.Context, stdout, stderr io.Writer, ctx workspace.Context, clustersDir string, runOpts workflow.RunOptions, applyTarget workflow.ApplyTarget, clusterScope string, plan WorkflowPlan, tasks []workflow.ApplyTask, limits workflow.ConcurrencyLimits, usesAnsible bool, bundleResult bundle.AnsibleBundleResult, bundleVersionMarker string, reporter ApplyRunReporter, applyReporter workflow.ApplyReporter) (render.Result, bundle.AnsibleBundleResult, workflow.RunLedger, error) {
	if reporter != nil {
		reporter.RenderStart()
	}
	renderResult, err := workflow.RenderOnly(ctx.RenderedDir, clustersDir, ctx.SecretsDir, plan.State)
	if err != nil {
		return render.Result{}, bundleResult, workflow.RunLedger{}, err
	}
	prepared, err := workflow.PrepareApplyTaskGraph(cmdCtx, ctx.RunsDir, runOpts, tasks, limits)
	if err != nil {
		return render.Result{}, bundleResult, workflow.RunLedger{}, err
	}
	// Record the exact input set this mutating run was launched from as a
	// per-run forensic output next to the run's ledger and task logs.
	if err := SnapshotMutatingRunInput(workflow.RunInputSnapshotDir(ctx.RunsDir, prepared.RunID), ctx); err != nil {
		return render.Result{}, bundleResult, workflow.RunLedger{}, err
	}
	if plan.TargetsClusters {
		reporter.ResolveInstallerStart()
		if _, err := workflow.ResolveInstallerForContext(ctx.Name, clustersDir, ctx.SecretsDir, plan.State); err != nil {
			return render.Result{}, bundleResult, workflow.RunLedger{}, err
		}
	}
	if !plan.NoRemoteWork && usesAnsible {
		reporter.BundleStart()
		bundleResult, err = PrepareWorkflowBundle(bundleResult.Dir, bundleVersionMarker, false)
		if err != nil {
			return render.Result{}, bundleResult, workflow.RunLedger{}, err
		}
		reporter.BundleReady(bundleResult)
		runOpts.BundleDir = bundleResult.Dir
	}
	ledger, err := workflow.RunPreparedApplyTaskGraph(cmdCtx, stdout, stderr, ctx.RunsDir, runOpts, applyTarget, clusterScope, prepared, applyReporter, nil)
	return renderResult, bundleResult, ledger, err
}
