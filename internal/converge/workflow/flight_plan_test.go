package workflow

import "testing"

func flightPlanTask(id, cluster, clusterKind string, deps ...string) ApplyTask {
	return ApplyTask{Entry: TaskLedgerEntry{
		ID:           id,
		Kind:         "test",
		Label:        id,
		Cluster:      cluster,
		ClusterKind:  clusterKind,
		Dependencies: deps,
	}}
}

func TestFlightPlanPutsIndependentClustersInOneStage(t *testing.T) {
	tasks := []ApplyTask{
		flightPlanTask("storage.ceph", "ceph", ApplyClusterKindStorage),
		flightPlanTask("wait.ocp-01", "ocp-01", ApplyClusterKindContainer),
		flightPlanTask("wait.ocp-02", "ocp-02", ApplyClusterKindContainer),
	}
	plan := BuildFlightPlan(tasks, nil)
	if len(plan.Stages) != 1 {
		t.Fatalf("independent tasks must share one stage, got %d stages", len(plan.Stages))
	}
	if got := len(plan.Stages[0].Lanes); got != 3 {
		t.Fatalf("stage lane count got %d, want 3", got)
	}
	for _, cluster := range []string{"ocp-01", "ocp-02"} {
		stage, ok := plan.StageOf("wait." + cluster)
		if !ok || stage != 0 {
			t.Fatalf("%s stage got %d (found=%v), want 0", cluster, stage, ok)
		}
	}
}

func TestFlightPlanStagesFollowLongestPath(t *testing.T) {
	tasks := []ApplyTask{
		flightPlanTask("storage.ceph", "ceph", ApplyClusterKindStorage),
		flightPlanTask("wait.ocp-01", "ocp-01", ApplyClusterKindContainer),
		flightPlanTask("addon.ocp-01.df", "ocp-01", ApplyClusterKindContainer, "wait.ocp-01", "storage.ceph"),
		flightPlanTask("addon.ocp-01.logging", "ocp-01", ApplyClusterKindContainer, "wait.ocp-01"),
	}
	plan := BuildFlightPlan(tasks, nil)
	if len(plan.Stages) != 2 {
		t.Fatalf("stage count got %d, want 2", len(plan.Stages))
	}
	for _, id := range []string{"addon.ocp-01.df", "addon.ocp-01.logging"} {
		stage, ok := plan.StageOf(id)
		if !ok || stage != 1 {
			t.Fatalf("%s stage got %d (found=%v), want 1", id, stage, ok)
		}
	}
	if got := plan.StepCount(); got != len(tasks) {
		t.Fatalf("step count got %d, want %d", got, len(tasks))
	}
}

func TestFlightPlanOrdersLanesInfraFirstThenTopology(t *testing.T) {
	tasks := []ApplyTask{
		flightPlanTask("addon.ocp-01.a", "ocp-01", ApplyClusterKindContainer),
		flightPlanTask("storage.ceph", "ceph", ApplyClusterKindStorage),
		flightPlanTask("provider.bastion", "", ""),
	}
	plan := BuildFlightPlan(tasks, []string{"ceph", "ocp-01"})
	lanes := plan.Stages[0].Lanes
	want := []string{"", "ceph", "ocp-01"}
	if len(lanes) != len(want) {
		t.Fatalf("lane count got %d, want %d", len(lanes), len(want))
	}
	for i, cluster := range want {
		if lanes[i].Cluster != cluster {
			t.Fatalf("lane %d got %q, want %q", i, lanes[i].Cluster, cluster)
		}
	}
}

func TestFlightPlanCountsOrderingDependencies(t *testing.T) {
	gated := flightPlanTask("playbook.ocp-01", "ocp-01", ApplyClusterKindContainer)
	gated.Entry.OrderingDependencies = []string{"wait.ocp-01"}
	tasks := []ApplyTask{
		flightPlanTask("wait.ocp-01", "ocp-01", ApplyClusterKindContainer),
		gated,
	}
	plan := BuildFlightPlan(tasks, nil)
	stage, ok := plan.StageOf("playbook.ocp-01")
	if !ok || stage != 1 {
		t.Fatalf("an ordering dependency must advance the stage, got %d (found=%v)", stage, ok)
	}
}

func TestFlightPlanCountsSuccessDependencies(t *testing.T) {
	gated := flightPlanTask("cleanup", "", "")
	gated.Entry.SuccessDependencies = []string{"mutation"}
	tasks := []ApplyTask{
		flightPlanTask("mutation", "", ""),
		gated,
	}
	plan := BuildFlightPlan(tasks, nil)
	stage, ok := plan.StageOf("cleanup")
	if !ok || stage != 1 {
		t.Fatalf("a success dependency must advance the stage, got %d (found=%v)", stage, ok)
	}
}

func TestFlightPlanIgnoresUnknownAndSelfDependencies(t *testing.T) {
	self := flightPlanTask("wait.ocp-01", "ocp-01", ApplyClusterKindContainer, "wait.ocp-01", "iso.ocp-01")
	plan := BuildFlightPlan([]ApplyTask{self}, nil)
	if len(plan.Stages) != 1 {
		t.Fatalf("stage count got %d, want 1", len(plan.Stages))
	}
	if stage, ok := plan.StageOf("wait.ocp-01"); !ok || stage != 0 {
		t.Fatalf("stage got %d (found=%v), want 0", stage, ok)
	}
}
