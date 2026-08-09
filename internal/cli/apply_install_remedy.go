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
	case workflow.ClusterInstallRemedyRegenerateISO:
		regenerate, commandErr := invocation.regenerateClusterISORetry(remedy.Cluster)
		if commandErr != nil {
			return fmt.Errorf("%w; cannot construct the exact ISO-regeneration command: %v", err, commandErr)
		}
		resume, commandErr := invocation.retry(retryIntent{})
		if commandErr != nil {
			return fmt.Errorf("%w; regenerate the ISO with `%s`; cannot construct the exact command that resumes this work: %v", err, regenerate.String(), commandErr)
		}
		return fmt.Errorf("%w; regenerate only this cluster's agent ISO with `%s`, then resume the original selected work with `%s`", err, regenerate.String(), resume.String())
	case workflow.ClusterInstallRemedyDestroyAndReapply:
		destroy, commandErr := invocation.destroyIncompleteClusterRetry(remedy.Cluster)
		if commandErr != nil {
			return fmt.Errorf("%w; cannot construct the exact cluster destroy command: %v", err, commandErr)
		}
		reapply, commandErr := invocation.reapplyDestroyedClusterRetry(remedy.Cluster)
		if commandErr != nil {
			return fmt.Errorf("%w; destroy the incomplete cluster with `%s`; cannot construct the exact reapply command: %v", err, destroy.String(), commandErr)
		}
		return fmt.Errorf("%w; deliberately reset only this cluster's incomplete install with `%s`, then reinstall it with `%s`", err, destroy.String(), reapply.String())
	case workflow.ClusterInstallRemedyFutureRebuild:
		command, commandErr := invocation.rebuildInstalledClusterRetry(remedy.Cluster)
		if commandErr != nil {
			return fmt.Errorf("%w; cannot construct the exact cluster rebuild command: %v", err, commandErr)
		}
		return fmt.Errorf("%w; rebuild only this installed cluster with `%s`", err, command.String())
	default:
		return err
	}
}

func hasApplyInstallRemedy(err error) bool {
	var remedial workflow.ClusterInstallRemedialError
	return errors.As(err, &remedial)
}
