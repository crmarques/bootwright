package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge"
)

func applyRenameOrphanRefusal(err error, invocation resolvedInvocation) error {
	var refusal *converge.ApplyRenameOrphanError
	if !errors.As(err, &refusal) {
		return err
	}
	destroyCommand, commandErr := invocation.destroyClustersRetry(refusal.Undeclared)
	if commandErr != nil {
		return fmt.Errorf("%w; cannot construct the exact teardown command: %v", err, commandErr)
	}
	applyCommand, commandErr := invocation.retry(retryIntent{})
	if commandErr != nil {
		return fmt.Errorf("%w; cannot construct the exact reapply command: %v", err, commandErr)
	}
	document := "cluster YAML"
	if refusal.Kind == v1alpha1.KindStorageCluster {
		document = "StorageCluster YAML"
	}
	return fmt.Errorf("%w. To rename, restore the old metadata.name. To replace, temporarily restore the old %s (metadata.name %s), run `%s`, then remove that YAML and re-run `%s` — destroy resolves --clusters against the declared state, so the old cluster can only be torn down while its YAML is present; to keep both, leave the old cluster declared", err, document, strings.Join(refusal.Undeclared, ", "), destroyCommand.String(), applyCommand.String())
}
