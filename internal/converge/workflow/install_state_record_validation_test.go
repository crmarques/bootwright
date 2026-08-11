package workflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/converge/remedy"
)

func TestValidateClusterInstallRecordStateMatrix(t *testing.T) {
	statuses := []ClusterInstallStatus{
		ClusterInstallStatusInstalling,
		ClusterInstallStatusFailed,
		ClusterInstallStatusInstalled,
		ClusterInstallStatusDestroyed,
		"",
		"future-status",
	}
	phases := []ClusterInstallPhase{
		"",
		ClusterInstallPhaseCreatingISO,
		ClusterInstallPhaseISOCreated,
		ClusterInstallPhaseBooting,
		ClusterInstallPhaseNodesBooted,
		ClusterInstallPhaseWaitingBootstrap,
		ClusterInstallPhaseBootstrapComplete,
		ClusterInstallPhaseWaiting,
		ClusterInstallPhaseComplete,
		"future-phase",
	}
	valid := map[ClusterInstallStatus]map[ClusterInstallPhase]bool{
		ClusterInstallStatusInstalling: {
			"":                                   true,
			ClusterInstallPhaseCreatingISO:       true,
			ClusterInstallPhaseISOCreated:        true,
			ClusterInstallPhaseBooting:           true,
			ClusterInstallPhaseNodesBooted:       true,
			ClusterInstallPhaseWaitingBootstrap:  true,
			ClusterInstallPhaseBootstrapComplete: true,
			ClusterInstallPhaseWaiting:           true,
		},
		ClusterInstallStatusFailed: {
			"":                                   true,
			ClusterInstallPhaseCreatingISO:       true,
			ClusterInstallPhaseISOCreated:        true,
			ClusterInstallPhaseBooting:           true,
			ClusterInstallPhaseNodesBooted:       true,
			ClusterInstallPhaseWaitingBootstrap:  true,
			ClusterInstallPhaseBootstrapComplete: true,
			ClusterInstallPhaseWaiting:           true,
		},
		ClusterInstallStatusInstalled: {
			ClusterInstallPhaseComplete: true,
		},
		ClusterInstallStatusDestroyed: {
			ClusterInstallPhaseComplete: true,
		},
	}
	clustersDir := filepath.Join(t.TempDir(), "clusters")
	const cluster = "ocp"

	for _, status := range statuses {
		for _, phase := range phases {
			name := fmt.Sprintf("status=%q/phase=%q", status, phase)
			t.Run(name, func(t *testing.T) {
				record := syntheticClusterInstallRecord(cluster, status, phase, time.Now().UTC())
				err := validateClusterInstallRecordState(clustersDir, cluster, record)
				if valid[status][phase] {
					if err != nil {
						t.Fatalf("valid install record state rejected: %v", err)
					}
					return
				}
				if err == nil {
					t.Fatal("invalid install record state accepted")
				}
				var stateErr *ClusterInstallStateError
				if !errors.As(err, &stateErr) {
					t.Fatalf("error = %T, want *ClusterInstallStateError", err)
				}
				wantCondition := ClusterInstallConditionInvalidRecordState
				if phase == "future-phase" {
					wantCondition = ClusterInstallConditionUnrecognizedPhase
				}
				if stateErr.Condition != wantCondition || stateErr.Cluster != cluster || stateErr.Status != status || stateErr.Phase != phase || stateErr.RecordPath != ClusterInstallRecordPath(clustersDir, cluster) {
					t.Fatalf("typed state error = %#v, want condition=%q cluster=%q status=%q phase=%q path=%q", stateErr, wantCondition, cluster, status, phase, ClusterInstallRecordPath(clustersDir, cluster))
				}
				for _, want := range []string{cluster, fmt.Sprintf("%q", phase), "refuses before any mutation"} {
					if !strings.Contains(err.Error(), want) {
						t.Fatalf("error %q does not contain %q", err, want)
					}
				}
				assertClusterInstallRemedy(t, err, remedy.ActionRebuildCluster, cluster)
				assertInstallErrorHasNoArgv(t, err)
			})
		}
	}
}

func TestValidateClusterInstallRecordEvidenceMatrix(t *testing.T) {
	const cluster = "ocp"
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	installed := syntheticClusterInstallRecord(cluster, ClusterInstallStatusInstalled, ClusterInstallPhaseComplete, now)
	installing := syntheticClusterInstallRecord(cluster, ClusterInstallStatusInstalling, ClusterInstallPhaseNodesBooted, now)
	tests := []struct {
		name   string
		record ClusterInstallRecord
		valid  bool
		detail string
	}{
		{name: "current installed", record: installed, valid: true},
		{name: "previous schema installed", record: mutateInstallRecord(installed, func(record *ClusterInstallRecord) { record.HashSchema = ConvergeHashSchema - 1 }), valid: true},
		{name: "current incomplete", record: installing, valid: true},
		{name: "released sentinel", record: ClusterInstallRecord{Cluster: cluster, Status: ClusterInstallStatusDestroyed, Phase: ClusterInstallPhaseComplete}, valid: true},
		{name: "wrong cluster", record: mutateInstallRecord(installed, func(record *ClusterInstallRecord) { record.Cluster = "other" }), detail: `record cluster is "other", want "ocp"`},
		{name: "empty cluster", record: mutateInstallRecord(installed, func(record *ClusterInstallRecord) { record.Cluster = "" }), detail: `record cluster is "", want "ocp"`},
		{name: "missing schema", record: mutateInstallRecord(installed, func(record *ClusterInstallRecord) { record.HashSchema = 0 }), detail: "hashSchema is 0"},
		{name: "future schema", record: mutateInstallRecord(installed, func(record *ClusterInstallRecord) { record.HashSchema++ }), detail: "hashSchema is 5"},
		{name: "previous schema incomplete", record: mutateInstallRecord(installing, func(record *ClusterInstallRecord) { record.HashSchema-- }), detail: "hashSchema is 3, want 4"},
		{name: "missing desired hash", record: mutateInstallRecord(installed, func(record *ClusterInstallRecord) { record.DesiredHash = "" }), detail: "desiredHash is not sha256"},
		{name: "short desired hash", record: mutateInstallRecord(installed, func(record *ClusterInstallRecord) { record.DesiredHash = "sha256:abcd" }), detail: "desiredHash is not sha256"},
		{name: "uppercase desired hash", record: mutateInstallRecord(installed, func(record *ClusterInstallRecord) { record.DesiredHash = "sha256:" + strings.Repeat("A", 64) }), detail: "desiredHash is not sha256"},
		{name: "missing structural hash", record: mutateInstallRecord(installed, func(record *ClusterInstallRecord) { record.StructuralHash = "" }), detail: "structuralHash is not sha256"},
		{name: "invalid structural hash", record: mutateInstallRecord(installed, func(record *ClusterInstallRecord) { record.StructuralHash = "sha256:" + strings.Repeat("g", 64) }), detail: "structuralHash is not sha256"},
		{name: "missing run ID", record: mutateInstallRecord(installed, func(record *ClusterInstallRecord) { record.RunID = "" }), detail: "runId is empty"},
		{name: "blank run ID", record: mutateInstallRecord(installed, func(record *ClusterInstallRecord) { record.RunID = " \t" }), detail: "runId is empty"},
		{name: "missing installed start", record: mutateInstallRecord(installed, func(record *ClusterInstallRecord) { record.StartedAt = time.Time{} }), detail: "startedAt is missing"},
		{name: "missing installed update", record: mutateInstallRecord(installed, func(record *ClusterInstallRecord) { record.UpdatedAt = time.Time{} }), detail: "updatedAt is missing"},
		{name: "update before start", record: mutateInstallRecord(installed, func(record *ClusterInstallRecord) { record.UpdatedAt = record.StartedAt.Add(-time.Second) }), detail: "updatedAt precedes startedAt"},
		{name: "missing installed time", record: mutateInstallRecord(installed, func(record *ClusterInstallRecord) { record.InstalledAt = nil }), detail: "installedAt is missing"},
		{name: "zero installed time", record: mutateInstallRecord(installed, func(record *ClusterInstallRecord) { zero := time.Time{}; record.InstalledAt = &zero }), detail: "installedAt is missing"},
		{name: "installed before start", record: mutateInstallRecord(installed, func(record *ClusterInstallRecord) {
			value := record.StartedAt.Add(-time.Second)
			record.InstalledAt = &value
		}), detail: "installedAt precedes startedAt"},
		{name: "installed after update", record: mutateInstallRecord(installed, func(record *ClusterInstallRecord) {
			value := record.UpdatedAt.Add(time.Second)
			record.InstalledAt = &value
		}), detail: "installedAt follows updatedAt"},
		{name: "installed time on incomplete", record: mutateInstallRecord(installing, func(record *ClusterInstallRecord) { value := record.UpdatedAt; record.InstalledAt = &value }), detail: `installedAt is present for status "installing"`},
		{name: "missing incomplete update", record: mutateInstallRecord(installing, func(record *ClusterInstallRecord) { record.UpdatedAt = time.Time{} }), detail: "updatedAt is missing"},
		{name: "missing postboot start uses resume gate", record: mutateInstallRecord(installing, func(record *ClusterInstallRecord) { record.StartedAt = time.Time{} }), valid: true},
		{name: "missing ISO update uses freshness gate", record: mutateInstallRecord(syntheticClusterInstallRecord(cluster, ClusterInstallStatusInstalling, ClusterInstallPhaseISOCreated, now), func(record *ClusterInstallRecord) { record.UpdatedAt = time.Time{} }), valid: true},
		{name: "wrong cluster released sentinel", record: ClusterInstallRecord{Cluster: "other", Status: ClusterInstallStatusDestroyed, Phase: ClusterInstallPhaseComplete}, detail: `record cluster is "other", want "ocp"`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateClusterInstallRecordState(t.TempDir(), cluster, tc.record)
			if tc.valid {
				if err != nil {
					t.Fatalf("valid install record evidence rejected: %v", err)
				}
				return
			}
			var stateErr *ClusterInstallStateError
			if !errors.As(err, &stateErr) || stateErr.Condition != ClusterInstallConditionInvalidRecordEvidence {
				t.Fatalf("error = %#v, want invalid-record-evidence", err)
			}
			if !strings.Contains(err.Error(), tc.detail) || !strings.Contains(err.Error(), "refuses before any mutation") {
				t.Fatalf("error %q does not contain %q and refusal boundary", err, tc.detail)
			}
			assertClusterInstallRemedy(t, err, remedy.ActionRebuildCluster, cluster)
			assertInstallErrorHasNoArgv(t, err)
		})
	}
}

func TestCopiedClusterInstallRecordCannotAuthorizeSkipResumeOrRebuild(t *testing.T) {
	state := loadWorkflowFixtureState(t, "001-sno-libvirt")
	const cluster = "sno-libvirt"
	const sourceCluster = "copied-source"
	tasks := mustPlanApplyTasks(applyContainerClusterTarget(), state)
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		status ClusterInstallStatus
		phase  ClusterInstallPhase
	}{
		{name: "installed record cannot authorize healthy skip", status: ClusterInstallStatusInstalled, phase: ClusterInstallPhaseComplete},
		{name: "ISO record cannot authorize node boot resume", status: ClusterInstallStatusInstalling, phase: ClusterInstallPhaseISOCreated},
		{name: "postboot record cannot authorize wait resume", status: ClusterInstallStatusFailed, phase: ClusterInstallPhaseNodesBooted},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			clustersDir := filepath.Join(dir, "clusters")
			secretsDir := writeWorkflowInstallerSecrets(t, dir)
			record := matchingLifecycleRecord(t, state, secretsDir, cluster, now)
			record.Cluster = sourceCluster
			record.Status = tc.status
			record.Phase = tc.phase
			if tc.status == ClusterInstallStatusInstalled {
				installedAt := now
				record.InstalledAt = &installedAt
			}
			copyClusterInstallRecordToPath(t, clustersDir, record, cluster)
			checker := &fakeClusterAvailabilityChecker{available: true}
			planned, installed, err := ReconcileApplyClusterInstallState(context.Background(), clustersDir, filepath.Join(dir, "runs"), "test", secretsDir, "next", state, tasks, ApplyModeReconcile, nil, checker, now)
			var stateErr *ClusterInstallStateError
			if !errors.As(err, &stateErr) || stateErr.Condition != ClusterInstallConditionInvalidRecordEvidence {
				t.Fatalf("copied record error = %#v, want invalid-record-evidence", err)
			}
			if !reflect.DeepEqual(planned, tasks) || len(installed) != 0 || len(checker.paths) != 0 {
				t.Fatalf("copied record authorized planning: tasks=%#v installed=%v probes=%v", planned, installed, checker.paths)
			}
			if got := BootProvenContainerClusters(clustersDir, tasks); len(got) != 0 {
				t.Fatalf("copied record proved boot for %v", got)
			}
		})
	}

	dir := t.TempDir()
	clustersDir := filepath.Join(dir, "clusters")
	secretsDir := writeWorkflowInstallerSecrets(t, dir)
	record := matchingLifecycleRecord(t, state, secretsDir, cluster, now)
	record.Cluster = sourceCluster
	record.Status = ClusterInstallStatusInstalled
	record.Phase = ClusterInstallPhaseComplete
	installedAt := now
	record.InstalledAt = &installedAt
	copyClusterInstallRecordToPath(t, clustersDir, record, cluster)
	reinstalls, err := OverrideRebuildInstalledClusters(context.Background(), clustersDir, filepath.Join(dir, "runs"), "test", secretsDir, state, tasks, &fakeClusterAvailabilityChecker{available: true})
	if err != nil {
		t.Fatalf("OverrideRebuildInstalledClusters: %v", err)
	}
	if len(reinstalls) != 1 || reinstalls[0].Name != cluster || !strings.Contains(reinstalls[0].Descriptor, `record cluster is "copied-source", want "sno-libvirt"`) {
		t.Fatalf("copied installed record rebuild preview = %#v", reinstalls)
	}
	_, _, err = ReconcileApplyClusterInstallState(context.Background(), clustersDir, filepath.Join(dir, "runs"), "test", secretsDir, "next", state, tasks, ApplyModeRebuild, nil, &fakeClusterAvailabilityChecker{available: true}, now)
	assertClusterInstallRemedy(t, err, remedy.ActionRebuildCluster, cluster)
	planned, installed, err := ReconcileApplyClusterInstallState(context.Background(), clustersDir, filepath.Join(dir, "runs"), "test", secretsDir, "next", state, tasks, ApplyModeRebuild, []string{cluster}, &fakeClusterAvailabilityChecker{available: true}, now)
	if err != nil || !reflect.DeepEqual(planned, tasks) || len(installed) != 0 {
		t.Fatalf("acknowledged copied-record rebuild = tasks=%#v installed=%v err=%v", planned, installed, err)
	}
}

func syntheticClusterInstallRecord(cluster string, status ClusterInstallStatus, phase ClusterInstallPhase, now time.Time) ClusterInstallRecord {
	record := ClusterInstallRecord{
		Cluster:        cluster,
		DesiredHash:    "sha256:" + strings.Repeat("a", 64),
		StructuralHash: "sha256:" + strings.Repeat("b", 64),
		HashSchema:     ConvergeHashSchema,
		Status:         status,
		Phase:          phase,
		RunID:          "seeded-run",
		StartedAt:      now.UTC(),
		UpdatedAt:      now.UTC(),
	}
	if status == ClusterInstallStatusInstalled {
		installedAt := now.UTC()
		record.InstalledAt = &installedAt
	}
	return record
}

func mutateInstallRecord(record ClusterInstallRecord, mutate func(*ClusterInstallRecord)) ClusterInstallRecord {
	mutate(&record)
	return record
}

func copyClusterInstallRecordToPath(t *testing.T, clustersDir string, record ClusterInstallRecord, destinationCluster string) {
	t.Helper()
	if err := SaveClusterInstallRecord(clustersDir, record); err != nil {
		t.Fatalf("save source install record: %v", err)
	}
	data, err := os.ReadFile(ClusterInstallRecordPath(clustersDir, record.Cluster))
	if err != nil {
		t.Fatalf("read source install record: %v", err)
	}
	path := ClusterInstallRecordPath(clustersDir, destinationCluster)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create copied install record directory: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write copied install record: %v", err)
	}
}

func TestReconcileApplyClusterInstallStateRejectsInvalidRecordBeforePlanning(t *testing.T) {
	state := loadWorkflowFixtureState(t, "001-sno-libvirt")
	const cluster = "sno-libvirt"
	tasks := mustPlanApplyTasks(applyContainerClusterTarget(), state)
	tests := []struct {
		name      string
		status    ClusterInstallStatus
		phase     ClusterInstallPhase
		condition ClusterInstallCondition
	}{
		{name: "unknown status", status: "future-status", phase: ClusterInstallPhaseCreatingISO, condition: ClusterInstallConditionInvalidRecordState},
		{name: "installing complete", status: ClusterInstallStatusInstalling, phase: ClusterInstallPhaseComplete, condition: ClusterInstallConditionInvalidRecordState},
		{name: "failed complete", status: ClusterInstallStatusFailed, phase: ClusterInstallPhaseComplete, condition: ClusterInstallConditionInvalidRecordState},
		{name: "installed nonterminal", status: ClusterInstallStatusInstalled, phase: ClusterInstallPhaseWaiting, condition: ClusterInstallConditionInvalidRecordState},
		{name: "destroyed nonterminal", status: ClusterInstallStatusDestroyed, phase: "", condition: ClusterInstallConditionInvalidRecordState},
		{name: "unknown phase", status: ClusterInstallStatusInstalling, phase: "future-phase", condition: ClusterInstallConditionUnrecognizedPhase},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			clustersDir := filepath.Join(dir, "clusters")
			if err := SaveClusterInstallRecord(clustersDir, ClusterInstallRecord{
				Cluster: cluster,
				Status:  tc.status,
				Phase:   tc.phase,
			}); err != nil {
				t.Fatalf("SaveClusterInstallRecord: %v", err)
			}
			checker := &fakeClusterAvailabilityChecker{available: true}
			planned, installed, err := ReconcileApplyClusterInstallState(context.Background(), clustersDir, "", "test", filepath.Join(dir, "missing-secrets"), "run", state, tasks, ApplyModeReconcile, nil, checker, time.Now())
			if err == nil {
				t.Fatal("invalid install record state was planned")
			}
			var stateErr *ClusterInstallStateError
			if !errors.As(err, &stateErr) || stateErr.Condition != tc.condition {
				t.Fatalf("error = %#v, want condition %q", err, tc.condition)
			}
			assertClusterInstallRemedy(t, err, remedy.ActionRebuildCluster, cluster)
			if !reflect.DeepEqual(planned, tasks) {
				t.Fatalf("invalid record changed task plan:\ngot  %#v\nwant %#v", planned, tasks)
			}
			if len(installed) != 0 || len(checker.paths) != 0 {
				t.Fatalf("invalid record reached install or availability planning: installed=%v probes=%v", installed, checker.paths)
			}
		})
	}
}

func TestInvalidClusterInstallRecordRequiresAndAllowsAcknowledgedRebuild(t *testing.T) {
	state := loadWorkflowFixtureState(t, "001-sno-libvirt")
	const cluster = "sno-libvirt"
	tasks := mustPlanApplyTasks(applyContainerClusterTarget(), state)
	dir := t.TempDir()
	clustersDir := filepath.Join(dir, "clusters")
	secretsDir := writeWorkflowInstallerSecrets(t, dir)
	if err := SaveClusterInstallRecord(clustersDir, ClusterInstallRecord{
		Cluster: cluster,
		Status:  ClusterInstallStatusInstalling,
		Phase:   ClusterInstallPhaseComplete,
	}); err != nil {
		t.Fatalf("SaveClusterInstallRecord: %v", err)
	}
	checker := &fakeClusterAvailabilityChecker{available: true}
	reinstalls, err := OverrideRebuildInstalledClusters(context.Background(), clustersDir, "", "test", secretsDir, state, tasks, checker)
	if err != nil {
		t.Fatalf("OverrideRebuildInstalledClusters: %v", err)
	}
	if len(reinstalls) != 1 || reinstalls[0].Name != cluster || !strings.Contains(reinstalls[0].Descriptor, "install record has unsupported lifecycle state") || !strings.Contains(reinstalls[0].Descriptor, "wipes its node disks") {
		t.Fatalf("invalid record rebuild preview = %#v", reinstalls)
	}
	if len(checker.paths) != 0 {
		t.Fatalf("invalid record rebuild preview probed availability: %v", checker.paths)
	}

	_, _, err = ReconcileApplyClusterInstallState(context.Background(), clustersDir, "", "test", secretsDir, "run", state, tasks, ApplyModeRebuild, nil, checker, time.Now())
	if err == nil {
		t.Fatal("unacknowledged invalid-record rebuild was planned")
	}
	assertClusterInstallRemedy(t, err, remedy.ActionRebuildCluster, cluster)

	planned, installed, err := ReconcileApplyClusterInstallState(context.Background(), clustersDir, "", "test", secretsDir, "run", state, tasks, ApplyModeRebuild, []string{cluster}, checker, time.Now())
	if err != nil {
		t.Fatalf("acknowledged invalid-record rebuild: %v", err)
	}
	if !reflect.DeepEqual(planned, tasks) || len(installed) != 0 || len(checker.paths) != 0 {
		t.Fatalf("acknowledged invalid-record rebuild did not preserve the full install plan: tasks=%#v installed=%v probes=%v", planned, installed, checker.paths)
	}
}

func TestAcknowledgedInvalidClusterInstallRecordStartsFreshLifecycle(t *testing.T) {
	state := loadWorkflowFixtureState(t, "001-sno-libvirt")
	const cluster = "sno-libvirt"
	task := lifecycleTask(t, mustPlanApplyTasks(applyContainerClusterTarget(), state), ApplyTaskKindClusterISO)
	dir := t.TempDir()
	clustersDir := filepath.Join(dir, "clusters")
	secretsDir := writeWorkflowInstallerSecrets(t, dir)
	oldStartedAt := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	installedAt := oldStartedAt.Add(time.Hour)
	if err := SaveClusterInstallRecord(clustersDir, ClusterInstallRecord{
		Cluster:     cluster,
		Status:      ClusterInstallStatusInstalling,
		Phase:       ClusterInstallPhaseComplete,
		RunID:       "old-run",
		StartedAt:   oldStartedAt,
		UpdatedAt:   installedAt,
		InstalledAt: &installedAt,
	}); err != nil {
		t.Fatalf("SaveClusterInstallRecord: %v", err)
	}
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	if err := MarkClusterInstallTaskStarted(clustersDir, "test", secretsDir, "rebuild-run", task, now); err != nil {
		t.Fatalf("MarkClusterInstallTaskStarted: %v", err)
	}
	record, found, err := LoadClusterInstallRecord(clustersDir, cluster)
	if err != nil || !found {
		t.Fatalf("LoadClusterInstallRecord: found=%v err=%v", found, err)
	}
	if record.Status != ClusterInstallStatusInstalling || record.Phase != ClusterInstallPhaseCreatingISO || record.RunID != "rebuild-run" || !record.StartedAt.Equal(now) || record.InstalledAt != nil {
		t.Fatalf("fresh rebuild lifecycle record = %#v", record)
	}
}
