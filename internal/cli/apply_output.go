package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

// applyReporter drives the live apply progress surface. It owns a single
// output.RunView fed from the run ledger (see applyRunFrame): the view redraws
// the step list in place on a TTY and emits append-only transition lines
// otherwise. The scheduler calls these methods from one goroutine, so the view
// needs no locking.
type applyReporter struct {
	stdout      io.Writer
	stderr      io.Writer
	contextName string
	runsDir     string
	clustersDir string
	view        *output.RunView
}

func newApplyReporter(stdout, stderr io.Writer, contextName string, runsDir string, clustersDir string, streamAnsible bool) *applyReporter {
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
		view:        view,
	}
}

func (r *applyReporter) RunStart(ledger workflow.RunLedger) {
	printApplyRunStart(r.stdout, r.contextName, r.runsDir, ledger)
}

func (r *applyReporter) StageSnapshot(ledger workflow.RunLedger) {
	r.view.Render(applyRunFrame(ledger))
}

func (r *applyReporter) RunSummary(ledger workflow.RunLedger) {
	r.view.Finish(applyRunFrame(ledger))
	printApplyRunSummary(r.stdout, r.clustersDir, ledger)
}

func (r *applyReporter) PromptGap() {
	output.NewContinuation(r.stderr).BlankLine()
}

// printApplyRunStart writes the run-identity fields under the already-open
// "Run" section (opened by the workflow reporter); it no longer opens its own
// section, so the run shows a single "Run" heading.
func printApplyRunStart(stdout io.Writer, contextName string, runsDir string, ledger workflow.RunLedger) {
	p := output.NewContinuation(stdout)
	fields := []output.Field{
		{Key: "ID", Value: ledger.RunID},
		{Key: "Target", Value: ledger.Target},
		{Key: "Tasks", Value: fmt.Sprintf("%d", len(ledger.Tasks))},
		{Key: "Parallelism", Value: fmt.Sprintf("%d tasks, %d per host, %d Redfish",
			ledger.Limits.Parallelism,
			ledger.Limits.ParallelismPerHost,
			ledger.Limits.ParallelismRedfish,
		)},
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

func printApplyRunSummary(stdout io.Writer, clustersDir string, ledger workflow.RunLedger) {
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
	lines := make([]output.TaskLine, 0, len(ledger.ClusterNames()))
	for _, cluster := range ledger.ClusterNames() {
		clusterStatus, clusterDetail := applyClusterLifecycleSummary(ledger, cluster)
		lines = append(lines, output.TaskLine{
			Status: clusterStatus,
			Label:  cluster,
			Detail: clusterDetail,
		})
	}
	p.Tasks(lines)
	p.Fields(applyClusterLogFields(clustersDir, ledger))
	printApplyFailureDetails(stdout, clustersDir, ledger)
}

// applyClusterLogFields lists, per cluster, the bootwright run log (and the
// OpenShift installer log for container clusters) so the Summary always points
// the operator at the on-disk logs — ansible output never reaches the terminal.
func applyClusterLogFields(clustersDir string, ledger workflow.RunLedger) []output.Field {
	var fields []output.Field
	for _, cluster := range ledger.ClusterNames() {
		tasks := ledger.TasksForCluster(cluster)
		fields = append(fields, output.Field{Key: cluster + " log", Value: workflow.ApplyClusterLogPath(clustersDir, ledger.RunID, cluster)})
		if applyClusterKind(tasks) == "ContainerCluster" {
			fields = append(fields, output.Field{Key: cluster + " installer log", Value: workflow.OpenShiftInstallerLogPath(clustersDir, cluster)})
		}
	}
	return fields
}

func printApplyFailureDetails(stdout io.Writer, clustersDir string, ledger workflow.RunLedger) {
	p := output.NewContinuation(stdout)
	for _, task := range ledger.FailedTasks() {
		fields := []output.Field{
			{Key: "failed task", Value: applyTaskDisplayLabel(task.Label)},
			{Key: "phase", Value: applyFailedPhase(ledger, task)},
			{Key: "reason", Value: applyFailureReason(task.Failure)},
		}
		if task.LogPath != "" {
			fields = append(fields, output.Field{Key: "log", Value: task.LogPath})
		}
		p.Details(fields)
	}
	for _, task := range ledger.BlockedTasks() {
		fields := []output.Field{
			{Key: "blocked task", Value: applyTaskDisplayLabel(task.Label)},
			{Key: "phase", Value: applyFailedPhase(ledger, task)},
			{Key: "reason", Value: applyBlockedReason(task)},
		}
		if task.ClusterLogPath != "" {
			fields = append(fields, output.Field{Key: "log", Value: task.ClusterLogPath})
		} else if task.LogPath != "" {
			fields = append(fields, output.Field{Key: "log", Value: task.LogPath})
		} else if task.Cluster != "" {
			fields = append(fields, output.Field{Key: "log", Value: workflow.ApplyClusterLogPath(clustersDir, ledger.RunID, task.Cluster)})
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
		return "Wait install " + strings.TrimPrefix(label, "wait install ")
	case strings.HasPrefix(label, "addon "):
		return "Addon " + strings.TrimPrefix(label, "addon ")
	case strings.HasPrefix(label, "storage attachment "):
		return "Storage attachment " + strings.TrimPrefix(label, "storage attachment ")
	case strings.HasPrefix(label, "storage "):
		return "Provision " + strings.TrimPrefix(label, "storage ")
	default:
		return label
	}
}
