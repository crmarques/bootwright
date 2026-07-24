package workflow

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
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
		FabricHosts:      hosts,
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

func TestPlanDestroyMachineScopeRunsRegistrationThenMachineInfra(t *testing.T) {
	state := loadWorkflowFixtureState(t, "003-3nodes-libvirt")
	tasks, err := PlanDestroyTasks("infra", state, "", []string{DestroyMachineScopeExtraVar + "=master-0"}, nil)
	if err != nil {
		t.Fatalf("plan machine-scoped destroy: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("machine-scoped destroy should plan the registration and machine-infra steps, got %d tasks: %v", len(tasks), planTaskIDSet(tasks))
	}
	if tasks[0].Entry.Kind != DestroyTaskKindMachineRegistration {
		t.Fatalf("machine-scoped destroy step[0] kind = %q, want %q", tasks[0].Entry.Kind, DestroyTaskKindMachineRegistration)
	}
	if tasks[1].Entry.Kind != DestroyTaskKindMachineInfra {
		t.Fatalf("machine-scoped destroy step[1] kind = %q, want %q", tasks[1].Entry.Kind, DestroyTaskKindMachineInfra)
	}
}

func TestPlanDestroyInfraRunsFullChain(t *testing.T) {
	state := loadWorkflowFixtureState(t, "003-3nodes-libvirt")
	tasks, err := PlanDestroyTasks("infra", state, "", nil, nil)
	if err != nil {
		t.Fatalf("plan infra destroy: %v", err)
	}
	if len(tasks) != 4 {
		t.Fatalf("infra destroy should plan the full 4-step chain, got %d", len(tasks))
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

func TestPlanApplyClusterScopeExcludesUnconsumedFabricHost(t *testing.T) {
	state := loadWorkflowFixtureState(t, "001-sno-libvirt")
	state.Machines = append(state.Machines, v1alpha1.Machine{
		Metadata: v1alpha1.Metadata{Name: "unused-service-host"},
		Spec: v1alpha1.MachineSpec{
			Capabilities: []string{
				v1alpha1.MachineCapabilityContainerRuntime,
				v1alpha1.MachineCapabilityProxy,
			},
			OS: v1alpha1.MachineOSSpec{Provided: v1alpha1.BoolPtr(true)},
		},
	})
	state.InfraComponents = append(state.InfraComponents, v1alpha1.InfraComponent{
		Metadata: v1alpha1.Metadata{Name: "unused-proxy"},
		Spec: v1alpha1.InfraComponentSpec{
			Type: v1alpha1.ComponentSlotProxy,
			Proxy: &v1alpha1.ProxyComponent{
				Implementation: v1alpha1.InfraComponentTypeSquid,
				MachineRef:     v1alpha1.LocalObjectReference{Name: "unused-service-host"},
				Port:           v1alpha1.DefaultSquidPort,
			},
		},
	})
	workMachines, _ := stategraph.ApplyWorkObjects(state, []string{"sno-libvirt"}, nil)
	scoped := stategraph.FilterStateToApplyClusterRoots(state, []string{"sno-libvirt"}, nil)
	target := applyAllTarget()
	target.FabricHosts = workMachines

	tasks, err := PlanApplyTasksChecked(target, scoped)
	if err != nil {
		t.Fatalf("plan cluster-scoped apply: %v", err)
	}
	ids := planTaskIDSet(tasks)
	if !ids["infra-component.bastion"] {
		t.Fatalf("cluster scope dropped consumed fabric host: %v", ids)
	}
	if ids["infra-component.unused-service-host"] {
		t.Fatalf("cluster scope planned unconsumed fabric host: %v", ids)
	}
}
