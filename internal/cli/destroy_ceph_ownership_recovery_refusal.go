package cli

import (
	"errors"
	"fmt"

	"github.com/crmarques/bootwright/internal/converge"
)

func destroyCephOwnershipRecoveryRefusal(err error, ownershipDir string, invocation resolvedInvocation) error {
	var conflict *converge.CephOwnershipRecoveryConflictError
	if !errors.As(err, &conflict) {
		return err
	}
	command, commandErr := invocation.retry(retryIntent{})
	if commandErr != nil {
		return fmt.Errorf("%w; cannot construct the exact retry command: %v", err, commandErr)
	}
	return fmt.Errorf("%w; inspect the StorageCluster record under %s and compare its API version, kind, name, owner role, context, cluster, host, seedHost, and FSID with trusted live evidence. Restore the correct record or remove the contradictory record only after proving it is stale; no --authorize token bypasses contradictory ownership evidence. Then re-run `%s`", err, ownershipDir, command.String())
}
