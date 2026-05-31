package workflow

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	extensionplan "github.com/crmarques/bootwright/internal/addons/plan"
	"github.com/crmarques/bootwright/internal/converge/ansible"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/state/graph"
	storageapply "github.com/crmarques/bootwright/internal/storage"
)

const (
	ApplyTaskKindProvider               = "providerServices"
	ApplyTaskKindClusterInfra           = "clusterInfra"
	ApplyTaskKindClusterISO             = "clusterISO"
	ApplyTaskKindNodeBoot               = "nodeBoot"
	ApplyTaskKindInstallWait            = "installWait"
	ApplyTaskKindStorageCluster         = "storageCluster"
	ApplyTaskKindStorageAttachmentApply = "storageAttachmentApply"
	ApplyTaskKindClusterAddonApply      = "clusterAddonApply"
	ApplyTaskKindClusterAddonWait       = "clusterAddonWait"

	ApplyClusterKindContainer = "container"
	ApplyClusterKindStorage   = "storage"

	ApplyPhaseProvider         = "provider"
	ApplyPhaseClusterInfra     = "cluster-infra"
	ApplyPhaseStorageCluster   = "storage-cluster"
	ApplyPhaseContainerCluster = "container-cluster"
	ApplyPhaseAddons           = "addons"

	applyProviderPlaybook     = "playbooks/layers/providers/apply.yml"
	applyClusterInfraPlaybook = "playbooks/layers/cluster_infra/apply.yml"
	applyCreateISOPlaybook    = "playbooks/layers/openshift/create-agent-iso.yml"
	applyBootMachinePlaybook  = "playbooks/layers/openshift/boot-agent-machine.yml"
	applyWaitInstallPlaybook  = "playbooks/layers/openshift/wait-agent-install.yml"
	applyStoragePlaybook      = "playbooks/layers/storage/apply.yml"
)

type ApplyTarget struct {
	Name       string
	PhaseNames []string
}

type ApplyTask struct {
	Entry             TaskLedgerEntry
	Playbook          string
	Limit             string
	Forks             int
	RedfishSlots      int
	ExtraVarPairs     []string
	State             v1alpha1.State
	Extension         *extensionplan.ExtensionPlan
	StorageAttachment *StorageAttachmentPlan
}

type applyTaskResult struct {
	id            string
	skipped       bool
	skippedReason string
	failure       string
	err           error
}

type ApplyReporter interface {
	RunStart(ledger RunLedger)
	StageSnapshot(ledger RunLedger)
	RunSummary(ledger RunLedger)
	PromptGap()
}

type ApplyTaskRunnerFactory func(stdout io.Writer, stderr io.Writer) ansible.Runner

type PreparedApplyTaskGraph struct {
	RunID     string
	StartedAt time.Time
	Tasks     []ApplyTask
	Limits    ConcurrencyLimits
}

func PlanApplyTasks(target ApplyTarget, state v1alpha1.State) []ApplyTask {
	tasks, _ := PlanApplyTasksChecked(target, state)
	return tasks
}

func PlanApplyTasksChecked(target ApplyTarget, state v1alpha1.State) ([]ApplyTask, error) {
	phaseSet := map[string]bool{}
	for _, phase := range target.PhaseNames {
		phaseSet[phase] = true
	}
	var tasks []ApplyTask
	providerTaskIDs := []string{}
	kubeVirtDepsByCluster := map[string][]string{}
	if phaseSet[ApplyPhaseClusterInfra] && phaseSet[ApplyPhaseContainerCluster] && phaseSet[ApplyPhaseAddons] {
		var err error
		kubeVirtDepsByCluster, err = kubeVirtHostClusterApplyDeps(state)
		if err != nil {
			return nil, err
		}
	}
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
	storageDepsByCluster := map[string][]string{}
	if phaseSet[ApplyPhaseStorageCluster] {
		for _, cluster := range state.StorageClusters {
			if !storageClusterManaged(cluster) {
				continue
			}
			taskID := "storage." + cluster.Metadata.Name
			storageDepsByCluster[cluster.Metadata.Name] = []string{taskID}
			tasks = append(tasks, ApplyTask{
				Entry: TaskLedgerEntry{
					ID:           taskID,
					Kind:         ApplyTaskKindStorageCluster,
					Label:        "storage " + cluster.Metadata.Name,
					Cluster:      cluster.Metadata.Name,
					ClusterKind:  ApplyClusterKindStorage,
					Status:       TaskStatusPending,
					Dependencies: append([]string(nil), providerTaskIDs...),
					ResourceKeys: []string{"storage:" + cluster.Metadata.Name},
				},
				Playbook:      applyStoragePlaybook,
				Limit:         render.StorageSeedHostName(cluster.Metadata.Name),
				ExtraVarPairs: []string{"bootwright_task_storage_cluster_name=" + cluster.Metadata.Name},
				State:         storageTaskState(state, cluster.Metadata.Name),
			})
		}
	}
	infraDepsByCluster := map[string][]string{}
	clusterNames := applyClusterNames(state)
	for _, name := range clusterNames {
		if phaseSet[ApplyPhaseClusterInfra] {
			clusterState := stategraph.FilterStateToClusters(state, []string{name})
			infraHosts := render.HostGroupMembers(clusterState)[render.GroupInfraHosts]
			deps := append([]string(nil), providerTaskIDs...)
			deps = append(deps, kubeVirtDepsByCluster[name]...)
			resourceKeys := kubeVirtResourceKeys(state, name)
			if len(infraHosts) == 0 {
				taskID := "infra." + name
				infraDepsByCluster[name] = append(infraDepsByCluster[name], taskID)
				tasks = append(tasks, ApplyTask{
					Entry: TaskLedgerEntry{
						ID:           taskID,
						Kind:         ApplyTaskKindClusterInfra,
						Label:        "infra " + name,
						Cluster:      name,
						ClusterKind:  ApplyClusterKindContainer,
						Status:       TaskStatusPending,
						Dependencies: deps,
						ResourceKeys: resourceKeys,
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
						ClusterKind:  ApplyClusterKindContainer,
						Host:         host,
						ResourceKeys: append([]string{hostMutationResource(host)}, resourceKeys...),
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
		if phaseSet[ApplyPhaseContainerCluster] {
			clusterState := stategraph.FilterStateToClusters(state, []string{name})
			isoTaskID := "iso." + name
			tasks = append(tasks, ApplyTask{
				Entry: TaskLedgerEntry{
					ID:           isoTaskID,
					Kind:         ApplyTaskKindClusterISO,
					Label:        "iso " + name,
					Cluster:      name,
					ClusterKind:  ApplyClusterKindContainer,
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
				tasks = append(tasks, ApplyTask{
					Entry: TaskLedgerEntry{
						ID:           bootTaskID,
						Kind:         ApplyTaskKindNodeBoot,
						Label:        "boot " + name + " nodes",
						Cluster:      name,
						ClusterKind:  ApplyClusterKindContainer,
						ResourceKeys: applyNodeBootResourceKeys(state, name, machineNames),
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
					ClusterKind:  ApplyClusterKindContainer,
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
	if phaseSet[ApplyPhaseAddons] {
		addonTasks, err := planExtensionTasks(state, phaseSet[ApplyPhaseContainerCluster])
		if err != nil {
			return tasks, err
		}
		tasks = append(tasks, addonTasks...)
		tasks = append(tasks, planStorageAttachmentTasks(state, phaseSet[ApplyPhaseContainerCluster], storageDepsByCluster)...)
	}
	return tasks, nil
}

func planExtensionTasks(state v1alpha1.State, installPhasePlanned bool) ([]ApplyTask, error) {
	plans, err := extensionplan.BindingPlans(state)
	if err != nil {
		return nil, err
	}
	var tasks []ApplyTask
	for _, binding := range plans {
		deps := []string{}
		if installPhasePlanned {
			deps = append(deps, "wait."+binding.Cluster)
		}
		for _, extension := range binding.Addons {
			extension := extension
			applyID := "addon." + binding.Cluster + "." + extension.Name + ".apply"
			waitID := "addon." + binding.Cluster + "." + extension.Name + ".wait"
			tasks = append(tasks, ApplyTask{
				Entry: TaskLedgerEntry{
					ID:           applyID,
					Kind:         ApplyTaskKindClusterAddonApply,
					Label:        "addon " + binding.Cluster + " " + extension.Name + " apply",
					Cluster:      binding.Cluster,
					ClusterKind:  ApplyClusterKindContainer,
					Status:       TaskStatusPending,
					Dependencies: append([]string(nil), deps...),
				},
				State:     stategraph.FilterStateToClusters(state, []string{binding.Cluster}),
				Extension: &extension,
			})
			tasks = append(tasks, ApplyTask{
				Entry: TaskLedgerEntry{
					ID:           waitID,
					Kind:         ApplyTaskKindClusterAddonWait,
					Label:        "addon " + binding.Cluster + " " + extension.Name + " wait",
					Cluster:      binding.Cluster,
					ClusterKind:  ApplyClusterKindContainer,
					Status:       TaskStatusPending,
					Dependencies: []string{applyID},
				},
				State:     stategraph.FilterStateToClusters(state, []string{binding.Cluster}),
				Extension: &extension,
			})
			deps = []string{waitID}
		}
	}
	return tasks, nil
}

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
	tasks, err = ReconcileApplyClusterInstallState(ctx, opts.ClustersDir, opts.SecretsDir, runID, opts.State, tasks, opts.InstallOverride, opts.ClusterAvailabilityChecker, startedAt)
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

func blockedApplyTaskReason(ledger RunLedger, task TaskLedgerEntry) string {
	unresolved := []string{}
	for _, dep := range task.Dependencies {
		depTask, ok := ledger.Task(dep)
		if !ok {
			unresolved = append(unresolved, dep+" (missing)")
			continue
		}
		switch depTask.Status {
		case TaskStatusOK, TaskStatusSkipped:
		default:
			unresolved = append(unresolved, fmt.Sprintf("%s (%s)", dep, depTask.Status))
		}
	}
	if len(unresolved) > 0 {
		return "dependencies did not complete: " + strings.Join(unresolved, ", ")
	}
	return "apply task graph could not make progress"
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
	result := runOneApplyTaskInner(ctx, stdout, stderr, runsDir, runID, opts, task, runnerFactory)
	if result.err == nil {
		return result
	}
	failure := conciseApplyTaskFailure(result.err.Error())
	logPath := TaskLogPath(runsDir, runID, task.Entry.ID)
	result.failure = failure
	result.err = fmt.Errorf("%s failed: %s (log: %s)", task.Entry.Label, failure, logPath)
	return result
}

func runOneApplyTaskInner(ctx context.Context, stdout io.Writer, stderr io.Writer, runsDir, runID string, opts RunOptions, task ApplyTask, runnerFactory ApplyTaskRunnerFactory) applyTaskResult {
	if task.Entry.Kind == ApplyTaskKindClusterAddonApply || task.Entry.Kind == ApplyTaskKindClusterAddonWait {
		return runOneExtensionTask(ctx, stdout, stderr, runsDir, runID, opts, task)
	}
	if task.Entry.Kind == ApplyTaskKindStorageAttachmentApply {
		return runOneStorageAttachmentTask(ctx, stdout, stderr, runsDir, runID, opts, task)
	}
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
	if err := MarkClusterInstallTaskStarted(opts.ClustersDir, opts.SecretsDir, runID, task, now); err != nil {
		return applyTaskResult{id: task.Entry.ID, err: err}
	}
	result, err := Run(ctx, taskOpts, runner, nil)
	now = time.Now()
	if err != nil {
		if recordErr := MarkClusterInstallTaskFailed(opts.ClustersDir, opts.SecretsDir, runID, task, now); recordErr != nil {
			err = fmt.Errorf("%w; additionally failed to record cluster install state: %v", err, recordErr)
		}
		return applyTaskResult{id: task.Entry.ID, skipped: result.Skipped, err: err}
	}
	if task.Entry.Kind == ApplyTaskKindStorageCluster && !result.Skipped {
		if err := storageapply.PersistCephApplyResult(storageapply.CephApplyResultOptions{
			State:              task.State,
			ClustersDir:        opts.ClustersDir,
			StorageClusterName: strings.TrimPrefix(task.Entry.ID, "storage."),
			ResultPath:         filepath.Join(taskOpts.ArtifactsRoot, "storage-result.json"),
		}); err != nil {
			return applyTaskResult{id: task.Entry.ID, skipped: result.Skipped, err: err}
		}
	}
	if !result.Skipped {
		if recordErr := MarkClusterInstallTaskSucceeded(opts.ClustersDir, opts.SecretsDir, runID, task, now); recordErr != nil {
			return applyTaskResult{id: task.Entry.ID, skipped: result.Skipped, err: recordErr}
		}
	}
	return applyTaskResult{id: task.Entry.ID, skipped: result.Skipped, err: err}
}

func conciseApplyTaskFailure(message string) string {
	lines := strings.Split(message, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "failure:") {
			return trimApplyTaskFailure(strings.TrimSpace(strings.TrimPrefix(line, "failure:")))
		}
	}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "last ") || strings.HasPrefix(line, "underlying error:") {
			continue
		}
		return trimApplyTaskFailure(line)
	}
	return "task failed"
}

func trimApplyTaskFailure(value string) string {
	const limit = 180
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit-3] + "..."
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
