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

func unreadableOwnershipRefusal(ctx workspace.Context, skipped []error) error {
	details := make([]string, 0, len(skipped))
	for _, warning := range skipped {
		details = append(details, warning.Error())
	}
	return fmt.Errorf("%d ownership record(s) under %s could not be read and their resources would be silently left standing: %s; fix or remove the corrupted record file(s), or re-run `bootwright destroy --authorize %s` to destroy the rest without them", len(skipped), ctx.OwnershipDir, strings.Join(details, "; "), authorizeUnreadableRecords)
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
	return out
}

func destroyScopeConflictGates(state v1alpha1.State, sel clusteraccess.Selection, runScope converge.Scope, fullDestroy bool, runsDir, clustersDir string, ownershipRecords []ownership.ResourceRecord) error {
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
			return converge.FormatKubeVirtTenantConflicts(conflicts)
		}
	}
	return nil
}

func destroyStorageConsumerGate(auth *authorizations, state v1alpha1.State, sel clusteraccess.Selection, runScope converge.Scope, dryRun bool) (bool, string, error) {
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
		if dryRun {
			return true, "", nil
		}
		return true, "", fmt.Errorf("%w; this infra-stage teardown destroys the storage cluster's machine substrate, losing its OSD data; destroy the consuming cluster(s) first, or re-run with --authorize %s to proceed anyway", clusteraccess.FormatStorageConsumerConflicts(conflicts), authorizeSharedInfra)
	}
	return true, clusteraccess.FormatStorageConsumerConflicts(conflicts).Error() + "; proceeding because --authorize " + authorizeSharedInfra + " was supplied", nil
}

func destroyInfraComponentGate(auth *authorizations, contextName string, refs []converge.InfraComponentServiceRef, records []ownership.ResourceRecord, artifactServerOnly, dryRun bool) (converge.InfraComponentDestroyDecision, bool, error) {
	decision, blocksErr := converge.PlanInfraComponentDestroyBlocks(contextName, refs, records, artifactServerOnly)
	reached := false
	if blocksErr != nil {
		reached = true
		if !auth.allows(authorizeSharedInfra) && !dryRun {
			return decision, reached, fmt.Errorf("cannot verify whether shared services are owned or referenced by other contexts: %w; resolve the contexts directory or re-run with --authorize %s to tear them down regardless", blocksErr, authorizeSharedInfra)
		}
	}
	if err := converge.InfraComponentDestroyBlockError(decision.Blocks); err != nil {
		reached = true
		if !auth.allows(authorizeSharedInfra) && !dryRun {
			return decision, reached, err
		}
	}
	return decision, reached, nil
}
