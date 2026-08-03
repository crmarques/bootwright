package cli

import (
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/api/v1alpha1"
	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/preflight"
	"github.com/crmarques/bootwright/internal/workspace"
)

const (
	preflightResultFail        = "fail"
	preflightResultAnsibleName = "Ansible preflight"
)

type preflightCheckResult struct {
	Name   string `json:"name"`
	Target string `json:"target,omitempty"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
	Impact string `json:"impact,omitempty"`
	Fix    string `json:"fix,omitempty"`
}

type preflightResultSummary struct {
	Total  int `json:"total"`
	Failed int `json:"failed"`
}

type preflightResultReport struct {
	Context string                 `json:"context"`
	Target  string                 `json:"target"`
	OK      bool                   `json:"ok"`
	Checks  []preflightCheckResult `json:"checks"`
	Summary preflightResultSummary `json:"summary"`
	Log     string                 `json:"log,omitempty"`
}

func registerScopePreflightFlags(cmd *cobra.Command, f *scopeCommonFlags, scope converge.Scope) {
	f.executable = workspace.ResolveAnsiblePlaybook()
	addOutputFlag(cmd, &f.output)
	if !scopeAllowsClusterScope(scope, false) {
		return
	}
	targetKind := scopeTargetKind(scope)
	cmd.Flags().StringVar(&f.clusterScope, "clusters", "", "comma-separated "+targetKind+" names to preflight (default: all)")
	registerClusterScopeCompletion(cmd, clusterKindForTargetLabel(targetKind))
}

func runScopePreflightJSON(cmd *cobra.Command, stdout io.Writer, cf *commonFlags, flags scopeCommonFlags, scope converge.Scope, state v1alpha1.State, limit string, verbose bool, hostTrustScope map[string]bool, secretScope *preflight.SecretScope) error {
	return runPreflightResultJSON(cmd, stdout, cf, flags, scope, state, limit, verbose, hostTrustScope, secretScope, nil)
}

func runAllPreflightJSON(cmd *cobra.Command, stdout io.Writer, cf *commonFlags, flags scopeCommonFlags, state v1alpha1.State, verbose bool) error {
	bastion := preflightCheckResults(preflightChecksToOutput(preflight.CollectBastionChecks(preflight.DefaultDeps)))
	return runPreflightResultJSON(cmd, stdout, cf, flags, converge.AllScope, state, converge.AllScope.AnsibleLimit, verbose, nil, nil, bastion)
}

func runPreflightResultJSON(cmd *cobra.Command, stdout io.Writer, cf *commonFlags, flags scopeCommonFlags, scope converge.Scope, state v1alpha1.State, limit string, verbose bool, hostTrustScope map[string]bool, secretScope *preflight.SecretScope, leading []preflightCheckResult) error {
	ctx := cf.ctx
	clustersDir := workspace.ControllerClustersDir(ctx.Name)
	checks, err := collectHostChecks(state, scope.Phases(), ctx.Name, ctx.SecretsDir, clustersDir, ctx.RunsDir, hostTrustScope, secretScope)
	if err != nil {
		return failErr(1, err)
	}
	results := append(leading, preflightCheckResults(checks)...)
	if preflightFailedResults(results) > 0 {
		return writePreflightResultJSON(stdout, ctx.Name, scope.Name, results, "")
	}
	bundle, err := prepareWorkflowBundle(false)
	if err != nil {
		return failErr(1, err)
	}
	logPath, runErr := converge.RunScopePreflight(cmd.Context(), io.Discard, io.Discard, ctx, clustersDir, flags.executable, bundle.Dir, scope, state, limit, false, verbose, false, newWorkflowReporter(io.Discard, "Run"))
	return writePreflightResultJSON(stdout, ctx.Name, scope.Name, append(results, ansiblePreflightResult(scope.Name, logPath, runErr)), logPath)
}

func writePreflightResultJSON(stdout io.Writer, contextName, target string, results []preflightCheckResult, logPath string) error {
	failed := preflightFailedResults(results)
	report := preflightResultReport{
		Context: contextName,
		Target:  target,
		OK:      failed == 0,
		Checks:  results,
		Summary: preflightResultSummary{Total: len(results), Failed: failed},
		Log:     logPath,
	}
	if err := cliout.JSON(stdout, report); err != nil {
		return failErr(1, err)
	}
	if failed > 0 {
		return silentExit(1)
	}
	return nil
}

func preflightCheckResults(checks []preflightCheck) []preflightCheckResult {
	results := make([]preflightCheckResult, 0, len(checks))
	for _, check := range checks {
		results = append(results, preflightCheckResult{
			Name:   check.Name,
			Target: check.Group,
			Status: preflightResultStatus(check.Status),
			Detail: check.Evidence,
			Impact: check.Impact,
			Fix:    check.Remediation,
		})
	}
	return results
}

func preflightResultStatus(status cliout.Status) string {
	switch status {
	case cliout.StatusFailed:
		return preflightResultFail
	case cliout.StatusSkipped:
		return strings.ToLower(string(cliout.StatusSkip))
	default:
		return strings.ToLower(string(status))
	}
}

func preflightFailedResults(results []preflightCheckResult) int {
	failed := 0
	for _, result := range results {
		if result.Status == preflightResultFail {
			failed++
		}
	}
	return failed
}

func ansiblePreflightResult(scopeName, logPath string, err error) preflightCheckResult {
	result := preflightCheckResult{
		Name:   scopeName + " preflight",
		Target: preflightResultAnsibleName,
		Status: strings.ToLower(string(cliout.StatusOK)),
		Detail: "every host check passed",
	}
	if err == nil {
		return result
	}
	result.Status = preflightResultFail
	result.Detail = err.Error()
	result.Impact = "the selected hosts are not proven ready to apply"
	result.Fix = "read the failing task in " + logPath + ", fix the host, and rerun this preflight"
	return result
}
