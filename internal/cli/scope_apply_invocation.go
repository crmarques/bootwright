package cli

import "github.com/crmarques/bootwright/internal/converge/workflow"

func newScopeApplyInvocation(contextName string, mode workflow.ApplyMode, selection runSelection, reclaimDevices string, authorizations []string, dryRun bool, output string, yes, askBecomePass, trustOnFirstUse, verbose bool, clusterInstallParallelism int) (resolvedInvocation, error) {
	return newResolvedInvocation(invocationApply, contextName, invocationFlags{
		mode:                      mode,
		selection:                 selection,
		reclaimDevices:            reclaimDevices,
		authorizations:            authorizations,
		dryRun:                    dryRun,
		output:                    output,
		yes:                       yes,
		askBecomePass:             askBecomePass,
		trustOnFirstUse:           trustOnFirstUse,
		verbose:                   verbose,
		clusterInstallParallelism: clusterInstallParallelism,
	})
}
