package workflow

import (
	"sort"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func independencePlanningState() v1alpha1.State {
	return v1alpha1.State{
		ContainerClusters: []v1alpha1.ContainerCluster{
			{Metadata: v1alpha1.Metadata{Name: "ocp-01"}},
			{Metadata: v1alpha1.Metadata{Name: "ocp-02"}},
		},
		ClusterAddons: []v1alpha1.ClusterAddon{
			{Metadata: v1alpha1.Metadata{Name: "logging"}, Spec: v1alpha1.ClusterAddonSpec{Type: v1alpha1.ClusterAddonTypeManifestSet}},
			{Metadata: v1alpha1.Metadata{Name: "metrics"}, Spec: v1alpha1.ClusterAddonSpec{Type: v1alpha1.ClusterAddonTypeManifestSet}},
		},
		ClusterAddonBindings: []v1alpha1.ClusterAddonBinding{
			{
				Metadata: v1alpha1.Metadata{Name: "binding-01"},
				Spec: v1alpha1.ClusterAddonBindingSpec{
					ClusterRef: v1alpha1.LocalObjectReference{Name: "ocp-01"},
					AddonRefs:  []v1alpha1.LocalObjectReference{{Name: "logging"}, {Name: "metrics"}},
				},
			},
			{
				Metadata: v1alpha1.Metadata{Name: "binding-02"},
				Spec: v1alpha1.ClusterAddonBindingSpec{
					ClusterRef: v1alpha1.LocalObjectReference{Name: "ocp-02"},
					AddonRefs:  []v1alpha1.LocalObjectReference{{Name: "logging"}, {Name: "metrics"}},
				},
			},
		},
	}
}

func planIndependenceTasks(t *testing.T, state v1alpha1.State) []ApplyTask {
	t.Helper()
	tasks, err := PlanApplyTasksChecked(ApplyTarget{Name: "add-ons", PhaseNames: []string{ApplyPhaseAddons}}, state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	if len(tasks) == 0 {
		t.Fatal("planner produced no tasks; the independence guard would pass vacuously")
	}
	return tasks
}

func dependencyPathExists(tasks []ApplyTask, from, to string) bool {
	edges := map[string][]string{}
	for _, task := range tasks {
		edges[task.Entry.ID] = flightPlanDependsOn(task)
	}
	seen := map[string]bool{}
	var walk func(string) bool
	walk = func(id string) bool {
		if id == to {
			return true
		}
		if seen[id] {
			return false
		}
		seen[id] = true
		for _, dep := range edges[id] {
			if walk(dep) {
				return true
			}
		}
		return false
	}
	return walk(from)
}

func taskIDsForCluster(tasks []ApplyTask, cluster string) []string {
	var out []string
	for _, task := range tasks {
		if task.Entry.Cluster == cluster {
			out = append(out, task.Entry.ID)
		}
	}
	sort.Strings(out)
	return out
}

func TestPlanKeepsIndependentClusterLanesUnlinked(t *testing.T) {
	tasks := planIndependenceTasks(t, independencePlanningState())
	first := taskIDsForCluster(tasks, "ocp-01")
	second := taskIDsForCluster(tasks, "ocp-02")
	if len(first) == 0 || len(second) == 0 {
		t.Fatalf("both clusters must plan work, got %v and %v", first, second)
	}
	for _, a := range first {
		for _, b := range second {
			if dependencyPathExists(tasks, a, b) {
				t.Errorf("%s depends on %s; independent clusters must plan no cross edge", a, b)
			}
			if dependencyPathExists(tasks, b, a) {
				t.Errorf("%s depends on %s; independent clusters must plan no cross edge", b, a)
			}
		}
	}
}

func TestPlanKeepsIndependentAddonsInOneStage(t *testing.T) {
	tasks := planIndependenceTasks(t, independencePlanningState())
	plan := BuildFlightPlan(tasks, []string{"ocp-01", "ocp-02"})
	if len(plan.Stages) != 1 {
		t.Fatalf("independent add-ons across two clusters must share one stage, got %d stages", len(plan.Stages))
	}
	if got, want := plan.StepCount(), len(tasks); got != want {
		t.Fatalf("flight plan step count got %d, want %d", got, want)
	}
	lanes := plan.Stages[0].Lanes
	if len(lanes) != 2 {
		t.Fatalf("stage lane count got %d, want 2", len(lanes))
	}
	for _, lane := range lanes {
		if len(lane.Steps) != 2 {
			t.Fatalf("lane %s step count got %d, want 2", lane.Cluster, len(lane.Steps))
		}
	}
}

func TestPlanStillLinksAddonsThatDeclareACapability(t *testing.T) {
	state := independencePlanningState()
	state.ClusterAddons[0].Spec.Provides = []string{"log-store"}
	state.ClusterAddons[1].Spec.Requires = []string{"log-store"}
	tasks := planIndependenceTasks(t, state)
	if !dependencyPathExists(tasks, "addon.ocp-01.metrics", "addon.ocp-01.logging") {
		t.Fatal("a declared requires/provides pair must still create an edge inside one cluster")
	}
	if dependencyPathExists(tasks, "addon.ocp-02.metrics", "addon.ocp-01.logging") {
		t.Fatal("a capability edge must not reach across cluster lanes")
	}
	plan := BuildFlightPlan(tasks, []string{"ocp-01", "ocp-02"})
	stage, ok := plan.StageOf("addon.ocp-01.metrics")
	if !ok || stage != 1 {
		t.Fatalf("the requiring add-on must fly one stage later, got %d (found=%v)", stage, ok)
	}
}

func TestPlanIndependenceGuardNamesEveryPlannedCluster(t *testing.T) {
	tasks := planIndependenceTasks(t, independencePlanningState())
	seen := map[string]bool{}
	for _, task := range tasks {
		if task.Entry.Cluster != "" {
			seen[task.Entry.Cluster] = true
		}
	}
	for _, cluster := range []string{"ocp-01", "ocp-02"} {
		if !seen[cluster] {
			t.Fatalf("cluster %s planned no task; guard coverage would be silently lost", cluster)
		}
	}
	for _, task := range tasks {
		if !strings.HasPrefix(task.Entry.ID, "addon.") {
			t.Fatalf("add-ons-only plan produced unexpected task %s", task.Entry.ID)
		}
	}
}
