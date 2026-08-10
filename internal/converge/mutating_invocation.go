package converge

const (
	MutatingInvocationExtraVar                 = "bootwright_mutating_invocation"
	ApplyReconcileInvocationExtraVar           = "bootwright_apply_reconcile_invocation"
	ApplyRebuildInvocationExtraVar             = "bootwright_apply_rebuild_invocation"
	ApplyReclaimInvocationExtraVar             = "bootwright_apply_reclaim_invocation"
	ApplyReclaimDevicesExtraVar                = "bootwright_apply_reclaim_devices"
	ApplyControllerDNSInvocationExtraVar       = "bootwright_apply_controller_dns_invocation"
	ApplyControllerDNSRepairInvocationExtraVar = "bootwright_apply_controller_dns_repair_invocation"
	ApplyControllerDNSResumeInvocationExtraVar = "bootwright_apply_controller_dns_resume_invocation"
	ApplyFullInvocationExtraVar                = "bootwright_apply_full_invocation"
	ApplyThroughBaseInvocationExtraVar         = "bootwright_apply_through_base_invocation"
	ArbiterDegradedInvocationExtraVar          = "bootwright_arbiter_degraded_invocation"
	ArbiterSameSiteInvocationExtraVar          = "bootwright_arbiter_same_site_invocation"
	ArbiterUnreachableInvocationExtraVar       = "bootwright_arbiter_unreachable_invocation"
	ApplyReclaimInvocationSentinel             = "__BOOTWRIGHT_RUNTIME_RECLAIM_DEVICES_7EF51C56__"
)
