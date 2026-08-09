package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func applyModePreflightRefusal(err error, invocation resolvedInvocation) error {
	if hasApplyInstallRemedy(err) {
		return applyInstallRemedialError(err, invocation)
	}
	var refusal *workflow.ApplyModePreflightRefusal
	if !errors.As(err, &refusal) {
		return err
	}
	if clusters := refusal.ReplacementArbiterClusters(); len(clusters) > 0 {
		commands := make([]string, 0, len(clusters))
		for _, cluster := range clusters {
			command, commandErr := invocation.replaceArbiterRetry(cluster)
			if commandErr != nil {
				return fmt.Errorf("%w; cannot construct the sanctioned replace-arbiter command: %v", err, commandErr)
			}
			commands = append(commands, "`"+command.String()+"`")
		}
		return fmt.Errorf("%w; move the authored tiebreaker with %s", err, strings.Join(commands, ", "))
	}
	mode, retryable := refusal.RetryMode()
	if !retryable {
		return err
	}
	intent := retryIntent{mode: mode}
	if refusal.RequiresDataLossAuthorization() {
		intent.requiredAuthorizations = []string{authorizeDataLoss}
	}
	command, retryErr := invocation.retry(intent)
	if retryErr != nil {
		return fmt.Errorf("%w; cannot construct the sanctioned retry: %v", err, retryErr)
	}
	if mode == workflow.ApplyModeReconcile {
		return fmt.Errorf("%w; re-run `%s` to reconcile exactly this selected work set", err, command.String())
	}
	return fmt.Errorf("%w; after resolving any foreign ownership named above, re-run `%s` to rebuild exactly this selected work set", err, command.String())
}
