package cli

import (
	"fmt"
	"io"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/preflight"
)

func runScopeHostCheck(stdout io.Writer, stderr io.Writer, state v1alpha1.State, selected []converge.Phase, contextName, secretsDir, clustersDir string, hostTrustScope map[string]bool, secretScope *preflight.SecretScope) error {
	// Standalone preflight/check has no planned task graph; an unscoped run
	// passes nil scopes and checks every managed-trust machine and every
	// declared object's secrets. A --clusters-scoped preflight passes the
	// selection's work objects so it mirrors the scoped apply: it does not fail
	// closed on trust/secrets for out-of-scope hosts and render-reference
	// objects it will never act on.
	return runApplyHostCheck(stdout, stderr, state, selected, contextName, secretsDir, clustersDir, hostTrustScope, secretScope)
}

func runApplyHostCheck(stdout io.Writer, _ io.Writer, state v1alpha1.State, selected []converge.Phase, contextName, secretsDir, clustersDir string, hostTrustScope map[string]bool, secretScope *preflight.SecretScope) error {
	checks := preflight.CollectChecks(state, preflightPhases(selected), true, contextName, secretsDir, clustersDir, preflight.DefaultDeps, hostTrustScope, secretScope)
	return renderCheckResults(stdout, "host check", preflightChecksToOutput(checks))
}

func renderCheckResults(stdout io.Writer, label string, checks []preflightCheck) error {
	p := output.NewContinuation(stdout)
	p.Checks(checks)
	failed := failedCheckCount(checks)
	if failed > 0 {
		p.Summary(output.StatusFail, label, checkSummary(len(checks), failed))
		return failf(1, "%s failed: %d required check(s) failed", label, failed)
	}
	p.Summary(output.StatusOK, label, checkSummary(len(checks), failed))
	return nil
}

func failedCheckCount(checks []preflightCheck) int {
	failed := 0
	for _, check := range checks {
		if check.Status == output.StatusFail {
			failed++
		}
	}
	return failed
}

func checkSummary(total, failed int) string {
	if failed == 0 {
		return fmt.Sprintf("all %d check(s) passed", total)
	}
	return fmt.Sprintf("%d of %d check(s) failed", failed, total)
}
