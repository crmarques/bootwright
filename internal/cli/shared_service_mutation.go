package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/clusteraccess"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/ownership"
	"github.com/crmarques/bootwright/internal/workspace"
)

type applySharedServiceMutation struct {
	lease                 *workflow.CommandRunLease
	runContext            context.Context
	refusal               error
	artifactServerTargets []converge.ArtifactServerReclaimTarget
}

type destroySharedServiceMutation struct {
	lease      *workflow.CommandRunLease
	runContext context.Context
	decision   converge.InfraComponentDestroyDecision
	refusal    error
	reached    bool
}

func prepareApplySharedServiceMutation(parent context.Context, contextName string, state v1alpha1.State, sel clusteraccess.Selection, runScope converge.Scope, dryRun bool, invocation resolvedInvocation) (applySharedServiceMutation, error) {
	result := applySharedServiceMutation{runContext: parent, artifactServerTargets: installOnlyArtifactServerTargets(state)}
	var refs []converge.InfraComponentServiceRef
	var hosts map[string]bool
	if sel.MachineSelection {
		hosts = sel.MachineProvision
	}
	fabricSelected := converge.ScopeIncludesApplyPhase(runScope, converge.PhaseFabric)
	if fabricSelected {
		refs = selectedInfraComponentServiceRefs(sel.RenderState, false, false, hosts)
		if dryRun {
			degrading := selectedInfraComponentServiceRefs(sel.RenderState, false, true, hosts)
			decision, scanErr := converge.PlanInfraComponentApplyBlocks(contextName, degrading)
			result.refusal = converge.InfraComponentApplyRefusal(decision, scanErr)
		}
	}
	controllerProofSelected := converge.ScopeIncludesApplyPhase(runScope, converge.PhaseMachines) ||
		converge.ScopeIncludesApplyPhase(runScope, converge.PhaseDeps) ||
		converge.ScopeIncludesApplyPhase(runScope, converge.PhaseBase) ||
		converge.ScopeIncludesApplyPhase(runScope, converge.PhaseAddons)
	if !fabricSelected && controllerProofSelected {
		refs = selectedControllerNameResolutionServiceRefs(sel.RenderState, hosts)
	}
	if converge.ScopeUsesAnsible(runScope) && len(result.artifactServerTargets) > 0 {
		refs = append(refs, selectedInfraComponentServiceRefs(state, true, false, nil)...)
	}
	controllerResolverSelected := false
	for _, ref := range refs {
		if strings.TrimSpace(ref.Kind) == v1alpha1.ComponentSlotNameResolution {
			controllerResolverSelected = true
			break
		}
	}
	if dryRun && controllerResolverSelected && result.refusal == nil {
		claims, warnings, scanErr := converge.OtherContextControllerResolverClaims(contextName)
		result.refusal = converge.ControllerResolverClaimRefusal(claims, warnings, scanErr)
	}
	if dryRun {
		return result, nil
	}
	lease, err := acquireSharedServiceMutationLease(parent, contextName, "apply", refs, nil, invocation)
	result.lease = lease
	if err != nil {
		return result, err
	}
	if lease != nil {
		result.runContext = lease.Context()
	}
	if controllerResolverSelected {
		claims, warnings, scanErr := converge.OtherContextControllerResolverClaims(contextName)
		result.refusal = converge.ControllerResolverClaimRefusal(claims, warnings, scanErr)
	}
	if result.refusal == nil && converge.ScopeIncludesApplyPhase(runScope, converge.PhaseFabric) {
		degrading := selectedInfraComponentServiceRefs(sel.RenderState, false, true, hosts)
		decision, scanErr := converge.PlanInfraComponentApplyBlocks(contextName, degrading)
		result.refusal = converge.InfraComponentApplyRefusal(decision, scanErr)
	}
	if result.refusal != nil {
		return result, applyInstallRemedialError(result.refusal, invocation)
	}
	return result, nil
}

func prepareDestroySharedServiceMutation(parent context.Context, contextName string, state v1alpha1.State, sel clusteraccess.Selection, runScope converge.Scope, artifactServerOnly, dryRun, evidenceDegraded bool, records []ownership.ResourceRecord, auth *authorizations, invocation resolvedInvocation) (destroySharedServiceMutation, error) {
	result := destroySharedServiceMutation{runContext: parent}
	refs, selectedRecords := infraComponentDestroyConsequence(state, sel, runScope, artifactServerOnly, evidenceDegraded, records)
	if len(refs) == 0 && len(selectedRecords) == 0 {
		return result, nil
	}
	controllerResolverSelected := controllerResolverDestroySelected(refs, selectedRecords)
	if dryRun && controllerResolverSelected {
		claims, warnings, scanErr := converge.OtherContextControllerResolverClaims(contextName)
		result.refusal = converge.ControllerResolverClaimRefusal(claims, warnings, scanErr)
	}
	if !dryRun {
		lease, err := acquireSharedServiceMutationLease(parent, contextName, "destroy", refs, selectedRecords, invocation)
		result.lease = lease
		if err != nil {
			return result, err
		}
		if lease != nil {
			result.runContext = lease.Context()
		}
		if controllerResolverSelected {
			claims, warnings, scanErr := converge.OtherContextControllerResolverClaims(contextName)
			result.refusal = converge.ControllerResolverClaimRefusal(claims, warnings, scanErr)
			if result.refusal != nil {
				return result, applyInstallRemedialError(result.refusal, invocation)
			}
		}
	}
	decision, blocked, err := destroyInfraComponentGate(auth, contextName, refs, selectedRecords, artifactServerOnly, dryRun, invocation)
	result.decision = decision
	result.reached = blocked
	return result, err
}

func requireSharedServiceMutationLease(lease *workflow.CommandRunLease, phase string) error {
	if lease == nil {
		return nil
	}
	if err := lease.RequireOwned(); err != nil {
		return fmt.Errorf("shared-service mutation lease was lost %s: %w", phase, err)
	}
	return nil
}

func acquireSharedServiceMutationLease(parent context.Context, contextName, command string, refs []converge.InfraComponentServiceRef, records []ownership.ResourceRecord, invocation resolvedInvocation) (*workflow.CommandRunLease, error) {
	if len(refs) == 0 && len(records) == 0 {
		return nil, nil
	}
	lease, err := workflow.AcquireSharedServiceMutationLease(parent, workspace.SharedServiceMutationRunsDir(), contextName, command)
	if err == nil {
		return lease, nil
	}
	retry, retryErr := invocation.retry(retryIntent{})
	if retryErr != nil {
		return nil, fmt.Errorf("another context may be mutating shared infra-component services: %w; inspect the controller-global lease %s and repair or remove it only after proving its run is no longer active; cannot construct the exact retry command: %v", err, workflow.LeasePath(workspace.SharedServiceMutationRunsDir()), retryErr)
	}
	return nil, fmt.Errorf("another context may be mutating shared infra-component services: %w; inspect the controller-global lease %s, wait for its run to finish, or repair/remove a stale or corrupt lease only after proving no such run is active; then re-run `%s` with exactly the same selected work and intent", err, workflow.LeasePath(workspace.SharedServiceMutationRunsDir()), retry.String())
}

func infraComponentDestroyConsequence(state v1alpha1.State, sel clusteraccess.Selection, runScope converge.Scope, artifactServerOnly, evidenceDegraded bool, records []ownership.ResourceRecord) ([]converge.InfraComponentServiceRef, []ownership.ResourceRecord) {
	if artifactServerOnly {
		refs := selectedInfraComponentServiceRefs(state, true, false, nil)
		recordRefs := destroyRecordScopeRefs(refs, evidenceDegraded)
		return refs, filterInfraComponentRecords(records, nil, recordRefs, true)
	}
	if !converge.ScopeTearsMachineLayer(runScope) {
		return nil, nil
	}
	if sel.MachineSelection {
		refs := selectedInfraComponentServiceRefs(state, false, false, sel.MachineProvision)
		recordRefs := destroyRecordScopeRefs(refs, evidenceDegraded)
		selectedRecords := filterInfraComponentRecords(records, sel.MachineProvision, recordRefs, false)
		selectedRecords = append(selectedRecords, filterControllerResolverRecords(records, sel.MachineProvision, recordRefs)...)
		return refs, selectedRecords
	}
	refs := selectedInfraComponentServiceRefs(sel.RenderState, false, false, nil)
	if !sel.Active {
		recordRefs := destroyRecordScopeRefs(refs, evidenceDegraded)
		selectedRecords := filterInfraComponentRecords(records, nil, recordRefs, false)
		selectedRecords = append(selectedRecords, filterControllerResolverRecords(records, nil, recordRefs)...)
		return refs, selectedRecords
	}
	selectedRecords := filterInfraComponentRecords(records, nil, refs, false)
	selectedRecords = append(selectedRecords, filterControllerResolverRecords(records, nil, refs)...)
	return refs, selectedRecords
}

func destroyRecordScopeRefs(refs []converge.InfraComponentServiceRef, evidenceDegraded bool) []converge.InfraComponentServiceRef {
	if !evidenceDegraded {
		return nil
	}
	return append([]converge.InfraComponentServiceRef{}, refs...)
}

func filterInfraComponentRecords(records []ownership.ResourceRecord, hosts map[string]bool, refs []converge.InfraComponentServiceRef, artifactServerOnly bool) []ownership.ResourceRecord {
	type identity struct {
		name string
		host string
	}
	wanted := map[identity]bool{}
	for _, ref := range refs {
		wanted[identity{name: strings.TrimSpace(ref.Name), host: strings.TrimSpace(ref.Host)}] = true
	}
	var out []ownership.ResourceRecord
	for _, record := range records {
		if strings.TrimSpace(record.Kind) != "infra-component" {
			continue
		}
		if artifactServerOnly && strings.TrimSpace(record.Labels["bootwright.kind"]) != "artifacts" {
			continue
		}
		if hosts != nil && !hosts[strings.TrimSpace(record.Host)] {
			continue
		}
		if refs != nil && !wanted[identity{name: strings.TrimSpace(record.Name), host: strings.TrimSpace(record.Host)}] {
			continue
		}
		out = append(out, record)
	}
	return out
}

func filterControllerResolverRecords(records []ownership.ResourceRecord, hosts map[string]bool, refs []converge.InfraComponentServiceRef) []ownership.ResourceRecord {
	wanted := map[string]bool{}
	for _, ref := range refs {
		if strings.TrimSpace(ref.Kind) == v1alpha1.ComponentSlotNameResolution {
			wanted[strings.TrimSpace(ref.Name)] = true
		}
	}
	var out []ownership.ResourceRecord
	for _, record := range records {
		if strings.TrimSpace(record.Kind) != string(ownership.KindControllerNameResolver) {
			continue
		}
		if hosts != nil && !hosts[strings.TrimSpace(record.Attributes["machineRef"])] {
			continue
		}
		identity := strings.TrimSpace(record.Provider) + "-" + strings.TrimSpace(record.Attributes["component"])
		if refs != nil && !wanted[identity] {
			continue
		}
		out = append(out, record)
	}
	return out
}

func controllerResolverDestroySelected(refs []converge.InfraComponentServiceRef, records []ownership.ResourceRecord) bool {
	for _, ref := range refs {
		if strings.TrimSpace(ref.Kind) == v1alpha1.ComponentSlotNameResolution {
			return true
		}
	}
	for _, record := range records {
		if strings.TrimSpace(record.Kind) == string(ownership.KindControllerNameResolver) {
			return true
		}
	}
	return false
}
