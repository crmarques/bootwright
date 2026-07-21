package workflow

import (
	"testing"

	stategraph "github.com/crmarques/bootwright/internal/state/graph"
)

func planTaskIDSet(tasks []ApplyTask) map[string]bool {
	out := make(map[string]bool, len(tasks))
	for _, task := range tasks {
		out[task.Entry.ID] = true
	}
	return out
}

func TestPlanApplyMachineScopeNarrowsContainerNodes(t *testing.T) {
	state := loadWorkflowFixtureState(t, "003-3nodes-libvirt")
	provision, hosts := stategraph.MachineWorkObjects(state, []string{"master-0"})
	target := ApplyTarget{
		Name:             "infra",
		PhaseNames:       []string{ApplyPhaseFabric, ApplyPhaseMachines},
		MachineProvision: provision,
		MachineHosts:     hosts,
	}
	tasks, err := PlanApplyTasksChecked(target, state)
	if err != nil {
		t.Fatalf("plan machine-scoped apply: %v", err)
	}
	ids := planTaskIDSet(tasks)
	if !ids["infra.3-nodes-ocp-libvirt.master-0"] {
		t.Fatalf("expected selected machine task infra.3-nodes-ocp-libvirt.master-0; got %v", ids)
	}
	for _, unwanted := range []string{"infra.3-nodes-ocp-libvirt.master-1", "infra.3-nodes-ocp-libvirt.master-2"} {
		if ids[unwanted] {
			t.Fatalf("machine scope should not plan %s; got %v", unwanted, ids)
		}
	}
}

func TestPlanDestroyMachineScopeRunsOnlyMachineInfra(t *testing.T) {
	state := loadWorkflowFixtureState(t, "003-3nodes-libvirt")
	tasks, err := PlanDestroyTasks("infra", state, "", []string{DestroyMachineScopeExtraVar + "=master-0"}, nil)
	if err != nil {
		t.Fatalf("plan machine-scoped destroy: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("machine-scoped destroy should plan exactly the machine-infra step, got %d tasks: %v", len(tasks), planTaskIDSet(tasks))
	}
	if tasks[0].Entry.Kind != DestroyTaskKindMachineInfra {
		t.Fatalf("machine-scoped destroy step kind = %q, want %q", tasks[0].Entry.Kind, DestroyTaskKindMachineInfra)
	}
}

func TestPlanDestroyInfraRunsFullChain(t *testing.T) {
	state := loadWorkflowFixtureState(t, "003-3nodes-libvirt")
	tasks, err := PlanDestroyTasks("infra", state, "", nil, nil)
	if err != nil {
		t.Fatalf("plan infra destroy: %v", err)
	}
	if len(tasks) != 3 {
		t.Fatalf("infra destroy should plan the full 3-step chain, got %d", len(tasks))
	}
}

func TestPlanApplyUnscopedPlansAllContainerNodes(t *testing.T) {
	state := loadWorkflowFixtureState(t, "003-3nodes-libvirt")
	target := ApplyTarget{Name: "infra", PhaseNames: []string{ApplyPhaseFabric, ApplyPhaseMachines}}
	tasks, err := PlanApplyTasksChecked(target, state)
	if err != nil {
		t.Fatalf("plan unscoped apply: %v", err)
	}
	ids := planTaskIDSet(tasks)
	for _, want := range []string{
		"infra.3-nodes-ocp-libvirt.master-0",
		"infra.3-nodes-ocp-libvirt.master-1",
		"infra.3-nodes-ocp-libvirt.master-2",
	} {
		if !ids[want] {
			t.Fatalf("unscoped apply should plan %s; got %v", want, ids)
		}
	}
}
