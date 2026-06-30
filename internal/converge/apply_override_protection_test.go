package converge

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func protectedEnvState() v1alpha1.State {
	return v1alpha1.State{Environments: []v1alpha1.Environment{{
		Metadata: v1alpha1.Metadata{Name: "nprd"},
		Spec:     v1alpha1.EnvironmentSpec{Safety: v1alpha1.EnvironmentSafetySpec{DestroyProtection: v1alpha1.EnvironmentDestroyProtectionRequiredOverride}}},
	}}
}

// driftedObjects classifies a single drifted object of the given task kind by
// seeding a converge-safety record with a stale hash, so the gate sees real drift.
func driftedObjects(t *testing.T, kind, id, cluster string) []workflow.ObjectClassification {
	t.Helper()
	runsDir := t.TempDir()
	task := workflow.ApplyTask{Entry: workflow.TaskLedgerEntry{ID: id, Kind: kind, Label: id, Cluster: cluster}}
	if err := workflow.SaveConvergeSafetyRecord(runsDir, workflow.ConvergeSafetyRecord{
		APIVersion:   workflow.ConvergeSafetyAPIVersion,
		ResourceID:   kind + "/" + id,
		ResourceKind: kind,
		TaskID:       id,
		TaskKind:     kind,
		DesiredHash:  "sha256:stale",
		Owner:        workflow.ConvergeSafetyOwnerIdentity{Manager: workflow.ConvergeSafetyOwner},
	}); err != nil {
		t.Fatalf("seed drift record: %v", err)
	}
	objs, err := workflow.ClassifyApplyObjects([]workflow.ApplyTask{task}, runsDir)
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	return objs
}

func TestCheckApplyOverrideDestroyProtectionScopeAware(t *testing.T) {
	destructive := driftedObjects(t, workflow.ApplyTaskKindStorageCluster, "storage.ceph", "ceph")
	reconfigure := driftedObjects(t, workflow.ApplyTaskKindInfraComponentServices, "infra-component.bastion", "")

	// Unprotected environment: never blocks, even on destructive drift.
	if err := CheckApplyOverrideDestroyProtection(v1alpha1.State{}, destructive); err != nil {
		t.Fatalf("unprotected env must not block: %v", err)
	}
	// Protected + no drift to rebuild: greenfield/no-op override proceeds.
	if err := CheckApplyOverrideDestroyProtection(protectedEnvState(), nil); err != nil {
		t.Fatalf("protected env with nothing destructive must proceed: %v", err)
	}
	// Protected + reconfigure-only drift: the drifted shared service is reconciled
	// in place, so the destroy boundary is not crossed.
	if err := CheckApplyOverrideDestroyProtection(protectedEnvState(), reconfigure); err != nil {
		t.Fatalf("protected env with only reconfigure-only drift must proceed: %v", err)
	}
	// Protected + destructive drift: fail closed, naming the object and the remedy.
	err := CheckApplyOverrideDestroyProtection(protectedEnvState(), destructive)
	if err == nil {
		t.Fatal("protected env with destructive drift must fail closed")
	}
	for _, want := range []string{"StorageCluster/ceph", "nprd", "destroy --override"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("gate error must contain %q: %v", want, err)
		}
	}
}
