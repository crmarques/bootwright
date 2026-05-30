package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

type applyReporter struct {
	stdout      io.Writer
	stderr      io.Writer
	clustersDir string
}

func newApplyReporter(stdout, stderr io.Writer, clustersDir string) *applyReporter {
	return &applyReporter{stdout: stdout, stderr: stderr, clustersDir: clustersDir}
}

func (r *applyReporter) RunStart(ledger workflow.RunLedger) {
	printApplyRunStart(r.stdout, ledger)
}

func (r *applyReporter) ClusterLogPaths(ledger workflow.RunLedger) {
	printApplyClusterLogPaths(r.stdout, r.clustersDir, ledger)
}

func (r *applyReporter) StageSnapshot(ledger workflow.RunLedger) {
	printApplyStageSnapshot(r.stdout, ledger)
}

func (r *applyReporter) AnsibleExecutionStart() {
	printApplyAnsibleExecutionStart(r.stdout)
}

func (r *applyReporter) TaskStart(ledger workflow.RunLedger, id string) {
	printApplyTaskStart(r.stdout, r.clustersDir, ledger, id)
}

func (r *applyReporter) TaskResult(ledger workflow.RunLedger, id string) {
	printApplyTaskResult(r.stdout, ledger, id)
}

func (r *applyReporter) RunSummary(ledger workflow.RunLedger) {
	printApplyRunSummary(r.stdout, ledger)
}

func (r *applyReporter) PromptGap() {
	output.NewContinuation(r.stderr).BlankLine()
}

func printApplyRunStart(stdout io.Writer, ledger workflow.RunLedger) {
	p := output.NewContinuation(stdout)
	p.Section("Apply run")
	fields := []output.Field{
		{Key: "Run", Value: ledger.RunID},
		{Key: "Target", Value: ledger.Target},
		{Key: "Tasks", Value: fmt.Sprintf("%d", len(ledger.Tasks))},
		{Key: "Parallelism", Value: fmt.Sprintf("%d task(s), %d per host, %d Redfish",
			ledger.Limits.Parallelism,
			ledger.Limits.ParallelismPerHost,
			ledger.Limits.ParallelismRedfish,
		)},
	}
	if ledger.Scope != "" {
		fields = append(fields, output.Field{Key: "Scope", Value: ledger.Scope})
	}
	p.Fields(fields)
	printApplyProgress(stdout, ledger)
}

func printApplyAnsibleExecutionStart(stdout io.Writer) {
	output.NewContinuation(stdout).Section("Ansible execution")
}

func printApplyClusterLogPaths(stdout io.Writer, clustersDir string, ledger workflow.RunLedger) {
	paths := applyClusterLogPaths(ledger)
	installerPaths := applyClusterInstallerLogPaths(clustersDir, ledger)
	if len(paths) == 0 && len(installerPaths) == 0 {
		return
	}
	p := output.NewContinuation(stdout)
	p.Section("Logs")
	fields := make([]output.Field, 0, len(paths)+len(installerPaths))
	for _, cluster := range ledger.ClusterNames() {
		if path := paths[cluster]; path != "" {
			fields = append(fields, output.Field{Key: cluster, Value: path})
		}
		if path := installerPaths[cluster]; path != "" {
			fields = append(fields, output.Field{Key: cluster + " installer log", Value: path})
		}
	}
	p.Fields(fields)
}

func printApplyTaskStart(stdout io.Writer, clustersDir string, ledger workflow.RunLedger, id string) {
	task, ok := ledger.Task(id)
	if !ok {
		return
	}
	output.NewContinuation(stdout).Tasks([]output.TaskLine{{
		Status: output.StatusRunning,
		Label:  applyTaskDisplayLabel(task.Label),
		Detail: applyTaskRunningDetail(clustersDir, task),
	}})
}

func printApplyTaskResult(stdout io.Writer, ledger workflow.RunLedger, id string) {
	task, ok := ledger.Task(id)
	if !ok {
		return
	}
	status := output.StatusOK
	detail := "complete"
	switch task.Status {
	case workflow.TaskStatusSkipped:
		status = output.StatusSkip
		detail = task.SkippedReason
	case workflow.TaskStatusFailed:
		status = output.StatusFail
		detail = task.Failure
	case workflow.TaskStatusBlocked:
		status = output.StatusSkip
		detail = task.SkippedReason
	}
	output.NewContinuation(stdout).Tasks([]output.TaskLine{{
		Status: status,
		Label:  applyTaskDisplayLabel(task.Label),
		Detail: detail,
	}})
	printApplyProgress(stdout, ledger)
}

func printApplyStageSnapshot(stdout io.Writer, ledger workflow.RunLedger) {
	output.NewContinuation(stdout).Tasks(applyStageLines(ledger))
}

func printApplyRunSummary(stdout io.Writer, ledger workflow.RunLedger) {
	p := output.NewContinuation(stdout)
	p.Section("Summary")
	status := output.StatusOK
	detail := "complete"
	switch ledger.Status {
	case workflow.RunStatusFailed:
		status = output.StatusFail
		detail = "failed"
	case workflow.RunStatusRunning:
		status = output.StatusRunning
		detail = "running"
	case workflow.RunStatusCancelled:
		status = output.StatusSkip
		detail = "cancelled"
	}
	p.Status(status, ledger.Target+" apply", detail)
	var lines []output.TaskLine
	for _, cluster := range ledger.ClusterNames() {
		clusterStatus, clusterDetail := applyClusterSummary(ledger.TasksForCluster(cluster))
		lines = append(lines, output.TaskLine{
			Status: clusterStatus,
			Label:  cluster,
			Detail: clusterDetail,
		})
	}
	p.Tasks(lines)
}

func printApplyProgress(stdout io.Writer, ledger workflow.RunLedger) {
	var fields []output.ProgressField
	for _, count := range ledger.ProgressCounts() {
		fields = append(fields, output.ProgressField{Label: string(count.Status), Count: count.Count})
	}
	output.NewContinuation(stdout).Progress("Progress", fields)
}

func applyClusterLogPaths(ledger workflow.RunLedger) map[string]string {
	paths := map[string]string{}
	for _, task := range ledger.Tasks {
		if task.Cluster != "" && task.ClusterLogPath != "" {
			paths[task.Cluster] = task.ClusterLogPath
		}
	}
	return paths
}

func applyClusterInstallerLogPaths(clustersDir string, ledger workflow.RunLedger) map[string]string {
	paths := map[string]string{}
	if strings.TrimSpace(clustersDir) == "" {
		return paths
	}
	for _, task := range ledger.Tasks {
		if task.Kind == workflow.ApplyTaskKindInstallWait && task.Cluster != "" {
			paths[task.Cluster] = workflow.OpenShiftInstallerLogPath(clustersDir, task.Cluster)
		}
	}
	return paths
}

func applyTaskRunningDetail(clustersDir string, task workflow.TaskLedgerEntry) string {
	if task.Kind == workflow.ApplyTaskKindInstallWait && task.Cluster != "" && strings.TrimSpace(clustersDir) != "" {
		return "running; installer log " + workflow.OpenShiftInstallerLogPath(clustersDir, task.Cluster)
	}
	return "running"
}

func applyStageLines(ledger workflow.RunLedger) []output.TaskLine {
	lines := []output.TaskLine{{
		Status: output.StatusDone,
		Label:  "Render inputs",
	}}
	if applyLedgerHasAnyKind(ledger, workflow.ApplyTaskKindClusterISO, workflow.ApplyTaskKindNodeBoot, workflow.ApplyTaskKindInstallWait) {
		lines = append(lines, output.TaskLine{Status: output.StatusDone, Label: "Resolve installer inputs"})
	}
	for _, stage := range []struct {
		label string
		kinds []string
	}{
		{label: "Provider services", kinds: []string{workflow.ApplyTaskKindProvider}},
		{label: "Cluster infrastructure", kinds: []string{workflow.ApplyTaskKindClusterInfra}},
		{label: "Provision storage", kinds: []string{workflow.ApplyTaskKindStorageCluster}},
		{label: "Create agent ISOs", kinds: []string{workflow.ApplyTaskKindClusterISO}},
		{label: "Boot nodes", kinds: []string{workflow.ApplyTaskKindNodeBoot}},
		{label: "Wait for installs", kinds: []string{workflow.ApplyTaskKindInstallWait}},
		{label: "Apply extensions", kinds: []string{workflow.ApplyTaskKindClusterExtensionApply}},
		{label: "Wait for extensions", kinds: []string{workflow.ApplyTaskKindClusterExtensionWait}},
		{label: "Apply storage bindings", kinds: []string{workflow.ApplyTaskKindStorageClusterBindingApply}},
	} {
		status, detail, ok := applyStageStatus(ledger, stage.kinds...)
		if ok {
			lines = append(lines, output.TaskLine{Status: status, Label: stage.label, Detail: detail})
		}
	}
	return lines
}

func applyLedgerHasAnyKind(ledger workflow.RunLedger, kinds ...string) bool {
	kindSet := map[string]bool{}
	for _, kind := range kinds {
		kindSet[kind] = true
	}
	for _, task := range ledger.Tasks {
		if kindSet[task.Kind] {
			return true
		}
	}
	return false
}

func applyStageStatus(ledger workflow.RunLedger, kinds ...string) (output.Status, string, bool) {
	kindSet := map[string]bool{}
	for _, kind := range kinds {
		kindSet[kind] = true
	}
	counts := map[workflow.TaskStatus]int{}
	total := 0
	for _, task := range ledger.Tasks {
		if !kindSet[task.Kind] {
			continue
		}
		total++
		counts[task.Status]++
	}
	if total == 0 {
		return output.StatusPending, "", false
	}
	done := counts[workflow.TaskStatusOK] + counts[workflow.TaskStatusSkipped]
	running := counts[workflow.TaskStatusRunning] + counts[workflow.TaskStatusReady]
	failed := counts[workflow.TaskStatusFailed] + counts[workflow.TaskStatusBlocked] + counts[workflow.TaskStatusCancelled]
	pending := counts[workflow.TaskStatusPending]
	detail := ""
	if total > 1 {
		parts := []string{fmt.Sprintf("%d/%d done", done, total)}
		if running > 0 {
			parts = append(parts, fmt.Sprintf("%d running", running))
		}
		if pending > 0 {
			parts = append(parts, fmt.Sprintf("%d pending", pending))
		}
		if failed > 0 {
			parts = append(parts, fmt.Sprintf("%d failed", failed))
		}
		detail = strings.Join(parts, ", ")
	}
	switch {
	case failed > 0:
		return output.StatusFail, detail, true
	case done == total:
		return output.StatusDone, detail, true
	case running > 0 || done > 0:
		return output.StatusRunning, detail, true
	default:
		return output.StatusPending, detail, true
	}
}

func applyTaskDisplayLabel(label string) string {
	switch {
	case label == "provider services" || strings.HasPrefix(label, "provider services "):
		return "Provider services" + strings.TrimPrefix(label, "provider services")
	case strings.HasPrefix(label, "infra "):
		return "Infra " + strings.TrimPrefix(label, "infra ")
	case strings.HasPrefix(label, "install "):
		return "Install " + strings.TrimPrefix(label, "install ")
	case strings.HasPrefix(label, "iso "):
		return "Create ISO " + strings.TrimPrefix(label, "iso ")
	case strings.HasPrefix(label, "boot "):
		return "Boot " + strings.TrimPrefix(label, "boot ")
	case strings.HasPrefix(label, "wait install "):
		return "Wait install " + strings.TrimPrefix(label, "wait install ")
	case strings.HasPrefix(label, "extension "):
		return "Extension " + strings.TrimPrefix(label, "extension ")
	default:
		return label
	}
}
