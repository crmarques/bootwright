package cli

import (
	"fmt"
	"io"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/cli/output"
)

func runScopeHostCheck(stdout io.Writer, stderr io.Writer, state v1alpha1.State, selected []Phase, secretsDir, runtimeDir string) error {
	return runApplyHostCheck(stdout, stderr, state, selected, secretsDir, runtimeDir)
}

func runApplyHostCheck(stdout io.Writer, _ io.Writer, state v1alpha1.State, selected []Phase, secretsDir, runtimeDir string) error {
	checks := collectPreflightChecks(state, selected, true, secretsDir, runtimeDir, defaultPreflightDeps)
	return renderCheckResults(stdout, "host check", checks)
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
