package roles

// Playbook entry points in the bootwright.core collection. Alongside the role
// contracts in this package, these constants are the only place
// bootwright.core names may be spelled in Go source; a repository fitness
// test enforces it.
const (
	PlaybookWorkflowInfraApply                   = "bootwright.core.workflow_infra_apply"
	PlaybookWorkflowInfraDestroy                 = "bootwright.core.workflow_infra_destroy"
	PlaybookWorkflowInfraDestroyArtifactServer   = "bootwright.core.workflow_infra_destroy_artifact_server"
	PlaybookWorkflowClustersApply                = "bootwright.core.workflow_clusters_apply"
	PlaybookWorkflowClustersDestroy              = "bootwright.core.workflow_clusters_destroy"
	PlaybookWorkflowContainerClusterApply        = "bootwright.core.workflow_container_cluster_apply"
	PlaybookWorkflowAllApply                     = "bootwright.core.workflow_all_apply"
	PlaybookWorkflowBastionApplyTools            = "bootwright.core.workflow_bastion_apply_tools"
	PlaybookCheckPreflight                       = "bootwright.core.check_preflight"
	PlaybookTaskProviderServicesApply            = "bootwright.core.task_provider_services_apply"
	PlaybookTaskInfraComponentServicesApply      = "bootwright.core.task_infra_component_services_apply"
	PlaybookTaskMachineInfraPrepare              = "bootwright.core.task_machine_infra_prepare"
	PlaybookTaskMachineInfraApply                = "bootwright.core.task_machine_infra_apply"
	PlaybookTaskMachineInfraFinalize             = "bootwright.core.task_machine_infra_finalize"
	PlaybookTaskManagedMachineOSApply            = "bootwright.core.task_managed_machine_os_apply"
	PlaybookTaskContainerClusterCreateAgentISO   = "bootwright.core.task_container_cluster_create_agent_iso"
	PlaybookTaskContainerClusterBootAgentMachine = "bootwright.core.task_container_cluster_boot_agent_machine"
	PlaybookTaskContainerClusterWaitAgentInstall = "bootwright.core.task_container_cluster_wait_agent_install"
	PlaybookTaskStorageClusterApply              = "bootwright.core.task_storage_cluster_apply"
)
