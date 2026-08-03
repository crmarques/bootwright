package cli

import (
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

func replaceArbiterRequiredAuthorizations(auth *authorizations, sameSite bool, degraded string, sameSiteReason string) []requiredAuthorization {
	forecast := newAuthorizationForecast(auth)
	forecast.consult(authorizeSameSiteArbiter, sameSite, sameSiteReason)
	forecast.consult(authorizeDegradedQuorum, degraded != "", degraded)
	forecast.mayConsult(authorizeUnreachableNodes, true, "retiring the replaced arbiter needs its host; whether it proves unreachable is decided when the run contacts it, so a preview cannot settle it")
	return forecast.list()
}
