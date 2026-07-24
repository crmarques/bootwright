package cli

import (
	"fmt"
	"io"
	"strings"

	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge"
)

func checkCurrentApplyBeforeMutation(runsDir string) error {
	return converge.CheckCurrentApplyActive(runsDir)
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
