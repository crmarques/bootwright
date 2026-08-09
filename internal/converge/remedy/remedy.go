package remedy

type Action string

const (
	ActionRetrySameInvocation                             Action = "retry-same-invocation"
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
	TargetRoleMachineLayer     TargetRole = "machine-layer"
	TargetRoleClusterLayer     TargetRole = "cluster-layer"
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
