package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/status"
)

func printDestroySafety(stdout io.Writer, decision workflow.DestroySafetyDecision, authorized bool, dryRun bool) {
	if len(decision.Reasons) == 0 {
		return
	}
	message := decision.Summary()
	if authorized {
		output.NewContinuation(stdout).Warning("authorize "+authorizeProtected, message+"; --authorize "+authorizeProtected+" supplied for this command only")
		return
	}
	if dryRun {
		output.NewContinuation(stdout).Warning("destroy protection", message+"; a mutating destroy requires --authorize "+authorizeProtected)
	}
}

func destroyPurgeHistoryNotice(dryRun bool) string {
	lead := "on success this also deletes"
	if dryRun {
		lead = "a real run of this destroy would also delete"
	}
	return lead + " the destroyed component(s)' whole state tree under clusters/ — installer working directory, install records, kubeconfig, captured cluster secrets — and their per-run task/flow logs under runs/ (a fully successful unscoped destroy sweeps the whole run history, keeping only this destroy run's own record) — this history is not recoverable"
}

type destroyGateForecast struct {
	runScope            converge.Scope
	noRemoteWork        bool
	staleInput          bool
	unreadableRecords   bool
	sharedInfra         bool
	installedNode       bool
	protected           bool
	protectedReason     string
	dataLoss            bool
	dataLossReason      string
	unreadableRecordDir string
}

func destroyRequiredAuthorizations(auth *authorizations, gates destroyGateForecast) []requiredAuthorization {
	forecast := newAuthorizationForecast(auth)
	forecast.consult(authorizeStaleInput, gates.staleInput, "input document(s) of this context no longer decode or validate against this build, so a teardown can only be planned by skipping exactly those documents")
	forecast.consult(authorizeUnreadableRecords, gates.unreadableRecords, "ownership record(s) under "+gates.unreadableRecordDir+" could not be read, so their resources would be left standing")
	forecast.consult(authorizeSharedInfra, gates.sharedInfra, "the selected teardown crosses a storage-consumer or cross-context infra-component boundary")
	forecast.consult(authorizeInstalledClusterNode, gates.installedNode, "a selected machine is a node of an installed cluster")
	forecast.consult(authorizeProtected, gates.protected, gates.protectedReason)
	forecast.consult(authorizeDataLoss, gates.dataLoss, gates.dataLossReason)
	forecast.mayConsult(authorizeUnownedVMs, converge.ScopeTearsMachineLayer(gates.runScope), "this run tears the machine layer down; whether a VM matching the Bootwright naming carries no ownership marker is decided on the provider, so a preview cannot settle it")
	forecast.mayConsult(authorizeUnownedNetworks, converge.ScopeTearsMachineLayer(gates.runScope), "this run tears the machine layer down; whether a libvirt network, KubeVirt DataVolume, or PersistentVolumeClaim is unowned is decided on the provider, so a preview cannot settle it")
	forecast.mayConsult(authorizeUnownedDevices, converge.ScopeTearsClusterLayer(gates.runScope), "this run tears the cluster layer down; whether a declared OSD device carries signatures or holders without a Bootwright OSD ownership record is decided on the node, so a preview cannot settle it")
	forecast.mayConsult(authorizeUnreachableNodes, !gates.noRemoteWork, "whether a selected node proves unreachable is decided when the run contacts it, so a preview cannot settle it")
	return forecast.list()
}

type destroyReporter struct {
	stdout  io.Writer
	stderr  io.Writer
	runsDir string
	view    *output.RunView
}

func newDestroyReporter(stdout, stderr io.Writer, runsDir string, streamAnsible bool) *destroyReporter {
	view := output.NewRunView(stdout)
	if streamAnsible {
		view.Streaming()
	}
	return &destroyReporter{stdout: stdout, stderr: stderr, runsDir: runsDir, view: view}
}

func (r *destroyReporter) RunStart(ledger workflow.RunLedger) {
	printDestroyRunStart(r.stdout, r.runsDir, ledger)
}

func (r *destroyReporter) StageSnapshot(ledger workflow.RunLedger) {
	r.view.Render(destroyRunFrame(ledger))
}

func (r *destroyReporter) RunSummary(ledger workflow.RunLedger) {
	r.view.Finish(destroyRunFrame(ledger))
	printDestroyRunSummary(r.stdout, r.runsDir, ledger)
}

func (r *destroyReporter) PromptGap() {
	output.NewContinuation(r.stderr).BlankLine()
}

func destroyRunFrame(ledger workflow.RunLedger) output.RunFrame {
	return newRunFrame("Teardown", destroyStepGroups(ledger))
}

func destroyPlanGroups(tasks []workflow.TaskLedgerEntry) []output.StepGroup {
	return destroyStepGroups(workflow.RunLedger{Tasks: tasks})
}

func destroyStepGroups(ledger workflow.RunLedger) []output.StepGroup {
	steps := runPhaseSteps("", ledger.Tasks, ledger, phaseDetailOptions{resources: true})
	if len(steps) == 0 {
		return nil
	}
	return []output.StepGroup{{Steps: steps}}
}

func printDestroyRunStart(stdout io.Writer, runsDir string, ledger workflow.RunLedger) {
	output.NewContinuation(stdout).Fields([]output.Field{
		{Key: "ID", Value: ledger.RunID},
		{Key: "Target", Value: ledger.Target},
		{Key: "Tasks", Value: fmt.Sprintf("%d", len(ledger.Tasks))},
		{Key: "Run log", Value: workflow.ApplyRunLogPath(runsDir, ledger.RunID)},
	})
}

func printDestroyRunSummary(stdout io.Writer, runsDir string, ledger workflow.RunLedger) {
	p := output.NewContinuation(stdout)
	p.Section("Summary")
	summaryStatus := output.StatusOK
	detail := "complete"
	switch ledger.Status {
	case workflow.RunStatusFailed:
		summaryStatus = output.StatusFailed
		detail = "failed"
	case workflow.RunStatusCancelled:
		summaryStatus = output.StatusCancel
		detail = "cancelled"
	}
	p.Status(summaryStatus, ledger.Target, detail)
	p.Fields([]output.Field{{Key: "Run log", Value: workflow.ApplyRunLogPath(runsDir, ledger.RunID)}})
	for _, task := range ledger.FailedTasks() {
		fields := []output.Field{
			{Key: "failed task", Value: task.Label},
		}
		if covered := strings.Join(workflow.DestroyTaskClusterKeys(task), ", "); covered != "" {
			fields = append(fields, output.Field{Key: "clusters", Value: covered})
		}
		fields = append(fields, output.Field{Key: "reason", Value: status.ApplyFailureReason(task.Failure)})
		if logPath := writtenTaskLogPath(task.LogPath); logPath != "" {
			fields = append(fields, output.Field{Key: "log", Value: logPath})
		}
		p.Details(fields)
	}
	for _, task := range ledger.BlockedTasks() {
		fields := []output.Field{{Key: "blocked task", Value: task.Label}}
		if covered := strings.Join(workflow.DestroyTaskClusterKeys(task), ", "); covered != "" {
			fields = append(fields, output.Field{Key: "clusters", Value: covered})
		}
		fields = append(fields, output.Field{Key: "reason", Value: applyBlockedReason(ledger, task)})
		p.Details(fields)
	}
}
