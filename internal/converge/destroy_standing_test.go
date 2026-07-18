package converge

import (
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/ownership"
	stategraph "github.com/crmarques/bootwright/internal/state/graph"
)

func sharedServiceConflictFixture() []stategraph.DestroyScopeConflict {
	return []stategraph.DestroyScopeConflict{{
		Slot:             "artifactServer",
		Provider:         "InfraComponent",
		Name:             "artifact-server",
		ScopedClusters:   []string{"ceph-ibm"},
		UnscopedClusters: []string{"ocp"},
	}}
}

func standingFixtureTasks(t *testing.T, state v1alpha1.State) []workflow.ApplyTask {
	t.Helper()
	tasks, err := workflow.PlanApplyTasksChecked(AllScope.ApplyTarget(), state)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	return tasks
}

func TestStandingConflictsDroppedWhenUnscopedClustersHaveNoRecords(t *testing.T) {
	state := destroyResetState(v1alpha1.StorageCephDistributionOSS)
	tasks := standingFixtureTasks(t, state)
	got := workflow.StandingDestroyScopeConflicts(t.TempDir(), t.TempDir(), state, nil, tasks, sharedServiceConflictFixture())
	if len(got) != 0 {
		t.Fatalf("with no ownership, install, or converge records the unscoped clusters are not standing and the scoped destroy must proceed, got %v", got)
	}
}

func TestStandingConflictsKeptWhenOwnershipRecordNamesUnscopedCluster(t *testing.T) {
	state := destroyResetState(v1alpha1.StorageCephDistributionOSS)
	tasks := standingFixtureTasks(t, state)
	records := []ownership.ResourceRecord{{Kind: "kubevirt-machine", Name: "ocp-cp-0", Owner: ownership.Owner, Cluster: "ocp"}}
	got := workflow.StandingDestroyScopeConflicts(t.TempDir(), t.TempDir(), state, records, tasks, sharedServiceConflictFixture())
	if len(got) != 1 || strings.Join(got[0].UnscopedClusters, ",") != "ocp" {
		t.Fatalf("an ownership record proves the unscoped cluster still stands, got %v", got)
	}
}

func TestStandingConflictsKeptWhenInstallRecordExists(t *testing.T) {
	state := destroyResetState(v1alpha1.StorageCephDistributionOSS)
	tasks := standingFixtureTasks(t, state)
	clustersDir := t.TempDir()
	if err := workflow.SaveClusterInstallRecord(clustersDir, workflow.ClusterInstallRecord{
		Cluster:   "ocp",
		Status:    workflow.ClusterInstallStatusInstalled,
		Phase:     workflow.ClusterInstallPhaseComplete,
		UpdatedAt: time.Unix(1700000000, 0),
	}); err != nil {
		t.Fatalf("seed install record: %v", err)
	}
	got := workflow.StandingDestroyScopeConflicts(t.TempDir(), clustersDir, state, nil, tasks, sharedServiceConflictFixture())
	if len(got) != 1 {
		t.Fatalf("an install record proves the unscoped cluster still stands, got %v", got)
	}
}

func TestStandingConflictsKeptWhenConvergeRecordExists(t *testing.T) {
	state := destroyResetState(v1alpha1.StorageCephDistributionOSS)
	tasks := standingFixtureTasks(t, state)
	runsDir := t.TempDir()
	marked := false
	for _, task := range tasks {
		if task.Entry.Cluster != "ocp" {
			continue
		}
		if err := workflow.MarkApplyTaskConvergeSafety(runsDir, "ctx", "apply", task, workflow.ConvergeSafetyStatusReconciled, time.Unix(1700000000, 0)); err != nil {
			t.Fatalf("mark %s: %v", task.Entry.ID, err)
		}
		marked = true
		break
	}
	if !marked {
		t.Fatalf("fixture planned no task for cluster ocp: %v", tasks)
	}
	got := workflow.StandingDestroyScopeConflicts(runsDir, t.TempDir(), state, nil, tasks, sharedServiceConflictFixture())
	if len(got) != 1 {
		t.Fatalf("a converge record proves the unscoped cluster still stands, got %v", got)
	}
}
