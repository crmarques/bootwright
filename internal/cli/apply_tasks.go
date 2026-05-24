package cli

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
	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/provisioning/render"
	"github.com/crmarques/bootwright/internal/stategraph"
	"github.com/crmarques/bootwright/internal/workflow"
)

const (
	applyTaskKindProvider     = "providerServices"
	applyTaskKindClusterInfra = "clusterInfra"
	applyTaskKindClusterISO   = "clusterISO"
	applyTaskKindNodeBoot     = "nodeBoot"
	applyTaskKindInstallWait  = "installWait"
)

type applyTask struct {
	entry         workflow.TaskLedgerEntry
	playbook      string
	limit         string
	forks         int
	extraVarPairs []string
	state         v1alpha1.State
}

type applyTaskResult struct {
	id      string
	skipped bool
	err     error
}

func planApplyTasks(scope scopeSpec, state v1alpha1.State) []applyTask {
	phaseSet := map[string]bool{}
	for _, phase := range scope.phases() {
		phaseSet[phase.Name] = true
	}
	var tasks []applyTask
	providerTaskIDs := []string{}
	if phaseSet["provider"] {
		for _, host := range render.HostGroupMembers(state)[render.GroupProviderHosts] {
			taskID := "provider." + host
			providerTaskIDs = append(providerTaskIDs, taskID)
			tasks = append(tasks, applyTask{
				entry: workflow.TaskLedgerEntry{
					ID:           taskID,
					Kind:         applyTaskKindProvider,
					Label:        "provider services " + host,
					Host:         host,
					ResourceKeys: []string{hostMutationResource(host)},
					Status:       workflow.TaskStatusPending,
				},
				playbook: phases["provider"].ApplyPlaybook,
				limit:    host,
				forks:    1,
				state:    state,
			})
		}
	}
	infraDepsByCluster := map[string][]string{}
	clusterNames := applyClusterNames(state)
	for _, name := range clusterNames {
		if phaseSet["cluster"] {
			clusterState := stategraph.FilterStateToClusters(state, []string{name})
			infraHosts := render.HostGroupMembers(clusterState)[render.GroupInfraHosts]
			deps := append([]string(nil), providerTaskIDs...)
			if len(infraHosts) == 0 {
				taskID := "infra." + name
				infraDepsByCluster[name] = append(infraDepsByCluster[name], taskID)
				tasks = append(tasks, applyTask{
					entry: workflow.TaskLedgerEntry{
						ID:           taskID,
						Kind:         applyTaskKindClusterInfra,
						Label:        "infra " + name,
						Cluster:      name,
						Status:       workflow.TaskStatusPending,
						Dependencies: deps,
					},
					playbook: phases["cluster"].ApplyPlaybook,
					limit:    render.GroupInfraHosts,
					state:    clusterState,
				})
				continue
			}
			for _, host := range infraHosts {
				taskID := "infra." + name + "." + host
				infraDepsByCluster[name] = append(infraDepsByCluster[name], taskID)
				tasks = append(tasks, applyTask{
					entry: workflow.TaskLedgerEntry{
						ID:           taskID,
						Kind:         applyTaskKindClusterInfra,
						Label:        "infra " + name + " on " + host,
						Cluster:      name,
						Host:         host,
						ResourceKeys: []string{hostMutationResource(host)},
						Status:       workflow.TaskStatusPending,
						Dependencies: deps,
					},
					playbook: phases["cluster"].ApplyPlaybook,
					limit:    host,
					forks:    1,
					state:    clusterState,
				})
			}
		}
	}
	for _, name := range clusterNames {
		deps := append([]string(nil), infraDepsByCluster[name]...)
		if phaseSet["clusters"] {
			clusterState := stategraph.FilterStateToClusters(state, []string{name})
			isoTaskID := "iso." + name
			tasks = append(tasks, applyTask{
				entry: workflow.TaskLedgerEntry{
					ID:           isoTaskID,
					Kind:         applyTaskKindClusterISO,
					Label:        "iso " + name,
					Cluster:      name,
					Status:       workflow.TaskStatusPending,
					Dependencies: deps,
				},
				playbook:      "playbooks/layers/openshift/create-agent-iso.yml",
				limit:         render.GroupOCPHosts,
				forks:         1,
				extraVarPairs: []string{"bootwright_task_cluster_name=" + name},
				state:         clusterState,
			})
			bootTaskIDs := []string{}
			for _, machineName := range applyClusterMachineNames(state, name) {
				taskID := "boot." + name + "." + machineName
				bootTaskIDs = append(bootTaskIDs, taskID)
				tasks = append(tasks, applyTask{
					entry: workflow.TaskLedgerEntry{
						ID:           taskID,
						Kind:         applyTaskKindNodeBoot,
						Label:        "boot " + name + "/" + machineName,
						Cluster:      name,
						Node:         machineName,
						ResourceKeys: []string{applyNodeRedfishResource(state, name, machineName)},
						Status:       workflow.TaskStatusPending,
						Dependencies: []string{isoTaskID},
					},
					playbook: "playbooks/layers/openshift/boot-agent-machine.yml",
					limit:    render.GroupOCPHosts + ":" + render.GroupBootHosts,
					extraVarPairs: []string{
						"bootwright_task_cluster_name=" + name,
						"bootwright_task_machine_name=" + machineName,
					},
					state: clusterState,
					forks: 1,
				})
			}
			waitDeps := append([]string(nil), bootTaskIDs...)
			if len(waitDeps) == 0 {
				waitDeps = append(waitDeps, isoTaskID)
			}
			tasks = append(tasks, applyTask{
				entry: workflow.TaskLedgerEntry{
					ID:           "wait." + name,
					Kind:         applyTaskKindInstallWait,
					Label:        "wait install " + name,
					Cluster:      name,
					Status:       workflow.TaskStatusPending,
					Dependencies: waitDeps,
				},
				playbook:      "playbooks/layers/openshift/wait-agent-install.yml",
				limit:         render.GroupOCPHosts,
				forks:         1,
				extraVarPairs: []string{"bootwright_task_cluster_name=" + name},
				state:         clusterState,
			})
		}
	}
	return tasks
}

func runApplyTaskGraph(ctx context.Context, stdout io.Writer, stderr io.Writer, stateDir string, opts workflow.RunOptions, scope scopeSpec, clusterScope string, tasks []applyTask, limits workflow.ConcurrencyLimits) (workflow.RunLedger, error) {
	startedAt := time.Now()
	runID := applyRunID(startedAt)
	limits = resolveApplyConcurrencyLimits(limits, tasks)
	tasks = annotateApplyTaskClusterLogPaths(stateDir, runID, tasks)
	ledger := workflow.NewRunLedger(runID, scope.name, clusterScope, limits, taskLedgerEntries(tasks), startedAt)
	if err := workflow.SaveRunLedger(stateDir, ledger); err != nil {
		return ledger, err
	}
	multiClusterOutput := len(ledger.ClusterNames()) > 1
	clusterLogs, err := openApplyClusterLogs(stateDir, ledger)
	if err != nil {
		return ledger, err
	}
	defer clusterLogs.Close()
	printApplyRunStart(stdout, ledger)
	if multiClusterOutput {
		printApplyClusterLogPaths(stdout, ledger)
		printApplyStageSnapshot(stdout, ledger)
	} else {
		printApplyAnsibleExecutionStart(stdout)
	}
	if len(tasks) == 0 {
		ledger.Finish(workflow.RunStatusOK, time.Now())
		_ = workflow.SaveRunLedger(stateDir, ledger)
		printApplyRunSummary(stdout, ledger)
		return ledger, nil
	}
	outputMu := &sync.Mutex{}
	taskStdout := &applyTaskOutputWriter{mu: outputMu, w: stdout}
	taskStderr := &applyTaskOutputWriter{mu: outputMu, w: stderr}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	taskByID := map[string]applyTask{}
	for _, task := range tasks {
		taskByID[task.entry.ID] = task
	}
	parallelism := limits.Parallelism
	redfishLimit := limits.ParallelismRedfish
	events := make(chan applyTaskResult)
	started := map[string]bool{}
	runningResources := map[string]int{}
	running := 0
	runningRedfish := 0
	completed := 0
	var firstErr error

	for completed < len(tasks) {
		startedAny := false
		for _, task := range tasks {
			if running >= parallelism {
				break
			}
			if started[task.entry.ID] || !taskReady(ledger, task.entry) || !taskSlotAvailable(task, runningRedfish, redfishLimit) || !taskResourcesAvailable(task, runningResources) {
				continue
			}
			started[task.entry.ID] = true
			startedAny = true
			running++
			if task.entry.Kind == applyTaskKindNodeBoot {
				runningRedfish++
			}
			acquireTaskResources(task, runningResources)
			logPath := taskLogPath(stateDir, ledger.RunID, task.entry.ID)
			ledger.MarkReady(task.entry.ID)
			ledger.MarkRunning(task.entry.ID, logPath, time.Now())
			if err := workflow.SaveRunLedger(stateDir, ledger); err != nil && firstErr == nil {
				firstErr = err
				cancel()
			}
			if multiClusterOutput {
				printApplyStageSnapshot(taskStdout, ledger)
			} else {
				printApplyTaskStart(taskStdout, ledger, task.entry.ID)
			}
			if opts.AskBecomePass && !multiClusterOutput {
				output.NewContinuation(taskStderr).BlankLine()
			}
			stdoutWriter, stderrWriter := applyTaskWriters(task, taskStdout, taskStderr, clusterLogs, multiClusterOutput)
			go func(task applyTask, taskOut, taskErr io.Writer) {
				events <- runOneApplyTask(ctx, taskOut, taskErr, stateDir, ledger.RunID, opts, task)
			}(task, stdoutWriter, stderrWriter)
		}
		if firstErr != nil && running == 0 {
			break
		}
		if running == 0 && !startedAny {
			break
		}
		event := <-events
		running--
		if taskByID[event.id].entry.Kind == applyTaskKindNodeBoot {
			runningRedfish--
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
		saveErr := workflow.SaveRunLedger(stateDir, ledger)
		if saveErr != nil && firstErr == nil {
			firstErr = saveErr
			cancel()
		}
		if multiClusterOutput {
			printApplyStageSnapshot(taskStdout, ledger)
		} else {
			printApplyTaskResult(taskStdout, ledger, event.id)
		}
	}

	if firstErr != nil {
		ledger.Finish(workflow.RunStatusFailed, time.Now())
		_ = workflow.SaveRunLedger(stateDir, ledger)
		printApplyRunSummary(stdout, ledger)
		return ledger, firstErr
	}
	ledger.Finish(workflow.RunStatusOK, time.Now())
	if err := workflow.SaveRunLedger(stateDir, ledger); err != nil {
		return ledger, err
	}
	printApplyRunSummary(stdout, ledger)
	return ledger, nil
}

func taskSlotAvailable(task applyTask, runningRedfish, redfishLimit int) bool {
	if task.entry.Kind != applyTaskKindNodeBoot {
		return true
	}
	return runningRedfish < redfishLimit
}

func taskResourcesAvailable(task applyTask, running map[string]int) bool {
	for _, key := range task.entry.ResourceKeys {
		if key == "" {
			continue
		}
		if running[key] > 0 {
			return false
		}
	}
	return true
}

func acquireTaskResources(task applyTask, running map[string]int) {
	for _, key := range task.entry.ResourceKeys {
		if key != "" {
			running[key]++
		}
	}
}

func releaseTaskResources(task applyTask, running map[string]int) {
	for _, key := range task.entry.ResourceKeys {
		if key == "" {
			continue
		}
		running[key]--
		if running[key] <= 0 {
			delete(running, key)
		}
	}
}

func runOneApplyTask(ctx context.Context, stdout io.Writer, stderr io.Writer, stateDir, runID string, opts workflow.RunOptions, task applyTask) applyTaskResult {
	renderDir := filepath.Join(stateDir, "workflow", "runs", runID, task.entry.ID, "render")
	taskOpts := opts
	taskOpts.State = task.state
	taskOpts.RenderDir = renderDir
	taskOpts.Playbook = task.playbook
	taskOpts.Limit = task.limit
	taskOpts.ExtraVarPairs = append(append([]string(nil), opts.ExtraVarPairs...), task.extraVarPairs...)
	taskOpts.ArtifactsBaseName = task.entry.ID
	taskOpts.ResolveInstaller = false
	taskOpts.Label = task.entry.Label
	taskOpts.Forks = task.forks
	runner := ansible.CommandRunner{Stdout: stdout, Stderr: stderr}
	result, err := workflow.Run(ctx, taskOpts, runner, nil)
	return applyTaskResult{id: task.entry.ID, skipped: result.Skipped, err: err}
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

func openApplyClusterLogs(stateDir string, ledger workflow.RunLedger) (*applyClusterLogSet, error) {
	names := ledger.ClusterNames()
	logs := &applyClusterLogSet{writers: map[string]io.Writer{}}
	if len(names) <= 1 {
		return logs, nil
	}
	for _, name := range names {
		path := applyClusterLogPath(stateDir, ledger.RunID, name)
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

func applyTaskWriters(task applyTask, stdout io.Writer, stderr io.Writer, clusterLogs *applyClusterLogSet, multiClusterOutput bool) (io.Writer, io.Writer) {
	if !multiClusterOutput {
		return stdout, stderr
	}
	if task.entry.Cluster == "" {
		return io.Discard, io.Discard
	}
	writer := clusterLogs.Writer(task.entry.Cluster)
	return writer, writer
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

func taskLedgerEntries(tasks []applyTask) []workflow.TaskLedgerEntry {
	entries := make([]workflow.TaskLedgerEntry, 0, len(tasks))
	for _, task := range tasks {
		entries = append(entries, task.entry)
	}
	return entries
}

func annotateApplyTaskClusterLogPaths(stateDir, runID string, tasks []applyTask) []applyTask {
	out := make([]applyTask, len(tasks))
	copy(out, tasks)
	for i := range out {
		if out[i].entry.Cluster != "" {
			out[i].entry.ClusterLogPath = applyClusterLogPath(stateDir, runID, out[i].entry.Cluster)
		}
	}
	return out
}

func taskReady(ledger workflow.RunLedger, task workflow.TaskLedgerEntry) bool {
	for _, dep := range task.Dependencies {
		depTask, ok := ledger.Task(dep)
		if !ok {
			return false
		}
		switch depTask.Status {
		case workflow.TaskStatusOK, workflow.TaskStatusSkipped:
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

func resolveApplyConcurrencyLimits(limits workflow.ConcurrencyLimits, tasks []applyTask) workflow.ConcurrencyLimits {
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

func nodeBootTaskCount(tasks []applyTask) int {
	count := 0
	for _, task := range tasks {
		if task.entry.Kind == applyTaskKindNodeBoot {
			count++
		}
	}
	return count
}

func taskLogPath(stateDir, runID, taskID string) string {
	return filepath.Join(stateDir, "workflow", "runs", runID, taskID, "render", "ansible", "artifacts", taskID, ansible.OutputLogName)
}

func applyClusterLogPath(stateDir, runID, cluster string) string {
	return filepath.Join(stateDir, "workflow", "runs", runID, "clusters", cluster, "install.log")
}

func ansibleForksForLimit(state v1alpha1.State, limit string) int {
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
