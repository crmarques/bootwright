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

func CheckApplyOverrideDestroyProtection(state v1alpha1.State, objects []workflow.ObjectClassification, reinstallDescriptors []string) error {
	destructive := append(workflow.OverrideDestructiveDriftedObjects(objects), reinstallDescriptors...)
	if len(destructive) == 0 {
		return nil
	}
	if protected := workflow.ProtectedEnvironments(state); len(protected) > 0 {
		machineLabels, machineClusters := workflow.OverrideDestructiveMachineSubstrate(objects)
		hasMachine := len(machineLabels) > 0
		hasCluster := len(destructive) > len(machineLabels)
		remedy := overrideDestroyRemedy(hasMachine, hasCluster, machineClusters, workflow.OverrideDestructiveClusterScope(objects))
		return fmt.Errorf("apply --override would destructively rebuild protected resource(s) %s in Environment %s; %s, then re-apply (drifted reconfigure-only services do not trip this — align their desired state or let --override reconcile them in place)", strings.Join(destructive, ", "), strings.Join(protected, ", "), remedy)
	}
	protectedKinds := workflow.ProtectedKindSet(state)
	blocked := workflow.OverrideDestructiveKindProtected(objects, protectedKinds)
	if protectedKinds[v1alpha1.KindContainerCluster] {
		blocked = append(blocked, reinstallDescriptors...)
	}
	if len(blocked) == 0 {
		return nil
	}
	machineLabels, machineClusters := workflow.OverrideDestructiveMachineSubstrate(objects)
	hasMachine := protectedKinds[v1alpha1.KindMachine] && len(machineLabels) > 0
	clusterNames := workflow.OverrideDestructiveProtectedClusterScope(objects, protectedKinds)
	hasCluster := len(clusterNames) > 0 || (protectedKinds[v1alpha1.KindContainerCluster] && len(reinstallDescriptors) > 0)
	remedy := overrideDestroyRemedy(hasMachine, hasCluster, machineClusters, clusterNames)
	return fmt.Errorf("apply --override would destructively rebuild %s, protected by spec.safety.protectedKinds; %s, then re-apply (drifted reconfigure-only services do not trip this)", strings.Join(blocked, ", "), remedy)
}

func CheckApplyRenameOrphan(state v1alpha1.State, objects []workflow.ObjectClassification, clustersDir string) error {
	provisioned, err := workflow.RecordedProvisionedClusters(clustersDir)
	if err != nil {
		return err
	}
	if len(provisioned) == 0 {
		return nil
	}
	declared := map[string]bool{}
	for _, cluster := range state.ContainerClusters {
		declared[cluster.Metadata.Name] = true
	}
	var undeclared []string
	for _, name := range provisioned {
		if !declared[name] {
			undeclared = append(undeclared, name)
		}
	}
	if len(undeclared) == 0 {
		return nil
	}
	var created []string
	for _, o := range objects {
		if o.Kind == workflow.ObjectKindContainerCluster && !o.Recorded() {
			created = append(created, strings.TrimPrefix(o.Label, "ContainerCluster/"))
		}
	}
	if len(created) == 0 {
		return nil
	}
	return fmt.Errorf("apply would provision new ContainerCluster(s) %s while %s remain provisioned but are no longer declared — the signature of a rename, which bootwright cannot do in place: it would re-provision the new name from scratch and orphan the old cluster (re-imaging its hosts on bare-metal). To rename, restore the old metadata.name; to replace, temporarily restore the old cluster YAML (metadata.name %s), run `bootwright destroy --clusters %s`, then remove that YAML and re-apply — destroy resolves --clusters against the declared state, so the old cluster can only be torn down while its YAML is present; to keep both, leave the old cluster declared and destroy it the same way when you no longer need it", strings.Join(created, ", "), strings.Join(undeclared, ", "), strings.Join(undeclared, ", "), strings.Join(undeclared, ","))
}

func overrideDestroyRemedy(hasMachine, hasCluster bool, machineClusters, clusterNames []string) string {
	infra := "bootwright destroy --stage infra --override"
	if len(machineClusters) > 0 {
		infra = "bootwright destroy --stage infra --clusters " + strings.Join(machineClusters, ",") + " --override"
	}
	cluster := "bootwright destroy --override"
	clusterScoped := false
	if len(clusterNames) > 0 {
		cluster = "bootwright destroy --clusters " + strings.Join(clusterNames, ",") + " --override"
		clusterScoped = true
	}
	skipHint := " (add --skip-unreachable if a machine's host substrate was never provisioned or is powered off)"
	switch {
	case hasMachine && !hasCluster:
		return "machine substrate is torn down by the infra stage, not the clusters stage, so run `" + infra + "` first" + skipHint
	case hasMachine && hasCluster:
		return "run `" + cluster + "` for the cluster scope AND `" + infra + "` for the machine substrate (torn down only by the infra stage) first" + skipHint
	default:
		if clusterScoped {
			return "run `" + cluster + "` first"
		}
		return "run `bootwright destroy --override` for that scope first"
	}
}

func CheckReclaimDestroyProtection(state v1alpha1.State, ownedClusters []string, override bool) error {
	if len(ownedClusters) == 0 || override {
		return nil
	}
	if protected := workflow.ProtectedEnvironments(state); len(protected) > 0 {
		return fmt.Errorf("--reclaim-devices would wipe declared OSD device(s) of Ceph cluster(s) %s in Environment %s with spec.safety.destroyProtection=%s; add --override to authorize this protected data-loss operation", strings.Join(ownedClusters, ", "), strings.Join(protected, ", "), v1alpha1.EnvironmentDestroyProtectionRequiredOverride)
	}
	if workflow.ProtectedKindSet(state)[v1alpha1.KindStorageCluster] {
		return fmt.Errorf("--reclaim-devices would wipe declared OSD device(s) of Ceph cluster(s) %s, protected by spec.safety.protectedKinds; add --override to authorize this protected data-loss operation", strings.Join(ownedClusters, ", "))
	}
	return nil
}

func StateHasManagedStorageCluster(state v1alpha1.State) bool {
	for _, cluster := range state.StorageClusters {
		if cluster.Spec.Ceph != nil && v1alpha1.StorageClusterManaged(cluster) {
			return true
		}
	}
	return false
}

func OverrideStorageDeviceGateApplies(selectionActive bool, selectedStorage map[string]bool, state v1alpha1.State) bool {
	if !selectionActive {
		return StateHasManagedStorageCluster(state)
	}
	for _, cluster := range state.StorageClusters {
		if selectedStorage[cluster.Metadata.Name] && cluster.Spec.Ceph != nil && v1alpha1.StorageClusterManaged(cluster) {
			return true
		}
	}
	return false
}

func PlanScopedApply(runScope Scope, plan *WorkflowPlan, mode workflow.ApplyMode, selectedStorageNames []string, clusterSelectionActive bool, limits workflow.ConcurrencyLimits, runsDir string) (workflow.ApplyTarget, []workflow.ApplyTask, workflow.ConcurrencyLimits, []workflow.ApplyTask, error) {
	plan.ExtraVarPairs = append(plan.ExtraVarPairs, "bootwright_apply_mode="+string(mode))
	applyTarget := runScope.ApplyTarget()
	if clusterSelectionActive {
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

type ApplyRunReporter interface {
	RenderStart()
	ResolveInstallerStart()
	BundleStart()
	BundleReady(result bundle.AnsibleBundleResult)
}

func reportRenderStart(r ApplyRunReporter) {
	if r != nil {
		r.RenderStart()
	}
}

func reportResolveInstallerStart(r ApplyRunReporter) {
	if r != nil {
		r.ResolveInstallerStart()
	}
}

func reportBundleStart(r ApplyRunReporter) {
	if r != nil {
		r.BundleStart()
	}
}

func reportBundleReady(r ApplyRunReporter, result bundle.AnsibleBundleResult) {
	if r != nil {
		r.BundleReady(result)
	}
}

func ExecuteApply(cmdCtx context.Context, stdout, stderr io.Writer, ctx workspace.Context, clustersDir string, runOpts workflow.RunOptions, applyTarget workflow.ApplyTarget, clusterScope string, plan WorkflowPlan, tasks []workflow.ApplyTask, limits workflow.ConcurrencyLimits, usesAnsible bool, bundleResult bundle.AnsibleBundleResult, bundleVersionMarker string, reporter ApplyRunReporter, applyReporter workflow.ApplyReporter) (render.Result, bundle.AnsibleBundleResult, workflow.RunLedger, error) {
	reportRenderStart(reporter)
	renderResult, err := workflow.RenderOnly(ctx.RenderedDir, clustersDir, ctx.SecretsDir, plan.State)
	if err != nil {
		return render.Result{}, bundleResult, workflow.RunLedger{}, err
	}
	prepared, err := workflow.PrepareApplyTaskGraph(cmdCtx, ctx.RunsDir, runOpts, tasks, limits)
	if err != nil {
		return render.Result{}, bundleResult, workflow.RunLedger{}, err
	}
	if err := SnapshotMutatingRunInput(workflow.RunInputSnapshotDir(ctx.RunsDir, prepared.RunID), ctx); err != nil {
		return render.Result{}, bundleResult, workflow.RunLedger{}, err
	}
	if plan.TargetsClusters {
		reportResolveInstallerStart(reporter)
		if _, err := workflow.ResolveInstallerForContext(ctx.Name, clustersDir, ctx.SecretsDir, plan.State); err != nil {
			return render.Result{}, bundleResult, workflow.RunLedger{}, err
		}
	}
	if !plan.NoRemoteWork && usesAnsible {
		reportBundleStart(reporter)
		bundleResult, err = PrepareWorkflowBundle(bundleResult.Dir, bundleVersionMarker, false)
		if err != nil {
			return render.Result{}, bundleResult, workflow.RunLedger{}, err
		}
		reportBundleReady(reporter, bundleResult)
		runOpts.BundleDir = bundleResult.Dir
	}
	ledger, err := workflow.RunPreparedApplyTaskGraph(cmdCtx, stdout, stderr, ctx.RunsDir, runOpts, applyTarget, clusterScope, prepared, applyReporter, nil)
	return renderResult, bundleResult, ledger, err
}
