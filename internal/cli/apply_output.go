package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/status"
)

func writtenTaskLogPath(path string) string {
	if path == "" {
		return ""
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() == 0 {
		return ""
	}
	return path
}

type applyReporter struct {
	stdout      io.Writer
	stderr      io.Writer
	contextName string
	runsDir     string
	clustersDir string
	displays    map[string]clusterDisplay
	view        *output.RunView
}

func newApplyReporter(stdout, stderr io.Writer, contextName string, runsDir string, clustersDir string, displays map[string]clusterDisplay, streamAnsible bool) *applyReporter {
	view := output.NewRunView(stdout)
	if streamAnsible {
		view.Streaming()
	}
	return &applyReporter{
		stdout:      stdout,
		stderr:      stderr,
		contextName: contextName,
		runsDir:     runsDir,
		clustersDir: clustersDir,
		displays:    displays,
		view:        view,
	}
}

func (r *applyReporter) RunStart(ledger workflow.RunLedger) {
	printApplyRunStart(r.stdout, r.contextName, r.runsDir, ledger)
}

func (r *applyReporter) StageSnapshot(ledger workflow.RunLedger) {
	r.view.Render(applyRunFrame(ledger, r.displays))
}

func (r *applyReporter) RunSummary(ledger workflow.RunLedger) {
	r.view.Finish(applyRunFrame(ledger, r.displays))
	printApplyRunSummary(r.stdout, r.runsDir, r.clustersDir, r.displays, ledger)
}

func (r *applyReporter) PromptGap() {
	output.NewContinuation(r.stderr).BlankLine()
}

func printApplyRunStart(stdout io.Writer, contextName string, runsDir string, ledger workflow.RunLedger) {
	p := output.NewContinuation(stdout)
	fields := []output.Field{
		{Key: "ID", Value: ledger.RunID},
		{Key: "Target", Value: ledger.Target},
		{Key: "Tasks", Value: fmt.Sprintf("%d", len(ledger.Tasks))},
		{Key: "Parallelism", Value: applyLimitsSummary(ledger.Limits)},
		{Key: "Run log", Value: workflow.ApplyRunLogPath(runsDir, ledger.RunID)},
	}
	if contextName != "" {
		fields = append([]output.Field{{Key: "Context", Value: contextName}}, fields...)
	}
	if ledger.Scope != "" {
		fields = append(fields, output.Field{Key: "Scope", Value: ledger.Scope})
	}
	p.Fields(fields)
}

func applyLimitsSummary(limits workflow.ConcurrencyLimits) string {
	return fmt.Sprintf("tasks %s, per host %s, Redfish %s",
		applyLimitValue(limits.Parallelism, limits.ParallelismUnbounded()),
		applyLimitValue(limits.ParallelismPerHost, limits.ParallelismPerHostUnbounded()),
		applyLimitValue(limits.ParallelismRedfish, limits.ParallelismRedfishUnbounded()),
	)
}

func applyLimitValue(value int, unbounded bool) string {
	if unbounded {
		return "unbounded"
	}
	return fmt.Sprintf("%d", value)
}

func printApplyRunSummary(stdout io.Writer, runsDir string, clustersDir string, displays map[string]clusterDisplay, ledger workflow.RunLedger) {
	p := output.NewContinuation(stdout)
	p.Section("Summary")
	status := output.StatusOK
	detail := "complete"
	switch ledger.Status {
	case workflow.RunStatusFailed:
		status = output.StatusFailed
		detail = "failed"
	case workflow.RunStatusRunning:
		status = output.StatusRunning
		detail = "running"
	case workflow.RunStatusCancelled:
		status = output.StatusCancel
		detail = "cancelled"
	}
	p.Status(status, ledger.Target+" apply", detail)
	names := orderClusterNames(ledger.ClusterNames(), displays)
	lines := make([]output.TaskLine, 0, len(names))
	for _, cluster := range names {
		clusterStatus, clusterDetail := applyClusterLifecycleSummary(ledger, cluster)
		lines = append(lines, output.TaskLine{
			Status: clusterStatus,
			Label:  clusterGroupTitle(cluster, displays, ""),
			Detail: clusterDetail,
		})
	}
	p.Tasks(lines)
	p.Fields(applyClusterLogFields(runsDir, clustersDir, ledger))
	printApplyFailureDetails(stdout, runsDir, clustersDir, ledger)
}

func applyClusterLogFields(runsDir string, clustersDir string, ledger workflow.RunLedger) []output.Field {
	var fields []output.Field
	for _, cluster := range orderClusterNames(ledger.ClusterNames(), nil) {
		tasks := ledger.TasksForCluster(cluster)
		fields = append(fields, output.Field{Key: cluster + " log", Value: workflow.ApplyClusterLogPath(runsDir, ledger.RunID, cluster)})
		if status.ApplyClusterKind(tasks) == "ContainerCluster" && !applyClusterFullyDone(tasks) {
			fields = append(fields, output.Field{Key: cluster + " installer log", Value: workflow.OpenShiftInstallerLogPath(clustersDir, cluster)})
		}
	}
	return fields
}

func applyClusterFullyDone(tasks []workflow.TaskLedgerEntry) bool {
	for _, task := range tasks {
		switch task.Status {
		case workflow.TaskStatusOK, workflow.TaskStatusSkipped:
			continue
		default:
			return false
		}
	}
	return true
}

func printApplyFailureDetails(stdout io.Writer, runsDir string, clustersDir string, ledger workflow.RunLedger) {
	p := output.NewContinuation(stdout)
	for _, task := range ledger.FailedTasks() {
		fields := []output.Field{
			{Key: "failed task", Value: applyTaskDisplayLabel(task.Label)},
			{Key: "phase", Value: status.ApplyFailedPhase(ledger, task)},
			{Key: "reason", Value: status.ApplyFailureReason(task.Failure)},
		}
		if logPath := writtenTaskLogPath(task.LogPath); logPath != "" {
			fields = append(fields, output.Field{Key: "task log", Value: logPath})
		}
		if task.Kind == workflow.ApplyTaskKindInstallWait && task.ClusterKind == workflow.ApplyClusterKindContainer && task.Cluster != "" {
			fields = append(fields, output.Field{Key: "installer log", Value: workflow.OpenShiftInstallerLogPath(clustersDir, task.Cluster)})
		}
		p.Details(fields)
	}
	for _, task := range ledger.BlockedTasks() {
		fields := []output.Field{
			{Key: "blocked task", Value: applyTaskDisplayLabel(task.Label)},
			{Key: "phase", Value: status.ApplyFailedPhase(ledger, task)},
			{Key: "reason", Value: applyBlockedReason(ledger, task)},
		}
		if task.ClusterLogPath != "" {
			fields = append(fields, output.Field{Key: "cluster log", Value: task.ClusterLogPath})
		} else if task.LogPath != "" {
			fields = append(fields, output.Field{Key: "cluster log", Value: task.LogPath})
		} else if task.Cluster != "" {
			fields = append(fields, output.Field{Key: "cluster log", Value: workflow.ApplyClusterLogPath(runsDir, ledger.RunID, task.Cluster)})
		}
		p.Details(fields)
	}
}

func applyTaskDisplayLabel(label string) string {
	switch {
	case label == "provider services" || strings.HasPrefix(label, "provider services "):
		return "Provider services" + strings.TrimPrefix(label, "provider services")
	case label == "infra component services" || strings.HasPrefix(label, "infra component services "):
		return "Infra component services" + strings.TrimPrefix(label, "infra component services")
	case strings.HasPrefix(label, "provision machine "):
		return "Provision machine " + strings.TrimPrefix(label, "provision machine ")
	case strings.HasPrefix(label, "finalize infra "):
		return "Finalize infra " + strings.TrimPrefix(label, "finalize infra ")
	case strings.HasPrefix(label, "machine infra "):
		return "Machine infra " + strings.TrimPrefix(label, "machine infra ")
	case strings.HasPrefix(label, "managed OS "):
		return "Managed OS " + strings.TrimPrefix(label, "managed OS ")
	case strings.HasPrefix(label, "infra "):
		return "Infra " + strings.TrimPrefix(label, "infra ")
	case strings.HasPrefix(label, "install "):
		return "Install " + strings.TrimPrefix(label, "install ")
	case strings.HasPrefix(label, "iso "):
		return "Create ISO " + strings.TrimPrefix(label, "iso ")
	case strings.HasPrefix(label, "boot "):
		return "Boot " + strings.TrimPrefix(label, "boot ")
	case strings.HasPrefix(label, "wait install "):
		return "Install " + strings.TrimPrefix(label, "wait install ")
	case strings.HasPrefix(label, "addon "):
		return "Install add-on " + strings.TrimPrefix(label, "addon ")
	case strings.HasPrefix(label, "storage "):
		return "Provision " + strings.TrimPrefix(label, "storage ")
	case strings.HasPrefix(label, "playbook "):
		return "Custom playbook " + strings.TrimPrefix(label, "playbook ")
	default:
		return label
	}
}
