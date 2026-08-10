package workflow

type taskDependencyPolicy uint8

const (
	taskDependencyAllowsSkipped taskDependencyPolicy = iota
	taskDependencyRequiresSuccess
	taskDependencyRequiresTerminal
)

type taskDependencyRef struct {
	id     string
	policy taskDependencyPolicy
}

func taskDependencyRefs(task TaskLedgerEntry) []taskDependencyRef {
	refs := make([]taskDependencyRef, 0, len(task.Dependencies)+len(task.SuccessDependencies)+len(task.OrderingDependencies))
	for _, id := range task.Dependencies {
		refs = append(refs, taskDependencyRef{id: id, policy: taskDependencyAllowsSkipped})
	}
	for _, id := range task.SuccessDependencies {
		refs = append(refs, taskDependencyRef{id: id, policy: taskDependencyRequiresSuccess})
	}
	for _, id := range task.OrderingDependencies {
		refs = append(refs, taskDependencyRef{id: id, policy: taskDependencyRequiresTerminal})
	}
	return refs
}

func taskBlockingDependencyIDs(task TaskLedgerEntry) []string {
	ids := append([]string(nil), task.Dependencies...)
	return append(ids, task.SuccessDependencies...)
}

func taskDependencyIDs(task TaskLedgerEntry) []string {
	refs := taskDependencyRefs(task)
	ids := make([]string, 0, len(refs))
	for _, ref := range refs {
		ids = append(ids, ref.id)
	}
	return ids
}

func taskDependencySatisfied(status TaskStatus, policy taskDependencyPolicy) bool {
	switch policy {
	case taskDependencyAllowsSkipped:
		return status == TaskStatusOK || status == TaskStatusSkipped
	case taskDependencyRequiresSuccess:
		return status == TaskStatusOK
	case taskDependencyRequiresTerminal:
		return taskTerminal(status)
	default:
		return false
	}
}
