package cli

import (
	"strings"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
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

func refreshArbiterConvergeRecords(ctx workspace.Context, state v1alpha1.State, cluster string) error {
	tasks, err := workflow.PlanApplyTasksChecked(converge.AllScope.ApplyTarget(), state)
	if err != nil {
		return err
	}
	return workflow.RefreshStorageClusterConvergeSafety(ctx.RunsDir, ctx.Name, "", cluster, tasks, time.Now().UTC())
}

func replaceArbiterRequiredAuthorizations(auth *authorizations, sameSite bool, degraded string, sameSiteReason string) []requiredAuthorization {
	forecast := newAuthorizationForecast(auth)
	forecast.consult(authorizeSameSiteArbiter, sameSite, sameSiteReason)
	forecast.consult(authorizeDegradedQuorum, degraded != "", degraded)
	forecast.mayConsult(authorizeUnreachableNodes, true, "retiring the replaced arbiter needs its host; whether it proves unreachable is decided when the run contacts it, so a preview cannot settle it")
	return forecast.list()
}
