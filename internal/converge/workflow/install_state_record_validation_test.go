package workflow

import (
	"context"
	"errors"
	"fmt"
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
				record := ClusterInstallRecord{Cluster: cluster, Status: status, Phase: phase}
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
	if len(reinstalls) != 1 || reinstalls[0].Name != cluster || !strings.Contains(reinstalls[0].Descriptor, "unsupported lifecycle state") || !strings.Contains(reinstalls[0].Descriptor, "wipes its node disks") {
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
