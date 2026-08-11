package workflow

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/remedy"
)

func assertLegacyConvergenceEvidenceRemedy(t *testing.T, err error, resourceID string) *LegacyConvergenceEvidenceError {
	t.Helper()
	var typed *LegacyConvergenceEvidenceError
	if !errors.As(err, &typed) {
		t.Fatalf("error %T does not retain typed legacy evidence: %v", err, err)
	}
	if typed.ResourceID != resourceID || typed.Cause == nil {
		t.Fatalf("legacy evidence = %#v, want resource %s with cause", typed, resourceID)
	}
	var remedial remedy.Error
	if !errors.As(err, &remedial) || remedial.Remedy().Action != remedy.ActionRebuildSameSelection {
		t.Fatalf("legacy evidence error lacks same-selection rebuild action: %v", err)
	}
	for _, forbidden := range []string{"bootwright apply", "bootwright destroy"} {
		if strings.Contains(err.Error(), forbidden) {
			t.Fatalf("legacy evidence error embeds state-changing argv %q: %v", forbidden, err)
		}
	}
	return typed
}

func TestLegacyStorageSubObjectEvidenceCarriesTypedSameSelectionRebuild(t *testing.T) {
	state := storageSubObjectState()
	sub := storageSubObject{Kind: storageSubObjectKindPool, Cluster: "demo", Name: "p1"}
	desiredHash, err := storageSubObjectDesiredHash(state, sub)
	if err != nil {
		t.Fatal(err)
	}
	record := ConvergeSafetyRecord{
		APIVersion:   ConvergeSafetyAPIVersion,
		ResourceID:   sub.resourceID(),
		ResourceKind: sub.Kind,
		TaskID:       "storage.demo",
		TaskKind:     ApplyTaskKindStorageCluster,
		DesiredHash:  desiredHash,
		HashSchema:   ConvergeHashSchema - 1,
		Owner:        ConvergeSafetyOwnerIdentity{Manager: ConvergeSafetyOwner, Context: "ctx"},
		Status:       ConvergeSafetyStatusReconciled,
		RunID:        "missing-run",
	}
	class, err := classifyStorageSubObjectWithRecordForContext(state, sub, t.TempDir(), "ctx", record, desiredHash)
	if class != ConvergeSafetyUnknown || err == nil {
		t.Fatalf("classification = %q, %v, want unknown typed refusal", class, err)
	}
	typed := assertLegacyConvergenceEvidenceRemedy(t, err, sub.resourceID())
	if !strings.Contains(typed.Cause.Error(), "predates immutable successful-input snapshots") {
		t.Fatalf("schema-3 cause = %v", typed.Cause)
	}
}

func TestSchemaThreeRebuildRequiresExactAuthority(t *testing.T) {
	task, record, _ := identityValidationTask(t)
	record.HashSchema = ConvergeHashSchema - 1
	record.RunID = "../never-read"
	runsDir := t.TempDir()
	if err := SaveConvergeSafetyRecord(runsDir, record); err != nil {
		t.Fatal(err)
	}
	objects, err := ClassifyApplyObjectsForMode([]ApplyTask{task}, runsDir, "ctx", ApplyModeRebuild)
	if err != nil || len(objects) != 1 || !objects[0].HasStructuralDrift() || objects[0].HasReconcilableDrift() {
		t.Fatalf("exact schema-3 rebuild = %v, %v", objects, err)
	}

	record.Owner.Context = "copied-context"
	if err := SaveConvergeSafetyRecord(runsDir, record); err != nil {
		t.Fatal(err)
	}
	if objects, err := ClassifyApplyObjectsForMode([]ApplyTask{task}, runsDir, "ctx", ApplyModeRebuild); err == nil || objects != nil {
		t.Fatalf("schema-3 rebuild adopted copied context: objects=%v err=%v", objects, err)
	} else {
		var untrusted *UntrustedConvergenceEvidenceError
		if !errors.As(err, &untrusted) {
			t.Fatalf("schema-3 copied context = %T, want untrusted: %v", err, err)
		}
	}
}

func storageSubObjectState() v1alpha1.State {
	return v1alpha1.State{
		StorageClusters: []v1alpha1.StorageCluster{{Metadata: v1alpha1.Metadata{Name: "demo"}}},
		StoragePools:    []v1alpha1.StoragePool{storageSubObjectTestPool("p1", 3)},
	}
}

func TestLegacyConvergenceEvidenceSourcesNeverEmbedStateChangeArgv(t *testing.T) {
	for _, name := range []string{"converge_safety.go", "apply_storage_subobjects.go", "convergence_evidence_remedy.go"} {
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"bootwright apply", "bootwright destroy"} {
			if strings.Contains(string(source), forbidden) {
				t.Fatalf("%s embeds state-changing argv %q; return ActionRebuildSameSelection and let internal/cli render the resolved invocation", name, forbidden)
			}
		}
	}
}
