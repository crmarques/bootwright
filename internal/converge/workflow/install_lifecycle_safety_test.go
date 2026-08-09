package workflow

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/ansible"
)

func TestClusterInstallResumeCeilingBoundsRepeatedWaits(t *testing.T) {
	dir := t.TempDir()
	state := loadWorkflowFixtureState(t, "001-sno-libvirt")
	secretsDir := writeWorkflowInstallerSecrets(t, dir)
	clustersDir := filepath.Join(dir, "clusters")
	cluster := "sno-libvirt"
	now := time.Date(2026, 8, 9, 18, 0, 0, 0, time.UTC)
	record := matchingLifecycleRecord(t, state, secretsDir, cluster, now)
	record.Status = ClusterInstallStatusFailed
	record.Phase = ClusterInstallPhaseWaitingBootstrap
	record.StartedAt = now.Add(-ClusterInstallResumeCeiling)
	if err := SaveClusterInstallRecord(clustersDir, record); err != nil {
		t.Fatalf("SaveClusterInstallRecord: %v", err)
	}

	_, _, err := ReconcileApplyClusterInstallState(context.Background(), clustersDir, filepath.Join(dir, "runs"), "test", secretsDir, "next", state, mustPlanApplyTasks(applyContainerClusterTarget(), state), ApplyModeReconcile, nil, &fakeClusterAvailabilityChecker{}, now)
	var expired *ClusterInstallResumeExpiredError
	if !errors.As(err, &expired) {
		t.Fatalf("ReconcileApplyClusterInstallState error = %v, want ClusterInstallResumeExpiredError", err)
	}
	if expired.Deadline != now || expired.Phase != ClusterInstallPhaseWaitingBootstrap {
		t.Fatalf("expired error = %+v, want bootstrap-wait deadline %s", expired, now)
	}
	if got := expired.ClusterInstallRemedy(); got.Action != ClusterInstallRemedyDestroyAndReapply || got.Cluster != cluster {
		t.Fatalf("remedy = %+v, want scoped destroy-and-reapply for %s", got, cluster)
	}

	record.StartedAt = now.Add(-ClusterInstallResumeCeiling).Add(time.Second)
	if err := SaveClusterInstallRecord(clustersDir, record); err != nil {
		t.Fatalf("SaveClusterInstallRecord inside ceiling: %v", err)
	}
	planned, _, err := ReconcileApplyClusterInstallState(context.Background(), clustersDir, filepath.Join(dir, "runs"), "test", secretsDir, "next", state, mustPlanApplyTasks(applyContainerClusterTarget(), state), ApplyModeReconcile, nil, &fakeClusterAvailabilityChecker{}, now)
	if err != nil {
		t.Fatalf("ReconcileApplyClusterInstallState inside ceiling: %v", err)
	}
	assertLifecycleTaskStatus(t, planned, ApplyTaskKindClusterISO, TaskStatusSkipped)
	assertLifecycleTaskStatus(t, planned, ApplyTaskKindNodeBoot, TaskStatusSkipped)
	assertLifecycleTaskStatus(t, planned, ApplyTaskKindBootstrapWait, TaskStatusPending)
}

func TestClusterInstallResumeWithoutStartTimeFailsClosed(t *testing.T) {
	dir := t.TempDir()
	state := loadWorkflowFixtureState(t, "001-sno-libvirt")
	secretsDir := writeWorkflowInstallerSecrets(t, dir)
	clustersDir := filepath.Join(dir, "clusters")
	cluster := "sno-libvirt"
	now := time.Date(2026, 8, 9, 18, 0, 0, 0, time.UTC)
	record := matchingLifecycleRecord(t, state, secretsDir, cluster, now)
	record.Status = ClusterInstallStatusFailed
	record.Phase = ClusterInstallPhaseNodesBooted
	record.StartedAt = time.Time{}
	if err := SaveClusterInstallRecord(clustersDir, record); err != nil {
		t.Fatalf("SaveClusterInstallRecord: %v", err)
	}

	_, _, err := ReconcileApplyClusterInstallState(context.Background(), clustersDir, filepath.Join(dir, "runs"), "test", secretsDir, "next", state, mustPlanApplyTasks(applyContainerClusterTarget(), state), ApplyModeReconcile, nil, &fakeClusterAvailabilityChecker{}, now)
	var expired *ClusterInstallResumeExpiredError
	if !errors.As(err, &expired) || !expired.StartedAt.IsZero() {
		t.Fatalf("ReconcileApplyClusterInstallState error = %v, want unknown-age resume refusal", err)
	}
}

func TestClusterInstallPrebootVersionMismatchRequiresISORegeneration(t *testing.T) {
	for _, installerVersion := range []string{"", "4.99.0"} {
		t.Run(map[bool]string{true: "missing", false: "mismatch"}[installerVersion == ""], func(t *testing.T) {
			dir := t.TempDir()
			state := loadWorkflowFixtureState(t, "001-sno-libvirt")
			secretsDir := writeWorkflowInstallerSecrets(t, dir)
			clustersDir := filepath.Join(dir, "clusters")
			cluster := "sno-libvirt"
			now := time.Date(2026, 8, 9, 18, 0, 0, 0, time.UTC)
			record := matchingLifecycleRecord(t, state, secretsDir, cluster, now)
			record.Status = ClusterInstallStatusInstalling
			record.Phase = ClusterInstallPhaseISOCreated
			record.InstallerVersion = installerVersion
			if err := SaveClusterInstallRecord(clustersDir, record); err != nil {
				t.Fatalf("SaveClusterInstallRecord: %v", err)
			}

			_, _, err := ReconcileApplyClusterInstallState(context.Background(), clustersDir, filepath.Join(dir, "runs"), "test", secretsDir, "next", state, mustPlanApplyTasks(applyContainerClusterTarget(), state), ApplyModeReconcile, nil, &fakeClusterAvailabilityChecker{}, now)
			var versionErr *ClusterInstallVersionError
			if !errors.As(err, &versionErr) {
				t.Fatalf("ReconcileApplyClusterInstallState error = %v, want ClusterInstallVersionError", err)
			}
			if versionErr.NodesMayHaveBooted || versionErr.InstallCompleted {
				t.Fatalf("version error = %+v, want preboot refusal", versionErr)
			}
			if got := versionErr.ClusterInstallRemedy(); got.Action != ClusterInstallRemedyRegenerateISO || got.Cluster != cluster {
				t.Fatalf("remedy = %+v, want scoped ISO regeneration", got)
			}
		})
	}
}

func TestClusterInstallPostbootVersionSkewResumesToWait(t *testing.T) {
	for _, installerVersion := range []string{"", "4.99.0"} {
		t.Run(map[bool]string{true: "missing", false: "mismatch"}[installerVersion == ""], func(t *testing.T) {
			dir := t.TempDir()
			state := loadWorkflowFixtureState(t, "001-sno-libvirt")
			secretsDir := writeWorkflowInstallerSecrets(t, dir)
			clustersDir := filepath.Join(dir, "clusters")
			cluster := "sno-libvirt"
			now := time.Date(2026, 8, 9, 18, 0, 0, 0, time.UTC)
			record := matchingLifecycleRecord(t, state, secretsDir, cluster, now)
			record.Status = ClusterInstallStatusFailed
			record.Phase = ClusterInstallPhaseNodesBooted
			record.InstallerVersion = installerVersion
			record.StartedAt = now.Add(-time.Hour)
			if err := SaveClusterInstallRecord(clustersDir, record); err != nil {
				t.Fatalf("SaveClusterInstallRecord: %v", err)
			}

			planned, _, err := ReconcileApplyClusterInstallState(context.Background(), clustersDir, filepath.Join(dir, "runs"), "test", secretsDir, "next", state, mustPlanApplyTasks(applyContainerClusterTarget(), state), ApplyModeReconcile, nil, &fakeClusterAvailabilityChecker{}, now)
			if err != nil {
				t.Fatalf("ReconcileApplyClusterInstallState: %v", err)
			}
			assertLifecycleTaskStatus(t, planned, ApplyTaskKindClusterISO, TaskStatusSkipped)
			assertLifecycleTaskStatus(t, planned, ApplyTaskKindNodeBoot, TaskStatusSkipped)
			assertLifecycleTaskStatus(t, planned, ApplyTaskKindBootstrapWait, TaskStatusPending)
		})
	}
}

func TestCompletedPostbootSkewIsNonzeroAndKeepsInstalledEvidence(t *testing.T) {
	dir := t.TempDir()
	state := loadWorkflowFixtureState(t, "001-sno-libvirt")
	secretsDir := writeWorkflowInstallerSecrets(t, dir)
	clustersDir := filepath.Join(dir, "clusters")
	runsDir := filepath.Join(dir, "runs")
	cluster := "sno-libvirt"
	now := time.Now().UTC()
	record := matchingLifecycleRecord(t, state, secretsDir, cluster, now)
	record.Status = ClusterInstallStatusFailed
	record.Phase = ClusterInstallPhaseBootstrapComplete
	record.InstallerVersion = "4.99.0"
	record.StartedAt = now.Add(-time.Hour)
	if err := SaveClusterInstallRecord(clustersDir, record); err != nil {
		t.Fatalf("SaveClusterInstallRecord: %v", err)
	}
	task := lifecycleTask(t, mustPlanApplyTasks(applyContainerClusterTarget(), state), ApplyTaskKindInstallWait)
	runner := &fakeRunner{}
	result := runOneApplyTask(context.Background(), io.Discard, io.Discard, runsDir, "run", RunOptions{
		State:              state,
		RenderedDir:        filepath.Join(dir, "rendered"),
		ClustersDir:        clustersDir,
		RunsDir:            runsDir,
		SecretsDir:         secretsDir,
		ManagedServicesDir: filepath.Join(dir, "managed-services"),
		ProviderStateDir:   filepath.Join(dir, "provider-state"),
		BundleDir:          filepath.Join(dir, "bundle"),
	}, task, func(io.Writer, io.Writer) ansible.Runner { return runner })
	var versionErr *ClusterInstallVersionError
	if !errors.As(result.err, &versionErr) || !versionErr.InstallCompleted {
		t.Fatalf("runOneApplyTask error = %v, want completed ClusterInstallVersionError", result.err)
	}
	if got := versionErr.ClusterInstallRemedy(); got.Action != ClusterInstallRemedyFutureRebuild || got.Cluster != cluster {
		t.Fatalf("remedy = %+v, want scoped future rebuild", got)
	}
	got, found, err := LoadClusterInstallRecord(clustersDir, cluster)
	if err != nil || !found {
		t.Fatalf("LoadClusterInstallRecord found=%v err=%v", found, err)
	}
	if got.Status != ClusterInstallStatusInstalled || got.Phase != ClusterInstallPhaseComplete || got.InstalledAt == nil || got.InstallerVersion != "4.99.0" {
		t.Fatalf("record = %+v, want retained installed evidence and ISO version", got)
	}
}

func TestFailedISORebuildRestoresPriorInstalledEvidence(t *testing.T) {
	dir := t.TempDir()
	state := loadWorkflowFixtureState(t, "001-sno-libvirt")
	secretsDir := writeWorkflowInstallerSecrets(t, dir)
	clustersDir := filepath.Join(dir, "clusters")
	runsDir := filepath.Join(dir, "runs")
	cluster := "sno-libvirt"
	now := time.Now().UTC()
	prior := matchingLifecycleRecord(t, state, secretsDir, cluster, now)
	prior.Status = ClusterInstallStatusInstalled
	prior.Phase = ClusterInstallPhaseComplete
	prior.InstallerVersion = clusterInstallDeclaredVersion(state, cluster)
	prior.InstalledAt = &now
	if err := SaveClusterInstallRecord(clustersDir, prior); err != nil {
		t.Fatalf("SaveClusterInstallRecord: %v", err)
	}
	task := lifecycleTask(t, mustPlanApplyTasks(applyContainerClusterTarget(), state), ApplyTaskKindClusterISO)
	runner := &fakeRunner{skipInstallerVersion: true}
	result := runOneApplyTask(context.Background(), io.Discard, io.Discard, runsDir, "rebuild", RunOptions{
		State:              state,
		RenderedDir:        filepath.Join(dir, "rendered"),
		ClustersDir:        clustersDir,
		RunsDir:            runsDir,
		SecretsDir:         secretsDir,
		ManagedServicesDir: filepath.Join(dir, "managed-services"),
		ProviderStateDir:   filepath.Join(dir, "provider-state"),
		BundleDir:          filepath.Join(dir, "bundle"),
	}, task, func(io.Writer, io.Writer) ansible.Runner { return runner })
	if result.err == nil {
		t.Fatal("runOneApplyTask succeeded without installer-version provenance")
	}
	got, found, err := LoadClusterInstallRecord(clustersDir, cluster)
	if err != nil || !found {
		t.Fatalf("LoadClusterInstallRecord found=%v err=%v", found, err)
	}
	if got.Status != prior.Status || got.Phase != prior.Phase || got.DesiredHash != prior.DesiredHash || got.InstallerVersion != prior.InstallerVersion || got.InstalledAt == nil {
		t.Fatalf("record = %+v, want prior installed evidence %+v", got, prior)
	}
}

func matchingLifecycleRecord(t *testing.T, state v1alpha1.State, secretsDir, cluster string, now time.Time) ClusterInstallRecord {
	t.Helper()
	hash, structuralHash, err := clusterInstallHashes("test", state, cluster, secretsDir)
	if err != nil {
		t.Fatalf("clusterInstallHashes: %v", err)
	}
	return ClusterInstallRecord{
		Cluster:          cluster,
		DesiredHash:      hash,
		StructuralHash:   structuralHash,
		HashSchema:       ConvergeHashSchema,
		InstallerVersion: clusterInstallDeclaredVersion(state, cluster),
		StartedAt:        now.UTC(),
		UpdatedAt:        now.UTC(),
	}
}

func lifecycleTask(t *testing.T, tasks []ApplyTask, kind string) ApplyTask {
	t.Helper()
	for _, task := range tasks {
		if task.Entry.Kind == kind {
			return task
		}
	}
	t.Fatalf("missing task kind %s", kind)
	return ApplyTask{}
}

func assertLifecycleTaskStatus(t *testing.T, tasks []ApplyTask, kind string, want TaskStatus) {
	t.Helper()
	task := lifecycleTask(t, tasks, kind)
	if task.Entry.Status != want {
		t.Fatalf("task %s status = %s, want %s", task.Entry.ID, task.Entry.Status, want)
	}
}
