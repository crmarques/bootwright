package converge

import (
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func bareMetalCephDestroyState() v1alpha1.State {
	node := func(name, address string) v1alpha1.Machine {
		return v1alpha1.Machine{
			Metadata: v1alpha1.Metadata{Name: name},
			Spec: v1alpha1.MachineSpec{
				Capabilities: []string{v1alpha1.MachineCapabilityCephNode},
				Substrate:    v1alpha1.MachineSubstrate{ProviderRef: v1alpha1.LocalObjectReference{Name: "bare-metal"}},
				OS: v1alpha1.MachineOSSpec{
					Provided:          v1alpha1.BoolPtr(false),
					InstallProfileRef: v1alpha1.LocalObjectReference{Name: "rhel-9-ceph-node"},
				},
				Addresses: []v1alpha1.MachineAddress{{Name: "ssh", Address: address}},
				Access: v1alpha1.MachineAccess{
					SSH: &v1alpha1.MachineSSHSpec{
						AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"},
						Auth:       v1alpha1.MachineSSHAuth{PrivateKeyRef: v1alpha1.SecretRef{Name: "ceph-node-ssh"}},
					},
				},
			},
		}
	}
	return v1alpha1.State{
		Environments: []v1alpha1.Environment{{Metadata: v1alpha1.Metadata{Name: "lab"}}},
		InfraProviders: []v1alpha1.InfraProvider{{
			Metadata: v1alpha1.Metadata{Name: "bare-metal"},
			Spec:     v1alpha1.InfraProviderSpec{Type: v1alpha1.ProvisionerBareMetal},
		}},
		Machines: []v1alpha1.Machine{node("ceph-0", "10.10.10.10"), node("ceph-1", "10.10.10.11")},
		StorageClusters: []v1alpha1.StorageCluster{{
			Metadata: v1alpha1.Metadata{Name: "ceph-bm"},
			Spec: v1alpha1.StorageClusterSpec{
				Type:       v1alpha1.StorageClusterTypeCeph,
				Management: v1alpha1.StorageClusterManagementManaged,
				Ceph: &v1alpha1.StorageClusterCephSpec{
					Cephadm: v1alpha1.StorageCephadmSpec{
						AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"},
						Bootstrap:  v1alpha1.StorageCephadmBootstrap{Node: "ceph-0"},
					},
					Topology: v1alpha1.StorageCephTopology{
						Nodes: []v1alpha1.StorageCephNode{
							{Name: "ceph-0", MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-0"}, Site: "dc1", Roles: []string{v1alpha1.StorageCephRoleMON, v1alpha1.StorageCephRoleOSD}},
							{Name: "ceph-1", MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-1"}, Site: "dc1", Roles: []string{v1alpha1.StorageCephRoleMON, v1alpha1.StorageCephRoleOSD}},
						},
					},
				},
			},
		}},
	}
}

func twoClusterBareMetalCephDestroyState() v1alpha1.State {
	base := bareMetalCephDestroyState()
	second := bareMetalCephDestroyState()
	second.StorageClusters[0].Metadata.Name = "ceph-b"
	second.StorageClusters[0].Spec.Ceph.Cephadm.Bootstrap.Node = "ceph-b0"
	second.Machines[0].Metadata.Name = "ceph-b0"
	second.Machines[1].Metadata.Name = "ceph-b1"
	second.Machines[0].Spec.Addresses[0].Address = "10.10.10.20"
	second.Machines[1].Spec.Addresses[0].Address = "10.10.10.21"
	second.StorageClusters[0].Spec.Ceph.Topology.Nodes[0].Name = "ceph-b0"
	second.StorageClusters[0].Spec.Ceph.Topology.Nodes[0].MachineRef.Name = "ceph-b0"
	second.StorageClusters[0].Spec.Ceph.Topology.Nodes[1].Name = "ceph-b1"
	second.StorageClusters[0].Spec.Ceph.Topology.Nodes[1].MachineRef.Name = "ceph-b1"

	out := base
	out.StorageClusters[0].Metadata.Name = "ceph-a"
	out.Machines = append(append([]v1alpha1.Machine(nil), base.Machines...), second.Machines...)
	out.StorageClusters = append(append([]v1alpha1.StorageCluster(nil), out.StorageClusters...), second.StorageClusters...)
	return out
}

func perClusterStorageDestroyLedger() workflow.RunLedger {
	return workflow.RunLedger{Tasks: []workflow.TaskLedgerEntry{
		{
			ID:           "destroy.storage-clusters.ceph-a",
			Kind:         workflow.DestroyTaskKindStorageCluster,
			ResourceKeys: []string{"ceph-a"},
			Status:       workflow.TaskStatusOK,
		},
		{
			ID:           "destroy.storage-clusters.ceph-b",
			Kind:         workflow.DestroyTaskKindStorageCluster,
			ResourceKeys: []string{"ceph-b"},
			Status:       workflow.TaskStatusFailed,
		},
		{
			ID:     "destroy.machine-infra",
			Kind:   workflow.DestroyTaskKindMachineInfra,
			Status: workflow.TaskStatusOK,
		},
	}}
}

func TestFailedClusterTeardownWithholdsOnlyThatClustersSubstrateRelease(t *testing.T) {
	st := twoClusterBareMetalCephDestroyState()
	succeeded := workflow.SucceededDestroyTaskKinds(perClusterStorageDestroyLedger())

	for _, skipUnreachable := range []bool{false, true} {
		runsDir := t.TempDir()
		clustersDir := t.TempDir()

		problems := ResetConvergeRecordsAfterDestroy(runsDir, clustersDir, "test", AllScope, st, nil, nil, nil, succeeded, false, skipUnreachable)
		if len(problems) != 0 {
			t.Fatalf("reset problems (skipUnreachable=%v): %v", skipUnreachable, problems)
		}
		released, err := workflow.ReleasedSubstrateClusters(runsDir)
		if err != nil {
			t.Fatalf("list releases: %v", err)
		}
		if slices.Contains(released, "ceph-b") {
			t.Fatalf("ceph-b's teardown failed, so its nodes still hold data: releasing its substrate re-arms a bare-metal reinstall over live disks (skipUnreachable=%v, released=%v)", skipUnreachable, released)
		}
		if skipUnreachable {
			continue
		}
		if !slices.Contains(released, "ceph-a") {
			t.Fatalf("ceph-a tore down cleanly and must keep its own release; a sibling cluster's failure must not withhold it, got %v", released)
		}
	}
}

func TestInfraDestroyRecordsSubstrateRelease(t *testing.T) {
	runsDir := t.TempDir()
	clustersDir := t.TempDir()
	st := bareMetalCephDestroyState()

	problems := ResetConvergeRecordsAfterDestroy(runsDir, clustersDir, "test", InfraScope, st, nil, nil, nil, nil, false, false)
	if len(problems) != 0 {
		t.Fatalf("reset problems: %v", problems)
	}
	released, err := workflow.ReleasedSubstrateClusters(runsDir)
	if err != nil {
		t.Fatalf("list releases: %v", err)
	}
	if strings.Join(released, ",") != "ceph-bm" {
		t.Fatalf("infra destroy must record a substrate release for ceph-bm so the next apply may reinstall its machines, got %v", released)
	}
}

func TestClustersDestroyRecordsNoSubstrateRelease(t *testing.T) {
	runsDir := t.TempDir()
	clustersDir := t.TempDir()
	st := bareMetalCephDestroyState()

	ResetConvergeRecordsAfterDestroy(runsDir, clustersDir, "test", ClustersScope, st, nil, nil, nil, nil, false, false)

	released, err := workflow.ReleasedSubstrateClusters(runsDir)
	if err != nil || len(released) != 0 {
		t.Fatalf("clusters-stage destroy leaves machines standing and must not authorize a reinstall, got %v err=%v", released, err)
	}
}

func TestFailedMachineTeardownRecordsNoSubstrateRelease(t *testing.T) {
	runsDir := t.TempDir()
	clustersDir := t.TempDir()
	st := bareMetalCephDestroyState()
	succeeded := map[string]bool{workflow.DestroyTaskKindInfraComponents: true}

	ResetConvergeRecordsAfterDestroy(runsDir, clustersDir, "test", InfraScope, st, nil, nil, nil, succeeded, false, false)

	released, err := workflow.ReleasedSubstrateClusters(runsDir)
	if err != nil || len(released) != 0 {
		t.Fatalf("a destroy whose machine teardown did not succeed must not authorize a reinstall, got %v err=%v", released, err)
	}
}

func TestSkipUnreachablePartialStorageDestroyWithholdsSubstrateRelease(t *testing.T) {
	runsDir := t.TempDir()
	clustersDir := t.TempDir()
	st := bareMetalCephDestroyState()

	problems := ResetConvergeRecordsAfterDestroy(runsDir, clustersDir, "test", AllScope, st, nil, []string{"ceph-bm"}, nil, nil, false, true)
	if len(problems) != 0 {
		t.Fatalf("reset problems: %v", problems)
	}
	released, err := workflow.ReleasedSubstrateClusters(runsDir)
	if err != nil || len(released) != 0 {
		t.Fatalf("a partial storage destroy leaves nodes with data standing and must not authorize their reinstall; re-running destroy is the completion path, got %v err=%v", released, err)
	}
}

func TestSkipUnreachableCompleteStorageDestroyRecordsSubstrateRelease(t *testing.T) {
	runsDir := t.TempDir()
	clustersDir := t.TempDir()
	st := bareMetalCephDestroyState()

	problems := ResetConvergeRecordsAfterDestroy(runsDir, clustersDir, "test", AllScope, st, nil, nil, nil, nil, false, true)
	if len(problems) != 0 {
		t.Fatalf("reset problems: %v", problems)
	}
	released, err := workflow.ReleasedSubstrateClusters(runsDir)
	if err != nil || strings.Join(released, ",") != "ceph-bm" {
		t.Fatalf("a completed storage teardown must authorize reinstall even when --authorize unreachable-nodes was enabled, got %v err=%v", released, err)
	}
}

func TestSkipUnreachableInfraDestroyWithholdsSubstrateRelease(t *testing.T) {
	runsDir := t.TempDir()
	clustersDir := t.TempDir()
	st := bareMetalCephDestroyState()

	problems := ResetConvergeRecordsAfterDestroy(runsDir, clustersDir, "test", InfraScope, st, nil, nil, nil, nil, false, true)
	if len(problems) != 0 {
		t.Fatalf("reset problems: %v", problems)
	}
	released, err := workflow.ReleasedSubstrateClusters(runsDir)
	if err != nil || len(released) != 0 {
		t.Fatalf("an infra-only --authorize unreachable-nodes destroy has no storage completion report and must not authorize reinstall, got %v err=%v", released, err)
	}
}

func TestSkipUnreachableFailedStorageDestroyWithholdsSubstrateRelease(t *testing.T) {
	runsDir := t.TempDir()
	clustersDir := t.TempDir()
	st := bareMetalCephDestroyState()
	succeeded := map[string]bool{workflow.DestroyTaskKindMachineInfra: true}

	problems := ResetConvergeRecordsAfterDestroy(runsDir, clustersDir, "test", AllScope, st, nil, nil, nil, succeeded, false, true)
	if len(problems) != 0 {
		t.Fatalf("reset problems: %v", problems)
	}
	released, err := workflow.ReleasedSubstrateClusters(runsDir)
	if err != nil || len(released) != 0 {
		t.Fatalf("a failed storage teardown must not authorize reinstall when only machine cleanup succeeded, got %v err=%v", released, err)
	}
}

func TestMachineScopedDestroyRecordsMachineRelease(t *testing.T) {
	runsDir := t.TempDir()
	st := bareMetalCephDestroyState()

	problems := ResetMachineConvergeRecordsAfterDestroy(runsDir, st, map[string]bool{"ceph-1": true}, nil, false, false)
	if len(problems) != 0 {
		t.Fatalf("reset problems: %v", problems)
	}
	records, err := workflow.ConsumableSubstrateReleases(runsDir, mustPlanMachineTasks(t, st))
	if err != nil {
		t.Fatalf("consumable: %v", err)
	}
	if len(records) != 1 || records[0].Cluster != "ceph-bm" {
		t.Fatalf("machine-scoped destroy must release its cluster substrate, got %v", records)
	}
	if strings.Join(records[0].Machines, ",") != "ceph-1" {
		t.Fatalf("release must cover only the destroyed machine, got %v", records[0].Machines)
	}

	problems = ResetMachineConvergeRecordsAfterDestroy(runsDir, st, map[string]bool{"ceph-1": true}, nil, false, true)
	if len(problems) != 0 {
		t.Fatalf("reset problems: %v", problems)
	}
	records, err = workflow.ConsumableSubstrateReleases(runsDir, mustPlanMachineTasks(t, st))
	if err != nil || len(records) != 1 || strings.Join(records[0].Machines, ",") != "ceph-1" {
		t.Fatalf("a --authorize unreachable-nodes machine destroy must not widen the release, got %v err=%v", records, err)
	}
}

func mustPlanMachineTasks(t *testing.T, st v1alpha1.State) []workflow.ApplyTask {
	t.Helper()
	tasks, err := workflow.PlanApplyTasksChecked(InfraScope.ApplyTarget(), st)
	if err != nil {
		t.Fatalf("plan tasks: %v", err)
	}
	return tasks
}
