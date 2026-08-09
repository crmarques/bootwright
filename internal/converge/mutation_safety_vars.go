package converge

type mutationSafetyVarClass string

const (
	mutationSafetyVarIntent        mutationSafetyVarClass = "intent"
	mutationSafetyVarAuthorization mutationSafetyVarClass = "authorization"
	mutationSafetyVarScope         mutationSafetyVarClass = "scope"
	mutationSafetyVarExecution     mutationSafetyVarClass = "execution"
)

type mutationSafetyVarContract struct {
	Name  string
	Class mutationSafetyVarClass
}

var mutationSafetyVars = []mutationSafetyVarContract{
	{Name: "bootwright_apply_full_invocation", Class: mutationSafetyVarExecution},
	{Name: "bootwright_apply_mode", Class: mutationSafetyVarIntent},
	{Name: "bootwright_apply_reclaim_devices", Class: mutationSafetyVarScope},
	{Name: "bootwright_apply_reclaim_invocation", Class: mutationSafetyVarExecution},
	{Name: "bootwright_apply_rebuild_invocation", Class: mutationSafetyVarExecution},
	{Name: "bootwright_apply_reconcile_invocation", Class: mutationSafetyVarExecution},
	{Name: "bootwright_apply_through_base_invocation", Class: mutationSafetyVarExecution},
	{Name: "bootwright_arbiter_allow_degraded", Class: mutationSafetyVarAuthorization},
	{Name: "bootwright_arbiter_allow_same_site", Class: mutationSafetyVarAuthorization},
	{Name: "bootwright_arbiter_cluster_name", Class: mutationSafetyVarScope},
	{Name: "bootwright_arbiter_degraded_invocation", Class: mutationSafetyVarExecution},
	{Name: "bootwright_arbiter_desired_addr", Class: mutationSafetyVarExecution},
	{Name: "bootwright_arbiter_desired_mon", Class: mutationSafetyVarExecution},
	{Name: "bootwright_arbiter_desired_node", Class: mutationSafetyVarScope},
	{Name: "bootwright_arbiter_desired_site", Class: mutationSafetyVarExecution},
	{Name: "bootwright_arbiter_failure_domain", Class: mutationSafetyVarExecution},
	{Name: "bootwright_arbiter_live_mon", Class: mutationSafetyVarExecution},
	{Name: "bootwright_arbiter_live_node", Class: mutationSafetyVarScope},
	{Name: "bootwright_arbiter_mon_hosts_after", Class: mutationSafetyVarExecution},
	{Name: "bootwright_arbiter_mon_hosts_during", Class: mutationSafetyVarExecution},
	{Name: "bootwright_arbiter_mon_locations", Class: mutationSafetyVarExecution},
	{Name: "bootwright_arbiter_mon_locations_after", Class: mutationSafetyVarExecution},
	{Name: "bootwright_arbiter_old_host_offline", Class: mutationSafetyVarAuthorization},
	{Name: "bootwright_arbiter_same_site_invocation", Class: mutationSafetyVarExecution},
	{Name: "bootwright_arbiter_tiebreaker_mon", Class: mutationSafetyVarExecution},
	{Name: "bootwright_arbiter_unreachable_invocation", Class: mutationSafetyVarExecution},
	{Name: "bootwright_ceph_authorize_foreign_daemons", Class: mutationSafetyVarAuthorization},
	{Name: "bootwright_ceph_authorize_unowned_devices", Class: mutationSafetyVarAuthorization},
	{Name: "bootwright_ceph_destroy_confirmed_fsids", Class: mutationSafetyVarAuthorization},
	{Name: "bootwright_ceph_filter_reclaim_clusters", Class: mutationSafetyVarAuthorization},
	{Name: "bootwright_ceph_rebuild_authorized_clusters", Class: mutationSafetyVarAuthorization},
	{Name: "bootwright_ceph_reclaim_clusters", Class: mutationSafetyVarAuthorization},
	{Name: "bootwright_ceph_reclaim_devices", Class: mutationSafetyVarAuthorization},
	{Name: "bootwright_ceph_reconcilable_only_clusters", Class: mutationSafetyVarScope},
	{Name: "bootwright_ceph_subobject_rebuild_authorized", Class: mutationSafetyVarAuthorization},
	{Name: "bootwright_destroy_authorize_unowned_networks", Class: mutationSafetyVarAuthorization},
	{Name: "bootwright_destroy_authorize_unowned_vms", Class: mutationSafetyVarAuthorization},
	{Name: "bootwright_destroy_cluster_levels", Class: mutationSafetyVarExecution},
	{Name: "bootwright_destroy_cluster_order", Class: mutationSafetyVarExecution},
	{Name: "bootwright_destroy_cluster_scope", Class: mutationSafetyVarScope},
	{Name: "bootwright_destroy_container_cluster", Class: mutationSafetyVarScope},
	{Name: "bootwright_destroy_machine_scope", Class: mutationSafetyVarScope},
	{Name: "bootwright_destroy_skip_unreachable", Class: mutationSafetyVarAuthorization},
	{Name: "bootwright_destroy_storage_scope", Class: mutationSafetyVarScope},
	{Name: "bootwright_infra_component_apply_skip_records", Class: mutationSafetyVarScope},
	{Name: "bootwright_infra_component_destroy_scope_records", Class: mutationSafetyVarScope},
	{Name: "bootwright_infra_component_reclaim_records", Class: mutationSafetyVarAuthorization},
	{Name: "bootwright_infra_component_service_scope", Class: mutationSafetyVarScope},
	{Name: "bootwright_infra_destroy_context_sweep", Class: mutationSafetyVarScope},
	{Name: "bootwright_install_wait_target", Class: mutationSafetyVarExecution},
	{Name: "bootwright_machine_infra_records_only", Class: mutationSafetyVarExecution},
	{Name: "bootwright_ocp_rebuild_authorized_clusters", Class: mutationSafetyVarAuthorization},
	{Name: "bootwright_substrate_reset_clusters", Class: mutationSafetyVarAuthorization},
	{Name: "bootwright_substrate_reset_machines", Class: mutationSafetyVarAuthorization},
	{Name: "bootwright_task_cluster_name", Class: mutationSafetyVarScope},
	{Name: "bootwright_task_host_cluster_name", Class: mutationSafetyVarScope},
	{Name: "bootwright_task_machine_names", Class: mutationSafetyVarScope},
	{Name: "bootwright_task_managed_os_group_name", Class: mutationSafetyVarScope},
	{Name: "bootwright_task_provider_host_name", Class: mutationSafetyVarScope},
	{Name: "bootwright_task_storage_cluster_name", Class: mutationSafetyVarScope},
	{Name: "bootwright_task_storage_prereqs_only", Class: mutationSafetyVarExecution},
	{Name: "bootwright_task_storage_skip_prereqs", Class: mutationSafetyVarExecution},
	{Name: "bootwright_agent_node_cluster_name", Class: mutationSafetyVarScope},
	{Name: "bootwright_agent_node_machine_name", Class: mutationSafetyVarScope},
	{Name: "bootwright_machine_task_cluster_name", Class: mutationSafetyVarScope},
	{Name: "bootwright_machine_task_machine_name", Class: mutationSafetyVarScope},
	{Name: "bootwright_machine_task_provider_host_name", Class: mutationSafetyVarScope},
	{Name: "bootwright_mutating_invocation", Class: mutationSafetyVarExecution},
}
