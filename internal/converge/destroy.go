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
	"github.com/crmarques/bootwright/internal/workspace"
)

func DestroyStageScope(stage string) (Scope, error) {
	switch strings.TrimSpace(stage) {
	case "infra":
		return InfraScope, nil
	case "clusters":
		return ClustersScope, nil
	default:
		return Scope{}, fmt.Errorf("--stage must be one of infra, clusters")
	}
}

func DestroyStageCommandLabel(stage, defaultLabel string) string {
	if strings.TrimSpace(stage) == "" {
		return defaultLabel
	}
	return strings.TrimSpace(stage) + " destroy"
}

func DestroyDryRunReportScope(scope Scope, stage string, stageSelector bool) Scope {
	if stageSelector && strings.TrimSpace(stage) == "clusters" {
		scope.Name = "clusters"
	}
	return scope
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
	result, err := workflow.Run(cmdCtx, workflow.RunOptions{
		State:              plan.State,
		RenderedDir:        ctx.RenderedDir,
		ClustersDir:        clustersDir,
		RunsDir:            ctx.RunsDir,
		ContextName:        ctx.Name,
		SecretsDir:         ctx.SecretsDir,
		ManagedServicesDir: ctx.ManagedServicesDir,
		ProviderStateDir:   ctx.ProviderStateDir,
		OwnershipDir:       ctx.OwnershipDir,
		Executable:         executable,
		BundleDir:          bundleDir,
		Playbook:           playbook,
		Limit:              plan.Limit,
		ExtraVarPairs:      plan.ExtraVarPairs,
		ArtifactsBaseName:  artifactsBaseName,
		OutputLogPath:      logPath,
		Check:              check,
		AskBecomePass:      plan.AskBecomePass && becomePasswordFile == "",
		BecomePasswordFile: becomePasswordFile,
		UseControllingTTY:  UseControllingTTYForWorkflow(plan.Selected, plan.AskBecomePass && becomePasswordFile == ""),
		DryRun:             dryRun,
		Label:              label,
		// Destroy mutates outside the apply scheduler; hold the run lease for
		// the teardown so it is mutually exclusive with a concurrent apply.
		AcquireRunLease: true,
	}, runner, reporter)
	return result, logPath, err
}

// ResetConvergeRecordsAfterDestroy resets convergence records for what a
// successful destroy tore down so a later apply re-sees those objects as
// missing: the default reconcile creates rather than skips a gone object as
// already-applied, and --expect-new no longer refuses it. Best-effort — a
// cleanup miss must not fail an otherwise-successful destroy. state is
// already scoped to the selected roots, so the planned tasks cover exactly
// what was destroyed.
func ResetConvergeRecordsAfterDestroy(runsDir, clustersDir string, runScope Scope, state v1alpha1.State) {
	if tasks, perr := workflow.PlanApplyTasksChecked(runScope.ApplyTarget(), state); perr == nil {
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
	for _, cluster := range state.StorageClusters {
		_ = workflow.RemoveStorageSubObjectsConvergeSafety(runsDir, state, cluster.Metadata.Name)
	}
}
