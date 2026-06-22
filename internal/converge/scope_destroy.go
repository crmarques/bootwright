package converge

import "strings"

// Teardown-scoping executor gate variables. These mirror the apply path, where
// converge.PlanScopedApply stamps bootwright_apply_mode: the executor-coupling
// extra-vars are composed here in the bounded-context root with named
// constants, not as raw string literals in a cobra command, so the values are
// owned and unit-testable next to the service that runs them.
const (
	// InfraDestroyContextSweepExtraVar tells the infra teardown to reclaim every
	// recorded ownership orphan, not only objects still in desired state. Used
	// for a whole-context (full or unscoped infra) destroy.
	InfraDestroyContextSweepExtraVar = "bootwright_infra_destroy_context_sweep"
	// DestroyClusterScopeExtraVar gates recorded-resource cleanup to the selected
	// cluster roots so a scoped destroy does not tear down a co-located cluster's
	// VMs/disks on a shared hypervisor.
	DestroyClusterScopeExtraVar = "bootwright_destroy_cluster_scope"
	// DestroyForceUnownedExtraVar relaxes the per-VM ownership-marker refusals in
	// the machine substrate destroy roles so a domain/VM matching the Bootwright
	// naming but carrying a missing/mismatched marker is still torn down.
	DestroyForceUnownedExtraVar = "bootwright_destroy_force_unowned"
)

// ApplyDestroyScopeExtraVars composes the teardown-scoping executor gate
// variables onto a destroy WorkflowPlan. When infraScope is set (infra or full
// destroy), an empty clusterScope sweeps the whole context, otherwise the
// cleanup is gated to resolvedClusterRoots. force-unowned is independent and
// applies to any destroy. Cluster-name resolution stays a CLI concern; the
// already-resolved roots are passed in (mirroring how PlanScopedApply takes the
// already-scoped state).
func ApplyDestroyScopeExtraVars(plan *WorkflowPlan, infraScope bool, clusterScope string, resolvedClusterRoots []string, forceUnowned bool) {
	if infraScope {
		if strings.TrimSpace(clusterScope) == "" {
			plan.ExtraVarPairs = append(plan.ExtraVarPairs, InfraDestroyContextSweepExtraVar+"=true")
		} else {
			plan.ExtraVarPairs = append(plan.ExtraVarPairs, DestroyClusterScopeExtraVar+"="+strings.Join(resolvedClusterRoots, ","))
		}
	}
	if forceUnowned {
		plan.ExtraVarPairs = append(plan.ExtraVarPairs, DestroyForceUnownedExtraVar+"=true")
	}
}
