package workflow

import (
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func recordStateCheckTask(t *testing.T, runsDir string, task ApplyTask) {
	t.Helper()
	if err := MarkApplyTaskConvergeSafety(runsDir, "test", "", task, ConvergeSafetyStatusReconciled, time.Unix(0, 0)); err != nil {
		t.Fatalf("record task %s: %v", task.Entry.ID, err)
	}
}

func requireSingleStateCheckDrift(t *testing.T, report StateCheckReport, action StateCheckDriftAction) {
	t.Helper()
	if report.InSync || len(report.Roots) != 1 || len(report.Roots[0].Resources) != 1 {
		t.Fatalf("state check = %+v, want one drifted resource", report)
	}
	resource := report.Roots[0].Resources[0]
	if resource.Classification != ConvergeSafetyDrift || resource.DriftAction != action {
		t.Fatalf("resource = %+v, want drift with action %q", resource, action)
	}
	if got := resource.Reconcilable; got != (action == StateCheckDriftActionReconcile) {
		t.Fatalf("resource reconcilable = %v, want %v for action %q", got, action == StateCheckDriftActionReconcile, action)
	}
}

func TestStateCheckClassifiesReferencedNetworkConfigNICEditAsRebuild(t *testing.T) {
	base := loadWorkflowFixtureState(t, "001-sno-libvirt")
	baseTask := ApplyTask{
		Entry: TaskLedgerEntry{
			ID:          "wait.sno-libvirt",
			Kind:        ApplyTaskKindInstallWait,
			Label:       "install wait sno-libvirt",
			Cluster:     "sno-libvirt",
			ClusterKind: ApplyClusterKindContainer,
		},
		State:              base,
		StructuralHashVars: containerClusterInstallStructuralHashVars(base),
	}
	runsDir := t.TempDir()
	recordStateCheckTask(t, runsDir, baseTask)

	matched, err := StateCheck([]ApplyTask{baseTask}, ApplyTarget{}, base, runsDir, "test")
	if err != nil {
		t.Fatalf("state check unchanged NetworkConfig: %v", err)
	}
	if !matched.InSync {
		t.Fatalf("unchanged NetworkConfig must match its record: %+v", matched)
	}

	changed := loadWorkflowFixtureState(t, "001-sno-libvirt")
	interfaces, ok := changed.NetworkConfigs[0].Spec.Template.NetworkConfig["interfaces"].([]any)
	if !ok || len(interfaces) == 0 {
		t.Fatalf("fixture NetworkConfig interfaces = %#v", changed.NetworkConfigs[0].Spec.Template.NetworkConfig["interfaces"])
	}
	primary, ok := interfaces[0].(map[string]any)
	if !ok {
		t.Fatalf("fixture primary NIC = %#v", interfaces[0])
	}
	primary["mtu"] = 9000
	changedTask := baseTask
	changedTask.State = changed
	changedTask.StructuralHashVars = containerClusterInstallStructuralHashVars(changed)

	report, err := StateCheck([]ApplyTask{changedTask}, ApplyTarget{}, changed, runsDir, "test")
	if err != nil {
		t.Fatalf("state check changed NetworkConfig: %v", err)
	}
	requireSingleStateCheckDrift(t, report, StateCheckDriftActionRebuild)
}

func TestStateCheckClassifiesCephFIPSPostureEditAsRebuild(t *testing.T) {
	base := bareMetalManagedOSState()
	base.StorageClusters[0].Spec.Ceph.Distribution = v1alpha1.StorageCephDistributionRedHat
	baseTask := storageTaskWith(
		storageClusterDesiredHashVars(base, "ceph-bm"),
		storageClusterStructuralHashVars(base, "ceph-bm"),
	)
	baseTask.Entry.ID = "storage.ceph-bm"
	baseTask.Entry.Cluster = "ceph-bm"
	baseTask.Entry.ClusterKind = ApplyClusterKindStorage
	baseTask.Entry.Label = "storage ceph-bm"
	runsDir := t.TempDir()
	recordStateCheckTask(t, runsDir, baseTask)

	matched, err := StateCheck([]ApplyTask{baseTask}, ApplyTarget{}, base, runsDir, "test")
	if err != nil {
		t.Fatalf("state check unchanged Ceph FIPS posture: %v", err)
	}
	if !matched.InSync {
		t.Fatalf("unchanged Ceph FIPS posture must match its record: %+v", matched)
	}

	changed := bareMetalManagedOSState()
	changed.StorageClusters[0].Spec.Ceph.Distribution = v1alpha1.StorageCephDistributionRedHat
	changed.StorageClusters[0].Spec.Ceph.Security.FIPS.Enabled = true
	changedTask := storageTaskWith(
		storageClusterDesiredHashVars(changed, "ceph-bm"),
		storageClusterStructuralHashVars(changed, "ceph-bm"),
	)
	changedTask.Entry.ID = "storage.ceph-bm"
	changedTask.Entry.Cluster = "ceph-bm"
	changedTask.Entry.ClusterKind = ApplyClusterKindStorage
	changedTask.Entry.Label = "storage ceph-bm"

	report, err := StateCheck([]ApplyTask{changedTask}, ApplyTarget{}, changed, runsDir, "test")
	if err != nil {
		t.Fatalf("state check changed Ceph FIPS posture: %v", err)
	}
	requireSingleStateCheckDrift(t, report, StateCheckDriftActionRebuild)
}
