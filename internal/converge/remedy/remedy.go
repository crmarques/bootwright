package remedy

type Action string

const (
	ActionRetrySameInvocation                             Action = "retry-same-invocation"
	ActionApplyAllConsumers                               Action = "apply-all-consumers"
	ActionResumeControllerDNSMutation                     Action = "resume-controller-dns-mutation"
	ActionReconcileSharedServiceThenRetrySameSelection    Action = "reconcile-shared-service-then-retry-same-selection"
	ActionReconcileSameSelection                          Action = "reconcile-same-selection"
	ActionReconcileContainerClusterThenRetrySameSelection Action = "reconcile-container-cluster-then-retry-same-selection"
	ActionRebuildSameSelection                            Action = "rebuild-same-selection"
	ActionRegenerateClusterISO                            Action = "regenerate-cluster-iso"
	ActionDestroyAndReapplyCluster                        Action = "destroy-and-reapply-cluster"
	ActionRebuildCluster                                  Action = "rebuild-cluster"
	ActionDestroyProtectedLayersThenRebuildSameSelection  Action = "destroy-protected-layers-then-rebuild-same-selection"
)

type TargetRole string

const (
	TargetRoleContainerCluster TargetRole = "container-cluster"
	TargetRoleClusterRoot      TargetRole = "cluster-root"
	TargetRoleMachineLayer     TargetRole = "machine-layer"
	TargetRoleClusterLayer     TargetRole = "cluster-layer"
	TargetRoleMachineLayerRoot TargetRole = "machine-layer-cluster-root"
	TargetRoleClusterLayerRoot TargetRole = "cluster-layer-cluster-root"
)

type Target struct {
	Role TargetRole
	Name string
}

type Request struct {
	Action  Action
	Targets []Target
}

type Error interface {
	error
	Remedy() Request
}

var registeredActions = []Action{
	ActionRetrySameInvocation,
	ActionApplyAllConsumers,
	ActionResumeControllerDNSMutation,
	ActionReconcileSharedServiceThenRetrySameSelection,
	ActionReconcileSameSelection,
	ActionReconcileContainerClusterThenRetrySameSelection,
	ActionRebuildSameSelection,
	ActionRegenerateClusterISO,
	ActionDestroyAndReapplyCluster,
	ActionRebuildCluster,
	ActionDestroyProtectedLayersThenRebuildSameSelection,
}

func RegisteredActions() []Action {
	return append([]Action(nil), registeredActions...)
}
