package converge

import (
	"strings"

	"github.com/crmarques/bootwright/internal/converge/workflow"
)

const (
	InfraDestroyContextSweepExtraVar = "bootwright_infra_destroy_context_sweep"
	DestroyClusterScopeExtraVar      = "bootwright_destroy_cluster_scope"
	DestroyForceUnownedExtraVar      = "bootwright_destroy_force_unowned"
	DestroySkipUnreachableExtraVar   = "bootwright_destroy_skip_unreachable"
)

func ApplyDestroyScopeExtraVars(plan *WorkflowPlan, infraScope bool, clusterScope string, resolvedClusterRoots []string, forceUnowned bool, skipUnreachable bool) {
	if infraScope {
		if strings.TrimSpace(clusterScope) == "" {
			plan.ExtraVarPairs = append(plan.ExtraVarPairs, InfraDestroyContextSweepExtraVar+"=true")
		} else {
			plan.ExtraVarPairs = append(plan.ExtraVarPairs, DestroyClusterScopeExtraVar+"="+strings.Join(resolvedClusterRoots, ","))
		}
	}
	if plan.StorageWorkNames != nil {
		plan.ExtraVarPairs = append(plan.ExtraVarPairs, workflow.DestroyStorageScopeExtraVar+"="+strings.Join(plan.StorageWorkNames, ","))
	}
	if forceUnowned {
		plan.ExtraVarPairs = append(plan.ExtraVarPairs, DestroyForceUnownedExtraVar+"=true")
	}
	if skipUnreachable {
		plan.ExtraVarPairs = append(plan.ExtraVarPairs, DestroySkipUnreachableExtraVar+"=true")
	}
}
