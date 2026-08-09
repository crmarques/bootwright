package cli

import (
	"errors"
	"fmt"

	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func applyInstallRemedialError(err error, invocation resolvedInvocation) error {
	var remedial workflow.ClusterInstallRemedialError
	if !errors.As(err, &remedial) {
		return err
	}
	remedy := remedial.ClusterInstallRemedy()
	switch remedy.Action {
	case workflow.ClusterInstallRemedyReconcile:
		command, commandErr := invocation.retry(retryIntent{mode: workflow.ApplyModeReconcile})
		if commandErr != nil {
			return fmt.Errorf("%w; cannot construct the exact reconcile command: %v", err, commandErr)
		}
		return fmt.Errorf("%w; re-run `%s` to reconcile exactly this selected work set", err, command.String())
	default:
		return err
	}
}
