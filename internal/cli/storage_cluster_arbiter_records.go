package cli

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/storage/arbiter"
	"github.com/crmarques/bootwright/internal/workspace"
)

func arbiterSameSiteReason(plan arbiter.Plan) string {
	if !plan.SameSite() {
		return ""
	}
	return "mon." + plan.DesiredMon + " sits at " + plan.FailureDomain + "=" + plan.DesiredSite + ", which already holds non-tiebreaker mon(s) " + strings.Join(plan.SameSiteMons, ", ")
}

func refreshArbiterConvergeRecords(ctx workspace.Context, before, after v1alpha1.State, cluster string) ([]string, error) {
	beforeTasks, err := workflow.PlanApplyTasksChecked(converge.AllScope.ApplyTarget(), before)
	if err != nil {
		return nil, err
	}
	afterTasks, err := workflow.PlanApplyTasksChecked(converge.AllScope.ApplyTarget(), after)
	if err != nil {
		return nil, err
	}
	return workflow.RefreshStorageClusterConvergeSafety(ctx.RunsDir, ctx.Name, "", cluster, beforeTasks, afterTasks, time.Now().UTC())
}

func reportArbiterConvergeRecords(stdout io.Writer, cluster string, carried []string, err error, invocation resolvedInvocation) {
	if err != nil {
		retry, retryErr := invocation.retry(retryIntent{})
		if retryErr != nil {
			cliout.NewContinuation(stdout).Warning("converge records", err.Error()+"; the recorded desired state of StorageCluster/"+cluster+" still names the previous tiebreaker, and Bootwright could not safely construct the exact replacement retry")
			return
		}
		cliout.NewContinuation(stdout).Warning("converge records", err.Error()+"; the recorded desired state of StorageCluster/"+cluster+" still names the previous tiebreaker, so the next apply refuses on drift this run created — once the records are writable, retry the exact original replacement with `"+retry.String()+"`")
		return
	}
	if len(carried) == 0 {
		return
	}
	apply, applyErr := invocation.clusterLifecycleRetry(invocationApply, cluster, converge.ClustersScope.Name, workflow.ApplyModeReconcile)
	if applyErr != nil {
		cliout.NewContinuation(stdout).Warning("converge records", "left "+strings.Join(carried, ", ")+" recorded as drifted: their recorded desired state already differed from this context before the arbiter moved, and this run converged only the tiebreaker; Bootwright could not safely construct the exact apply that converges the rest")
		return
	}
	cliout.NewContinuation(stdout).Warning("converge records", "left "+strings.Join(carried, ", ")+" recorded as drifted: their recorded desired state already differed from this context before the arbiter moved, and this run converged only the tiebreaker; converge the rest with `"+apply.String()+"`")
}

func reportArbiterRetirement(stdout io.Writer, auth *authorizations, runsDir string, runLease *workflow.CommandRunLease, invocation resolvedInvocation) {
	result, found, err := converge.ConsumeArbiterRetirement(runsDir, runLease)
	if err != nil {
		cliout.NewContinuation(stdout).Warning("arbiter retirement", arbiterRetirementEvidenceWarning(err.Error(), invocation))
	}
	if !found || !result.Authorized {
		return
	}
	if result.Offline {
		auth.note(authorizeUnreachableNodes)
		return
	}
	cliout.NewContinuation(stdout).Warning("--authorize "+authorizeUnreachableNodes, "not applied: `ceph orch host ls` reports host "+result.Host+" as reachable, so this run had no proof of absence and retired it on the normal `ceph orch host drain` path; the offline path removes a host without draining it, so it is taken only against proved absence")
}

func arbiterRetirementEvidenceWarning(message string, invocation resolvedInvocation) string {
	retry, err := invocation.retry(retryIntent{})
	if err != nil {
		return message + "; repair the reported controller state before another replacement; Bootwright could not safely construct the exact original retry"
	}
	return message + "; repair the reported controller state, then retry the exact original replacement with `" + retry.String() + "`"
}

func arbiterRetirementResetRefusal(err error, invocation resolvedInvocation) error {
	retry, retryErr := invocation.retry(retryIntent{})
	if retryErr != nil {
		return fmt.Errorf("%w; Bootwright will not start while a prior retirement result could be mistaken for current authorization evidence, and it cannot safely construct the exact retry: %v", err, retryErr)
	}
	return fmt.Errorf("%w; Bootwright will not start while a prior retirement result could be mistaken for current authorization evidence. Repair the reported controller path, then retry the exact original replacement with `%s`", err, retry.String())
}

func arbiterPrepareDriftRefusal(err error, applyInvocation, replaceInvocation resolvedInvocation) error {
	prepared := applyModePreflightRefusal(err, applyInvocation)
	retry, retryErr := replaceInvocation.retry(retryIntent{})
	if retryErr != nil {
		return fmt.Errorf("%w; replace-arbiter converges the replacement arbiter's machine through the deps stage before it moves the vote, so it refuses the same drift apply refuses rather than converging it unasked; Bootwright cannot safely construct the exact original retry: %v", prepared, retryErr)
	}
	return fmt.Errorf("%w; replace-arbiter converges the replacement arbiter's machine through the deps stage before it moves the vote, so it refuses the same drift apply refuses rather than converging it unasked; resolve that drift as the exact continuation above directs, then retry the original replacement with `%s`", prepared, retry.String())
}

func arbiterPrepareReleasedSubstrateRefusal(runsDir string, tasks []workflow.ApplyTask, invocation resolvedInvocation) error {
	released, err := workflow.ConsumableSubstrateReleases(runsDir, tasks)
	if err != nil {
		retry, retryErr := invocation.retry(retryIntent{})
		if retryErr != nil {
			return fmt.Errorf("read the substrate-release record(s) that authorize a reinstall: %w; repair or remove the reported record; Bootwright cannot safely construct the exact original retry: %v", err, retryErr)
		}
		return fmt.Errorf("read the substrate-release record(s) that authorize a reinstall: %w; repair or remove the reported record, then retry the exact original replacement with `%s`", err, retry.String())
	}
	names := workflow.SubstrateReleaseClusterNames(released)
	if len(names) == 0 {
		return nil
	}
	apply, applyErr := invocation.applyClustersRetry(names, authorizeDataLoss)
	if applyErr != nil {
		return fmt.Errorf("refusing to prepare the replacement arbiter: a previous destroy released the substrate of %s, so converging it here would re-create machine(s) and wipe their disks; Bootwright cannot construct the exact authorized apply prerequisite: %v", strings.Join(names, ", "), applyErr)
	}
	retry, retryErr := invocation.retry(retryIntent{})
	if retryErr != nil {
		return fmt.Errorf("refusing to prepare the replacement arbiter: a previous destroy released the substrate of %s, so converging it here would re-create machine(s) and wipe their disks; reinstall deliberately with `%s`; Bootwright cannot safely construct the exact original retry: %v", strings.Join(names, ", "), apply.String(), retryErr)
	}
	return fmt.Errorf("refusing to prepare the replacement arbiter: a previous destroy released the substrate of %s, so converging it here would re-create machine(s) and wipe their disks — data loss replace-arbiter has no token to authorize. Carry out that reinstall deliberately with `%s`, then retry the exact original replacement with `%s`", strings.Join(names, ", "), apply.String(), retry.String())
}

func replaceArbiterRequiredAuthorizations(auth *authorizations, sameSite bool, degraded string, sameSiteReason string, retiresHost bool) []requiredAuthorization {
	forecast := newAuthorizationForecast(auth)
	forecast.consult(authorizeSameSiteArbiter, sameSite, sameSiteReason)
	forecast.consult(authorizeDegradedQuorum, degraded != "", degraded)
	forecast.mayConsult(authorizeUnreachableNodes, retiresHost, "retiring the replaced arbiter needs its host; the offline path is taken only once `ceph orch host ls` reports that host offline, which is decided on the cluster, so a preview cannot settle it")
	return forecast.list()
}
