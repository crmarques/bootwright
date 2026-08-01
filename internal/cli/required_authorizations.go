package cli

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

const (
	authorizationRequired  = "required"
	authorizationSatisfied = "satisfied"
	authorizationMaybe     = "may be required"
)

type requiredAuthorization struct {
	Token  string `json:"token"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}

type authorizationForecast struct {
	auth    *authorizations
	entries []requiredAuthorization
}

func newAuthorizationForecast(auth *authorizations) *authorizationForecast {
	return &authorizationForecast{auth: auth}
}

func (f *authorizationForecast) consult(token string, reached bool, reason string) {
	f.record(token, reached, authorizationRequired, reason)
}

func (f *authorizationForecast) mayConsult(token string, reached bool, reason string) {
	f.record(token, reached, authorizationMaybe, reason)
}

func (f *authorizationForecast) record(token string, reached bool, status, reason string) {
	if f == nil || !reached {
		return
	}
	if f.auth.has(token) {
		status = authorizationSatisfied
	}
	f.entries = append(f.entries, requiredAuthorization{Token: token, Status: status, Reason: reason})
}

func (f *authorizationForecast) list() []requiredAuthorization {
	if f == nil {
		return nil
	}
	return f.entries
}

func printRequiredAuthorizations(stdout io.Writer, entries []requiredAuthorization) {
	p := output.NewContinuation(stdout)
	p.Section("Required authorizations")
	if len(entries) == 0 {
		p.Status(output.StatusOK, "none", "this preview reaches no gate that an --authorize token of this verb could clear")
		return
	}
	lines := make([]output.TaskLine, 0, len(entries))
	for _, entry := range entries {
		lines = append(lines, output.TaskLine{
			Status: authorizationStatusLabel(entry.Status),
			Label:  "--authorize " + entry.Token,
			Detail: entry.Status + " — " + entry.Reason,
		})
	}
	p.Tasks(lines)
	p.Status(output.StatusInfo, "preview", "a real run consults these gates; a token listed as "+authorizationMaybe+" is decided on the host and cannot be settled without contacting it")
}

func authorizationStatusLabel(status string) output.Status {
	switch status {
	case authorizationSatisfied:
		return output.StatusOK
	case authorizationMaybe:
		return output.StatusWarn
	default:
		return output.StatusMissing
	}
}

type dryRunReportWithAuthorizations struct {
	converge.DryRunReport
	RequiredAuthorizations []requiredAuthorization `json:"requiredAuthorizations"`
}

func runScopeDryRunJSONAuthorized(cmd *cobra.Command, stdout io.Writer, cf *commonFlags, flags scopeCommonFlags, scope converge.Scope, action string, state v1alpha1.State, selected []converge.Phase, playbook string, limit string, extraVarPairs []string, artifactsBaseName string, askBecomePass bool, resolveInstaller bool, limits workflow.ConcurrencyLimits, tasks []workflow.ApplyTask, destroySafety *converge.DryRunDestroySafety, transitions *converge.DryRunTransitions, forks int, required []requiredAuthorization) error {
	if len(required) == 0 {
		return runScopeDryRunJSON(cmd, stdout, cf, flags, scope, action, state, selected, playbook, limit, extraVarPairs, artifactsBaseName, false, askBecomePass, resolveInstaller, limits, tasks, destroySafety, transitions, forks)
	}
	bundleDir, err := resolveBundleDir()
	if err != nil {
		return failErr(1, err)
	}
	report, err := converge.BuildScopeDryRunReport(cmd.Context(), cf.ctx, flags.executable, bundleDir, scope, action, state, selected, playbook, limit, extraVarPairs, artifactsBaseName, false, askBecomePass, resolveInstaller, limits, tasks, destroySafety, transitions, forks)
	if err != nil {
		return failErr(1, err)
	}
	return output.JSON(stdout, dryRunReportWithAuthorizations{DryRunReport: report, RequiredAuthorizations: required})
}

func runDestroyDryRunJSON(cmd *cobra.Command, stdout io.Writer, cf *commonFlags, flags scopeCommonFlags, scope converge.Scope, plan converge.WorkflowPlan, playbook, artifactsBaseName string, artifactServerOnly bool, destroySafety *converge.DryRunDestroySafety, required []requiredAuthorization) error {
	if artifactServerOnly {
		return runScopeDryRunJSONAuthorized(cmd, stdout, cf, flags, scope, "destroy", plan.State, plan.Selected, playbook, plan.Limit, plan.ExtraVarPairs, artifactsBaseName, plan.AskBecomePass, false, workflow.ConcurrencyLimits{}, nil, destroySafety, nil, 0, required)
	}
	tasks, err := workflow.PlanDestroyTasks(scope.Name, plan.State, plan.Limit, plan.ExtraVarPairs, plan.StorageWorkNames)
	if err != nil {
		return failErr(1, err)
	}
	return runFullDestroyDryRunJSONAuthorized(stdout, cf, scope, plan, tasks, destroySafety, required)
}

func runFullDestroyDryRunJSONAuthorized(stdout io.Writer, cf *commonFlags, scope converge.Scope, plan converge.WorkflowPlan, tasks []workflow.ApplyTask, destroySafety *converge.DryRunDestroySafety, required []requiredAuthorization) error {
	if len(required) == 0 {
		return runFullDestroyDryRunJSON(stdout, cf, scope, plan, tasks, destroySafety)
	}
	bundleDir, err := resolveBundleDir()
	if err != nil {
		return failErr(1, err)
	}
	report, err := converge.BuildFullDestroyDryRunReport(cf.ctx, bundleDir, scope, plan.State, plan.Selected, tasks, plan.ExtraVarPairs, destroySafety)
	if err != nil {
		return failErr(1, err)
	}
	return output.JSON(stdout, dryRunReportWithAuthorizations{DryRunReport: report, RequiredAuthorizations: required})
}
