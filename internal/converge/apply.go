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
// DESTRUCTIVELY rebuild a drifted, destroy-protected resource.
//
// apply --override authorizes Bootwright-owned destructive rebuilds (managed-OS
// VM reinstall, owned-Ceph wipe-and-rebuild, cluster reinstall). On a
// destroy-protected Environment that destruction must cross the destroy
// authorization boundary instead of slipping in through apply, so fail closed
// before any mutation and direct the operator to destroy first.
//
// The gate is scope- and drift-aware (it takes the classified objects of the
// selected scope, not just the Environment): it fires only when an in-scope
// object is drifted AND its rebuild is destructive. A scoped apply whose only
// drift is a reconfigure-only fabric service (an infra-component or provider
// service that override merely re-applies, never wipes) is not blocked, so a
// drifted shared service can be reconciled in place without a destroy detour;
// likewise a greenfield (missing) object is created, not destroyed. Dry-run/plan
// still previews the override plan.
//
// The remedy it prints names the destroy scope that actually clears the blocked
// resource. A cluster (storage/container) rebuild is cleared by the clusters-stage
// destroy, but MACHINE SUBSTRATE (a managed-OS install) is torn down only by the
// infra stage — a clusters-stage destroy leaves it in place, so pointing a blocked
// managed-OS machine at `destroy --override` for the clusters scope would loop
// forever. The remedy routes machine substrate at `destroy --stage infra` instead.
func CheckApplyOverrideDestroyProtection(state v1alpha1.State, objects []workflow.ObjectClassification) error {
	destructive := workflow.OverrideDestructiveDriftedObjects(objects)
	if len(destructive) == 0 {
		return nil
	}
	// Fleet-wide requiredOverride blocks EVERY destructive rebuild in scope.
	if protected := workflow.ProtectedEnvironments(state); len(protected) > 0 {
		machineLabels, machineClusters := workflow.OverrideDestructiveMachineSubstrate(objects)
		hasMachine := len(machineLabels) > 0
		hasCluster := len(destructive) > len(machineLabels)
		remedy := overrideDestroyRemedy(hasMachine, hasCluster, machineClusters)
		return fmt.Errorf("apply --override would destructively rebuild protected resource(s) %s in Environment %s; %s, then re-apply (drifted reconfigure-only services do not trip this — align their desired state or let --override reconcile them in place)", strings.Join(destructive, ", "), strings.Join(protected, ", "), remedy)
	}
	// Granular protectedKinds blocks only destructive rebuilds of a protected kind,
	// even when the fleet default is allow — so a scratch ContainerCluster rebuilds
	// freely while a protected StorageCluster or Machine must cross the destroy boundary.
	blocked := workflow.OverrideDestructiveKindProtected(objects, workflow.ProtectedKindSet(state))
	if len(blocked) == 0 {
		return nil
	}
	return fmt.Errorf("apply --override would destructively rebuild %s, protected by spec.safety.protectedKinds; run `bootwright destroy --override` for that scope first, then re-apply (drifted reconfigure-only services do not trip this)", strings.Join(blocked, ", "))
}

// overrideDestroyRemedy builds the "run destroy first" clause, routing each
// blocked resource at the destroy scope that clears it: cluster (storage/
// container) rebuilds at the clusters-stage destroy, and machine substrate
// (managed-OS installs, torn down only by the infra stage) at `destroy --stage
// infra`, scoped to the affected clusters when they are known. hasMachine reports
// whether any blocked object is machine substrate; hasCluster whether any blocked
// object is cleared by the clusters-stage destroy; machineClusters is the affected
// clusters (empty when no machine substrate is blocked or its cluster is unknown).
func overrideDestroyRemedy(hasMachine, hasCluster bool, machineClusters []string) string {
	infra := "bootwright destroy --stage infra --override"
	if len(machineClusters) > 0 {
		infra = "bootwright destroy --stage infra --clusters " + strings.Join(machineClusters, ",") + " --override"
	}
	// Managed-OS substrate whose host was never provisioned or is powered off
	// (e.g. a nested cluster on a host cluster that never came up) needs
	// --skip-unreachable for the infra-stage destroy to skip the unreachable
	// target instead of failing closed; surface it so the operator does not have
	// to rediscover the flag.
	skipHint := " (add --skip-unreachable if a machine's host substrate was never provisioned or is powered off)"
	switch {
	case hasMachine && !hasCluster:
		return "machine substrate is torn down by the infra stage, not the clusters stage, so run `" + infra + "` first" + skipHint
	case hasMachine && hasCluster:
		return "run `bootwright destroy --override` for the cluster scope AND `" + infra + "` for the machine substrate (torn down only by the infra stage) first" + skipHint
	default:
		return "run `bootwright destroy --override` for that scope first"
	}
}

// PlanScopedApply stamps the apply safety mode onto the plan's extra vars,
// builds the apply target (restricting storage provisioning to the explicitly
// selected storage roots when a --clusters selection is active), plans the
// checked task graph, and resolves concurrency limits. The returned dryRunTasks
// carry dry-run cluster log annotations for plan/JSON output.
//
// selectedStorageNames is the StorageCluster names named directly in --clusters
// (empty when only container clusters are selected); clusterSelectionActive
// reports whether any --clusters selection is in force.
func PlanScopedApply(runScope Scope, plan *WorkflowPlan, mode workflow.ApplyMode, selectedStorageNames []string, clusterSelectionActive bool, limits workflow.ConcurrencyLimits, runsDir string) (workflow.ApplyTarget, []workflow.ApplyTask, workflow.ConcurrencyLimits, []workflow.ApplyTask, error) {
	// The explicit safety mode drives both the Go object preflight and the
	// per-role Ansible gate (create/continue/override). It replaces the legacy
	// bootwright_install_override boolean.
	plan.ExtraVarPairs = append(plan.ExtraVarPairs, "bootwright_apply_mode="+string(mode))
	applyTarget := runScope.ApplyTarget()
	if clusterSelectionActive {
		// plan.State includes managed StorageClusters pulled in only as render
		// references for a selected container cluster's data-foundation
		// attachment. Per ADR-0004 a scoped apply does not implicitly provision a
		// cross-cluster dependency, so provision only the storage roots named
		// directly in --clusters; co-select the storage cluster (or apply it
		// first) to provision it alongside its consumers. A non-nil empty slice
		// keeps every render-reference cluster out of the provisioning set.
		applyTarget.StorageClusterNames = append([]string{}, selectedStorageNames...)
	}
	tasks, err := workflow.PlanApplyTasksChecked(applyTarget, plan.State)
	if err != nil {
		return workflow.ApplyTarget{}, nil, workflow.ConcurrencyLimits{}, nil, err
	}
	limits = workflow.ResolveApplyConcurrencyLimits(limits, tasks)
	dryRunTasks := workflow.AnnotateApplyTaskClusterLogPaths(runsDir, "dry-run", tasks)
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
