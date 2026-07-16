package cli

import (
	"fmt"
	"io"

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
		cliout.NewContinuation(stdout).Warning("stale apply", fmt.Sprintf("marked %s cancelled: %s", cancelled.RunID, cancelled.Detail))
	}
	return nil
}
