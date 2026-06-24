package cli

import (
	"fmt"
	"io"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/preflight"
)

func runScopeHostCheck(stdout io.Writer, stderr io.Writer, state v1alpha1.State, selected []converge.Phase, contextName, secretsDir, clustersDir string) error {
	// Standalone preflight/check has no planned task graph and no --clusters
	// narrowing; check every managed-trust machine and every declared object's
	// secrets (nil scopes). The apply path narrows the host-trust scope to the
	// machines its tasks will SSH into and the secret scope to its work objects.
	return runApplyHostCheck(stdout, stderr, state, selected, contextName, secretsDir, clustersDir, nil, nil)
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
