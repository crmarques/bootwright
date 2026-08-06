package converge

import (
	"strings"

	"github.com/crmarques/bootwright/internal/converge/workflow"
)

const (
	InfraDestroyContextSweepExtraVar    = "bootwright_infra_destroy_context_sweep"
	DestroyClusterScopeExtraVar         = "bootwright_destroy_cluster_scope"
	DestroyAuthorizeUnownedVMsExtraVar  = "bootwright_destroy_authorize_unowned_vms"
	DestroyAuthorizeUnownedNetsExtraVar = "bootwright_destroy_authorize_unowned_networks"
	DestroySkipUnreachableExtraVar      = "bootwright_destroy_skip_unreachable"
	CephAuthorizeUnownedDevicesExtraVar = "bootwright_ceph_authorize_unowned_devices"
	CephAuthorizeForeignDaemonsExtraVar = "bootwright_ceph_authorize_foreign_daemons"
)

func ApplyDestroyScopeExtraVars(plan *WorkflowPlan, infraScope bool, clusterScope string, resolvedClusterRoots []string, machineScope []string, unownedVMs bool, unownedNetworks bool, skipUnreachable bool, unownedDevices bool) {
	switch {
	case len(machineScope) > 0:
		plan.ExtraVarPairs = append(plan.ExtraVarPairs, workflow.DestroyMachineScopeExtraVar+"="+strings.Join(machineScope, ","))
	case infraScope:
		if strings.TrimSpace(clusterScope) == "" {
			plan.ExtraVarPairs = append(plan.ExtraVarPairs, InfraDestroyContextSweepExtraVar+"=true")
		} else {
			plan.ExtraVarPairs = append(plan.ExtraVarPairs, DestroyClusterScopeExtraVar+"="+strings.Join(resolvedClusterRoots, ","))
			plan.ExtraVarPairs = append(plan.ExtraVarPairs, workflow.InfraComponentDestroyScopeRecordsExtraVar+"="+
				strings.Join(workflow.InfraComponentDestroyScopeRecords(plan.State, resolvedClusterRoots), ","))
		}
	}
	if plan.StorageWorkNames != nil {
		plan.ExtraVarPairs = append(plan.ExtraVarPairs, workflow.DestroyStorageScopeExtraVar+"="+strings.Join(plan.StorageWorkNames, ","))
	}
	if unownedVMs {
		plan.ExtraVarPairs = append(plan.ExtraVarPairs, DestroyAuthorizeUnownedVMsExtraVar+"=true")
	}
	if unownedNetworks {
		plan.ExtraVarPairs = append(plan.ExtraVarPairs, DestroyAuthorizeUnownedNetsExtraVar+"=true")
	}
	if skipUnreachable {
		plan.ExtraVarPairs = append(plan.ExtraVarPairs, DestroySkipUnreachableExtraVar+"=true")
	}
	ApplyCephUnownedDeviceAuthorizationExtraVar(plan, unownedDevices)
}
