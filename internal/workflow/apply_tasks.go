package workflow

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/ansible"
	"github.com/crmarques/bootwright/internal/provisioning/render"
	"github.com/crmarques/bootwright/internal/stategraph"
)

const (
	ApplyTaskKindProvider     = "providerServices"
	ApplyTaskKindClusterInfra = "clusterInfra"
	ApplyTaskKindClusterISO   = "clusterISO"
	ApplyTaskKindNodeBoot     = "nodeBoot"
	ApplyTaskKindInstallWait  = "installWait"

	ApplyPhaseProvider = "provider"
	ApplyPhaseCluster  = "cluster"
	ApplyPhaseClusters = "clusters"
)

const (
	applyProviderPlaybook     = "playbooks/layers/providers/apply.yml"
	applyClusterInfraPlaybook = "playbooks/layers/cluster_infra/apply.yml"
	applyCreateISOPlaybook    = "playbooks/layers/openshift/create-agent-iso.yml"
	applyBootMachinePlaybook  = "playbooks/layers/openshift/boot-agent-machine.yml"
	applyWaitInstallPlaybook  = "playbooks/layers/openshift/wait-agent-install.yml"
)

type ApplyTarget struct {
	Name       string
	PhaseNames []string
}

type ApplyTask struct {
	Entry         TaskLedgerEntry
	Playbook      string
	Limit         string
	Forks         int
	RedfishSlots  int
	ExtraVarPairs []string
	State         v1alpha1.State
}

type applyTaskResult struct {
	id      string
	skipped bool
	err     error
}

type ApplyReporter interface {
	RunStart(ledger RunLedger)
	ClusterLogPaths(ledger RunLedger)
	StageSnapshot(ledger RunLedger)
	AnsibleExecutionStart()
	TaskStart(ledger RunLedger, id string)
	TaskResult(ledger RunLedger, id string)
	RunSummary(ledger RunLedger)
	PromptGap()
}

type ApplyTaskRunnerFactory func(stdout io.Writer, stderr io.Writer) ansible.Runner

func PlanApplyTasks(target ApplyTarget, state v1alpha1.State) []ApplyTask {
	phaseSet := map[string]bool{}
	for _, phase := range target.PhaseNames {
		phaseSet[phase] = true
	}
	var tasks []ApplyTask
	providerTaskIDs := []string{}
	if phaseSet[ApplyPhaseProvider] {
		for _, host := range render.HostGroupMembers(state)[render.GroupProviderHosts] {
			taskID := "provider." + host
			providerTaskIDs = append(providerTaskIDs, taskID)
			tasks = append(tasks, ApplyTask{
				Entry: TaskLedgerEntry{
					ID:           taskID,
					Kind:         ApplyTaskKindProvider,
					Label:        "provider services " + host,
					Host:         host,
					ResourceKeys: []string{hostMutationResource(host)},
					Status:       TaskStatusPending,
				},
				Playbook: applyProviderPlaybook,
				Limit:    host,
				Forks:    1,
				State:    state,
			})
		}
	}
	infraDepsByCluster := map[string][]string{}
	clusterNames := applyClusterNames(state)
	for _, name := range clusterNames {
		if phaseSet[ApplyPhaseCluster] {
			clusterState := stategraph.FilterStateToClusters(state, []string{name})
			infraHosts := render.HostGroupMembers(clusterState)[render.GroupInfraHosts]
			deps := append([]string(nil), providerTaskIDs...)
			if len(infraHosts) == 0 {
				taskID := "infra." + name
				infraDepsByCluster[name] = append(infraDepsByCluster[name], taskID)
				tasks = append(tasks, ApplyTask{
					Entry: TaskLedgerEntry{
						ID:           taskID,
						Kind:         ApplyTaskKindClusterInfra,
						Label:        "infra " + name,
						Cluster:      name,
						Status:       TaskStatusPending,
						Dependencies: deps,
					},
					Playbook: applyClusterInfraPlaybook,
					Limit:    render.GroupInfraHosts,
					State:    clusterState,
				})
				continue
			}
			for _, host := range infraHosts {
				taskID := "infra." + name + "." + host
				infraDepsByCluster[name] = append(infraDepsByCluster[name], taskID)
				tasks = append(tasks, ApplyTask{
					Entry: TaskLedgerEntry{
						ID:           taskID,
						Kind:         ApplyTaskKindClusterInfra,
						Label:        "infra " + name + " on " + host,
						Cluster:      name,
						Host:         host,
						ResourceKeys: []string{hostMutationResource(host)},
						Status:       TaskStatusPending,
						Dependencies: deps,
					},
					Playbook: applyClusterInfraPlaybook,
					Limit:    host,
					Forks:    1,
					State:    clusterState,
				})
			}
		}
	}
	for _, name := range clusterNames {
		deps := append([]string(nil), infraDepsByCluster[name]...)
		if phaseSet[ApplyPhaseClusters] {
			clusterState := stategraph.FilterStateToClusters(state, []string{name})
			isoTaskID := "iso." + name
			tasks = append(tasks, ApplyTask{
				Entry: TaskLedgerEntry{
					ID:           isoTaskID,
					Kind:         ApplyTaskKindClusterISO,
					Label:        "iso " + name,
					Cluster:      name,
					Status:       TaskStatusPending,
					Dependencies: deps,
				},
				Playbook:      applyCreateISOPlaybook,
				Limit:         render.GroupOCPHosts,
				Forks:         1,
				ExtraVarPairs: []string{"bootwright_task_cluster_name=" + name},
				State:         clusterState,
			})
			machineNames := applyClusterMachineNames(state, name)
			bootTaskID := ""
			if len(machineNames) > 0 {
				bootTaskID = "boot." + name
				resourceKeys := make([]string, 0, len(machineNames))
				for _, machineName := range machineNames {
					resourceKeys = append(resourceKeys, applyNodeRedfishResource(state, name, machineName))
				}
				tasks = append(tasks, ApplyTask{
					Entry: TaskLedgerEntry{
						ID:           bootTaskID,
						Kind:         ApplyTaskKindNodeBoot,
						Label:        "boot " + name + " nodes",
						Cluster:      name,
						ResourceKeys: resourceKeys,
						Status:       TaskStatusPending,
						Dependencies: []string{isoTaskID},
					},
					Playbook:      applyBootMachinePlaybook,
					Limit:         render.AgentNodeGroupName(name),
					ExtraVarPairs: []string{"bootwright_task_cluster_name=" + name},
					State:         clusterState,
					Forks:         len(machineNames),
					RedfishSlots:  len(machineNames),
				})
			}
			waitDeps := []string{}
			if bootTaskID != "" {
				waitDeps = append(waitDeps, bootTaskID)
			} else {
				waitDeps = append(waitDeps, isoTaskID)
			}
			tasks = append(tasks, ApplyTask{
				Entry: TaskLedgerEntry{
					ID:           "wait." + name,
					Kind:         ApplyTaskKindInstallWait,
					Label:        "wait install " + name,
					Cluster:      name,
					Status:       TaskStatusPending,
					Dependencies: waitDeps,
				},
				Playbook:      applyWaitInstallPlaybook,
				Limit:         render.GroupOCPHosts,
				Forks:         1,
				ExtraVarPairs: []string{"bootwright_task_cluster_name=" + name},
				State:         clusterState,
			})
		}
	}
	return tasks
}

func RunApplyTaskGraph(ctx context.Context, stdout io.Writer, stderr io.Writer, runsDir string, opts RunOptions, target ApplyTarget, clusterScope string, tasks []ApplyTask, limits ConcurrencyLimits, reporter ApplyReporter, runnerFactory ApplyTaskRunnerFactory) (RunLedger, error) {
	startedAt := time.Now()
	runID := applyRunID(startedAt)
	if strings.TrimSpace(opts.RuntimeDir) == "" {
		return RunLedger{}, fmt.Errorf("runtime dir is required")
	}
	if strings.TrimSpace(opts.RenderedDir) == "" {
		return RunLedger{}, fmt.Errorf("rendered dir is required")
	}
	if strings.TrimSpace(opts.ManagedDir) == "" {
		return RunLedger{}, fmt.Errorf("managed dir is required")
	}
	if strings.TrimSpace(runsDir) == "" {
		return RunLedger{}, fmt.Errorf("runs dir is required")
	}
	opts.RunsDir = runsDir
	limits = ResolveApplyConcurrencyLimits(limits, tasks)
	tasks = AnnotateApplyTaskClusterLogPaths(runsDir, runID, tasks)
	var err error
	tasks, err = ReconcileApplyClusterInstallState(ctx, opts.RuntimeDir, opts.SecretsDir, runID, opts.State, tasks, opts.InstallOverride, opts.ClusterAvailabilityChecker, startedAt)
	if err != nil {
		return RunLedger{}, err
	}
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
	stopLeaseHeartbeat := startRunLeaseHeartbeat(ctx, runsDir, lease)
	finishRun := func(status RunStatus) error {
		stopLeaseHeartbeat()
		ledger.Finish(status, time.Now())
		if err := SaveRunLedger(runsDir, ledger); err != nil {
			return err
		}
		return RemoveRunLease(runsDir)
	}
	multiClusterOutput := len(ledger.ClusterNames()) > 1
	clusterLogs, err := openApplyClusterLogs(runsDir, ledger)
	if err != nil {
		_ = finishRun(RunStatusFailed)
		return ledger, err
	}
	defer clusterLogs.Close()
	if reporter != nil {
		reporter.RunStart(ledger)
	}
	if multiClusterOutput {
		if reporter != nil {
			reporter.ClusterLogPaths(ledger)
			reporter.StageSnapshot(ledger)
		}
	} else {
		if reporter != nil {
			reporter.AnsibleExecutionStart()
		}
	}
	if len(tasks) == 0 {
		_ = finishRun(RunStatusOK)
		if reporter != nil {
			reporter.RunSummary(ledger)
		}
		return ledger, nil
	}
	outputMu := &sync.Mutex{}
	taskStdout := &applyTaskOutputWriter{mu: outputMu, w: stdout}
	taskStderr := &applyTaskOutputWriter{mu: outputMu, w: stderr}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

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
			if multiClusterOutput {
				if reporter != nil {
					reporter.StageSnapshot(ledger)
				}
			} else {
				if reporter != nil {
					reporter.TaskStart(ledger, task.Entry.ID)
				}
			}
			if opts.AskBecomePass && !multiClusterOutput {
				if reporter != nil {
					reporter.PromptGap()
				}
			}
			stdoutWriter, stderrWriter := applyTaskWriters(task, taskStdout, taskStderr, clusterLogs, multiClusterOutput)
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
		event := <-events
		running--
		if taskByID[event.id].Entry.Kind == ApplyTaskKindNodeBoot {
			runningRedfish -= taskRedfishSlots(taskByID[event.id], redfishLimit)
		}
		releaseTaskResources(taskByID[event.id], runningResources)
		completed++
		if event.err != nil {
			ledger.MarkFailed(event.id, event.err.Error(), time.Now())
			if firstErr == nil {
				firstErr = event.err
				cancel()
			}
		} else if event.skipped {
			ledger.MarkSkipped(event.id, "no remote hosts matched task limit", time.Now())
		} else {
			ledger.MarkOK(event.id, time.Now())
		}
		saveErr := SaveRunLedger(runsDir, ledger)
		if saveErr != nil && firstErr == nil {
			firstErr = saveErr
			cancel()
		}
		if multiClusterOutput {
			if reporter != nil {
				reporter.StageSnapshot(ledger)
			}
		} else {
			if reporter != nil {
				reporter.TaskResult(ledger, event.id)
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

func startRunLeaseHeartbeat(ctx context.Context, runsDir string, lease RunLease) func() {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(ApplyLeaseHeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				lease.HeartbeatAt = now.UTC()
				_ = SaveRunLease(runsDir, lease)
			}
		}
	}()
	stopped := false
	return func() {
		if stopped {
			return
		}
		stopped = true
		cancel()
		<-done
	}
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

func runOneApplyTask(ctx context.Context, stdout io.Writer, stderr io.Writer, runsDir, runID string, opts RunOptions, task ApplyTask, runnerFactory ApplyTaskRunnerFactory) applyTaskResult {
	taskRoot := filepath.Join(runsDir, "history", runID, "tasks", task.Entry.ID)
	renderDir := filepath.Join(taskRoot, "rendered")
	taskOpts := opts
	taskOpts.State = task.State
	taskOpts.RenderDir = renderDir
	taskOpts.Playbook = task.Playbook
	taskOpts.Limit = task.Limit
	taskOpts.ExtraVarPairs = append(append([]string(nil), opts.ExtraVarPairs...), task.ExtraVarPairs...)
	taskOpts.ArtifactsBaseName = ""
	taskOpts.ResolveInstaller = false
	taskOpts.Label = task.Entry.Label
	taskOpts.Forks = task.Forks
	taskOpts.ArtifactsRoot = filepath.Join(taskRoot, "artifacts")
	taskOpts.OutputLogPath = TaskLogPath(runsDir, runID, task.Entry.ID)
	if runnerFactory == nil {
		runnerFactory = func(stdout io.Writer, stderr io.Writer) ansible.Runner {
			return ansible.CommandRunner{Stdout: stdout, Stderr: stderr}
		}
	}
	runner := runnerFactory(stdout, stderr)
	now := time.Now()
	if err := MarkClusterInstallTaskStarted(opts.RuntimeDir, opts.SecretsDir, runID, task, now); err != nil {
		return applyTaskResult{id: task.Entry.ID, err: err}
	}
	result, err := Run(ctx, taskOpts, runner, nil)
	now = time.Now()
	if err != nil {
		if recordErr := MarkClusterInstallTaskFailed(opts.RuntimeDir, opts.SecretsDir, runID, task, now); recordErr != nil {
			err = fmt.Errorf("%w; additionally failed to record cluster install state: %v", err, recordErr)
		}
		return applyTaskResult{id: task.Entry.ID, skipped: result.Skipped, err: err}
	}
	if !result.Skipped {
		if recordErr := MarkClusterInstallTaskSucceeded(opts.RuntimeDir, opts.SecretsDir, runID, task, now); recordErr != nil {
			return applyTaskResult{id: task.Entry.ID, skipped: result.Skipped, err: recordErr}
		}
	}
	return applyTaskResult{id: task.Entry.ID, skipped: result.Skipped, err: err}
}

type applyTaskOutputWriter struct {
	mu *sync.Mutex
	w  io.Writer
}

func (w *applyTaskOutputWriter) Write(p []byte) (int, error) {
	if w == nil || w.w == nil {
		return len(p), nil
	}
	if w.mu != nil {
		w.mu.Lock()
		defer w.mu.Unlock()
	}
	return w.w.Write(p)
}

type applyClusterLogSet struct {
	files   []*os.File
	writers map[string]io.Writer
}

func openApplyClusterLogs(runsDir string, ledger RunLedger) (*applyClusterLogSet, error) {
	names := ledger.ClusterNames()
	logs := &applyClusterLogSet{writers: map[string]io.Writer{}}
	if len(names) <= 1 {
		return logs, nil
	}
	for _, name := range names {
		path := ApplyClusterLogPath(runsDir, ledger.RunID, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			logs.Close()
			return nil, fmt.Errorf("create cluster log directory: %w", err)
		}
		if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
			logs.Close()
			return nil, fmt.Errorf("chmod cluster log directory: %w", err)
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			logs.Close()
			return nil, fmt.Errorf("create cluster install log: %w", err)
		}
		if err := os.Chmod(path, 0o600); err != nil {
			file.Close()
			logs.Close()
			return nil, fmt.Errorf("chmod cluster install log: %w", err)
		}
		logs.files = append(logs.files, file)
		logs.writers[name] = &applyTaskOutputWriter{mu: &sync.Mutex{}, w: file}
	}
	return logs, nil
}

func (s *applyClusterLogSet) Writer(cluster string) io.Writer {
	if s == nil {
		return io.Discard
	}
	if writer, ok := s.writers[cluster]; ok {
		return writer
	}
	return io.Discard
}

func (s *applyClusterLogSet) Close() error {
	if s == nil {
		return nil
	}
	var firstErr error
	for _, file := range s.files {
		if err := file.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func applyTaskWriters(task ApplyTask, stdout io.Writer, stderr io.Writer, clusterLogs *applyClusterLogSet, multiClusterOutput bool) (io.Writer, io.Writer) {
	if !multiClusterOutput {
		return stdout, stderr
	}
	if task.Entry.Cluster == "" {
		return io.Discard, io.Discard
	}
	writer := clusterLogs.Writer(task.Entry.Cluster)
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

func applyClusterNames(state v1alpha1.State) []string {
	names := make([]string, 0, len(state.ContainerClusters))
	for _, cluster := range state.ContainerClusters {
		names = append(names, cluster.Metadata.Name)
	}
	sort.Strings(names)
	return names
}

func applyClusterMachineNames(state v1alpha1.State, clusterName string) []string {
	var names []string
	for _, cluster := range state.ContainerClusters {
		if cluster.Metadata.Name != clusterName {
			continue
		}
		seen := map[string]bool{}
		for _, node := range cluster.Spec.Nodes {
			if node.MachineRef.Name == "" || seen[node.MachineRef.Name] {
				continue
			}
			seen[node.MachineRef.Name] = true
			names = append(names, node.MachineRef.Name)
		}
		break
	}
	sort.Strings(names)
	return names
}

func applyNodeRedfishResource(state v1alpha1.State, clusterName, machineName string) string {
	clusterInfraName := ""
	for _, cluster := range state.ContainerClusters {
		if cluster.Metadata.Name != clusterName {
			continue
		}
		for _, node := range cluster.Spec.Nodes {
			if node.MachineRef.Name == machineName {
				clusterInfraName = node.MachineRef.ClusterInfra
				break
			}
		}
		break
	}
	for _, infra := range state.ClusterInfras {
		if infra.Metadata.Name != clusterInfraName {
			continue
		}
		for _, machine := range infra.Spec.Components.Machines {
			if machine.Name != machineName || machine.From.Name == "" {
				continue
			}
			for _, provider := range state.InfraProviders {
				if provider.Metadata.Name != machine.From.Provider {
					continue
				}
				for _, providerMachine := range provider.Spec.Machines {
					if providerMachine.Name == machine.From.Name && providerMachine.BareMetal != nil && providerMachine.BareMetal.BMC.Address != "" {
						return "redfish:" + providerMachine.BareMetal.BMC.Address
					}
				}
			}
		}
	}
	return "redfish:" + clusterName + "/" + machineName
}

func hostMutationResource(host string) string {
	if host == "" {
		return ""
	}
	return "host:" + host + ":mutating"
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

func TaskLogPath(runsDir, runID, taskID string) string {
	return filepath.Join(runsDir, "history", runID, "tasks", taskID, ansible.OutputLogName)
}

func ApplyClusterLogPath(runsDir, runID, cluster string) string {
	return filepath.Join(runsDir, "history", runID, "clusters", cluster, "install.log")
}

func OpenShiftInstallerLogPath(runtimeDir, cluster string) string {
	return filepath.Join(runtimeDir, render.RuntimeRelativeDir, cluster, ".openshift_install.log")
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
