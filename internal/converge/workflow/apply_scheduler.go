package workflow

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
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
	opts.RunsDir = runsDir
	limits = ResolveApplyConcurrencyLimits(limits, tasks)
	tasks = AnnotateApplyTaskClusterLogPaths(opts.ClustersDir, runID, tasks)
	var err error
	tasks, err = ReconcileApplyClusterInstallState(ctx, opts.ClustersDir, opts.ContextName, opts.SecretsDir, runID, opts.State, tasks, opts.InstallOverride, opts.ClusterAvailabilityChecker, startedAt)
	if err != nil {
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

func RunPreparedApplyTaskGraph(ctx context.Context, _ io.Writer, _ io.Writer, runsDir string, opts RunOptions, target ApplyTarget, clusterScope string, prepared PreparedApplyTaskGraph, reporter ApplyReporter, runnerFactory ApplyTaskRunnerFactory) (RunLedger, error) {
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
	if err := SaveRunLedger(runsDir, ledger); err != nil {
		return ledger, err
	}
	lease := NewRunLease(runID, startedAt)
	if err := SaveRunLease(runsDir, lease); err != nil {
		ledger.Finish(RunStatusFailed, time.Now())
		_ = SaveRunLedger(runsDir, ledger)
		return ledger, err
	}
	stopLeaseHeartbeat, leaseErrors := startRunLeaseHeartbeat(ctx, runsDir, lease)
	finishRun := func(status RunStatus) error {
		stopLeaseHeartbeat()
		ledger.Finish(status, time.Now())
		if err := SaveRunLedger(runsDir, ledger); err != nil {
			return err
		}
		return RemoveRunLease(runsDir)
	}
	logs, err := openApplyLogs(runsDir, opts.ClustersDir, ledger)
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
	events := make(chan applyTaskResult)
	started := map[string]bool{}
	runningResources := map[string]int{}
	running := 0
	runningRedfish := 0
	completed := initiallyCompletedApplyTasks(tasks)
	var firstErr error

	for completed < len(tasks) {
		startedAny := false
		for _, task := range tasks {
			if running >= parallelism {
				break
			}
			if taskTerminal(task.Entry.Status) || started[task.Entry.ID] || !taskReady(ledger, task.Entry) || !taskSlotAvailable(task, runningRedfish, redfishLimit) || !taskResourcesAvailable(task, runningResources) {
				continue
			}
			redfishSlots := taskRedfishSlots(task, redfishLimit)
			taskToRun := task
			if task.Entry.Kind == ApplyTaskKindNodeBoot {
				taskToRun.Forks = redfishSlots
			}
			started[task.Entry.ID] = true
			startedAny = true
			running++
			if task.Entry.Kind == ApplyTaskKindNodeBoot {
				runningRedfish += redfishSlots
			}
			acquireTaskResources(task, runningResources)
			logPath := TaskLogPath(runsDir, ledger.RunID, task.Entry.ID)
			ledger.MarkReady(task.Entry.ID)
			ledger.MarkRunning(task.Entry.ID, logPath, time.Now())
			if err := SaveRunLedger(runsDir, ledger); err != nil && firstErr == nil {
				firstErr = err
				cancel()
			}
			if reporter != nil {
				reporter.StageSnapshot(ledger)
			}
			if opts.AskBecomePass {
				if reporter != nil {
					reporter.PromptGap()
				}
			}
			stdoutWriter, stderrWriter := applyTaskWriters(task, logs)
			go func(task ApplyTask, taskOut, taskErr io.Writer) {
				events <- runOneApplyTask(ctx, taskOut, taskErr, runsDir, ledger.RunID, opts, task, runnerFactory)
			}(taskToRun, stdoutWriter, stderrWriter)
		}
		if firstErr != nil && running == 0 {
			break
		}
		if running == 0 && !startedAny {
			break
		}
		var event applyTaskResult
		select {
		case event = <-events:
		case err := <-leaseErrors:
			if err != nil && firstErr == nil {
				firstErr = err
				cancel()
			}
			continue
		}
		running--
		if taskByID[event.id].Entry.Kind == ApplyTaskKindNodeBoot {
			runningRedfish -= taskRedfishSlots(taskByID[event.id], redfishLimit)
		}
		releaseTaskResources(taskByID[event.id], runningResources)
		completed++
		if event.err != nil {
			failure := event.failure
			if failure == "" {
				failure = event.err.Error()
			}
			ledger.MarkFailed(event.id, failure, time.Now())
			if firstErr == nil {
				firstErr = event.err
				cancel()
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
		if saveErr != nil && firstErr == nil {
			firstErr = saveErr
			cancel()
		}
		if reporter != nil {
			reporter.StageSnapshot(ledger)
		}
	}

	if firstErr == nil {
		blocked := blockUnfinishedApplyTasks(&ledger, time.Now())
		if len(blocked) > 0 {
			progressErr := fmt.Errorf("apply task graph could not make progress; blocked task(s): %s", strings.Join(blocked, ", "))
			if saveErr := SaveRunLedger(runsDir, ledger); saveErr != nil {
				firstErr = fmt.Errorf("%v; save apply ledger: %w", progressErr, saveErr)
			} else {
				firstErr = progressErr
			}
			if reporter != nil {
				reporter.StageSnapshot(ledger)
			}
		}
	}
	if firstErr != nil {
		_ = finishRun(RunStatusFailed)
		if reporter != nil {
			reporter.RunSummary(ledger)
		}
		return ledger, firstErr
	}
	if err := finishRun(RunStatusOK); err != nil {
		return ledger, err
	}
	if reporter != nil {
		reporter.RunSummary(ledger)
	}
	return ledger, nil
}

var (
	applyLeaseHeartbeatInterval = ApplyLeaseHeartbeatInterval
	saveRunLease                = SaveRunLease
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

func taskSlotAvailable(task ApplyTask, runningRedfish, redfishLimit int) bool {
	if task.Entry.Kind != ApplyTaskKindNodeBoot {
		return true
	}
	return runningRedfish+taskRedfishSlots(task, redfishLimit) <= redfishLimit
}

func taskRedfishSlots(task ApplyTask, redfishLimit int) int {
	if task.Entry.Kind != ApplyTaskKindNodeBoot {
		return 0
	}
	slots := task.RedfishSlots
	if slots < 1 {
		slots = 1
	}
	if redfishLimit > 0 && slots > redfishLimit {
		return redfishLimit
	}
	return slots
}

func taskResourcesAvailable(task ApplyTask, running map[string]int) bool {
	for _, key := range task.Entry.ResourceKeys {
		if key == "" {
			continue
		}
		if running[key] > 0 {
			return false
		}
	}
	return true
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

func applyTaskWriters(task ApplyTask, logs *applyLogSet) (io.Writer, io.Writer) {
	writer := logs.Writer(task.Entry.Cluster)
	return writer, writer
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

func TaskLedgerEntries(tasks []ApplyTask) []TaskLedgerEntry {
	entries := make([]TaskLedgerEntry, 0, len(tasks))
	for _, task := range tasks {
		entries = append(entries, task.Entry)
	}
	return entries
}

func AnnotateApplyTaskClusterLogPaths(clustersDir, runID string, tasks []ApplyTask) []ApplyTask {
	out := make([]ApplyTask, len(tasks))
	copy(out, tasks)
	for i := range out {
		if out[i].Entry.Cluster != "" {
			out[i].Entry.ClusterLogPath = ApplyClusterLogPath(clustersDir, runID, out[i].Entry.Cluster)
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
	return true
}

func applyRunID(now time.Time) string {
	return "apply-" + now.UTC().Format("20060102T150405.000000000Z")
}

func autoParallelism(taskCount int) int {
	if taskCount < 1 {
		return 1
	}
	return taskCount
}

func ResolveApplyConcurrencyLimits(limits ConcurrencyLimits, tasks []ApplyTask) ConcurrencyLimits {
	autoGlobal := autoParallelism(len(tasks))
	if limits.Parallelism <= 0 || limits.Parallelism > autoGlobal {
		limits.Parallelism = autoGlobal
	}
	autoPerHost := 1
	if limits.ParallelismPerHost <= 0 || limits.ParallelismPerHost > autoPerHost {
		limits.ParallelismPerHost = autoPerHost
	}
	autoRedfish := nodeBootTaskCount(tasks)
	if autoRedfish < 1 {
		autoRedfish = 1
	}
	if limits.ParallelismRedfish <= 0 || limits.ParallelismRedfish > autoRedfish {
		limits.ParallelismRedfish = autoRedfish
	}
	return limits
}

func nodeBootTaskCount(tasks []ApplyTask) int {
	count := 0
	for _, task := range tasks {
		if task.Entry.Kind == ApplyTaskKindNodeBoot {
			if task.RedfishSlots > 0 {
				count += task.RedfishSlots
			} else {
				count++
			}
		}
	}
	return count
}

func AnsibleForksForLimit(state v1alpha1.State, limit string) int {
	members := render.HostGroupMembers(state)
	selected := map[string]bool{}
	addAll := func(hosts []string) {
		for _, host := range hosts {
			if host != "" {
				selected[host] = true
			}
		}
	}
	if strings.TrimSpace(limit) == "" {
		for _, hosts := range members {
			addAll(hosts)
		}
	} else {
		for _, token := range strings.Split(limit, ":") {
			token = strings.TrimSpace(token)
			if token == "" {
				continue
			}
			if hosts, ok := members[token]; ok {
				addAll(hosts)
				continue
			}
			selected[token] = true
		}
	}
	if len(selected) < 1 {
		return 1
	}
	return len(selected)
}
