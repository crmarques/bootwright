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

func reportArbiterConvergeRecords(stdout io.Writer, cluster string, carried []string, err error) {
	if err != nil {
		cliout.NewContinuation(stdout).Warning("converge records", err.Error()+"; the recorded desired state of StorageCluster/"+cluster+" still names the previous tiebreaker, so the next `bootwright apply` refuses on drift this run created — re-run `"+workflow.TiebreakerReplacementCommand(cluster)+"` once the records are writable")
		return
	}
	if len(carried) == 0 {
		return
	}
	cliout.NewContinuation(stdout).Warning("converge records", "left "+strings.Join(carried, ", ")+" recorded as drifted: their recorded desired state already differed from this context before the arbiter moved, and this run converged only the tiebreaker; run `bootwright apply --stage clusters --clusters "+cluster+"` to converge the rest")
}

func reportArbiterRetirement(stdout io.Writer, auth *authorizations, runsDir string) {
	result, found, err := converge.ReadArbiterRetirement(runsDir)
	if err != nil {
		cliout.NewContinuation(stdout).Warning("arbiter retirement", err.Error())
		return
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

func arbiterPrepareDriftRefusal(cluster string, err error) error {
	return fmt.Errorf("%w. `bootwright storage-cluster replace-arbiter` converges the replacement arbiter's machine through the deps stage before it moves the vote, so it refuses the drift `bootwright apply` refuses rather than converging it unasked; resolve that drift as the refusal above directs, then re-run `%s`", err, workflow.TiebreakerReplacementCommand(cluster))
}

func arbiterPrepareReleasedSubstrateRefusal(runsDir, cluster string, tasks []workflow.ApplyTask) error {
	released, err := workflow.ConsumableSubstrateReleases(runsDir, tasks)
	if err != nil {
		return fmt.Errorf("read the substrate-release record(s) that authorize a reinstall: %w; repair or remove the reported record and re-run", err)
	}
	names := workflow.SubstrateReleaseClusterNames(released)
	if len(names) == 0 {
		return nil
	}
	return fmt.Errorf("refusing to prepare the replacement arbiter: a previous `bootwright destroy` released the substrate of %s, so converging it here would re-create machine(s) and wipe their disks — data loss `storage-cluster replace-arbiter` has no token to authorize. Carry out that reinstall deliberately with `bootwright apply --clusters %s --authorize %s`, then re-run `%s`",
		strings.Join(names, ", "), strings.Join(names, ","), authorizeDataLoss, workflow.TiebreakerReplacementCommand(cluster))
}

func replaceArbiterRequiredAuthorizations(auth *authorizations, sameSite bool, degraded string, sameSiteReason string, retiresHost bool) []requiredAuthorization {
	forecast := newAuthorizationForecast(auth)
	forecast.consult(authorizeSameSiteArbiter, sameSite, sameSiteReason)
	forecast.consult(authorizeDegradedQuorum, degraded != "", degraded)
	forecast.mayConsult(authorizeUnreachableNodes, retiresHost, "retiring the replaced arbiter needs its host; the offline path is taken only once `ceph orch host ls` reports that host offline, which is decided on the cluster, so a preview cannot settle it")
	return forecast.list()
}
