package cli

import (
	"fmt"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/clusteraccess"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/ownership"
	"github.com/crmarques/bootwright/internal/state/graph"
	"github.com/crmarques/bootwright/internal/workspace"
)

func unreadableOwnershipRefusal(ctx workspace.Context, skipped []error) error {
	details := make([]string, 0, len(skipped))
	for _, warning := range skipped {
		details = append(details, warning.Error())
	}
	return fmt.Errorf("%d ownership record(s) under %s could not be read and their resources would be silently left standing: %s; fix or remove the corrupted record file(s), or re-run `bootwright destroy --authorize %s` to destroy the rest without them", len(skipped), ctx.OwnershipDir, strings.Join(details, "; "), authorizeUnreadableRecords)
}

func destroyStorageConsumerGate(auth *authorizations, state v1alpha1.State, sel clusteraccess.Selection, runScope converge.Scope) (bool, string, error) {
	if !sel.Active || len(sel.StorageRoots) == 0 {
		return false, "", nil
	}
	conflicts := stategraph.StorageConsumerDestroyConflicts(state, sel.StorageRoots, sel.ContainerRoots)
	if len(conflicts) == 0 {
		return false, "", nil
	}
	if runScope.Name != "infra" {
		return false, "", clusteraccess.FormatStorageConsumerConflicts(conflicts)
	}
	if !auth.allows(authorizeSharedInfra) {
		return true, "", fmt.Errorf("%w; this infra-stage teardown destroys the storage cluster's machine substrate, losing its OSD data; destroy the consuming cluster(s) first, or re-run with --authorize %s to proceed anyway", clusteraccess.FormatStorageConsumerConflicts(conflicts), authorizeSharedInfra)
	}
	return true, clusteraccess.FormatStorageConsumerConflicts(conflicts).Error() + "; proceeding because --authorize " + authorizeSharedInfra + " was supplied", nil
}

func destroyInfraComponentGate(auth *authorizations, contextName string, refs []converge.InfraComponentServiceRef, records []ownership.ResourceRecord, artifactServerOnly bool) (converge.InfraComponentDestroyDecision, bool, error) {
	decision, blocksErr := converge.PlanInfraComponentDestroyBlocks(contextName, refs, records, artifactServerOnly)
	reached := false
	if blocksErr != nil {
		reached = true
		if !auth.allows(authorizeSharedInfra) {
			return decision, reached, fmt.Errorf("cannot verify whether shared services are owned or referenced by other contexts: %w; resolve the contexts directory or re-run with --authorize %s to tear them down regardless", blocksErr, authorizeSharedInfra)
		}
	}
	if err := converge.InfraComponentDestroyBlockError(decision.Blocks); err != nil {
		reached = true
		if !auth.allows(authorizeSharedInfra) {
			return decision, reached, err
		}
	}
	return decision, reached, nil
}
