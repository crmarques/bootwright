package cli

import (
	"fmt"
	"strings"

	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func (i resolvedInvocation) destroySelectedMachineLayerRetry() (retryCommand, error) {
	return i.destroyMachineLayerRetryForRoots(nil)
}

func (i resolvedInvocation) destroyMachineLayerRetryForRoots(implicitRoots []string) (retryCommand, error) {
	return i.destroySelectedLayerRetry(converge.InfraScope.Name, implicitRoots, authorizeProtected)
}

func (i resolvedInvocation) destroySelectedClusterLayerRetry() (retryCommand, error) {
	return i.destroyClusterLayerRetryForRoots(nil)
}

func (i resolvedInvocation) destroyClusterLayerRetryForRoots(implicitRoots []string) (retryCommand, error) {
	return i.destroySelectedLayerRetry(converge.ClustersScope.Name, implicitRoots, authorizeProtected, authorizeDataLoss)
}

func (i resolvedInvocation) destroySelectedLayerRetry(stage string, implicitRoots []string, requiredAuthorizations ...string) (retryCommand, error) {
	next := i
	if !i.flags.selection.hasExplicitTargets() {
		roots := workflow.UnionClusterNames(implicitRoots)
		if len(roots) == 0 {
			return retryCommand{}, fmt.Errorf("protected-layer recovery requires typed desired-state cluster roots when the original selection is implicit")
		}
		next.flags.selection.clusters = strings.Join(roots, ",")
		next.flags.selection.machines = ""
	}
	next.verb = invocationDestroy
	next.flags.mode = ""
	next.flags.selection.stage = stage
	next.flags.selection.through = ""
	next.flags.reclaimDevices = ""
	next.flags.clusterInstallParallelism = 0
	next.flags.recoverCephOwnership = ""
	next.flags.purgeHistory = false
	next.flags.trustOnFirstUse = false
	next.flags.clusterName = ""
	next.flags.newArbiterMachine = ""
	next.flags.authorizations = authorizationsAcceptedByVerb(next.flags.authorizations, i.verb, invocationDestroy)
	return next.retry(retryIntent{requiredAuthorizations: requiredAuthorizations})
}
