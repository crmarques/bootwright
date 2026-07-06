package workflow

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/converge/ansible"
)

func auditRunOptions(dir string) RunOptions {
	return RunOptions{
		State:              minimalState(),
		RenderedDir:        filepath.Join(dir, "rendered"),
		ClustersDir:        filepath.Join(dir, "clusters"),
		RunsDir:            filepath.Join(dir, "runs"),
		SecretsDir:         filepath.Join(dir, "secrets"),
		ManagedServicesDir: filepath.Join(dir, "managed-services"),
		ProviderStateDir:   filepath.Join(dir, "provider-state"),
		BundleDir:          filepath.Join(dir, "bundle"),
	}
}

// Note: the scheduler's stop-launching-after-context-cancel behavior (the guard
// added in apply_scheduler.go) is validated by TestRunApplyTaskGraphStopsLaunching
// AfterContextCancel in apply_tasks_test.go, owned by a parallel change.

// TestRunApplyTaskGraphStopsOnLeaseTakeover pins that a heartbeat which discovers
// the run lease was taken over by a newer run aborts THIS run (rather than silently
// continuing to launch mutating tasks lease-less, the double-mutator the lease is
// meant to prevent). Without the abort the blocking task below never gets cancelled
// and the test deadlocks.
func TestRunApplyTaskGraphStopsOnLeaseTakeover(t *testing.T) {
	dir := t.TempDir()
	oldSave := saveRunLease
	oldInterval := applyLeaseHeartbeatInterval
	saveRunLease = func(string, RunLease) error { return ErrLeaseNotOwned }
	applyLeaseHeartbeatInterval = time.Millisecond
	t.Cleanup(func() {
		saveRunLease = oldSave
		applyLeaseHeartbeatInterval = oldInterval
	})

	runner := &blockingApplyRunner{started: make(chan struct{})}
	task := ApplyTask{Entry: TaskLedgerEntry{ID: "provider.service-host", Kind: ApplyTaskKindProvider, Label: "provider services service-host", Status: TaskStatusPending}, Playbook: applyProviderPlaybook, State: minimalState()}
	_, err := RunApplyTaskGraph(context.Background(), io.Discard, io.Discard, filepath.Join(dir, "runs"), auditRunOptions(dir),
		ApplyTarget{Name: "infra", PhaseNames: []string{ApplyPhaseFabric}}, "", []ApplyTask{task},
		ConcurrencyLimits{Parallelism: 1}, nil, func(_ io.Writer, _ io.Writer) ansible.Runner { return runner })
	if err == nil || !strings.Contains(err.Error(), "taken over by another run") {
		t.Fatalf("RunApplyTaskGraph error = %v, want lease-takeover error", err)
	}
}

// TestReconcileOverrideProbeErrorRebuilds pins that under --override an availability
// probe ERROR (oc missing, or the API refusing connection on a hard-down cluster)
// does NOT fail the apply: override exists to rebuild an unreachable cluster, so an
// unverifiable probe leaves the install tasks scheduled to run (rebuild) rather than
// aborting the whole apply.
func TestReconcileOverrideProbeErrorRebuilds(t *testing.T) {
	state := loadWorkflowFixtureState(t, "001-sno-libvirt")
	const cluster = "sno-libvirt"
	tasks := mustPlanApplyTasks(applyContainerClusterTarget(), state)
	now := time.Now()
	dir := t.TempDir()
	clustersDir := filepath.Join(dir, "clusters")
	secretsDir := writeWorkflowInstallerSecrets(t, dir)
	hash, err := clusterInstallDesiredHashForContext("test", state, cluster, secretsDir)
	if err != nil {
		t.Fatalf("clusterInstallDesiredHash: %v", err)
	}
	if err := SaveClusterInstallRecord(clustersDir, ClusterInstallRecord{
		Cluster: cluster, DesiredHash: hash,
		Status: ClusterInstallStatusInstalled, Phase: ClusterInstallPhaseComplete,
		UpdatedAt: now.UTC(),
	}); err != nil {
		t.Fatalf("SaveClusterInstallRecord: %v", err)
	}
	writeAuditKubeconfig(t, clustersDir, cluster)
	checker := &fakeClusterAvailabilityChecker{err: errors.New("connection refused")}
	out, err := ReconcileApplyClusterInstallState(context.Background(), clustersDir, "", secretsDir, "run", state, tasks, ApplyModeOverride, checker, now)
	if err != nil {
		t.Fatalf("override with a probe error must not fail the apply: %v", err)
	}
	if skipped, total := auditInstallSkipped(out, cluster); total == 0 || skipped != 0 {
		t.Fatalf("override over an API-dead cluster must rebuild (skip 0 of %d install tasks), skipped %d", total, skipped)
	}
}

// TestReconcileContinueProbeErrorNamesRemedy pins that the default (continue) mode
// refusal on an unverifiable probe names an actionable remedy (--override) rather
// than dead-ending the operator.
func TestReconcileContinueProbeErrorNamesRemedy(t *testing.T) {
	state := loadWorkflowFixtureState(t, "001-sno-libvirt")
	const cluster = "sno-libvirt"
	tasks := mustPlanApplyTasks(applyContainerClusterTarget(), state)
	now := time.Now()
	dir := t.TempDir()
	clustersDir := filepath.Join(dir, "clusters")
	secretsDir := writeWorkflowInstallerSecrets(t, dir)
	hash, err := clusterInstallDesiredHashForContext("test", state, cluster, secretsDir)
	if err != nil {
		t.Fatalf("clusterInstallDesiredHash: %v", err)
	}
	if err := SaveClusterInstallRecord(clustersDir, ClusterInstallRecord{
		Cluster: cluster, DesiredHash: hash,
		Status: ClusterInstallStatusInstalled, Phase: ClusterInstallPhaseComplete,
		UpdatedAt: now.UTC(),
	}); err != nil {
		t.Fatalf("SaveClusterInstallRecord: %v", err)
	}
	writeAuditKubeconfig(t, clustersDir, cluster)
	checker := &fakeClusterAvailabilityChecker{err: errors.New("connection refused")}
	_, err = ReconcileApplyClusterInstallState(context.Background(), clustersDir, "", secretsDir, "run", state, tasks, ApplyModeContinue, checker, now)
	if err == nil {
		t.Fatal("continue mode must refuse an unverifiable probe")
	}
	if !strings.Contains(err.Error(), "availability could not be verified") || !strings.Contains(err.Error(), "--override") {
		t.Fatalf("refusal must name the --override remedy, got: %v", err)
	}
}

// TestReconcileRefusesUnrecordedAvailableCluster pins that a reachable cluster with
// NO install record is refused rather than silently adopted with today's hashes
// (which would absorb real install-input drift as installed+in-sync). A cluster with
// no kubeconfig yet is a fresh install and proceeds normally.
func TestReconcileRefusesUnrecordedAvailableCluster(t *testing.T) {
	state := loadWorkflowFixtureState(t, "001-sno-libvirt")
	const cluster = "sno-libvirt"
	tasks := mustPlanApplyTasks(applyContainerClusterTarget(), state)
	now := time.Now()

	t.Run("record-less reachable cluster is refused", func(t *testing.T) {
		dir := t.TempDir()
		clustersDir := filepath.Join(dir, "clusters")
		secretsDir := writeWorkflowInstallerSecrets(t, dir)
		writeAuditKubeconfig(t, clustersDir, cluster)
		checker := &fakeClusterAvailabilityChecker{available: true}
		_, err := ReconcileApplyClusterInstallState(context.Background(), clustersDir, "", secretsDir, "run", state, tasks, ApplyModeContinue, checker, now)
		if err == nil || !strings.Contains(err.Error(), "no install record") {
			t.Fatalf("record-less reachable cluster must be refused, got: %v", err)
		}
	})

	t.Run("fresh cluster with no kubeconfig installs", func(t *testing.T) {
		dir := t.TempDir()
		clustersDir := filepath.Join(dir, "clusters")
		secretsDir := writeWorkflowInstallerSecrets(t, dir)
		checker := &fakeClusterAvailabilityChecker{available: true}
		out, err := ReconcileApplyClusterInstallState(context.Background(), clustersDir, "", secretsDir, "run", state, tasks, ApplyModeContinue, checker, now)
		if err != nil {
			t.Fatalf("a fresh cluster (no kubeconfig) must install: %v", err)
		}
		if skipped, total := auditInstallSkipped(out, cluster); total == 0 || skipped != 0 {
			t.Fatalf("fresh install must not skip install tasks (skip 0 of %d), skipped %d", total, skipped)
		}
	})
}

// TestPlanMachinesSubPhaseLibvirt pins that the documented `--stage machines`
// sub-phase plans on a libvirt substrate: the machine-prepare tasks it schedules
// require provider.host-ready, which only the fabric phase provides — planning
// assumes a prior fabric apply rather than hard-failing Lower().
func TestPlanMachinesSubPhaseLibvirt(t *testing.T) {
	state := loadWorkflowFixtureState(t, "001-sno-libvirt")
	tasks, err := PlanApplyTasksChecked(ApplyTarget{Name: "machines", PhaseNames: []string{ApplyPhaseMachines}}, state)
	if err != nil {
		t.Fatalf("machines sub-phase must plan on a libvirt substrate: %v", err)
	}
	foundPrepare := false
	for _, task := range tasks {
		if task.Entry.Kind == ApplyTaskKindMachineInfraPrepare {
			foundPrepare = true
			break
		}
	}
	if !foundPrepare {
		t.Fatalf("machines plan should schedule a machine-prepare task (which required provider.host-ready): %v", taskKinds(tasks))
	}
}

func auditInstallSkipped(out []ApplyTask, cluster string) (skipped, total int) {
	for _, task := range out {
		if task.Entry.Cluster != cluster || !isClusterInstallTaskKind(task.Entry.Kind) {
			continue
		}
		total++
		if task.Entry.Status == TaskStatusSkipped {
			skipped++
		}
	}
	return skipped, total
}

func writeAuditKubeconfig(t *testing.T, clustersDir, cluster string) {
	t.Helper()
	kubeconfig := clusterKubeconfigPath(clustersDir, cluster)
	if err := os.MkdirAll(filepath.Dir(kubeconfig), 0o700); err != nil {
		t.Fatalf("mkdir kubeconfig dir: %v", err)
	}
	if err := os.WriteFile(kubeconfig, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
}
