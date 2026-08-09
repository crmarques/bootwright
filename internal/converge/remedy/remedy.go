package remedy

type Action string

const (
	ActionRetrySameInvocation      Action = "retry-same-invocation"
	ActionReconcileSameSelection   Action = "reconcile-same-selection"
	ActionRebuildSameSelection     Action = "rebuild-same-selection"
	ActionRegenerateClusterISO     Action = "regenerate-cluster-iso"
	ActionDestroyAndReapplyCluster Action = "destroy-and-reapply-cluster"
	ActionRebuildCluster           Action = "rebuild-cluster"
)

type TargetRole string

const (
	TargetRoleContainerCluster TargetRole = "container-cluster"
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
	ActionRebuildSameSelection,
	ActionRegenerateClusterISO,
	ActionDestroyAndReapplyCluster,
	ActionRebuildCluster,
}

func RegisteredActions() []Action {
	return append([]Action(nil), registeredActions...)
}
