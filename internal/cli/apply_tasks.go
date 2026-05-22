package cli

import (
	"context"
	"io"
	"path/filepath"
	"sort"
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
	providerTaskID := ""
	if phaseSet["provider"] {
		providerTaskID = "provider"
		tasks = append(tasks, applyTask{
			entry: workflow.TaskLedgerEntry{
				ID:     providerTaskID,
				Kind:   applyTaskKindProvider,
				Label:  "provider services",
				Status: workflow.TaskStatusPending,
			},
			playbook: phases["provider"].ApplyPlaybook,
			limit:    render.GroupProviderHosts,
			state:    state,
		})
	}
	clusterNames := applyClusterNames(state)
	for _, name := range clusterNames {
		deps := []string{}
		if providerTaskID != "" {
			deps = append(deps, providerTaskID)
		}
		if phaseSet["cluster"] {
			taskID := "infra." + name
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
				state:    stategraph.FilterStateToClusters(state, []string{name}),
			})
		}
	}
	for _, name := range clusterNames {
		deps := []string{}
		if phaseSet["cluster"] {
			deps = append(deps, "infra."+name)
		}
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
				extraVarPairs: []string{"bootwright_task_cluster_name=" + name},
				state:         clusterState,
			})
		}
	}
	return tasks
}

func runApplyTaskGraph(ctx context.Context, stdout io.Writer, stderr io.Writer, stateDir string, opts workflow.RunOptions, scope scopeSpec, clusterScope string, tasks []applyTask, limits workflow.ConcurrencyLimits) (workflow.RunLedger, error) {
	runID := applyRunID(time.Now())
	ledger := workflow.NewRunLedger(runID, scope.name, clusterScope, limits, taskLedgerEntries(tasks), time.Now())
	if err := workflow.SaveRunLedger(stateDir, ledger); err != nil {
		return ledger, err
	}
	printApplyRunStart(stdout, ledger)
	if len(tasks) == 0 {
		ledger.Finish(workflow.RunStatusOK, time.Now())
		_ = workflow.SaveRunLedger(stateDir, ledger)
		return ledger, nil
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	taskByID := map[string]applyTask{}
	for _, task := range tasks {
		taskByID[task.entry.ID] = task
	}
	parallelism := limits.Parallelism
	if parallelism <= 0 {
		parallelism = autoParallelism(len(tasks))
	}
	redfishLimit := limits.ParallelismRedfish
	if redfishLimit <= 0 {
		redfishLimit = parallelism
	}
	events := make(chan applyTaskResult)
	started := map[string]bool{}
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
			if started[task.entry.ID] || !taskReady(ledger, task.entry) || !taskSlotAvailable(task, runningRedfish, redfishLimit) {
				continue
			}
			started[task.entry.ID] = true
			startedAny = true
			running++
			if task.entry.Kind == applyTaskKindNodeBoot {
				runningRedfish++
			}
			logPath := taskLogPath(stateDir, ledger.RunID, task.entry.ID)
			ledger.MarkReady(task.entry.ID)
			ledger.MarkRunning(task.entry.ID, logPath, time.Now())
			if err := workflow.SaveRunLedger(stateDir, ledger); err != nil && firstErr == nil {
				firstErr = err
				cancel()
			}
			printApplyTaskStart(stdout, ledger, task.entry.ID)
			if opts.AskBecomePass {
				output.NewContinuation(stderr).BlankLine()
			}
			go func(task applyTask) {
				events <- runOneApplyTask(ctx, stderr, stateDir, ledger.RunID, opts, task)
			}(task)
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
		printApplyTaskResult(stdout, ledger, event.id)
	}

	if firstErr != nil {
		ledger.Finish(workflow.RunStatusFailed, time.Now())
		_ = workflow.SaveRunLedger(stateDir, ledger)
		return ledger, firstErr
	}
	ledger.Finish(workflow.RunStatusOK, time.Now())
	if err := workflow.SaveRunLedger(stateDir, ledger); err != nil {
		return ledger, err
	}
	return ledger, nil
}

func taskSlotAvailable(task applyTask, runningRedfish, redfishLimit int) bool {
	if task.entry.Kind != applyTaskKindNodeBoot {
		return true
	}
	return runningRedfish < redfishLimit
}

func runOneApplyTask(ctx context.Context, stderr io.Writer, stateDir, runID string, opts workflow.RunOptions, task applyTask) applyTaskResult {
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
	runner := ansible.CommandRunner{Stdout: io.Discard, Stderr: io.Discard}
	if taskOpts.AskBecomePass {
		runner.Stdout = stderr
		runner.Stderr = stderr
	}
	result, err := workflow.Run(ctx, taskOpts, runner, nil)
	return applyTaskResult{id: task.entry.ID, skipped: result.Skipped, err: err}
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

func taskLedgerEntries(tasks []applyTask) []workflow.TaskLedgerEntry {
	entries := make([]workflow.TaskLedgerEntry, 0, len(tasks))
	for _, task := range tasks {
		entries = append(entries, task.entry)
	}
	return entries
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
	if taskCount <= 1 {
		return 1
	}
	if taskCount < 4 {
		return taskCount
	}
	return 4
}

func taskLogPath(stateDir, runID, taskID string) string {
	return filepath.Join(stateDir, "workflow", "runs", runID, taskID, "render", "ansible", "artifacts", taskID, ansible.OutputLogName)
}
