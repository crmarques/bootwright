package workflow

import (
	"testing"
	"time"
)

func saveRecordWithHashes(t *testing.T, runsDir string, task ApplyTask, desiredHash, structuralHash string) {
	t.Helper()
	record := ConvergeSafetyRecord{
		APIVersion:     ConvergeSafetyAPIVersion,
		ResourceID:     applyTaskSafetyResourceID(task),
		DesiredHash:    desiredHash,
		StructuralHash: structuralHash,
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

// A StorageCluster whose only change is the OSD device selection (full desired
// hash moves, structural hash unchanged) classifies as reconcilable drift: it
// still displays as drift, but continue proceeds and --override does not wipe.
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

	// Device add: same structural projection, different device list.
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

	// Structural change (identity moved): not reconcilable.
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

// A record written before StructuralHash existed (empty) must fall back to
// structural drift on any change — never silently reconcilable — so upgrading
// bootwright never turns a real rebuild into a no-op.
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
	saveRecordWithHashes(t, runsDir, base, dh, "") // legacy record: no structural hash

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
