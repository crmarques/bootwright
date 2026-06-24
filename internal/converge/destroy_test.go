package converge

import (
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func TestInfraDestroyResetsClusterStageConvergeRecords(t *testing.T) {
	runsDir := t.TempDir()
	clustersDir := t.TempDir()
	now := time.Unix(1700000000, 0)
	before := destroyResetState(v1alpha1.StorageCephDistributionOSS)
	after := destroyResetState(v1alpha1.StorageCephDistributionIBM)

	beforeTasks, err := workflow.PlanApplyTasksChecked(AllScope.ApplyTarget(), before)
	if err != nil {
		t.Fatalf("plan before: %v", err)
	}
	for _, task := range beforeTasks {
		switch task.Entry.Kind {
		case workflow.ApplyTaskKindStorageInfra, workflow.ApplyTaskKindStorageCluster:
			if err := workflow.MarkApplyTaskConvergeSafety(runsDir, "ctx", "apply-before", task, workflow.ConvergeSafetyStatusReconciled, now); err != nil {
				t.Fatalf("mark %s: %v", task.Entry.ID, err)
			}
		}
	}
	if err := workflow.SaveClusterInstallRecord(clustersDir, workflow.ClusterInstallRecord{
		Cluster:   "ocp",
		Status:    workflow.ClusterInstallStatusInstalled,
		Phase:     workflow.ClusterInstallPhaseComplete,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed install record: %v", err)
	}

	afterTasks, err := workflow.PlanApplyTasksChecked(AllScope.ApplyTarget(), after)
	if err != nil {
		t.Fatalf("plan after: %v", err)
	}
	objects, err := workflow.ClassifyApplyObjects(afterTasks, runsDir)
	if err != nil {
		t.Fatalf("classify before reset: %v", err)
	}
	if err := workflow.EvaluateApplyModePreflight(workflow.ApplyModeContinue, objects); err == nil || !strings.Contains(err.Error(), "StorageCluster/ceph-ibm (drift)") {
		t.Fatalf("precondition: expected storage drift, got %v", err)
	}

	ResetConvergeRecordsAfterDestroy(runsDir, clustersDir, InfraScope, after, nil)

	objects, err = workflow.ClassifyApplyObjects(afterTasks, runsDir)
	if err != nil {
		t.Fatalf("classify after reset: %v", err)
	}
	if err := workflow.EvaluateApplyModePreflight(workflow.ApplyModeContinue, objects); err != nil {
		t.Fatalf("apply after infra destroy should rebuild missing objects, got %v", err)
	}
	if _, found, err := workflow.LoadClusterInstallRecord(clustersDir, "ocp"); err != nil || found {
		t.Fatalf("container install record must be cleared after infra destroy, found=%v err=%v", found, err)
	}
}

func destroyResetState(distribution string) v1alpha1.State {
	return v1alpha1.State{
		ContainerClusters: []v1alpha1.ContainerCluster{{
			Metadata: v1alpha1.Metadata{Name: "ocp"},
		}},
		StorageClusters: []v1alpha1.StorageCluster{{
			Metadata: v1alpha1.Metadata{Name: "ceph-ibm"},
			Spec: v1alpha1.StorageClusterSpec{
				Type:       v1alpha1.StorageClusterTypeCeph,
				Management: v1alpha1.StorageClusterManagementManaged,
				Ceph: &v1alpha1.StorageClusterCephSpec{
					Distribution: distribution,
				},
			},
		}},
	}
}
