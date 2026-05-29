package cli

import (
	"fmt"
	"strings"
	"time"

	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/workflow"
)

func printApplyLedgerStatus(p *cliout.Printer, runsDir string, ledger workflow.RunLedger, found bool, loadErr error) {
	p.Section("Current apply")
	if loadErr != nil {
		p.Status(cliout.StatusWarn, "Apply ledger", loadErr.Error())
		return
	}
	if !found {
		p.Status(cliout.StatusSkip, "Apply run", "no run ledger found")
		return
	}
	fields := []cliout.Field{
		{Key: "Run", Value: ledger.RunID},
		{Key: "Target", Value: ledger.Target},
		{Key: "Status", Value: string(ledger.Status)},
		{Key: "Started", Value: ledger.StartedAt.Format(time.RFC3339)},
	}
	if ledger.Scope != "" {
		fields = append(fields, cliout.Field{Key: "Scope", Value: ledger.Scope})
	}
	p.Fields(fields)
	if ledger.EndedAt != nil {
		p.Fields([]cliout.Field{{Key: "Ended", Value: ledger.EndedAt.Format(time.RFC3339)}})
	}
	if ledger.Active() {
		printApplyRunActivity(p, runsDir, ledger)
	}
	p.Section("Progress")
	p.Progress("Tasks", progressFields(ledger))
	printApplyLedgerRunning(p, ledger)
	printApplyLedgerClusters(p, ledger)
	printApplyLedgerBlocked(p, ledger)
	printApplyLedgerFailures(p, ledger)
}

func printApplyRunActivity(p *cliout.Printer, runsDir string, ledger workflow.RunLedger) {
	activity, err := workflow.AssessRunActivity(runsDir, ledger, time.Now())
	if err != nil {
		p.Status(cliout.StatusWarn, "Lease", err.Error())
		return
	}
	switch activity.State {
	case workflow.RunActivityActive:
		detail := activity.Detail
		if activity.Lease != nil {
			detail = fmt.Sprintf("%s; pid %d heartbeat %s", detail, activity.Lease.PID, activity.Lease.HeartbeatAt.Format(time.RFC3339))
		}
		p.Status(cliout.StatusRunning, "Lease", detail)
	case workflow.RunActivityStale:
		p.Status(cliout.StatusWarn, "Lease", activity.Detail+"; next apply or destroy will mark it cancelled")
	}
}

func printApplyLedgerRunning(p *cliout.Printer, ledger workflow.RunLedger) {
	running := ledger.RunningTasks()
	if len(running) == 0 {
		return
	}
	p.Section("Running work")
	lines := make([]cliout.TaskLine, 0, len(running))
	for _, task := range running {
		detail := task.Kind
		if task.LogPath != "" {
			detail += "; log " + task.LogPath
		}
		lines = append(lines, cliout.TaskLine{Status: cliout.StatusWarn, Label: applyTaskDisplayLabel(task.Label), Detail: detail})
	}
	p.Tasks(lines)
}

func printApplyLedgerClusters(p *cliout.Printer, ledger workflow.RunLedger) {
	names := ledger.ClusterNames()
	if len(names) == 0 {
		return
	}
	p.Section("Apply clusters")
	var items []cliout.TaskLine
	for _, name := range names {
		tasks := ledger.TasksForCluster(name)
		status, detail := applyClusterSummary(tasks)
		items = append(items, cliout.TaskLine{Status: status, Label: name, Detail: detail})
	}
	p.Tasks(items)
}

func applyClusterSummary(tasks []workflow.TaskLedgerEntry) (cliout.Status, string) {
	status := cliout.StatusOK
	kindStatuses := map[string]workflow.TaskStatus{}
	var otherParts []string
	nodeCounts := map[workflow.TaskStatus]int{}
	nodeTotal := 0
	for _, task := range tasks {
		status = applyLedgerTaskStatus(status, task.Status)
		if task.Kind == workflow.ApplyTaskKindNodeBoot {
			nodeTotal++
			nodeCounts[task.Status]++
			continue
		}
		if applyLedgerKnownClusterKind(task.Kind) {
			kindStatuses[task.Kind] = task.Status
			continue
		}
		otherParts = append(otherParts, applyLedgerTaskKindLabel(task.Kind)+" "+string(task.Status))
	}
	parts := make([]string, 0, len(tasks))
	for _, kind := range []string{workflow.ApplyTaskKindClusterInfra, workflow.ApplyTaskKindClusterISO} {
		if taskStatus, ok := kindStatuses[kind]; ok {
			parts = append(parts, applyLedgerTaskKindLabel(kind)+" "+string(taskStatus))
		}
	}
	if nodeTotal > 0 {
		parts = append(parts, applyNodeBootSummary(nodeTotal, nodeCounts))
	}
	if taskStatus, ok := kindStatuses[workflow.ApplyTaskKindInstallWait]; ok {
		parts = append(parts, applyLedgerTaskKindLabel(workflow.ApplyTaskKindInstallWait)+" "+string(taskStatus))
	}
	if taskStatus, ok := kindStatuses[workflow.ApplyTaskKindClusterExtensionApply]; ok {
		parts = append(parts, applyLedgerTaskKindLabel(workflow.ApplyTaskKindClusterExtensionApply)+" "+string(taskStatus))
	}
	if taskStatus, ok := kindStatuses[workflow.ApplyTaskKindClusterExtensionWait]; ok {
		parts = append(parts, applyLedgerTaskKindLabel(workflow.ApplyTaskKindClusterExtensionWait)+" "+string(taskStatus))
	}
	parts = append(parts, otherParts...)
	return status, strings.Join(parts, ", ")
}

func applyLedgerKnownClusterKind(kind string) bool {
	switch kind {
	case workflow.ApplyTaskKindClusterInfra, workflow.ApplyTaskKindClusterISO, workflow.ApplyTaskKindInstallWait,
		workflow.ApplyTaskKindClusterExtensionApply, workflow.ApplyTaskKindClusterExtensionWait:
		return true
	default:
		return false
	}
}

func applyLedgerTaskKindLabel(kind string) string {
	switch kind {
	case workflow.ApplyTaskKindClusterInfra:
		return "infra"
	case workflow.ApplyTaskKindClusterISO:
		return "ISO"
	case workflow.ApplyTaskKindInstallWait:
		return "install wait"
	case workflow.ApplyTaskKindClusterExtensionApply:
		return "extension apply"
	case workflow.ApplyTaskKindClusterExtensionWait:
		return "extension wait"
	default:
		return kind
	}
}

func applyNodeBootSummary(total int, counts map[workflow.TaskStatus]int) string {
	parts := []string{fmt.Sprintf("%d/%d boot stages done", counts[workflow.TaskStatusOK], total)}
	for _, status := range []workflow.TaskStatus{
		workflow.TaskStatusRunning,
		workflow.TaskStatusReady,
		workflow.TaskStatusPending,
		workflow.TaskStatusBlocked,
		workflow.TaskStatusFailed,
		workflow.TaskStatusSkipped,
		workflow.TaskStatusCancelled,
	} {
		if counts[status] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[status], status))
		}
	}
	return strings.Join(parts, ", ")
}

func applyLedgerTaskStatus(current cliout.Status, taskStatus workflow.TaskStatus) cliout.Status {
	next := cliout.StatusOK
	switch taskStatus {
	case workflow.TaskStatusFailed:
		next = cliout.StatusFail
	case workflow.TaskStatusRunning, workflow.TaskStatusReady:
		next = cliout.StatusWarn
	case workflow.TaskStatusPending, workflow.TaskStatusBlocked, workflow.TaskStatusSkipped, workflow.TaskStatusCancelled:
		next = cliout.StatusSkip
	}
	if applyStatusPriority(next) > applyStatusPriority(current) {
		return next
	}
	return current
}

func applyStatusPriority(status cliout.Status) int {
	switch status {
	case cliout.StatusFail:
		return 3
	case cliout.StatusWarn:
		return 2
	case cliout.StatusSkip:
		return 1
	default:
		return 0
	}
}

func printApplyLedgerBlocked(p *cliout.Printer, ledger workflow.RunLedger) {
	blocked := ledger.BlockedTasks()
	if len(blocked) == 0 {
		return
	}
	p.Section("Blocked work")
	lines := make([]cliout.TaskLine, 0, len(blocked))
	for _, task := range blocked {
		lines = append(lines, cliout.TaskLine{Status: cliout.StatusSkip, Label: applyTaskDisplayLabel(task.Label), Detail: task.SkippedReason})
	}
	p.Tasks(lines)
}

func printApplyLedgerFailures(p *cliout.Printer, ledger workflow.RunLedger) {
	failed := ledger.FailedTasks()
	if len(failed) == 0 {
		return
	}
	p.Section("Failures")
	lines := make([]cliout.TaskLine, 0, len(failed))
	for _, task := range failed {
		detail := task.Failure
		if task.LogPath != "" {
			detail = strings.TrimSpace(detail + "; log " + task.LogPath)
		}
		lines = append(lines, cliout.TaskLine{Status: cliout.StatusFail, Label: applyTaskDisplayLabel(task.Label), Detail: detail})
	}
	p.Tasks(lines)
}

func progressFields(ledger workflow.RunLedger) []cliout.ProgressField {
	fields := make([]cliout.ProgressField, 0, len(ledger.ProgressCounts()))
	for _, count := range ledger.ProgressCounts() {
		fields = append(fields, cliout.ProgressField{Label: string(count.Status), Count: count.Count})
	}
	return fields
}

func ledgerNextSteps(ledger workflow.RunLedger, activity workflow.RunActivity, existing []string) []string {
	switch ledger.Status {
	case workflow.RunStatusRunning:
		if activity.State == workflow.RunActivityStale {
			return append([]string{fmt.Sprintf("bootwright apply %s --yes", ledger.Target)}, existing...)
		}
		return append([]string{"bootwright status --watch"}, existing...)
	case workflow.RunStatusFailed:
		hints := []string{fmt.Sprintf("bootwright apply %s --yes", ledger.Target)}
		for _, task := range ledger.FailedTasks() {
			if task.LogPath != "" {
				hints = append(hints, "inspect "+task.LogPath)
			}
		}
		return append(hints, existing...)
	default:
		return existing
	}
}
