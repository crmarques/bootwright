package status

import (
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func TestApplyClusterPhasesAggregateContainerAndStorageStates(t *testing.T) {
	now := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	ledger := workflow.NewRunLedger("apply-test", "all", "", workflow.ConcurrencyLimits{}, []workflow.TaskLedgerEntry{
		{ID: "infra.cluster-a", Kind: workflow.ApplyTaskKindClusterInstall, Cluster: "cluster-a", ClusterKind: workflow.ApplyClusterKindContainer, Status: workflow.TaskStatusOK},
		{ID: "iso.cluster-a", Kind: workflow.ApplyTaskKindClusterISO, Cluster: "cluster-a", ClusterKind: workflow.ApplyClusterKindContainer, Status: workflow.TaskStatusOK},
		{ID: "boot.cluster-a", Kind: workflow.ApplyTaskKindNodeBoot, Cluster: "cluster-a", ClusterKind: workflow.ApplyClusterKindContainer, Status: workflow.TaskStatusRunning},
		{ID: "wait.cluster-a", Kind: workflow.ApplyTaskKindInstallWait, Cluster: "cluster-a", ClusterKind: workflow.ApplyClusterKindContainer, Status: workflow.TaskStatusPending},
		{ID: "storageinfra.ceph-a", Kind: workflow.ApplyTaskKindStorageInfra, Cluster: "ceph-a", ClusterKind: workflow.ApplyClusterKindStorage, Status: workflow.TaskStatusOK},
		{ID: "storage.ceph-a", Kind: workflow.ApplyTaskKindStorageCluster, Cluster: "ceph-a", ClusterKind: workflow.ApplyClusterKindStorage, Status: workflow.TaskStatusOK},
		{ID: "addon.cluster-a.openshift-data-foundation", Kind: workflow.ApplyTaskKindClusterAddon, Cluster: "cluster-a", ClusterKind: workflow.ApplyClusterKindContainer, Status: workflow.TaskStatusBlocked, Dependencies: []string{"storage.ceph-a"}},
	}, now)

	if kind := ApplyClusterKind(ledger.TasksForCluster("cluster-a")); kind != v1alpha1.KindContainerCluster {
		t.Fatalf("cluster-a kind = %q, want %s", kind, v1alpha1.KindContainerCluster)
	}
	if kind := ApplyClusterKind(ledger.TasksForCluster("ceph-a")); kind != v1alpha1.KindStorageCluster {
		t.Fatalf("ceph-a kind = %q, want %s", kind, v1alpha1.KindStorageCluster)
	}

	container := ApplyClusterPhases(ledger, "cluster-a")
	requireApplyPhase(t, "cluster-a", container, PhaseMachines, workflow.TaskStatusOK)
	requireApplyPhase(t, "cluster-a", container, PhasePrerequisites, workflow.TaskStatusOK)
	requireApplyPhase(t, "cluster-a", container, PhaseClusterInstall, workflow.TaskStatusRunning)
	requireApplyPhase(t, "cluster-a", container, PhaseAddOns, workflow.TaskStatusBlocked)

	storage := ApplyClusterPhases(ledger, "ceph-a")
	requireApplyPhase(t, "ceph-a", storage, PhasePrerequisites, workflow.TaskStatusOK)
	requireApplyPhase(t, "ceph-a", storage, PhaseClusterInstall, workflow.TaskStatusOK)
	if phasePresent(storage, PhaseMachines) {
		t.Fatalf("a phase with no planned task must not be listed: %+v", storage)
	}
	if phasePresent(storage, PhaseAddOns) {
		t.Fatalf("a phase with no planned task must not be listed: %+v", storage)
	}
}

func TestApplyFailedPhaseNamesTheTaskOwnPhase(t *testing.T) {
	task := workflow.TaskLedgerEntry{ID: "boot.a", Kind: workflow.ApplyTaskKindNodeBoot, Cluster: "a", Status: workflow.TaskStatusFailed}
	if got := ApplyFailedPhase(workflow.RunLedger{Tasks: []workflow.TaskLedgerEntry{task}}, task); got != PhaseClusterInstall {
		t.Fatalf("failed phase = %q, want %q", got, PhaseClusterInstall)
	}
}

func TestRunPhasesFoldDestroyPlumbingIntoLifecyclePhases(t *testing.T) {
	phases := RunPhases([]workflow.TaskLedgerEntry{
		{ID: "destroy.machine-registration", Kind: workflow.DestroyTaskKindMachineRegistration, Status: workflow.TaskStatusOK},
		{ID: "destroy.infra-components", Kind: workflow.DestroyTaskKindInfraComponents, Status: workflow.TaskStatusOK},
		{ID: "destroy.machine-infra", Kind: workflow.DestroyTaskKindMachineInfra, Status: workflow.TaskStatusRunning},
		{ID: "destroy.provider-services", Kind: workflow.DestroyTaskKindProviderServices, Status: workflow.TaskStatusPending},
		{ID: "destroy.storage-node-access", Kind: workflow.DestroyTaskKindStorageNodeAccess, Status: workflow.TaskStatusPending},
	})
	if len(phases) != 2 {
		t.Fatalf("phases = %+v, want Machines and Shared services only", phases)
	}
	if phases[0].Label != PhaseMachines || phases[0].Status != workflow.TaskStatusRunning {
		t.Fatalf("first phase = %+v, want %s running", phases[0], PhaseMachines)
	}
	if phases[1].Label != PhaseSharedServices || phases[1].Status != workflow.TaskStatusRunning {
		t.Fatalf("second phase = %+v, want %s running", phases[1], PhaseSharedServices)
	}
}

func TestApplyPhaseStatusTerminalStates(t *testing.T) {
	cases := []struct {
		name   string
		tasks  []workflow.TaskLedgerEntry
		status workflow.TaskStatus
	}{
		{name: "failed", tasks: []workflow.TaskLedgerEntry{{Status: workflow.TaskStatusOK}, {Status: workflow.TaskStatusFailed}}, status: workflow.TaskStatusFailed},
		{name: "blocked", tasks: []workflow.TaskLedgerEntry{{Status: workflow.TaskStatusBlocked}, {Status: workflow.TaskStatusPending}}, status: workflow.TaskStatusBlocked},
		{name: "cancelled", tasks: []workflow.TaskLedgerEntry{{Status: workflow.TaskStatusOK}, {Status: workflow.TaskStatusCancelled}}, status: workflow.TaskStatusCancelled},
		{name: "skipped", tasks: []workflow.TaskLedgerEntry{{Status: workflow.TaskStatusSkipped}, {Status: workflow.TaskStatusSkipped}}, status: workflow.TaskStatusSkipped},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := applyPhaseStatus(tc.tasks); got != tc.status {
				t.Fatalf("status = %s, want %s", got, tc.status)
			}
		})
	}
}

func requireApplyPhase(t *testing.T, cluster string, phases []ApplyPhase, label string, want workflow.TaskStatus) {
	t.Helper()
	for _, phase := range phases {
		if phase.Label == label {
			if phase.Status != want {
				t.Fatalf("%s phase %s = %s, want %s", cluster, label, phase.Status, want)
			}
			return
		}
	}
	t.Fatalf("%s missing phase %s in %+v", cluster, label, phases)
}

func phasePresent(phases []ApplyPhase, label string) bool {
	for _, phase := range phases {
		if phase.Label == label {
			return true
		}
	}
	return false
}
