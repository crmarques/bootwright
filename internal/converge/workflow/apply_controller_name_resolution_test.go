package workflow

import (
	"errors"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	convergeremedy "github.com/crmarques/bootwright/internal/converge/remedy"
	"github.com/crmarques/bootwright/internal/render"
	desiredstate "github.com/crmarques/bootwright/internal/state/desired"
	stategraph "github.com/crmarques/bootwright/internal/state/graph"
)

func loadControllerResolverState(t *testing.T, example string) v1alpha1.State {
	t.Helper()
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "..", "examples", example)})
	if err != nil {
		t.Fatalf("load %s: %v", example, err)
	}
	return state
}

func controllerResolverTask(t *testing.T, tasks []ApplyTask, name string) ApplyTask {
	t.Helper()
	id := "controller-name-resolution." + v1alpha1.KindInfraComponent + "." + name
	for _, task := range tasks {
		if task.Entry.ID == id {
			return task
		}
	}
	t.Fatalf("no %s task in %v", id, planTaskIDSet(tasks))
	return ApplyTask{}
}

func TestControllerNameResolutionRunsAfterFabricAndBeforeContainerMachines(t *testing.T) {
	state := loadControllerResolverState(t, "sno-libvirt-redfish")
	tasks, err := PlanApplyTasksChecked(ApplyTarget{PhaseNames: []string{ApplyPhaseFabric, ApplyPhaseMachines}}, state)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	resolver := controllerResolverTask(t, tasks, "name-resolution")
	if resolver.Playbook != applyControllerNameResolution || resolver.Limit != render.GroupControllerHosts {
		t.Fatalf("controller resolver task = playbook %q limit %q", resolver.Playbook, resolver.Limit)
	}
	if resolver.SkipWhenConverged {
		t.Fatal("controller resolver task may skip its live readiness probe when desired input is unchanged")
	}
	if !slices.Contains(resolver.ExtraVarPairs, "bootwright_controller_name_resolution_automatic_mutation=true") {
		t.Fatalf("single-service controller resolver mutation gate = %v, want the positive exact-service decision", resolver.ExtraVarPairs)
	}
	if !slices.Contains(resolver.Entry.Dependencies, "infra-component.bastion") {
		t.Fatalf("controller resolver dependencies = %v, want the DNS service apply", resolver.Entry.Dependencies)
	}
	machineTasks := 0
	for _, task := range tasks {
		if !strings.HasPrefix(task.Entry.ID, "infra.") && !strings.HasPrefix(task.Entry.ID, "infrafinalize.") {
			continue
		}
		machineTasks++
		if !slices.Contains(task.Entry.Dependencies, resolver.Entry.ID) {
			t.Errorf("machine task %s dependencies = %v, want %s", task.Entry.ID, task.Entry.Dependencies, resolver.Entry.ID)
		}
	}
	if machineTasks == 0 {
		t.Fatal("fixture planned no machines-phase task")
	}
}

func TestMachinesOnlyReapplyStillEstablishesControllerNameResolution(t *testing.T) {
	state := loadControllerResolverState(t, "sno-libvirt-redfish")
	tasks, err := PlanApplyTasksChecked(ApplyTarget{PhaseNames: []string{ApplyPhaseMachines}}, state)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	resolver := controllerResolverTask(t, tasks, "name-resolution")
	if planTaskIDSet(tasks)["infra-component.bastion"] {
		t.Fatal("machines-only apply unexpectedly reconverges the remote DNS service")
	}
	for _, task := range tasks {
		if strings.HasPrefix(task.Entry.ID, "infra.") && !slices.Contains(task.Entry.Dependencies, resolver.Entry.ID) {
			t.Fatalf("machines-only task %s can run before %s: %v", task.Entry.ID, resolver.Entry.ID, task.Entry.Dependencies)
		}
	}
}

func TestStorageOnlyMachinesWaitForControllerNameResolution(t *testing.T) {
	state := loadControllerResolverState(t, "ceph-ibm-libvirt-lab")
	if len(state.ContainerClusters) != 0 {
		t.Fatalf("storage-only fixture gained container clusters: %d", len(state.ContainerClusters))
	}
	tasks, err := PlanApplyTasksChecked(ApplyTarget{
		PhaseNames:  []string{ApplyPhaseMachines},
		ClusterKind: ApplyClusterKindStorage,
	}, state)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	resolver := controllerResolverTask(t, tasks, "lab-dns")
	seen := 0
	for _, task := range tasks {
		switch task.Entry.Kind {
		case ApplyTaskKindManagedMachineOS, ApplyTaskKindMachineRegistration, ApplyTaskKindMachineRepositories, ApplyTaskKindStorageNodeAccess:
			seen++
			if !slices.Contains(task.Entry.Dependencies, resolver.Entry.ID) {
				t.Errorf("storage machine task %s dependencies = %v, want %s", task.Entry.ID, task.Entry.Dependencies, resolver.Entry.ID)
			}
		}
	}
	if seen == 0 {
		t.Fatal("storage-only fixture planned no storage machine task")
	}
}

func TestControllerNameResolutionExecutionAndDependenciesAcrossEveryPhase(t *testing.T) {
	state := loadControllerResolverState(t, "sno-libvirt-redfish")
	cases := []struct {
		name             string
		phases           []string
		wantClass        ApplyTaskExecutionClass
		wantMutation     string
		wantRemedy       convergeremedy.Action
		wantDownstream   bool
		wantRemoteBefore bool
	}{
		{name: "fabric", phases: []string{ApplyPhaseFabric}, wantMutation: "true", wantRemedy: convergeremedy.ActionResumeControllerDNSMutation, wantRemoteBefore: true},
		{name: "machines", phases: []string{ApplyPhaseMachines}, wantMutation: "true", wantRemedy: convergeremedy.ActionResumeControllerDNSMutation, wantDownstream: true},
		{name: "deps", phases: []string{ApplyPhaseDeps}, wantClass: ApplyTaskExecutionLiveProof, wantMutation: "false", wantRemedy: convergeremedy.ActionReconcileSharedServiceThenRetrySameSelection, wantDownstream: true},
		{name: "base", phases: []string{ApplyPhaseBase}, wantClass: ApplyTaskExecutionLiveProof, wantMutation: "false", wantRemedy: convergeremedy.ActionReconcileSharedServiceThenRetrySameSelection, wantDownstream: true},
		{name: "add-ons", phases: []string{ApplyPhaseAddons}, wantClass: ApplyTaskExecutionLiveProof, wantMutation: "false", wantRemedy: convergeremedy.ActionReconcileSharedServiceThenRetrySameSelection, wantDownstream: true},
		{name: "machines through base", phases: []string{ApplyPhaseMachines, ApplyPhaseDeps, ApplyPhaseBase}, wantMutation: "true", wantRemedy: convergeremedy.ActionResumeControllerDNSMutation, wantDownstream: true},
		{name: "deps through add-ons", phases: []string{ApplyPhaseDeps, ApplyPhaseBase, ApplyPhaseAddons}, wantClass: ApplyTaskExecutionLiveProof, wantMutation: "false", wantRemedy: convergeremedy.ActionReconcileSharedServiceThenRetrySameSelection, wantDownstream: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tasks, err := PlanApplyTasksChecked(ApplyTarget{PhaseNames: tc.phases}, state)
			if err != nil {
				t.Fatalf("plan: %v", err)
			}
			resolver := controllerResolverTask(t, tasks, "name-resolution")
			if resolver.ExecutionClass != tc.wantClass {
				t.Fatalf("controller execution class = %q, want %q", resolver.ExecutionClass, tc.wantClass)
			}
			prefix := "bootwright_controller_name_resolution_mutation_selected="
			var decisions []string
			for _, pair := range resolver.ExtraVarPairs {
				if strings.HasPrefix(pair, prefix) {
					decisions = append(decisions, pair)
				}
			}
			wantDecision := prefix + tc.wantMutation
			if !reflect.DeepEqual(decisions, []string{wantDecision}) {
				t.Fatalf("controller mutation decisions = %v, want exactly %q", decisions, wantDecision)
			}
			if resolver.FailureRemedy.Action != tc.wantRemedy {
				t.Fatalf("controller failure remedy = %q, want %q for mutation=%s", resolver.FailureRemedy.Action, tc.wantRemedy, tc.wantMutation)
			}
			if tc.wantRemoteBefore && !slices.Contains(resolver.Entry.Dependencies, "infra-component.bastion") {
				t.Fatalf("fabric controller task dependencies = %v, want its selected remote service", resolver.Entry.Dependencies)
			}
			downstream := 0
			for _, task := range tasks {
				if task.Entry.ID == resolver.Entry.ID || controllerDNSDependencyClasses[task.Entry.Kind] != controllerDNSAfter {
					continue
				}
				downstream++
				if !slices.Contains(task.Entry.Dependencies, resolver.Entry.ID) {
					t.Errorf("downstream %s task %s dependencies = %v, want controller barrier %s", task.Entry.Kind, task.Entry.ID, task.Entry.Dependencies, resolver.Entry.ID)
				}
			}
			if tc.wantDownstream && downstream == 0 {
				t.Fatal("phase range planned no task classified after controller DNS; the dependency assertion would pass vacuously")
			}
		})
	}
}

func TestCustomPlaybookAnchorsCannotBypassControllerNameResolution(t *testing.T) {
	state := loadControllerResolverState(t, "sno-libvirt-redfish")
	cases := []struct {
		anchor    string
		anchorKey string
	}{
		{anchor: v1alpha1.CustomPlaybookAnchorFabric, anchorKey: anchorKeyGates},
		{anchor: v1alpha1.CustomPlaybookAnchorFabric, anchorKey: anchorKeyFollows},
		{anchor: v1alpha1.CustomPlaybookAnchorMachines, anchorKey: anchorKeyGates},
		{anchor: v1alpha1.CustomPlaybookAnchorMachines, anchorKey: anchorKeyFollows},
		{anchor: v1alpha1.CustomPlaybookAnchorDeps, anchorKey: anchorKeyGates},
		{anchor: v1alpha1.CustomPlaybookAnchorDeps, anchorKey: anchorKeyFollows},
		{anchor: v1alpha1.CustomPlaybookAnchorBase, anchorKey: anchorKeyGates},
		{anchor: v1alpha1.CustomPlaybookAnchorBase, anchorKey: anchorKeyFollows},
		{anchor: v1alpha1.CustomPlaybookAnchorAddOns, anchorKey: anchorKeyGates},
		{anchor: v1alpha1.CustomPlaybookAnchorAddOns, anchorKey: anchorKeyFollows},
	}
	for _, tc := range cases {
		t.Run(tc.anchor+" "+tc.anchorKey, func(t *testing.T) {
			configured := state
			name := tc.anchor + "-" + tc.anchorKey
			configured.CustomPlaybooks = []v1alpha1.CustomPlaybook{
				provisioningPlaybook(name, tc.anchor, tc.anchorKey, v1alpha1.CustomPlaybookTarget{Clusters: []string{"sno-libvirt"}}),
			}
			tasks, err := PlanApplyTasksChecked(ApplyTarget{PhaseNames: []string{tc.anchor}}, configured)
			if err != nil {
				t.Fatalf("plan: %v", err)
			}
			resolver := controllerResolverTask(t, tasks, "name-resolution")
			playbook := taskByID(t, tasks, "playbook."+name)
			count := 0
			for _, dependency := range playbook.Entry.Dependencies {
				if dependency == resolver.Entry.ID {
					count++
				}
			}
			if tc.anchor == v1alpha1.CustomPlaybookAnchorFabric && tc.anchorKey == anchorKeyGates {
				if count != 0 {
					t.Fatalf("fabric-gating playbook dependencies = %v, must precede rather than depend on controller resolver %s", playbook.Entry.Dependencies, resolver.Entry.ID)
				}
				service := taskByID(t, tasks, "infra-component.bastion")
				if !slices.Contains(service.Entry.Dependencies, playbook.Entry.ID) || !slices.Contains(resolver.Entry.Dependencies, service.Entry.ID) {
					t.Fatalf("fabric gate chain is playbook=%v service=%v resolver=%v, want playbook -> remote service -> controller barrier", playbook.Entry.Dependencies, service.Entry.Dependencies, resolver.Entry.Dependencies)
				}
				return
			}
			if count != 1 {
				t.Fatalf("custom playbook dependencies = %v, want controller resolver %s exactly once", playbook.Entry.Dependencies, resolver.Entry.ID)
			}
		})
	}
}

func TestControllerNameResolutionTaskHashUsesUnscopedDesiredState(t *testing.T) {
	full := loadControllerResolverState(t, "sno-libvirt-redfish")
	secondaryEntry := full.Environments[0].Spec.InfraComponents.NameResolution[0]
	secondaryEntry.Name = "secondary"
	secondaryEntry.ComponentRef.Name = "secondary-dns"
	full.Environments[0].Spec.InfraComponents.NameResolution = append(full.Environments[0].Spec.InfraComponents.NameResolution, secondaryEntry)
	secondaryComponent := full.InfraComponents[0]
	secondaryComponent.Metadata.Name = secondaryEntry.ComponentRef.Name
	secondaryDNS := *secondaryComponent.Spec.NameResolution
	secondaryDNS.BindAddress = "192.168.132.2"
	secondaryComponent.Spec.NameResolution = &secondaryDNS
	full.InfraComponents = append(full.InfraComponents, secondaryComponent)
	secondaryNetwork := full.NetworkConfigs[0]
	secondaryNetwork.Metadata.Name = "secondary-bridge"
	secondaryNetwork.Spec.NameResolutionRefs = []v1alpha1.LocalObjectReference{{Name: secondaryEntry.Name}}
	full.NetworkConfigs = append(full.NetworkConfigs, secondaryNetwork)
	other := full.ContainerClusters[0]
	other.Metadata.Name = "sno-other"
	other.Spec.Nodes = append(other.Spec.Nodes[:0:0], other.Spec.Nodes...)
	originalMachine := other.Spec.Nodes[0].MachineRef.Name
	other.Spec.Nodes[0].MachineRef.Name = "sno-other-master-0"
	full.ContainerClusters = append(full.ContainerClusters, other)
	copiedMachine := false
	for _, machine := range full.Machines {
		if machine.Metadata.Name != originalMachine {
			continue
		}
		machine.Metadata.Name = other.Spec.Nodes[0].MachineRef.Name
		machine.Spec.Network.Config.NetworkConfigRef.Name = secondaryNetwork.Metadata.Name
		full.Machines = append(full.Machines, machine)
		copiedMachine = true
		break
	}
	if !copiedMachine {
		t.Fatalf("fixture machine %q not found", originalMachine)
	}
	renderState := stategraph.FilterStateToApplyClusterRoots(full, []string{"sno-libvirt"}, nil)
	target := ApplyTarget{PhaseNames: []string{ApplyPhaseMachines}}
	wholeTasks, err := PlanApplyTasksCheckedWithHashState(target, full, full)
	if err != nil {
		t.Fatalf("whole plan: %v", err)
	}
	scopedTasks, err := PlanApplyTasksCheckedWithHashState(target, renderState, full)
	if err != nil {
		t.Fatalf("scoped plan: %v", err)
	}
	wholeHash, err := ApplyTaskDesiredHash(controllerResolverTask(t, wholeTasks, "name-resolution"))
	if err != nil {
		t.Fatalf("whole hash: %v", err)
	}
	scopedHash, err := ApplyTaskDesiredHash(controllerResolverTask(t, scopedTasks, "name-resolution"))
	if err != nil {
		t.Fatalf("scoped hash: %v", err)
	}
	if wholeHash != scopedHash {
		t.Fatalf("controller resolver hash depends on render scope: whole=%s scoped=%s", wholeHash, scopedHash)
	}
	edited := full
	edited.Environments = append([]v1alpha1.Environment(nil), full.Environments...)
	edited.Environments[0].Spec.Domains.Machines = "changed.example.test"
	editedRenderState := stategraph.FilterStateToApplyClusterRoots(edited, []string{"sno-libvirt"}, nil)
	editedTasks, err := PlanApplyTasksCheckedWithHashState(target, editedRenderState, edited)
	if err != nil {
		t.Fatalf("edited plan: %v", err)
	}
	editedHash, err := ApplyTaskDesiredHash(controllerResolverTask(t, editedTasks, "name-resolution"))
	if err != nil {
		t.Fatalf("edited hash: %v", err)
	}
	if editedHash == wholeHash {
		t.Fatal("a controller routing-domain edit did not move the task hash")
	}
}

func TestControllerNameResolutionPlannerRefusesNarrowedSharedService(t *testing.T) {
	full := loadControllerResolverState(t, "sno-libvirt-redfish")
	other := full.ContainerClusters[0]
	other.Metadata.Name = "sno-other"
	full.ContainerClusters = append(full.ContainerClusters, other)
	renderState := stategraph.FilterStateToApplyClusterRoots(full, []string{"sno-libvirt"}, nil)
	_, err := PlanApplyTasksCheckedWithHashState(ApplyTarget{PhaseNames: []string{ApplyPhaseMachines}}, renderState, full)
	if err == nil || !strings.Contains(err.Error(), "narrowed by selection") {
		t.Fatalf("narrowed shared controller DNS plan error = %v, want a fail-closed selection refusal", err)
	}
	var typed convergeremedy.Error
	if !errors.As(err, &typed) || typed.Remedy().Action != convergeremedy.ActionApplyAllConsumers {
		t.Fatalf("narrowed shared controller DNS remedy = %#v, want controller-built all-consumer apply", typed)
	}
	wantTargets := []convergeremedy.Target{
		{Role: convergeremedy.TargetRoleClusterRoot, Name: "sno-libvirt"},
		{Role: convergeremedy.TargetRoleClusterRoot, Name: "sno-other"},
	}
	if got := typed.Remedy().Targets; !reflect.DeepEqual(got, wantTargets) {
		t.Fatalf("narrowed shared controller DNS remedy targets = %#v, want exact consumers %#v", got, wantTargets)
	}
}

func TestControllerNameResolutionTaskHashContainsOnlyItsManagedService(t *testing.T) {
	state := loadControllerResolverState(t, "sno-libvirt-redfish")
	entry := state.Environments[0].Spec.InfraComponents.NameResolution[0]
	entry.Name = "secondary"
	entry.ComponentRef.Name = "secondary-dns"
	state.Environments[0].Spec.InfraComponents.NameResolution = append(state.Environments[0].Spec.InfraComponents.NameResolution, entry)
	state.NetworkConfigs[0].Spec.NameResolutionRefs = append(state.NetworkConfigs[0].Spec.NameResolutionRefs, v1alpha1.LocalObjectReference{Name: entry.Name})
	component := state.InfraComponents[0]
	component.Metadata.Name = entry.ComponentRef.Name
	dns := *component.Spec.NameResolution
	dns.BindAddress = "192.168.132.2"
	component.Spec.NameResolution = &dns
	state.InfraComponents = append(state.InfraComponents, component)

	target := ApplyTarget{PhaseNames: []string{ApplyPhaseMachines}}
	tasks, err := PlanApplyTasksChecked(target, state)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	primaryBefore, err := ApplyTaskDesiredHash(controllerResolverTask(t, tasks, "name-resolution"))
	if err != nil {
		t.Fatalf("primary hash: %v", err)
	}
	secondaryBefore, err := ApplyTaskDesiredHash(controllerResolverTask(t, tasks, "secondary-dns"))
	if err != nil {
		t.Fatalf("secondary hash: %v", err)
	}

	edited := state
	edited.InfraComponents = append([]v1alpha1.InfraComponent(nil), state.InfraComponents...)
	editedDNS := *edited.InfraComponents[1].Spec.NameResolution
	editedDNS.BindAddress = "192.168.132.3"
	edited.InfraComponents[1].Spec.NameResolution = &editedDNS
	editedTasks, err := PlanApplyTasksChecked(target, edited)
	if err != nil {
		t.Fatalf("edited plan: %v", err)
	}
	primaryAfter, err := ApplyTaskDesiredHash(controllerResolverTask(t, editedTasks, "name-resolution"))
	if err != nil {
		t.Fatalf("edited primary hash: %v", err)
	}
	secondaryAfter, err := ApplyTaskDesiredHash(controllerResolverTask(t, editedTasks, "secondary-dns"))
	if err != nil {
		t.Fatalf("edited secondary hash: %v", err)
	}
	if primaryAfter != primaryBefore {
		t.Fatalf("primary controller resolver hash absorbed another managed service: before=%s after=%s", primaryBefore, primaryAfter)
	}
	if secondaryAfter == secondaryBefore {
		t.Fatal("secondary managed-service edit did not move its controller resolver hash")
	}
	for _, name := range []string{"name-resolution", "secondary-dns"} {
		task := controllerResolverTask(t, tasks, name)
		if !slices.Contains(task.ExtraVarPairs, "bootwright_controller_name_resolution_automatic_mutation=false") {
			t.Fatalf("multi-service controller resolver %s mutation gate = %v, want fail-closed", name, task.ExtraVarPairs)
		}
	}
}

func TestControllerNameResolutionTaskHashExcludesRemoteOnlyDNSConfiguration(t *testing.T) {
	state := loadControllerResolverState(t, "sno-libvirt-redfish")
	target := ApplyTarget{PhaseNames: []string{ApplyPhaseMachines}}
	tasks, err := PlanApplyTasksChecked(target, state)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	before, err := ApplyTaskDesiredHash(controllerResolverTask(t, tasks, "name-resolution"))
	if err != nil {
		t.Fatalf("hash: %v", err)
	}

	edited := state
	edited.InfraComponents = append([]v1alpha1.InfraComponent(nil), state.InfraComponents...)
	dns := *edited.InfraComponents[0].Spec.NameResolution
	dns.Forwarders = append([]string(nil), dns.Forwarders...)
	dns.Forwarders = append(dns.Forwarders, "203.0.113.53")
	edited.InfraComponents[0].Spec.NameResolution = &dns
	editedTasks, err := PlanApplyTasksChecked(target, edited)
	if err != nil {
		t.Fatalf("edited plan: %v", err)
	}
	after, err := ApplyTaskDesiredHash(controllerResolverTask(t, editedTasks, "name-resolution"))
	if err != nil {
		t.Fatalf("edited hash: %v", err)
	}
	if after != before {
		t.Fatalf("controller resolver hash absorbed remote-only DNS forwarding: before=%s after=%s", before, after)
	}
}

func TestControllerNameResolutionRuntimeStateNarrowsByServiceIdentity(t *testing.T) {
	state := loadControllerResolverState(t, "sno-libvirt-redfish")
	entry := state.Environments[0].Spec.InfraComponents.NameResolution[0]
	entry.Name = "other"
	entry.ComponentRef.Name = "other-dns"
	state.Environments[0].Spec.InfraComponents.NameResolution = append(state.Environments[0].Spec.InfraComponents.NameResolution, entry)
	other := state.InfraComponents[0]
	other.Metadata.Name = "other-dns"
	dns := *other.Spec.NameResolution
	other.Spec.NameResolution = &dns
	other.Spec.NameResolution.MachineRef.Name = "other-host"
	state.InfraComponents = append(state.InfraComponents, other)
	filtered := controllerNameResolutionStateForTarget(state, render.ControllerNameResolutionTarget{
		MachineRef:   "bastion",
		ProviderName: v1alpha1.KindInfraComponent,
		Name:         "name-resolution",
		EntryNames:   []string{"default"},
	})
	got := filtered.Environments[0].Spec.InfraComponents.NameResolution
	if want := []v1alpha1.EnvironmentNameResolutionComponent{state.Environments[0].Spec.InfraComponents.NameResolution[0]}; !reflect.DeepEqual(got, want) {
		t.Fatalf("service-filtered resolver entries = %#v, want %#v", got, want)
	}
}
