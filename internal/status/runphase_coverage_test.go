package status

import (
	"testing"

	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func TestTaskPhaseLabelCoversEveryPlannedKind(t *testing.T) {
	kinds := append([]string(nil), workflow.ApplyTaskKinds()...)
	kinds = append(kinds,
		workflow.DestroyTaskKindMachineRegistration,
		workflow.DestroyTaskKindMachineInfra,
		workflow.DestroyTaskKindInfraComponents,
		workflow.DestroyTaskKindProviderServices,
		workflow.DestroyTaskKindStorageCluster,
		workflow.DestroyTaskKindContainerCluster,
		workflow.DestroyTaskKindContainerClusterRuntime,
		workflow.DestroyTaskKindStorageNodeAccess,
	)
	for _, kind := range kinds {
		label := TaskPhaseLabel(workflow.TaskLedgerEntry{Kind: kind})
		if label == PhaseWork {
			t.Fatalf("task kind %q has no phase and would print under the %q catch-all", kind, PhaseWork)
		}
	}
}

func TestContainerClusterPhasesFollowTheGraph(t *testing.T) {
	rank := map[string]int{
		PhasePrerequisites:  0,
		PhaseClusterInstall: 1,
		PhaseAddOns:         2,
	}
	ordered := []struct {
		kind string
		want int
	}{
		{workflow.ApplyTaskKindMachineInfraPrepare, 0},
		{workflow.ApplyTaskKindClusterInstall, 0},
		{workflow.ApplyTaskKindMachineInfraFinalize, 0},
		{workflow.ApplyTaskKindClusterISO, 0},
		{workflow.ApplyTaskKindNodeBoot, 1},
		{workflow.ApplyTaskKindBootstrapWait, 1},
		{workflow.ApplyTaskKindInstallWait, 1},
		{workflow.ApplyTaskKindClusterAddon, 2},
		{workflow.ApplyTaskKindNodeConfigApply, 2},
		{workflow.ApplyTaskKindHostVirtctl, 2},
	}
	for _, tc := range ordered {
		label := TaskPhaseLabel(workflow.TaskLedgerEntry{Kind: tc.kind, ClusterKind: workflow.ApplyClusterKindContainer})
		got, ok := rank[label]
		if !ok {
			t.Fatalf("container kind %q landed in %q, which is not a container cluster phase", tc.kind, label)
		}
		if got != tc.want {
			t.Fatalf("container kind %q is in %q (rank %d), want rank %d", tc.kind, label, got, tc.want)
		}
	}
}

func TestStorageClusterKeepsItsMachinePhase(t *testing.T) {
	for _, kind := range []string{
		workflow.ApplyTaskKindMachineInfraPrepare,
		workflow.ApplyTaskKindManagedMachineOS,
		workflow.ApplyTaskKindStorageNodeAccess,
		workflow.ApplyTaskKindMachineRegistration,
		workflow.ApplyTaskKindMachineRepositories,
	} {
		task := workflow.TaskLedgerEntry{Kind: kind, ClusterKind: workflow.ApplyClusterKindStorage}
		if got := TaskPhaseLabel(task); got != PhaseMachines {
			t.Fatalf("storage kind %q = %q, want %q; the storage node chain completes before storage prerequisites start", kind, got, PhaseMachines)
		}
	}
	if got := TaskPhaseLabel(workflow.TaskLedgerEntry{Kind: workflow.ApplyTaskKindStorageInfra, ClusterKind: workflow.ApplyClusterKindStorage}); got != PhasePrerequisites {
		t.Fatalf("storage infra = %q, want %q", got, PhasePrerequisites)
	}
}

func TestTasksInDisplayOrderHonoursOrderingDependencies(t *testing.T) {
	phases := RunPhases([]workflow.TaskLedgerEntry{
		{ID: "destroy.provider-services", Kind: workflow.DestroyTaskKindProviderServices, Status: workflow.TaskStatusPending, OrderingDependencies: []string{"destroy.container-clusters"}},
		{ID: "destroy.container-clusters", Kind: workflow.DestroyTaskKindContainerCluster, Status: workflow.TaskStatusPending, OrderingDependencies: []string{"destroy.storage-clusters"}},
		{ID: "destroy.storage-clusters", Kind: workflow.DestroyTaskKindStorageCluster, Status: workflow.TaskStatusPending},
	})
	want := []string{PhaseStorageClusters, PhaseContainerClusters, PhaseSharedServices}
	if len(phases) != len(want) {
		t.Fatalf("phases = %+v, want %v", phases, want)
	}
	for i := range want {
		if phases[i].Label != want[i] {
			t.Fatalf("phase %d = %q, want %q; teardown rows must follow the ordering edges the planner set", i, phases[i].Label, want[i])
		}
	}
}

func TestTasksInDisplayOrderHonoursSuccessDependencies(t *testing.T) {
	phases := RunPhases([]workflow.TaskLedgerEntry{
		{ID: "destroy.provider-services", Kind: workflow.DestroyTaskKindProviderServices, Status: workflow.TaskStatusPending, SuccessDependencies: []string{"destroy.storage-clusters"}},
		{ID: "destroy.storage-clusters", Kind: workflow.DestroyTaskKindStorageCluster, Status: workflow.TaskStatusPending},
	})
	want := []string{PhaseStorageClusters, PhaseSharedServices}
	if len(phases) != len(want) {
		t.Fatalf("phases = %+v, want %v", phases, want)
	}
	for i := range want {
		if phases[i].Label != want[i] {
			t.Fatalf("phase %d = %q, want %q; display order must include hard success edges", i, phases[i].Label, want[i])
		}
	}
}

func TestTaskUnitNamesCoversCollapsedAndSingleMachineTasks(t *testing.T) {
	cases := []struct {
		name string
		task workflow.TaskLedgerEntry
		want []string
	}{
		{
			name: "collapsed task names every machine it provisions",
			task: workflow.TaskLedgerEntry{Kind: workflow.ApplyTaskKindClusterInstall, Nodes: []string{"node01", "node02"}},
			want: []string{"node01", "node02"},
		},
		{
			name: "per-machine task still names its single node",
			task: workflow.TaskLedgerEntry{Kind: workflow.ApplyTaskKindClusterInstall, Node: "node01"},
			want: []string{"node01"},
		},
		{
			name: "task with no machine identity names nothing",
			task: workflow.TaskLedgerEntry{Kind: workflow.ApplyTaskKindClusterInstall},
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := TaskUnitNames(tc.task)
			if len(got) != len(tc.want) {
				t.Fatalf("TaskUnitNames = %v, want %v", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("TaskUnitNames = %v, want %v", got, tc.want)
				}
			}
		})
	}
}
