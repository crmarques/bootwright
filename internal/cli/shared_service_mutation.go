package cli

import (
	"context"
	"errors"
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
	manifest              converge.HostSharedServiceManifest
	artifactServerTargets []converge.ArtifactServerReclaimTarget
}

type destroySharedServiceMutation struct {
	lease      *workflow.CommandRunLease
	runContext context.Context
	decision   converge.InfraComponentDestroyDecision
	refusal    error
	reached    bool
	manifest   converge.HostSharedServiceManifest
}

func prepareApplySharedServiceMutation(parent context.Context, contextName string, state v1alpha1.State, sel clusteraccess.Selection, runScope converge.Scope, dryRun bool, invocation resolvedInvocation) (applySharedServiceMutation, error) {
	result := applySharedServiceMutation{runContext: parent, artifactServerTargets: installOnlyArtifactServerTargets(state)}
	var refs []converge.InfraComponentServiceRef
	var remoteRefs []converge.InfraComponentServiceRef
	var hosts map[string]bool
	if sel.MachineSelection {
		hosts = sel.WorkMachines
	}
	fabricSelected := converge.ScopeIncludesApplyPhase(runScope, converge.PhaseFabric)
	if fabricSelected {
		refs = selectedInfraComponentServiceRefs(sel.RenderState, false, false, hosts)
		refs = append(refs, selectedBMCEmulatorServiceRefs(sel.RenderState, hosts)...)
		remoteRefs = append(remoteRefs, refs...)
		if dryRun {
			degrading := selectedInfraComponentServiceRefs(sel.RenderState, false, true, hosts)
			decision, scanErr := converge.PlanInfraComponentApplyBlocks(contextName, degrading)
			result.refusal = converge.InfraComponentApplyRefusal(decision, scanErr)
			bmcDecision, bmcScanErr := converge.PlanExclusiveSharedServiceBlocks(contextName, selectedBMCEmulatorServiceRefs(sel.RenderState, hosts))
			result.refusal = errors.Join(result.refusal, converge.ExclusiveSharedServiceRefusal(bmcDecision, bmcScanErr))
		}
	}
	controllerProofSelected := converge.ScopeIncludesApplyPhase(runScope, converge.PhaseMachines) ||
		converge.ScopeIncludesApplyPhase(runScope, converge.PhaseDeps) ||
		converge.ScopeIncludesApplyPhase(runScope, converge.PhaseBase) ||
		converge.ScopeIncludesApplyPhase(runScope, converge.PhaseAddons)
	if !fabricSelected && controllerProofSelected {
		refs = append(refs, selectedControllerNameResolutionServiceRefs(sel.RenderState, hosts)...)
	}
	if converge.ScopeUsesAnsible(runScope) && len(result.artifactServerTargets) > 0 {
		artifactRefs := selectedInfraComponentServiceRefs(state, true, false, nil)
		refs = append(refs, artifactRefs...)
		remoteRefs = append(remoteRefs, artifactRefs...)
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
	manifest, manifestErr := converge.BuildHostSharedServiceManifest(contextName, "apply", hostSharedServiceManifestRefs(remoteRefs))
	result.manifest = manifest
	if manifestErr != nil {
		return result, applyInstallRemedialError(manifestErr, invocation)
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
	if converge.ScopeIncludesApplyPhase(runScope, converge.PhaseFabric) {
		bmcDecision, bmcScanErr := converge.PlanExclusiveSharedServiceBlocks(contextName, selectedBMCEmulatorServiceRefs(sel.RenderState, hosts))
		result.refusal = errors.Join(result.refusal, converge.ExclusiveSharedServiceRefusal(bmcDecision, bmcScanErr))
	}
	if result.refusal != nil {
		return result, applyInstallRemedialError(result.refusal, invocation)
	}
	return result, nil
}

func prepareDestroySharedServiceMutation(parent context.Context, contextName string, state v1alpha1.State, sel clusteraccess.Selection, runScope converge.Scope, artifactServerOnly, dryRun, evidenceDegraded bool, records []ownership.ResourceRecord, auth *authorizations, invocation resolvedInvocation) (destroySharedServiceMutation, error) {
	result := destroySharedServiceMutation{runContext: parent}
	infraRefs, infraRecords := infraComponentDestroyConsequence(state, sel, runScope, artifactServerOnly, evidenceDegraded, records)
	bmcRefs, bmcRecords := bmcEmulatorDestroyConsequence(state, sel, runScope, artifactServerOnly, evidenceDegraded, records)
	refs := append(append([]converge.InfraComponentServiceRef{}, infraRefs...), bmcRefs...)
	infraTransitionRecords := filterInfraComponentTransitionRecords(records, infraRefs, artifactServerOnly, !sel.Active && !artifactServerOnly)
	selectedRecords := append(append(append([]ownership.ResourceRecord{}, infraRecords...), infraTransitionRecords...), bmcRecords...)
	if len(refs) == 0 && len(selectedRecords) == 0 {
		return result, nil
	}
	manifestRefs := hostSharedServiceManifestRefs(refs)
	for _, record := range selectedRecords {
		kind := record.Kind
		name := record.Name
		host := record.Host
		var selectionDigests []string
		var claimDigests []string
		if kind == string(ownership.KindInfraComponent) {
			kind = record.Labels["bootwright.kind"]
			selectionDigest, claimDigest, digestErr := infraComponentSharedServiceRecordDigests(record)
			if digestErr != nil {
				manifestErr := &converge.HostSharedServiceManifestError{Err: fmt.Errorf("selected infra-component ownership record %q on %q is not exact trusted host consequence evidence: %w", record.Name, record.Host, digestErr)}
				return result, applyInstallRemedialError(manifestErr, invocation)
			}
			selectionDigests = []string{selectionDigest}
			claimDigests = []string{claimDigest}
		} else if kind == string(ownership.KindInfraComponentTransition) {
			kind = record.Labels["bootwright.kind"]
			selectionDigest, digestErr := infraComponentTransitionSelectionDigest(record)
			if digestErr != nil {
				manifestErr := &converge.HostSharedServiceManifestError{Err: fmt.Errorf("selected infra-component transition record %q on %q is not exact trusted recovery evidence: %w", record.Name, record.Host, digestErr)}
				return result, applyInstallRemedialError(manifestErr, invocation)
			}
			selectionDigests = []string{selectionDigest}
		} else if kind == string(ownership.KindControllerNameResolver) {
			continue
		} else if kind == string(ownership.KindBMCEmulator) {
			selectionDigest, claimDigest, digestErr := bmcSharedServiceRecordDigests(record)
			if digestErr != nil {
				manifestErr := &converge.HostSharedServiceManifestError{Err: fmt.Errorf("selected BMC ownership record %q on %q is not exact trusted host consequence evidence: %w", record.Name, record.Host, digestErr)}
				return result, applyInstallRemedialError(manifestErr, invocation)
			}
			selectionDigests = []string{selectionDigest}
			claimDigests = []string{claimDigest}
		}
		manifestRefs = append(manifestRefs, converge.InfraComponentServiceRef{
			Kind:             kind,
			Name:             name,
			Host:             host,
			SelectionDigests: selectionDigests,
			ClaimDigests:     claimDigests,
		})
	}
	manifest, manifestErr := converge.BuildHostSharedServiceManifest(contextName, "destroy", manifestRefs)
	result.manifest = manifest
	if manifestErr != nil {
		return result, applyInstallRemedialError(manifestErr, invocation)
	}
	controllerResolverSelected := controllerResolverDestroySelected(refs, selectedRecords)
	if dryRun && controllerResolverSelected {
		claims, warnings, scanErr := converge.OtherContextControllerResolverClaims(contextName)
		result.refusal = converge.ControllerResolverClaimRefusal(claims, warnings, scanErr)
	}
	if dryRun {
		bmcDecision, bmcScanErr := converge.PlanExclusiveSharedServiceBlocks(contextName, bmcScanRefs(bmcRefs, bmcRecords))
		result.refusal = errors.Join(result.refusal, converge.ExclusiveSharedServiceRefusal(bmcDecision, bmcScanErr))
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
		bmcDecision, bmcScanErr := converge.PlanExclusiveSharedServiceBlocks(contextName, bmcScanRefs(bmcRefs, bmcRecords))
		result.refusal = errors.Join(result.refusal, converge.ExclusiveSharedServiceRefusal(bmcDecision, bmcScanErr))
		if result.refusal != nil {
			return result, applyInstallRemedialError(result.refusal, invocation)
		}
	}
	decision, blocked, err := destroyInfraComponentGate(auth, contextName, infraRefs, infraRecords, artifactServerOnly, dryRun, invocation)
	result.decision = decision
	result.reached = blocked
	return result, err
}

func hostSharedServiceManifestRefs(refs []converge.InfraComponentServiceRef) []converge.InfraComponentServiceRef {
	out := make([]converge.InfraComponentServiceRef, 0, len(refs))
	for _, ref := range refs {
		ref.Kind = infraComponentHostLogicalKind(ref.Kind)
		out = append(out, ref)
	}
	return out
}

func infraComponentHostLogicalKind(kind string) string {
	switch kind {
	case v1alpha1.ComponentSlotArtifactServer:
		return "artifacts"
	case v1alpha1.ComponentSlotLoadBalancer:
		return "load-balancer"
	default:
		return kind
	}
}

func appendHostSharedServiceManifestExtraVar(plan *converge.WorkflowPlan, manifest converge.HostSharedServiceManifest) error {
	pair, err := manifest.ExtraVarPair()
	if err != nil {
		return err
	}
	if pair != "" {
		plan.ExtraVarPairs = append(plan.ExtraVarPairs, pair)
	}
	return nil
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
		return nil, fmt.Errorf("another context may be mutating shared machine services: %w; inspect the controller-global lease %s and repair or remove it only after proving its run is no longer active; cannot construct the exact retry command: %v", err, workflow.LeasePath(workspace.SharedServiceMutationRunsDir()), retryErr)
	}
	return nil, fmt.Errorf("another context may be mutating shared machine services: %w; inspect the controller-global lease %s, wait for its run to finish, or repair/remove a stale or corrupt lease only after proving no such run is active; then re-run `%s` with exactly the same selected work and intent", err, workflow.LeasePath(workspace.SharedServiceMutationRunsDir()), retry.String())
}
