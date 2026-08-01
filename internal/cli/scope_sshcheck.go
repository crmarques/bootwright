package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/preflight"
)

func runScopeHostCheck(stdout io.Writer, stderr io.Writer, state v1alpha1.State, selected []converge.Phase, contextName, secretsDir, clustersDir, runsDir string, hostTrustScope map[string]bool, secretScope *preflight.SecretScope) error {
	return runApplyHostCheck(stdout, stderr, state, selected, contextName, secretsDir, clustersDir, runsDir, hostTrustScope, secretScope)
}

func runApplyHostCheck(stdout io.Writer, _ io.Writer, state v1alpha1.State, selected []converge.Phase, contextName, secretsDir, clustersDir, runsDir string, hostTrustScope map[string]bool, secretScope *preflight.SecretScope) error {
	checks, err := collectHostChecks(state, selected, contextName, secretsDir, clustersDir, runsDir, hostTrustScope, secretScope)
	if err != nil {
		return err
	}
	return renderCheckResults(stdout, "machine check", checks)
}

func collectHostChecks(state v1alpha1.State, selected []converge.Phase, contextName, secretsDir, clustersDir, runsDir string, hostTrustScope map[string]bool, secretScope *preflight.SecretScope) (checks []preflightCheck, err error) {
	runtimeDir, err := os.MkdirTemp(runsDir, "preflight-runtime-")
	if err != nil {
		return nil, fmt.Errorf("create preflight runtime directory: %w", err)
	}
	defer func() {
		if cleanupErr := os.RemoveAll(runtimeDir); cleanupErr != nil {
			err = errors.Join(err, fmt.Errorf("remove preflight runtime directory %s: %w", runtimeDir, cleanupErr))
		}
	}()
	collected := preflight.CollectChecks(state, preflightPhases(selected), true, contextName, secretsDir, clustersDir, runtimeDir, preflight.DefaultDeps, hostTrustScope, secretScope)
	return preflightChecksToOutput(collected), nil
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
