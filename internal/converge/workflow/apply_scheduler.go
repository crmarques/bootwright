package workflow

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

func PrepareApplyTaskGraph(ctx context.Context, runsDir string, opts RunOptions, tasks []ApplyTask, limits ConcurrencyLimits) (PreparedApplyTaskGraph, error) {
	startedAt := time.Now()
	runID := applyRunID(startedAt)
	if strings.TrimSpace(opts.ClustersDir) == "" {
		return PreparedApplyTaskGraph{}, fmt.Errorf("clusters dir is required")
	}
	if strings.TrimSpace(opts.RenderedDir) == "" {
		return PreparedApplyTaskGraph{}, fmt.Errorf("rendered dir is required")
	}
	if strings.TrimSpace(opts.ManagedServicesDir) == "" {
		return PreparedApplyTaskGraph{}, fmt.Errorf("managed services dir is required")
	}
	if strings.TrimSpace(opts.ProviderStateDir) == "" {
		return PreparedApplyTaskGraph{}, fmt.Errorf("provider state dir is required")
	}
	if strings.TrimSpace(runsDir) == "" {
		return PreparedApplyTaskGraph{}, fmt.Errorf("runs dir is required")
	}
	limits = ResolveApplyConcurrencyLimits(limits, tasks)
	tasks = AnnotateApplyTaskClusterLogPaths(runsDir, runID, tasks)
	tasks, installedMatching, err := ReconcileApplyClusterInstallState(ctx, opts.ClustersDir, opts.ContextName, opts.SecretsDir, runID, opts.State, tasks, opts.ApplyMode, opts.OverrideAckedReinstalls, opts.ClusterAvailabilityChecker, startedAt)
	if err != nil {
		return PreparedApplyTaskGraph{}, err
	}
	if err := stampInstalledClusterConvergeRecords(runsDir, opts.ContextName, runID, tasks, installedMatching, startedAt); err != nil {
		return PreparedApplyTaskGraph{}, err
	}
	return PreparedApplyTaskGraph{
		RunID:     runID,
		StartedAt: startedAt,
		Tasks:     tasks,
		Limits:    limits,
	}, nil
}

func RunApplyTaskGraph(ctx context.Context, stdout io.Writer, stderr io.Writer, runsDir string, opts RunOptions, target ApplyTarget, clusterScope string, tasks []ApplyTask, limits ConcurrencyLimits, reporter ApplyReporter, runnerFactory ApplyTaskRunnerFactory) (RunLedger, error) {
	prepared, err := PrepareApplyTaskGraph(ctx, runsDir, opts, tasks, limits)
	if err != nil {
		return RunLedger{}, err
	}
	return RunPreparedApplyTaskGraph(ctx, stdout, stderr, runsDir, opts, target, clusterScope, prepared, reporter, runnerFactory)
}

type applyTaskExecutor func(ctx context.Context, stdout, stderr io.Writer, runsDir, runID string, opts RunOptions, task ApplyTask, runnerFactory ApplyTaskRunnerFactory) applyTaskResult

func RunPreparedApplyTaskGraph(ctx context.Context, streamOut io.Writer, streamErr io.Writer, runsDir string, opts RunOptions, target ApplyTarget, clusterScope string, prepared PreparedApplyTaskGraph, reporter ApplyReporter, runnerFactory ApplyTaskRunnerFactory) (RunLedger, error) {
	return runPreparedTaskGraph(ctx, streamOut, streamErr, runsDir, opts, target, clusterScope, prepared, reporter, runnerFactory, runOneApplyTask)
}

func RunPreparedDestroyTaskGraph(ctx context.Context, streamOut io.Writer, streamErr io.Writer, runsDir string, opts RunOptions, target ApplyTarget, clusterScope string, prepared PreparedApplyTaskGraph, reporter ApplyReporter, runnerFactory ApplyTaskRunnerFactory) (RunLedger, error) {
	return runPreparedTaskGraph(ctx, streamOut, streamErr, runsDir, opts, target, clusterScope, prepared, reporter, runnerFactory, runOneDestroyTask)
}

func runPreparedTaskGraph(ctx context.Context, streamOut io.Writer, streamErr io.Writer, runsDir string, opts RunOptions, target ApplyTarget, clusterScope string, prepared PreparedApplyTaskGraph, reporter ApplyReporter, runnerFactory ApplyTaskRunnerFactory, executor applyTaskExecutor) (RunLedger, error) {
	if strings.TrimSpace(opts.ClustersDir) == "" {
		return RunLedger{}, fmt.Errorf("clusters dir is required")
	}
	if strings.TrimSpace(opts.RenderedDir) == "" {
		return RunLedger{}, fmt.Errorf("rendered dir is required")
	}
	if strings.TrimSpace(opts.ManagedServicesDir) == "" {
		return RunLedger{}, fmt.Errorf("managed services dir is required")
	}
	if strings.TrimSpace(opts.ProviderStateDir) == "" {
		return RunLedger{}, fmt.Errorf("provider state dir is required")
	}
	if strings.TrimSpace(runsDir) == "" {
		return RunLedger{}, fmt.Errorf("runs dir is required")
	}
	if strings.TrimSpace(prepared.RunID) == "" {
		return RunLedger{}, fmt.Errorf("apply run ID is required")
	}
	if prepared.StartedAt.IsZero() {
		return RunLedger{}, fmt.Errorf("apply start time is required")
	}
	opts.RunsDir = runsDir
	tasks := prepared.Tasks
	limits := ResolveApplyConcurrencyLimits(prepared.Limits, tasks)
	runID := prepared.RunID
	startedAt := prepared.StartedAt
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ledger := NewRunLedger(runID, target.Name, clusterScope, limits, TaskLedgerEntries(tasks), startedAt)
	ledger.Machines = append([]string(nil), opts.SelectedMachines...)
	now := time.Now()
	lease := NewRunLease(runID, now)
	if err := AcquireRunLease(runsDir, lease, now); err != nil {
		return ledger, err
	}
	if err := SaveRunLedger(runsDir, ledger); err != nil {
		_ = RemoveRunLease(runsDir)
		return ledger, err
	}
	sweepStaleRuntimeSecrets(runsDir, runID)
	stopLeaseHeartbeat, leaseErrors := startRunLeaseHeartbeat(ctx, runsDir, lease)
	finishRun := func(status RunStatus) error {
		stopLeaseHeartbeat()
		ledger.Finish(status, time.Now())
		data, err := runLedgerBytes(ledger)
		if err != nil {
			return fmt.Errorf("encode apply ledger: %w", err)
		}
		if err := saveRunLedgerBytes(runsDir, data); err != nil {
			return err
		}
		if err := archiveRunLedgerBytes(runsDir, ledger.RunID, data); err != nil {
			return err
		}
		if err := RemoveRunLeaseIfOwner(runsDir, runID); err != nil && !isLeaseNotOwned(err) {
			return err
		}
		return nil
	}
	logs, err := openApplyLogs(runsDir, ledger)
	if err != nil {
		_ = finishRun(RunStatusFailed)
		return ledger, err
	}
	defer logs.Close()
	if reporter != nil {
		reporter.RunStart(ledger)
		reporter.StageSnapshot(ledger)
	}
	if len(tasks) == 0 {
		_ = finishRun(RunStatusOK)
		if reporter != nil {
			reporter.RunSummary(ledger)
		}
		return ledger, nil
	}

	taskByID := map[string]ApplyTask{}
	for _, task := range tasks {
		taskByID[task.Entry.ID] = task
	}
	parallelism := limits.Parallelism
	redfishLimit := limits.ParallelismRedfish
	perHostLimit := limits.ParallelismPerHost
	clusterInstallLimit := limits.ParallelismClusters
	events := make(chan applyTaskResult, len(tasks))
	started := map[string]bool{}
	runningResources := map[string]int{}
	runningHostSlots := map[string]int{}
	heldClusterInstalls := map[string]int{}
	grantedRedfish := map[string]int{}
	running := 0
	runningRedfish := 0
	completed := initiallyCompletedApplyTasks(tasks)
	clusterInitiated := map[string]bool{}
	clusterFinished := map[string]bool{}
	var fatalErr error
	var firstTaskErr error

	for completed < len(tasks) {
		startedAny := false
		releaseIdleClusterInstalls(ledger, tasks, started, heldClusterInstalls)
		slotAdmission := clusterInstallSlotAdmission(ledger)
		for _, task := range tasks {
			if fatalErr != nil || ctx.Err() != nil {
				break
			}
			if taskTerminal(task.Entry.Status) || started[task.Entry.ID] || !taskReady(ledger, task.Entry) {
				continue
			}
			ledger.MarkDependencyReady(task.Entry.ID, time.Now())
			if running >= parallelism {
				ledger.RecordBlockedOn(task.Entry.ID, applyTaskBudgetBlocker)
				continue
			}
			if blocker := taskDispatchBlocker(task, runningRedfish, redfishLimit, runningResources, runningHostSlots, perHostLimit, heldClusterInstalls, clusterInstallLimit, slotAdmission); blocker != "" {
				ledger.RecordBlockedOn(task.Entry.ID, blocker)
				continue
			}
			redfishSlots := taskRedfishGrant(task, runningRedfish, redfishLimit)
			taskToRun := task
			if redfishSlots > 0 && (task.Entry.Kind == ApplyTaskKindNodeBoot || task.Entry.Kind == ApplyTaskKindManagedMachineOS) {
				taskToRun.Forks = redfishSlots
			}
			started[task.Entry.ID] = true
			startedAny = true
			running++
			if redfishSlots > 0 {
				runningRedfish += redfishSlots
				grantedRedfish[task.Entry.ID] = redfishSlots
			}
			acquireTaskResources(task, runningResources)
			acquireTaskHostSlots(task, runningHostSlots)
			holdClusterInstall(task, heldClusterInstalls)
			logPath := TaskLogPath(runsDir, ledger.RunID, task.Entry)
			ledger.MarkReady(task.Entry.ID)
			ledger.MarkRunning(task.Entry.ID, logPath, time.Now())
			if cluster := task.Entry.Cluster; cluster != "" && !clusterInitiated[cluster] {
				clusterInitiated[cluster] = true
				logs.writeRunLine(applyClusterInitiatedLine(time.Now(), cluster, ApplyClusterLogPath(runsDir, ledger.RunID, cluster)))
			}
			if reporter != nil {
				reporter.StageSnapshot(ledger)
			}
			if opts.AskBecomePass {
				if reporter != nil {
					reporter.PromptGap()
				}
			}
			stdoutWriter, stderrWriter := applyTaskWriters(task, logs, streamOut, streamErr, opts.StreamAnsible)
			go func(task ApplyTask, taskOut, taskErr io.Writer) {
				events <- executor(ctx, taskOut, taskErr, runsDir, ledger.RunID, opts, task, runnerFactory)
			}(taskToRun, stdoutWriter, stderrWriter)
			if err := SaveRunLedger(runsDir, ledger); err != nil && fatalErr == nil {
				fatalErr = err
				cancel()
			}
		}
		if fatalErr != nil && running == 0 {
			break
		}
		if running == 0 && !startedAny {
			break
		}
		var event applyTaskResult
		select {
		case event = <-events:
		case err := <-leaseErrors:
			if err != nil && fatalErr == nil {
				fatalErr = err
				cancel()
			}
			continue
		}
		running--
		runningRedfish -= grantedRedfish[event.id]
		delete(grantedRedfish, event.id)
		releaseTaskResources(taskByID[event.id], runningResources)
		releaseTaskHostSlots(taskByID[event.id], runningHostSlots)
		if event.err != nil {
			failure := event.failure
			if failure == "" {
				failure = event.err.Error()
			}
			ledger.MarkFailed(event.id, failure, time.Now())
			if firstTaskErr == nil {
				firstTaskErr = event.err
			}
		} else if event.skipped {
			reason := event.skippedReason
			if reason == "" {
				reason = "no remote hosts matched task limit"
			}
			ledger.MarkSkipped(event.id, reason, time.Now())
		} else {
			ledger.MarkOK(event.id, time.Now())
		}
		saveErr := SaveRunLedger(runsDir, ledger)
		if saveErr != nil && fatalErr == nil {
			fatalErr = saveErr
			cancel()
		}
		completed = terminalApplyTasks(ledger.Tasks)
		flushFinishedClusterMarkers(logs, ledger, clusterInitiated, clusterFinished, time.Now())
		if reporter != nil {
			reporter.StageSnapshot(ledger)
		}
	}

	if fatalErr == nil {
		blocked := blockUnfinishedApplyTasks(&ledger, time.Now())
		if len(blocked) > 0 {
			progressErr := fmt.Errorf("apply task graph could not make progress; blocked task(s): %s", strings.Join(blocked, ", "))
			if saveErr := SaveRunLedger(runsDir, ledger); saveErr != nil {
				fatalErr = fmt.Errorf("%v; save apply ledger: %w", progressErr, saveErr)
			} else if firstTaskErr == nil {
				fatalErr = progressErr
			}
			if reporter != nil {
				reporter.StageSnapshot(ledger)
			}
		}
	}
	if fatalErr != nil {
		blockUnfinishedApplyTasks(&ledger, time.Now())
	}
	flushFinishedClusterMarkers(logs, ledger, clusterInitiated, clusterFinished, time.Now())
	failedStatus := RunStatusFailed
	if ctx.Err() != nil {
		failedStatus = RunStatusCancelled
	}
	if fatalErr != nil {
		finishRunReportingError(streamErr, finishRun, failedStatus)
		if reporter != nil {
			reporter.RunSummary(ledger)
		}
		return ledger, fatalErr
	}
	if firstTaskErr != nil {
		finishRunReportingError(streamErr, finishRun, failedStatus)
		if reporter != nil {
			reporter.RunSummary(ledger)
		}
		return ledger, firstTaskErr
	}
	if err := finishRun(RunStatusOK); err != nil {
		return ledger, err
	}
	if reporter != nil {
		reporter.RunSummary(ledger)
	}
	return ledger, nil
}

func finishRunReportingError(streamErr io.Writer, finishRun func(RunStatus) error, status RunStatus) {
	if err := finishRun(status); err != nil && streamErr != nil {
		fmt.Fprintf(streamErr, "warning: could not record the run's final %s status: %v; the run ledger may still show it as running\n", status, err)
	}
}

var (
	applyLeaseHeartbeatInterval = ApplyLeaseHeartbeatInterval
	saveRunLease                = SaveRunLeaseIfOwner
)

func startRunLeaseHeartbeat(ctx context.Context, runsDir string, lease RunLease) (func(), <-chan error) {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	errors := make(chan error, 1)
	go func() {
		defer close(done)
		ticker := time.NewTicker(applyLeaseHeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				lease.HeartbeatAt = now.UTC()
				if err := saveRunLease(runsDir, lease); err != nil {
					if isLeaseNotOwned(err) {
						errors <- fmt.Errorf("apply lease taken over by another run: %w", err)
						return
					}
					errors <- fmt.Errorf("refresh apply lease: %w", err)
					return
				}
			}
		}
	}()
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		cancel()
		<-done
	}
	return stop, errors
}

func blockUnfinishedApplyTasks(ledger *RunLedger, now time.Time) []string {
	blocked := []string{}
	for i := range ledger.Tasks {
		task := &ledger.Tasks[i]
		if taskTerminal(task.Status) {
			continue
		}
		task.Status = TaskStatusBlocked
		t := now.UTC()
		task.EndedAt = &t
		task.SkippedReason = blockedApplyTaskReason(*ledger, *task)
		blocked = append(blocked, task.ID)
	}
	sort.Strings(blocked)
	return blocked
}

const (
	applyTaskBudgetBlocker     = "task budget"
	ApplyClusterInstallBlocker = "cluster install budget"
)

func taskDispatchBlocker(task ApplyTask, runningRedfish, redfishLimit int, runningResources, runningHostSlots map[string]int, perHostLimit int, heldClusterInstalls map[string]int, clusterInstallLimit int, slotAdmission map[string]bool) string {
	if !taskRedfishAvailable(task, runningRedfish, redfishLimit) {
		return "redfish budget"
	}
	if key := busyTaskResourceKey(task, runningResources); key != "" {
		return "resource " + key
	}
	if !taskHostSlotAvailable(task, runningHostSlots, perHostLimit) {
		key, _ := taskHostSlot(task)
		return "host slot " + key
	}
	if !taskClusterInstallAvailable(task, heldClusterInstalls, clusterInstallLimit, slotAdmission) {
		return ApplyClusterInstallBlocker
	}
	return ""
}

func taskRedfishAvailable(task ApplyTask, runningRedfish, redfishLimit int) bool {
	if task.RedfishSlots < 1 {
		return true
	}
	return taskRedfishGrant(task, runningRedfish, redfishLimit) > 0
}

func taskRedfishGrant(task ApplyTask, runningRedfish, redfishLimit int) int {
	demand := taskRedfishSlots(task, redfishLimit)
	if demand < 1 {
		return 0
	}
	free := redfishLimit - runningRedfish
	if free < 1 {
		return 0
	}
	if demand > free {
		return free
	}
	return demand
}

func taskRedfishSlots(task ApplyTask, redfishLimit int) int {
	if task.RedfishSlots < 1 {
		return 0
	}
	slots := task.RedfishSlots
	if redfishLimit > 0 && slots > redfishLimit {
		return redfishLimit
	}
	return slots
}

func taskHostSlot(task ApplyTask) (string, int) {
	key := task.HostSlotKey
	if key == "" {
		key = task.Entry.HostSlotKey
	}
	if key == "" {
		return "", 0
	}
	count := task.HostSlotCount
	if count < 1 {
		count = task.Entry.HostSlotCount
	}
	if count < 1 {
		count = 1
	}
	return key, count
}

func taskHostSlotAvailable(task ApplyTask, running map[string]int, limit int) bool {
	key, count := taskHostSlot(task)
	if key == "" {
		return true
	}
	if limit < 1 {
		limit = 1
	}
	return running[key]+count <= limit
}

func taskClusterInstallAvailable(task ApplyTask, held map[string]int, limit int, slotAdmission map[string]bool) bool {
	cluster := clusterInstallKey(task.Entry)
	if cluster == "" {
		return true
	}
	if limit < 1 {
		limit = 1
	}
	if held[cluster] > 0 {
		return true
	}
	if slotAdmission != nil && !slotAdmission[cluster] {
		return false
	}
	return len(held) < limit
}

func clusterInstallSlotAdmission(ledger RunLedger) map[string]bool {
	candidates := map[string]bool{}
	unparked := map[string]bool{}
	for _, task := range ledger.Tasks {
		cluster := clusterInstallKey(task)
		if cluster == "" || taskTerminal(task.Status) || candidates[cluster] {
			continue
		}
		candidates[cluster] = true
		if !clusterInstallChainParked(ledger, cluster) {
			unparked[cluster] = true
		}
	}
	if len(unparked) > 0 {
		return unparked
	}
	return candidates
}

func clusterInstallChainParked(ledger RunLedger, cluster string) bool {
	for _, task := range ledger.Tasks {
		if clusterInstallKey(task) != cluster || taskTerminal(task.Status) {
			continue
		}
		if installChainWaitsOnAnotherCluster(ledger, task, cluster, map[string]bool{}) {
			return true
		}
	}
	return false
}

func installChainWaitsOnAnotherCluster(ledger RunLedger, task TaskLedgerEntry, cluster string, visiting map[string]bool) bool {
	if visiting[task.ID] {
		return false
	}
	visiting[task.ID] = true
	deps := append(append([]string(nil), task.Dependencies...), task.OrderingDependencies...)
	for _, dep := range deps {
		depTask, ok := ledger.Task(dep)
		if !ok || taskTerminal(depTask.Status) {
			continue
		}
		if depTask.Cluster != cluster && depTask.Cluster != "" {
			return true
		}
		if installChainWaitsOnAnotherCluster(ledger, depTask, cluster, visiting) {
			return true
		}
	}
	return false
}

func taskResourcesAvailable(task ApplyTask, running map[string]int) bool {
	return busyTaskResourceKey(task, running) == ""
}

func busyTaskResourceKey(task ApplyTask, running map[string]int) string {
	for _, key := range task.Entry.ResourceKeys {
		if key == "" {
			continue
		}
		if running[key] > 0 {
			return key
		}
	}
	return ""
}

func acquireTaskResources(task ApplyTask, running map[string]int) {
	for _, key := range task.Entry.ResourceKeys {
		if key != "" {
			running[key]++
		}
	}
}

func releaseTaskResources(task ApplyTask, running map[string]int) {
	for _, key := range task.Entry.ResourceKeys {
		if key == "" {
			continue
		}
		running[key]--
		if running[key] <= 0 {
			delete(running, key)
		}
	}
}

func acquireTaskHostSlots(task ApplyTask, running map[string]int) {
	key, count := taskHostSlot(task)
	if key == "" {
		return
	}
	running[key] += count
}

func releaseTaskHostSlots(task ApplyTask, running map[string]int) {
	key, count := taskHostSlot(task)
	if key == "" {
		return
	}
	running[key] -= count
	if running[key] <= 0 {
		delete(running, key)
	}
}

func holdClusterInstall(task ApplyTask, held map[string]int) {
	if cluster := clusterInstallKey(task.Entry); cluster != "" {
		held[cluster] = 1
	}
}

func releaseIdleClusterInstalls(ledger RunLedger, tasks []ApplyTask, started map[string]bool, held map[string]int) {
	if len(held) == 0 {
		return
	}
	inFlight := map[string]bool{}
	for _, task := range tasks {
		cluster := clusterInstallKey(task.Entry)
		if cluster == "" || inFlight[cluster] {
			continue
		}
		entry, ok := ledger.Task(task.Entry.ID)
		if !ok || taskTerminal(entry.Status) {
			continue
		}
		if started[entry.ID] || taskReady(ledger, entry) || clusterInstallChainAdvancing(ledger, entry) {
			inFlight[cluster] = true
		}
	}
	for cluster := range held {
		if !inFlight[cluster] {
			delete(held, cluster)
		}
	}
}

func clusterInstallChainAdvancing(ledger RunLedger, task TaskLedgerEntry) bool {
	return clusterInstallChainAdvancingFrom(ledger, task, map[string]bool{})
}

func clusterInstallChainAdvancingFrom(ledger RunLedger, task TaskLedgerEntry, visiting map[string]bool) bool {
	if visiting[task.ID] {
		return false
	}
	visiting[task.ID] = true
	defer delete(visiting, task.ID)
	advancing := false
	for _, dep := range task.Dependencies {
		depTask, ok := ledger.Task(dep)
		if !ok {
			return false
		}
		if depTask.Status == TaskStatusOK || depTask.Status == TaskStatusSkipped {
			continue
		}
		if !clusterInstallDependencyAdvancing(ledger, depTask, task.Cluster, visiting) {
			return false
		}
		advancing = true
	}
	for _, dep := range task.OrderingDependencies {
		depTask, ok := ledger.Task(dep)
		if !ok {
			return false
		}
		if taskTerminal(depTask.Status) {
			continue
		}
		if !clusterInstallDependencyAdvancing(ledger, depTask, task.Cluster, visiting) {
			return false
		}
		advancing = true
	}
	return advancing
}

func clusterInstallDependencyAdvancing(ledger RunLedger, dep TaskLedgerEntry, cluster string, visiting map[string]bool) bool {
	if taskTerminal(dep.Status) || dep.Cluster != cluster {
		return false
	}
	if dep.Status == TaskStatusReady || dep.Status == TaskStatusRunning {
		return true
	}
	return taskReady(ledger, dep) || clusterInstallChainAdvancingFrom(ledger, dep, visiting)
}

func TaskWaitingOnClusterInstallSlot(task TaskLedgerEntry) bool {
	if task.Status != TaskStatusPending || task.ReadyAt == nil {
		return false
	}
	for _, reason := range task.BlockedOn {
		if reason == ApplyClusterInstallBlocker {
			return true
		}
	}
	return false
}

func applyTaskWriters(task ApplyTask, logs *applyLogSet, streamOut, streamErr io.Writer, stream bool) (io.Writer, io.Writer) {
	writer := logs.Writer(task.Entry.Cluster)
	if !stream {
		return writer, writer
	}
	out := writer
	if streamOut != nil {
		out = io.MultiWriter(streamOut, writer)
	}
	err := writer
	if streamErr != nil {
		err = io.MultiWriter(streamErr, writer)
	}
	return out, err
}

func initiallyCompletedApplyTasks(tasks []ApplyTask) int {
	completed := 0
	for _, task := range tasks {
		if taskTerminal(task.Entry.Status) {
			completed++
		}
	}
	return completed
}

func terminalApplyTasks(tasks []TaskLedgerEntry) int {
	completed := 0
	for _, task := range tasks {
		if taskTerminal(task.Status) {
			completed++
		}
	}
	return completed
}

func TaskLedgerEntries(tasks []ApplyTask) []TaskLedgerEntry {
	entries := make([]TaskLedgerEntry, 0, len(tasks))
	for _, task := range tasks {
		entries = append(entries, task.Entry)
	}
	return entries
}

func AnnotateApplyTaskClusterLogPaths(runsDir, runID string, tasks []ApplyTask) []ApplyTask {
	out := make([]ApplyTask, len(tasks))
	copy(out, tasks)
	for i := range out {
		if out[i].Entry.Cluster != "" {
			out[i].Entry.ClusterLogPath = ApplyClusterLogPath(runsDir, runID, out[i].Entry.Cluster)
		}
	}
	return out
}

func taskReady(ledger RunLedger, task TaskLedgerEntry) bool {
	for _, dep := range task.Dependencies {
		depTask, ok := ledger.Task(dep)
		if !ok {
			return false
		}
		switch depTask.Status {
		case TaskStatusOK, TaskStatusSkipped:
			continue
		default:
			return false
		}
	}
	for _, dep := range task.OrderingDependencies {
		depTask, ok := ledger.Task(dep)
		if !ok {
			return false
		}
		if !taskTerminal(depTask.Status) {
			return false
		}
	}
	return true
}

func applyRunID(now time.Time) string {
	return "apply-" + now.UTC().Format("20060102T150405.000000000Z")
}
