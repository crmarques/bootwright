package converge

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/bundle"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/ownership"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/workspace"
)

func CheckApplyOverrideDestroyProtection(state v1alpha1.State, objects []workflow.ObjectClassification, reinstallDescriptors []string) error {
	destructive := append(workflow.OverrideDestructiveDriftedObjects(objects), reinstallDescriptors...)
	if len(destructive) == 0 {
		return nil
	}
	if protected := workflow.ProtectedEnvironments(state); len(protected) > 0 {
		machineLabels, _ := workflow.OverrideDestructiveMachineSubstrate(objects)
		clusterLayer := append(workflow.OverrideDestructiveClusterScope(objects), reinstallDescriptors...)
		return &ApplyOverrideDestroyProtectionError{
			Reason:                ApplyOverrideProtectionEnvironment,
			Destructive:           append([]string(nil), destructive...),
			ProtectedEnvironments: append([]string(nil), protected...),
			MachineLayer:          append([]string(nil), machineLabels...),
			ClusterLayer:          clusterLayer,
		}
	}
	protectedKinds := workflow.ProtectedKindSet(state)
	blocked := workflow.OverrideDestructiveKindProtected(objects, protectedKinds)
	if protectedKinds[v1alpha1.KindContainerCluster] {
		blocked = append(blocked, reinstallDescriptors...)
	}
	clusterLayer := workflow.OverrideDestructiveProtectedClusterScope(objects, protectedKinds)
	if protectedKinds[v1alpha1.KindContainerCluster] {
		clusterLayer = append(clusterLayer, reinstallDescriptors...)
	}
	machineLabels, machineClusters := workflow.OverrideDestructiveMachineSubstrate(objects)
	if rhsmClusters := managedRegistrationClustersInScope(state, machineClusters); len(rhsmClusters) > 0 {
		return &ApplyOverrideDestroyProtectionError{
			Reason:              ApplyOverrideProtectionManagedRHSM,
			ManagedRHSMClusters: append([]string(nil), rhsmClusters...),
			MachineLayer:        append([]string(nil), machineLabels...),
			ClusterLayer:        append([]string(nil), clusterLayer...),
		}
	}
	if len(blocked) == 0 {
		return nil
	}
	var machineLayer []string
	if protectedKinds[v1alpha1.KindMachine] {
		machineLayer = append(machineLayer, machineLabels...)
	}
	return &ApplyOverrideDestroyProtectionError{
		Reason:       ApplyOverrideProtectionKinds,
		Destructive:  append([]string(nil), blocked...),
		MachineLayer: machineLayer,
		ClusterLayer: clusterLayer,
	}
}

func CheckApplyRenameOrphan(state v1alpha1.State, objects []workflow.ObjectClassification, clustersDir string, ownershipRecords []ownership.ResourceRecord) error {
	if err := checkContainerRenameOrphan(state, objects, clustersDir); err != nil {
		return err
	}
	return checkStorageRenameOrphan(state, objects, ownershipRecords)
}

type ApplyRenameOrphanError struct {
	Kind       string
	Created    []string
	Undeclared []string
}

func (e *ApplyRenameOrphanError) Error() string {
	if e.Kind == v1alpha1.KindStorageCluster {
		return fmt.Sprintf("apply would provision new StorageCluster(s) %s while %s remain provisioned (Bootwright ownership records prove a live cephadm cluster) but are no longer declared — the signature of a rename, which bootwright cannot do in place: it would bootstrap the new name from scratch, reimaging its machines on bare metal, and orphan the old Ceph cluster with its OSD data", strings.Join(e.Created, ", "), strings.Join(e.Undeclared, ", "))
	}
	return fmt.Sprintf("apply would provision new ContainerCluster(s) %s while %s remain provisioned but are no longer declared — the signature of a rename, which bootwright cannot do in place: it would re-provision the new name from scratch and orphan the old cluster (re-imaging its hosts on bare-metal)", strings.Join(e.Created, ", "), strings.Join(e.Undeclared, ", "))
}

func checkStorageRenameOrphan(state v1alpha1.State, objects []workflow.ObjectClassification, ownershipRecords []ownership.ResourceRecord) error {
	declared := map[string]bool{}
	for _, cluster := range state.StorageClusters {
		declared[cluster.Metadata.Name] = true
	}
	seen := map[string]bool{}
	var undeclared []string
	for _, record := range ownershipRecords {
		if record.Kind != string(ownership.KindStorageCluster) || record.Cluster == "" {
			continue
		}
		if record.Owner != "" && record.Owner != ownership.Owner {
			continue
		}
		if declared[record.Cluster] || seen[record.Cluster] {
			continue
		}
		seen[record.Cluster] = true
		undeclared = append(undeclared, record.Cluster)
	}
	if len(undeclared) == 0 {
		return nil
	}
	var created []string
	for _, o := range objects {
		if o.Kind == workflow.ObjectKindStorageCluster && !o.Recorded() {
			created = append(created, strings.TrimPrefix(o.Label, workflow.ObjectKindStorageCluster+"/"))
		}
	}
	if len(created) == 0 {
		return nil
	}
	sort.Strings(undeclared)
	sort.Strings(created)
	return &ApplyRenameOrphanError{Kind: v1alpha1.KindStorageCluster, Created: created, Undeclared: undeclared}
}

func checkContainerRenameOrphan(state v1alpha1.State, objects []workflow.ObjectClassification, clustersDir string) error {
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
	return &ApplyRenameOrphanError{Kind: v1alpha1.KindContainerCluster, Created: created, Undeclared: undeclared}
}

func managedRegistrationClustersInScope(state v1alpha1.State, machineClusters []string) []string {
	var out []string
	for _, name := range machineClusters {
		for _, cluster := range state.StorageClusters {
			if cluster.Metadata.Name == name && v1alpha1.StorageClusterManagedRegistration(cluster, state) {
				out = append(out, name)
				break
			}
		}
	}
	return out
}

func CheckReclaimDestroyProtection(state v1alpha1.State, ownedClusters []string, override bool) error {
	if len(ownedClusters) == 0 || override {
		return nil
	}
	if protected := workflow.ProtectedEnvironments(state); len(protected) > 0 {
		return fmt.Errorf("--reclaim-devices would wipe declared OSD device(s) of Ceph cluster(s) %s in Environment %s with spec.safety.destroyProtection=%s; add --authorize data-loss to authorize this protected data-loss operation", strings.Join(ownedClusters, ", "), strings.Join(protected, ", "), v1alpha1.EnvironmentDestroyProtectionProtected)
	}
	if workflow.ProtectedKindSet(state)[v1alpha1.KindStorageCluster] {
		return fmt.Errorf("--reclaim-devices would wipe declared OSD device(s) of Ceph cluster(s) %s, protected by spec.safety.protectedKinds; add --authorize data-loss to authorize this protected data-loss operation", strings.Join(ownedClusters, ", "))
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

func PlanScopedApply(cmdCtx context.Context, runScope Scope, plan *WorkflowPlan, fullState v1alpha1.State, mode workflow.ApplyMode, selectedStorageNames []string, clusterSelectionActive bool, machineProvision, fabricHosts map[string]bool, limits workflow.ConcurrencyLimits, runsDir, contextName, secretsDir string) (workflow.ApplyTarget, []workflow.ApplyTask, workflow.ConcurrencyLimits, []workflow.ApplyTask, error) {
	gitSourceRoots, err := ResolveGitSources(cmdCtx, plan.State, contextName, secretsDir, GitSourceCacheDir(runsDir))
	if err != nil {
		return workflow.ApplyTarget{}, nil, workflow.ConcurrencyLimits{}, nil, err
	}
	plan.ExtraVarPairs = append(plan.ExtraVarPairs, "bootwright_apply_mode="+string(mode))
	applyTarget := runScope.ApplyTarget()
	if clusterSelectionActive {
		applyTarget.StorageClusterNames = append([]string{}, selectedStorageNames...)
	}
	applyTarget.MachineProvision = machineProvision
	applyTarget.FabricHosts = fabricHosts
	applyTarget.GitSourceRoots = gitSourceRoots
	tasks, err := workflow.PlanApplyTasksCheckedWithHashState(applyTarget, plan.State, fullState)
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

func ArbiterPreparePreflight(tasks []workflow.ApplyTask, runsDir, cluster string) error {
	objects, err := workflow.ClassifyApplyObjects(tasks, runsDir)
	if err != nil {
		return err
	}
	return workflow.EvaluateApplyModePreflight(workflow.ApplyModeReconcile, workflow.WithoutTiebreakerDrift(objects, cluster))
}

func BuildApplyRunOptions(ctx workspace.Context, clustersDir string, executable string, runScope Scope, plan WorkflowPlan, check bool, becomePasswordFile string, dryRun bool, label string, mode workflow.ApplyMode, streamAnsible bool, invocationArgs []string) workflow.RunOptions {
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
	opts.InvocationArgs = append([]string(nil), invocationArgs...)
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
	if runOpts.RunLease != nil {
		bound, release, err := runOpts.RunLease.BindContext(cmdCtx)
		if err != nil {
			return render.Result{}, bundleResult, workflow.RunLedger{}, err
		}
		defer release()
		cmdCtx = bound
	}
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
	if runOpts.RunLease != nil {
		if err := runOpts.RunLease.RequireOwned(); err != nil {
			return render.Result{}, bundleResult, workflow.RunLedger{}, err
		}
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

func OwnedStorageClusterRecordNames(ownershipRecords []ownership.ResourceRecord) []string {
	seen := map[string]bool{}
	var out []string
	for _, record := range ownershipRecords {
		if record.Kind != string(ownership.KindStorageCluster) || record.Cluster == "" {
			continue
		}
		if record.Owner != "" && record.Owner != ownership.Owner {
			continue
		}
		if seen[record.Cluster] {
			continue
		}
		seen[record.Cluster] = true
		out = append(out, record.Cluster)
	}
	sort.Strings(out)
	return out
}
