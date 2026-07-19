package workflow

import (
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func saveRecordWithHashes(t *testing.T, runsDir string, task ApplyTask, desiredHash, structuralHash string) {
	t.Helper()
	record := ConvergeSafetyRecord{
		APIVersion:     ConvergeSafetyAPIVersion,
		ResourceID:     applyTaskSafetyResourceID(task),
		DesiredHash:    desiredHash,
		StructuralHash: structuralHash,
		HashSchema:     ConvergeHashSchema,
		Owner:          ConvergeSafetyOwnerIdentity{Manager: ConvergeSafetyOwner},
		Status:         ConvergeSafetyStatusReconciled,
		UpdatedAt:      time.Unix(0, 0).UTC(),
	}
	if err := SaveConvergeSafetyRecord(runsDir, record); err != nil {
		t.Fatalf("save record: %v", err)
	}
}

func storageTaskWith(desired, structural any) ApplyTask {
	task := classifyTask("storage.demo", ApplyTaskKindStorageCluster, "demo")
	task.DesiredHashVars = desired
	task.StructuralHashVars = structural
	return task
}

func TestReconcilableDeviceDriftIsNotStructural(t *testing.T) {
	runsDir := t.TempDir()
	base := storageTaskWith(
		map[string]any{"id": "s1", "devices": []string{"/dev/sdb"}},
		map[string]any{"id": "s1"},
	)
	dh, err := ApplyTaskDesiredHash(base)
	if err != nil {
		t.Fatalf("desired hash: %v", err)
	}
	sh, err := ApplyTaskStructuralHash(base)
	if err != nil {
		t.Fatalf("structural hash: %v", err)
	}
	saveRecordWithHashes(t, runsDir, base, dh, sh)

	added := storageTaskWith(
		map[string]any{"id": "s1", "devices": []string{"/dev/sdb", "/dev/sdc"}},
		map[string]any{"id": "s1"},
	)
	objs, err := ClassifyApplyObjects([]ApplyTask{added}, runsDir)
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	o := objs[0]
	if o.Class != ConvergeSafetyDrift {
		t.Fatalf("device add should still DISPLAY as drift, got %q", o.Class)
	}
	if !o.HasReconcilableDrift() || o.HasStructuralDrift() {
		t.Fatalf("device add must be reconcilable, not structural: reconcilable=%v structural=%v", o.HasReconcilableDrift(), o.HasStructuralDrift())
	}
	if !o.Reconcilable {
		t.Fatalf("Reconcilable flag must be set for a device-only drift")
	}

	structural := storageTaskWith(
		map[string]any{"id": "s2", "devices": []string{"/dev/sdb"}},
		map[string]any{"id": "s2"},
	)
	objs2, err := ClassifyApplyObjects([]ApplyTask{structural}, runsDir)
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	if o2 := objs2[0]; !o2.HasStructuralDrift() || o2.HasReconcilableDrift() {
		t.Fatalf("identity change must be structural drift: structural=%v reconcilable=%v", o2.HasStructuralDrift(), o2.HasReconcilableDrift())
	}
}

func TestMissingStructuralHashFallsBackToStructural(t *testing.T) {
	runsDir := t.TempDir()
	base := storageTaskWith(
		map[string]any{"id": "s1", "devices": []string{"/dev/sdb"}},
		map[string]any{"id": "s1"},
	)
	dh, err := ApplyTaskDesiredHash(base)
	if err != nil {
		t.Fatalf("desired hash: %v", err)
	}
	saveRecordWithHashes(t, runsDir, base, dh, "")

	added := storageTaskWith(
		map[string]any{"id": "s1", "devices": []string{"/dev/sdb", "/dev/sdc"}},
		map[string]any{"id": "s1"},
	)
	objs, err := ClassifyApplyObjects([]ApplyTask{added}, runsDir)
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	if o := objs[0]; !o.HasStructuralDrift() || o.HasReconcilableDrift() {
		t.Fatalf("legacy record must fall back to structural drift: structural=%v reconcilable=%v", o.HasStructuralDrift(), o.HasReconcilableDrift())
	}
}

func TestIBMCallHomeDriftIsReconcilable(t *testing.T) {
	runsDir := t.TempDir()
	state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{{
		Metadata: v1alpha1.Metadata{Name: "demo"},
		Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Distribution: v1alpha1.StorageCephDistributionIBM,
			IBM:          &v1alpha1.StorageCephIBMSpec{CallHome: v1alpha1.StorageCephIBMCallHomeDisabled},
		}},
	}}}
	base := storageTaskWith(
		storageClusterDesiredHashVars(state, "demo"),
		storageClusterStructuralHashVars(state, "demo"),
	)
	desiredHash, err := ApplyTaskDesiredHash(base)
	if err != nil {
		t.Fatalf("desired hash: %v", err)
	}
	structuralHash, err := ApplyTaskStructuralHash(base)
	if err != nil {
		t.Fatalf("structural hash: %v", err)
	}
	saveRecordWithHashes(t, runsDir, base, desiredHash, structuralHash)

	state.StorageClusters[0].Spec.Ceph.IBM.CallHome = v1alpha1.StorageCephIBMCallHomeEnabled
	updated := storageTaskWith(
		storageClusterDesiredHashVars(state, "demo"),
		storageClusterStructuralHashVars(state, "demo"),
	)
	objects, err := ClassifyApplyObjects([]ApplyTask{updated}, runsDir)
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	if got := objects[0]; got.Class != ConvergeSafetyDrift || !got.HasReconcilableDrift() || got.HasStructuralDrift() || !got.Reconcilable {
		t.Fatalf("Call Home edit must be reconcilable drift: %+v", got)
	}
}
