package status

import (
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

// ApplyPhase is one fleet phase of a cluster's apply run, aggregated from the
// ledger tasks that make it up. Status uses the workflow task vocabulary; the
// CLI maps it to display statuses.
type ApplyPhase struct {
	Label  string
	Status workflow.TaskStatus
}

// ApplyClusterKind derives the cluster root kind from its ledger tasks:
// v1alpha1.KindContainerCluster, v1alpha1.KindStorageCluster, or the generic
// "Cluster" when the tasks carry no cluster kind.
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

// ApplyClusterPhases groups a cluster's ledger tasks into the fleet phases the
// dashboards report, each with its aggregated task status.
func ApplyClusterPhases(ledger workflow.RunLedger, cluster string) []ApplyPhase {
	tasks := ledger.TasksForCluster(cluster)
	switch ApplyClusterKind(tasks) {
	case v1alpha1.KindStorageCluster:
		// One "Infrastructure" phase covers host standup and storage prep. The
		// previous "Prepare" phase was computed from the identical task filter, so
		// it could never differ — drop it rather than show two columns that always
		// move in lockstep. Consumption (e.g. a Data Foundation add-on attaching
		// the exported storage) runs inside the consuming cluster's add-on tasks,
		// so it reports under that cluster, not here.
		return []ApplyPhase{
			{Label: "Infrastructure", Status: applyPhaseStatus(filterApplyTasksByKind(tasks, workflow.ApplyTaskKindMachineInfraPrepare, workflow.ApplyTaskKindManagedMachineOS, workflow.ApplyTaskKindStorageInfra))},
			{Label: "Provision", Status: applyPhaseStatus(filterApplyTasksByKind(tasks, workflow.ApplyTaskKindStorageCluster))},
		}
	case v1alpha1.KindContainerCluster:
		return []ApplyPhase{
			{Label: "Infrastructure", Status: applyPhaseStatus(filterApplyTasksByKind(tasks, workflow.ApplyTaskKindMachineInfraPrepare, workflow.ApplyTaskKindClusterInstall, workflow.ApplyTaskKindMachineInfraFinalize))},
			{Label: "Prepare", Status: applyPhaseStatus(filterApplyTasksByKind(tasks, workflow.ApplyTaskKindClusterISO, workflow.ApplyTaskKindNodeBoot))},
			{Label: "Install", Status: applyPhaseStatus(filterApplyTasksByKind(tasks, workflow.ApplyTaskKindInstallWait))},
			{Label: "Post-install", Status: applyPhaseStatus(filterApplyTasksByKind(tasks, workflow.ApplyTaskKindClusterAddon, workflow.ApplyTaskKindNodeConfigApply))},
		}
	default:
		return []ApplyPhase{{Label: "Work", Status: applyPhaseStatus(tasks)}}
	}
}

// ApplyFailedPhase names the first phase of the task's cluster that is failed,
// blocked, or cancelled, so failure summaries point at a fleet phase instead of
// a raw task ID.
func ApplyFailedPhase(ledger workflow.RunLedger, task workflow.TaskLedgerEntry) string {
	for _, phase := range ApplyClusterPhases(ledger, task.Cluster) {
		switch phase.Status {
		case workflow.TaskStatusFailed, workflow.TaskStatusBlocked, workflow.TaskStatusCancelled:
			return phase.Label
		}
	}
	return "Work"
}

// ApplyProgressDone counts the run's tasks that reached a terminal state.
func ApplyProgressDone(ledger workflow.RunLedger) int {
	done := 0
	for _, task := range ledger.Tasks {
		switch task.Status {
		case workflow.TaskStatusOK, workflow.TaskStatusSkipped, workflow.TaskStatusFailed, workflow.TaskStatusBlocked, workflow.TaskStatusCancelled:
			done++
		}
	}
	return done
}

// ApplyFailureReason extracts the concise operator-facing reason from a task's
// recorded failure text.
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

// ApplyBlockingRoot walks a blocked task's dependencies breadth-first to the
// first failed ancestor (the true root cause), falling back to the nearest
// blocked/cancelled ancestor. This lets a transitively-blocked child task point
// at the host parent's failed install rather than at its own sibling.
func ApplyBlockingRoot(ledger workflow.RunLedger, task workflow.TaskLedgerEntry) (workflow.TaskLedgerEntry, bool) {
	visited := map[string]bool{}
	queue := append([]string(nil), task.Dependencies...)
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
			queue = append(queue, dep.Dependencies...)
		}
	}
	return fallback, haveFallback
}

func filterApplyTasksByKind(tasks []workflow.TaskLedgerEntry, kinds ...string) []workflow.TaskLedgerEntry {
	kindSet := map[string]bool{}
	for _, kind := range kinds {
		kindSet[kind] = true
	}
	var out []workflow.TaskLedgerEntry
	for _, task := range tasks {
		if kindSet[task.Kind] {
			out = append(out, task)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// applyPhaseStatus aggregates the phase's task statuses: any failure dominates,
// then blocked, then cancelled; a mix of pending and finished work reads as
// running; an all-skipped phase reads skipped.
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

// middleEllipsisDetail shortens an over-long single-line detail to limit runes by
// eliding the MIDDLE, so a trailing actionable clause (e.g. "rerun with --override
// to rebuild it") survives next to the leading description instead of being cut off
// by tail truncation. Rune-based so a multibyte character is never split.
func middleEllipsisDetail(value string, limit int) string {
	r := []rune(value)
	if len(r) <= limit {
		return value
	}
	const tail = 44
	head := limit - tail - 1 // room for the ellipsis rune
	if head < 1 {
		return string(r[:limit])
	}
	return string(r[:head]) + "…" + string(r[len(r)-tail:])
}
