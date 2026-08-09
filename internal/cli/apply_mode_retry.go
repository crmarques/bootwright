package cli

import (
	"errors"
	"fmt"

	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func applyModePreflightRefusal(err error, invocation resolvedInvocation) error {
	var refusal *workflow.ApplyModePreflightRefusal
	if !errors.As(err, &refusal) {
		return err
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
