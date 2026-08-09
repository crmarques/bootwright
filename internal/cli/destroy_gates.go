package cli

import (
	"fmt"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/clusteraccess"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/ownership"
	"github.com/crmarques/bootwright/internal/state/graph"
	"github.com/crmarques/bootwright/internal/workspace"
)

func unreadableOwnershipRefusal(ctx workspace.Context, skipped []error, invocation resolvedInvocation) error {
	details := make([]string, 0, len(skipped))
	for _, warning := range skipped {
		details = append(details, warning.Error())
	}
	command, err := invocation.retry(retryIntent{requiredAuthorizations: []string{authorizeUnreadableRecords}})
	if err != nil {
		return err
	}
	return fmt.Errorf("%d ownership record(s) under %s could not be read and their resources would be silently left standing: %s; fix or remove the corrupted record file(s), or re-run `%s` to destroy the same selected work set without them", len(skipped), ctx.OwnershipDir, strings.Join(details, "; "), command.String())
}

func destroyDryRunDisclosure(safety workflow.DestroySafetyDecision, inputSkipped, ownershipSkipped []error, decision converge.InfraComponentDestroyDecision, auth *authorizations, purgeHistory bool) dryRunDisclosure {
	out := dryRunDisclosure{refusals: destroyGateForecastRefusals(safety, inputSkipped, ownershipSkipped, decision, auth)}
	if purgeHistory {
		out.purgeHistory = &converge.DryRunPurgeHistory{Scope: destroyPurgeHistoryNotice(true)}
	}
	return out
}

func destroyGateForecastRefusals(safety workflow.DestroySafetyDecision, inputSkipped, ownershipSkipped []error, decision converge.InfraComponentDestroyDecision, auth *authorizations) []string {
	var out []string
	if safety.RequiresAuthorization {
		out = append(out, safety.Summary())
	}
	if len(inputSkipped) > 0 && !auth.has(authorizeStaleInput) {
		out = append(out, fmt.Sprintf("%d input document(s) this build cannot read would be skipped", len(inputSkipped)))
	}
	if len(ownershipSkipped) > 0 && !auth.has(authorizeUnreadableRecords) {
		out = append(out, fmt.Sprintf("%d ownership record(s) could not be read and their resources would be left standing", len(ownershipSkipped)))
	}
	if err := converge.InfraComponentDestroyBlockError(decision.Blocks); err != nil && !auth.has(authorizeSharedInfra) {
		out = append(out, err.Error())
	}
	if err := converge.InfraComponentDestroyScanWarningError(decision.Warnings); err != nil && !auth.has(authorizeSharedInfra) {
		out = append(out, err.Error())
	}
	return out
}

func destroyScopeConflictGates(state v1alpha1.State, sel clusteraccess.Selection, runScope converge.Scope, fullDestroy bool, runsDir, clustersDir string, ownershipRecords []ownership.ResourceRecord, invocation resolvedInvocation) error {
	if (runScope.Name == "infra" || fullDestroy) && sel.Active && !sel.MachineSelection {
		conflicts := stategraph.SharedDestroyConflicts(state, sel.AllRoots)
		if len(conflicts) > 0 {
			if standingTasks, perr := workflow.PlanApplyTasksChecked(converge.AllScope.ApplyTarget(), state); perr == nil {
				conflicts = workflow.StandingDestroyScopeConflicts(runsDir, clustersDir, state, ownershipRecords, standingTasks, conflicts)
			}
		}
		if len(conflicts) > 0 {
			return clusteraccess.FormatDestroyScopeConflicts(conflicts, "--clusters")
		}
	}
	if sel.Active && len(sel.AllRoots) > 0 {
		if conflicts := converge.KubeVirtTenantDestroyConflicts(state, clustersDir, sel.AllRoots, converge.ProvisionedStorageTenants(ownershipRecords)); len(conflicts) > 0 {
			command, err := invocation.destroyClustersRetry(converge.KubeVirtConflictTenants(conflicts))
			if err != nil {
				return err
			}
			return converge.FormatKubeVirtTenantConflicts(conflicts, command.String())
		}
	}
	return nil
}

func destroyStorageConsumerGate(auth *authorizations, state v1alpha1.State, sel clusteraccess.Selection, runScope converge.Scope, dryRun bool, invocation resolvedInvocation) (bool, string, error) {
	if !sel.Active || len(sel.StorageRoots) == 0 {
		return false, "", nil
	}
	conflicts := stategraph.StorageConsumerDestroyConflicts(state, sel.StorageRoots, sel.ContainerRoots)
	if len(conflicts) == 0 {
		return false, "", nil
	}
	if converge.ScopeTearsClusterLayer(runScope) {
		command, err := invocation.destroyClustersRetry(clusteraccess.StorageConflictConsumers(conflicts))
		if err != nil {
			return false, "", err
		}
		return false, "", clusteraccess.FormatStorageConsumerConflicts(conflicts, command.String())
	}
	if !converge.ScopeTearsMachineLayer(runScope) {
		return false, "", nil
	}
	if !auth.allows(authorizeSharedInfra) {
		if dryRun {
			return true, "", nil
		}
		command, retryErr := invocation.retry(retryIntent{requiredAuthorizations: []string{authorizeSharedInfra}})
		if retryErr != nil {
			return true, "", retryErr
		}
		return true, "", fmt.Errorf("%w; this machine-layer teardown destroys the storage cluster's machine substrate, losing its OSD data; re-run `%s` to proceed with the same selected work set anyway", clusteraccess.FormatStorageConsumerConflicts(conflicts, ""), command.String())
	}
	return true, clusteraccess.FormatStorageConsumerConflicts(conflicts, "").Error() + "; proceeding because --authorize " + authorizeSharedInfra + " was supplied", nil
}

func destroyInfraComponentGate(auth *authorizations, contextName string, refs []converge.InfraComponentServiceRef, records []ownership.ResourceRecord, artifactServerOnly, dryRun bool, invocation resolvedInvocation) (converge.InfraComponentDestroyDecision, bool, error) {
	decision, blocksErr := converge.PlanInfraComponentDestroyBlocks(contextName, refs, records, artifactServerOnly)
	reached := false
	if blocksErr != nil {
		decision.Warnings = append(decision.Warnings, fmt.Errorf("scan sibling contexts: %w", blocksErr))
	}
	if warningErr := converge.InfraComponentDestroyScanWarningError(decision.Warnings); warningErr != nil {
		reached = true
		if !auth.allows(authorizeSharedInfra) && !dryRun {
			command, retryErr := invocation.retry(retryIntent{requiredAuthorizations: []string{authorizeSharedInfra}})
			if retryErr != nil {
				return decision, reached, retryErr
			}
			return decision, reached, fmt.Errorf("%w; repair the reported context or ownership evidence after verifying the live service, or re-run `%s` to tear down the same selected work set despite the missing proof", warningErr, command.String())
		}
	}
	if err := converge.InfraComponentDestroyBlockError(decision.Blocks); err != nil {
		reached = true
		if !auth.allows(authorizeSharedInfra) && !dryRun {
			command, retryErr := invocation.retry(retryIntent{requiredAuthorizations: []string{authorizeSharedInfra}})
			if retryErr != nil {
				return decision, reached, retryErr
			}
			return decision, reached, fmt.Errorf("%w; re-run `%s` to tear down the same selected work set regardless", err, command.String())
		}
	}
	return decision, reached, nil
}
