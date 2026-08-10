package status

import (
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

type ApplyPhase struct {
	Label  string
	Status workflow.TaskStatus
}

func ApplyClusterKind(tasks []workflow.TaskLedgerEntry) string {
	kind := ""
	for _, task := range tasks {
		switch task.ClusterKind {
		case workflow.ApplyClusterKindContainer:
			return v1alpha1.KindContainerCluster
		case workflow.ApplyClusterKindStorage:
			kind = v1alpha1.KindStorageCluster
		}
	}
	if kind != "" {
		return kind
	}
	return "Cluster"
}

func ApplyClusterPhases(ledger workflow.RunLedger, cluster string) []ApplyPhase {
	phases := RunPhases(ledger.TasksForCluster(cluster))
	out := make([]ApplyPhase, 0, len(phases))
	for _, phase := range phases {
		out = append(out, ApplyPhase{Label: phase.Label, Status: phase.Status})
	}
	return out
}

func ApplyFailedPhase(_ workflow.RunLedger, task workflow.TaskLedgerEntry) string {
	return TaskPhaseLabel(task)
}

func ApplyFailureReason(failure string) string {
	lines := strings.Split(failure, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "failure:") {
			return trimApplyDetail(strings.TrimSpace(strings.TrimPrefix(line, "failure:")))
		}
	}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "last ") && !strings.HasPrefix(line, "underlying error:") {
			return trimApplyDetail(line)
		}
	}
	return "task failed"
}

func ApplyBlockingRoot(ledger workflow.RunLedger, task workflow.TaskLedgerEntry) (workflow.TaskLedgerEntry, bool) {
	visited := map[string]bool{}
	queue := applyBlockingDependencyIDs(task)
	var fallback workflow.TaskLedgerEntry
	haveFallback := false
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if visited[id] {
			continue
		}
		visited[id] = true
		dep, ok := ledger.Task(id)
		if !ok {
			continue
		}
		switch dep.Status {
		case workflow.TaskStatusFailed:
			return dep, true
		case workflow.TaskStatusBlocked, workflow.TaskStatusCancelled:
			if !haveFallback {
				fallback = dep
				haveFallback = true
			}
			queue = append(queue, applyBlockingDependencyIDs(dep)...)
		}
	}
	return fallback, haveFallback
}

func applyBlockingDependencyIDs(task workflow.TaskLedgerEntry) []string {
	ids := append([]string(nil), task.Dependencies...)
	return append(ids, task.SuccessDependencies...)
}

func applyPhaseStatus(tasks []workflow.TaskLedgerEntry) workflow.TaskStatus {
	if len(tasks) == 0 {
		return workflow.TaskStatusPending
	}
	counts := map[workflow.TaskStatus]int{}
	for _, task := range tasks {
		counts[task.Status]++
	}
	switch {
	case counts[workflow.TaskStatusFailed] > 0:
		return workflow.TaskStatusFailed
	case counts[workflow.TaskStatusBlocked] > 0:
		return workflow.TaskStatusBlocked
	case counts[workflow.TaskStatusCancelled] > 0:
		return workflow.TaskStatusCancelled
	case counts[workflow.TaskStatusRunning]+counts[workflow.TaskStatusReady] > 0:
		return workflow.TaskStatusRunning
	case counts[workflow.TaskStatusPending] > 0:
		if counts[workflow.TaskStatusOK]+counts[workflow.TaskStatusSkipped] > 0 {
			return workflow.TaskStatusRunning
		}
		return workflow.TaskStatusPending
	case counts[workflow.TaskStatusSkipped] == len(tasks):
		return workflow.TaskStatusSkipped
	default:
		return workflow.TaskStatusOK
	}
}

func trimApplyDetail(value string) string {
	return middleEllipsisDetail(strings.TrimSpace(value), 180)
}

func middleEllipsisDetail(value string, limit int) string {
	r := []rune(value)
	if len(r) <= limit {
		return value
	}
	const tail = 44
	head := limit - tail - 1
	if head < 1 {
		return string(r[:limit])
	}
	return string(r[:head]) + "…" + string(r[len(r)-tail:])
}
