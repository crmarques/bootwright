package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func closeMutatingRunLease(runErr error, lease *workflow.CommandRunLease) error {
	if lease == nil {
		return runErr
	}
	closeErr := lease.Close()
	if closeErr == nil {
		return runErr
	}
	if runErr == nil {
		return failErr(1, closeErr)
	}
	var exited *exitError
	if errors.As(runErr, &exited) {
		return failErr(exited.code, fmt.Errorf("%w; additionally failed to close the mutating run transaction: %v", runErr, closeErr))
	}
	return fmt.Errorf("%w; additionally failed to close the mutating run transaction: %v", runErr, closeErr)
}

func checkCurrentApplyBeforeMutation(runsDir string) error {
	return converge.CheckCurrentApplyActive(runsDir)
}

func mutatingRunLeaseRefusal(err error, invocation resolvedInvocation) error {
	command, retryErr := invocation.retry(retryIntent{})
	if retryErr != nil {
		return fmt.Errorf("%w; additionally could not construct the exact retry: %v", err, retryErr)
	}
	return fmt.Errorf("%w; after the active run finishes, or after following the stale/corrupt lease recovery above, re-run `%s`", err, command.String())
}

func reconcileCurrentApplyBeforeMutation(stdout io.Writer, runsDir string) error {
	cancelled, err := converge.ReconcileCurrentApplyBeforeMutation(runsDir)
	if err != nil {
		return err
	}
	if cancelled != nil {
		msg := fmt.Sprintf("marked %s cancelled: %s", cancelled.RunID, cancelled.Detail)
		if len(cancelled.InFlight) > 0 {
			msg += fmt.Sprintf("; it stopped while running %s", strings.Join(cancelled.InFlight, ", "))
		}
		msg += "; this apply resumes from recorded progress"
		cliout.NewContinuation(stdout).Warning("stale apply", msg)
	}
	return nil
}
