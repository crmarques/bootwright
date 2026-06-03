package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func applyClusterPhaseLines(clustersDir string, ledger workflow.RunLedger) []output.ClusterPhaseLine {
	names := ledger.ClusterNames()
	lines := make([]output.ClusterPhaseLine, 0, len(names))
	for _, name := range names {
		tasks := ledger.TasksForCluster(name)
		kind := applyClusterKind(tasks)
		fields := []output.Field{{Key: "Bootwright log", Value: workflow.ApplyClusterLogPath(clustersDir, ledger.RunID, name)}}
		if kind == "ContainerCluster" {
			fields = append(fields, output.Field{Key: "Installation log", Value: workflow.OpenShiftInstallerLogPath(clustersDir, name)})
		}
		lines = append(lines, output.ClusterPhaseLine{
			Name:   name,
			Kind:   kind,
			Fields: fields,
			Phases: applyClusterPhases(ledger, name, kind, tasks),
		})
	}
	return lines
}

func applyClusterLifecycleSummary(ledger workflow.RunLedger, cluster string) (output.Status, string) {
	tasks := ledger.TasksForCluster(cluster)
	kind := applyClusterKind(tasks)
	phases := applyClusterPhases(ledger, cluster, kind, tasks)
	if len(phases) == 0 {
		return output.StatusOK, ""
	}
	status := output.StatusOK
	parts := make([]string, 0, len(phases))
	for _, phase := range phases {
		status = applyOutputStatus(status, phase.Status)
		parts = append(parts, phase.Label+" "+string(phase.Status))
	}
	return status, strings.Join(parts, ", ")
}

func applyClusterKind(tasks []workflow.TaskLedgerEntry) string {
	kind := ""
	for _, task := range tasks {
		switch task.ClusterKind {
		case workflow.ApplyClusterKindContainer:
			return "ContainerCluster"
		case workflow.ApplyClusterKindStorage:
			kind = "StorageCluster"
		}
	}
	if kind != "" {
		return kind
	}
	return "Cluster"
}

func applyClusterPhases(ledger workflow.RunLedger, cluster string, kind string, tasks []workflow.TaskLedgerEntry) []output.PhaseStatus {
	switch kind {
	case "StorageCluster":
		storageInfraTasks := filterApplyTasksByKind(tasks, workflow.ApplyTaskKindStorageInfra)
		storageTasks := filterApplyTasksByKind(tasks, workflow.ApplyTaskKindStorageCluster)
		storageInfraStatus := output.StatusOK
		if len(storageInfraTasks) > 0 {
			storageInfraStatus = applyPhaseStatus(storageInfraTasks, output.StatusPending)
		}
		return []output.PhaseStatus{
			{Label: "Infrastructure", Status: storageInfraStatus},
			{Label: "Prepare", Status: storageInfraStatus},
			{Label: "Provision", Status: applyPhaseStatus(storageTasks, output.StatusPending)},
			{Label: "Publish", Status: applyPhaseStatus(applyStoragePublishTasks(ledger, cluster), output.StatusPending)},
		}
	case "ContainerCluster":
		return []output.PhaseStatus{
			{Label: "Infrastructure", Status: applyPhaseStatus(filterApplyTasksByKind(tasks, workflow.ApplyTaskKindClusterInfra), output.StatusPending)},
			{Label: "Prepare", Status: applyPhaseStatus(filterApplyTasksByKind(tasks, workflow.ApplyTaskKindClusterISO, workflow.ApplyTaskKindNodeBoot), output.StatusPending)},
			{Label: "Install", Status: applyPhaseStatus(filterApplyTasksByKind(tasks, workflow.ApplyTaskKindInstallWait), output.StatusPending)},
			{Label: "Post-install", Status: applyPhaseStatus(filterApplyTasksByKind(tasks, workflow.ApplyTaskKindClusterAddonApply, workflow.ApplyTaskKindClusterAddonWait, workflow.ApplyTaskKindStorageAttachmentApply), output.StatusPending)},
		}
	default:
		return []output.PhaseStatus{{Label: "Work", Status: applyPhaseStatus(tasks, output.StatusPending)}}
	}
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

func applyStoragePublishTasks(ledger workflow.RunLedger, cluster string) []workflow.TaskLedgerEntry {
	dependency := "storage." + cluster
	var out []workflow.TaskLedgerEntry
	for _, task := range ledger.Tasks {
		if task.Kind != workflow.ApplyTaskKindStorageAttachmentApply {
			continue
		}
		for _, dep := range task.Dependencies {
			if dep == dependency {
				out = append(out, task)
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func applyPhaseStatus(tasks []workflow.TaskLedgerEntry, defaultStatus output.Status) output.Status {
	if len(tasks) == 0 {
		return defaultStatus
	}
	counts := map[workflow.TaskStatus]int{}
	for _, task := range tasks {
		counts[task.Status]++
	}
	switch {
	case counts[workflow.TaskStatusFailed] > 0:
		return output.StatusFailed
	case counts[workflow.TaskStatusBlocked] > 0:
		return output.StatusBlocked
	case counts[workflow.TaskStatusCancelled] > 0:
		return output.StatusCancel
	case counts[workflow.TaskStatusRunning]+counts[workflow.TaskStatusReady] > 0:
		return output.StatusRunning
	case counts[workflow.TaskStatusPending] > 0:
		if counts[workflow.TaskStatusOK]+counts[workflow.TaskStatusSkipped] > 0 {
			return output.StatusRunning
		}
		return output.StatusPending
	case counts[workflow.TaskStatusSkipped] == len(tasks):
		return output.StatusSkipped
	default:
		return output.StatusOK
	}
}

func applyOutputStatus(current output.Status, next output.Status) output.Status {
	if applyOutputStatusPriority(next) > applyOutputStatusPriority(current) {
		return next
	}
	return current
}

func applyOutputStatusPriority(status output.Status) int {
	switch status {
	case output.StatusFailed, output.StatusFail:
		return 6
	case output.StatusBlocked:
		return 5
	case output.StatusCancel:
		return 4
	case output.StatusRunning, output.StatusWarn:
		return 3
	case output.StatusPending:
		return 2
	case output.StatusSkipped, output.StatusSkip:
		return 1
	default:
		return 0
	}
}

func applyProgressDone(ledger workflow.RunLedger) int {
	done := 0
	for _, task := range ledger.Tasks {
		switch task.Status {
		case workflow.TaskStatusOK, workflow.TaskStatusSkipped, workflow.TaskStatusFailed, workflow.TaskStatusBlocked, workflow.TaskStatusCancelled:
			done++
		}
	}
	return done
}

func applyProgressFields(ledger workflow.RunLedger) []output.ProgressField {
	fields := make([]output.ProgressField, 0, len(ledger.ProgressCounts()))
	for _, count := range ledger.ProgressCounts() {
		fields = append(fields, output.ProgressField{Label: string(count.Status), Count: count.Count})
	}
	return fields
}

func applyTaskRunDetail(clustersDir string, task workflow.TaskLedgerEntry) string {
	switch {
	case task.Kind == workflow.ApplyTaskKindInstallWait && task.Cluster != "":
		return "installer log " + workflow.OpenShiftInstallerLogPath(clustersDir, task.Cluster)
	case task.ClusterLogPath != "":
		return "log " + task.ClusterLogPath
	case task.LogPath != "":
		return "log " + task.LogPath
	default:
		return task.Kind
	}
}

func applyFailureReason(failure string) string {
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

func trimApplyDetail(value string) string {
	const limit = 180
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit-3] + "..."
}

func applyFailedPhase(ledger workflow.RunLedger, task workflow.TaskLedgerEntry) string {
	for _, phase := range applyClusterPhases(ledger, task.Cluster, applyClusterKind(ledger.TasksForCluster(task.Cluster)), ledger.TasksForCluster(task.Cluster)) {
		if phase.Status == output.StatusFailed || phase.Status == output.StatusBlocked || phase.Status == output.StatusCancel {
			return phase.Label
		}
	}
	return "Work"
}

func applyBlockedReason(task workflow.TaskLedgerEntry) string {
	if task.SkippedReason != "" {
		return task.SkippedReason
	}
	return fmt.Sprintf("%s %s", task.Kind, task.Status)
}
