package cli

import (
	"errors"
	"fmt"
)

func finishHostSharedServiceOperations(runErr error, finalize func() error) error {
	if finalize == nil {
		return runErr
	}
	finalizeErr := finalize()
	if finalizeErr == nil {
		return runErr
	}
	if runErr == nil {
		return failErr(1, finalizeErr)
	}
	var exited *exitError
	if errors.As(runErr, &exited) {
		return failErr(exited.code, fmt.Errorf("%w; additionally failed to finalize the exact host-wide shared-service operation: %v", runErr, finalizeErr))
	}
	return fmt.Errorf("%w; additionally failed to finalize the exact host-wide shared-service operation: %v", runErr, finalizeErr)
}

func hostSharedServiceFinalizationRefusal(err error, invocation resolvedInvocation) error {
	retry, retryErr := invocation.retry(retryIntent{})
	if retryErr != nil {
		return fmt.Errorf("%w; the host-wide operation guard was retained because finalization was not proven, and the exact retry could not be constructed: %v", err, retryErr)
	}
	return fmt.Errorf("%w; the host-wide operation guard was retained because finalization was not proven. Restore access to every reported host, inspect the exact operation and durable transition evidence, then re-run `%s`; never remove another live controller's guard", err, retry.String())
}
