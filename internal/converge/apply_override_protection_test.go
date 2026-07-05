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

// TestCheckApplyOverrideDestroyProtectionGranularKinds locks in the per-kind
// tightening: on an allow-default environment, a destructive rebuild is blocked only
// when its kind is in spec.safety.protectedKinds — so a protected StorageCluster is
// gated while an unprotected ContainerCluster rebuild proceeds.
func TestCheckApplyOverrideDestroyProtectionGranularKinds(t *testing.T) {
	storage := driftedObjects(t, workflow.ApplyTaskKindStorageCluster, "storage.ceph", "ceph")
	protect := func(kinds ...string) v1alpha1.State {
		return v1alpha1.State{Environments: []v1alpha1.Environment{{
			Metadata: v1alpha1.Metadata{Name: "nprd"},
			Spec:     v1alpha1.EnvironmentSpec{Safety: v1alpha1.EnvironmentSafetySpec{ProtectedKinds: kinds}},
		}}}
	}

	// StorageCluster protected on an allow-default env: blocked with guidance.
	err := CheckApplyOverrideDestroyProtection(protect(v1alpha1.KindStorageCluster), storage)
	if err == nil {
		t.Fatal("protected StorageCluster kind must fail closed even on an allow-default env")
	}
	for _, want := range []string{"StorageCluster/ceph", "protectedKinds", "destroy --override"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("granular gate error must contain %q: %v", want, err)
		}
	}
	// Only ContainerCluster protected: a StorageCluster rebuild proceeds.
	if err := CheckApplyOverrideDestroyProtection(protect(v1alpha1.KindContainerCluster), storage); err != nil {
		t.Fatalf("a StorageCluster rebuild must proceed when only ContainerCluster is protected: %v", err)
	}
}

// TestCheckApplyOverrideDestroyProtectionMachineSubstrateRemedy locks in the fix
// for the destroy/re-apply loop: a blocked managed-OS machine (torn down only by
// the infra stage) must be pointed at `destroy --stage infra --clusters <name>`,
// NOT the clusters-stage `destroy --override` that can never clear its records —
// following which would loop forever.
func TestCheckApplyOverrideDestroyProtectionMachineSubstrateRemedy(t *testing.T) {
	machine := driftedObjects(t, workflow.ApplyTaskKindManagedMachineOS, "osinstall.ceph-nprd", "ceph-nprd")

	err := CheckApplyOverrideDestroyProtection(protectedEnvState(), machine)
	if err == nil {
		t.Fatal("protected env with a drifted managed-OS machine must fail closed")
	}
	// The remedy must name the infra-stage destroy scoped to the affected cluster.
	if want := "bootwright destroy --stage infra --clusters ceph-nprd --override"; !strings.Contains(err.Error(), want) {
		t.Fatalf("machine-substrate remedy must direct to %q: %v", want, err)
	}
	// It must NOT emit the old dead-end guidance that the clusters-stage destroy
	// clears the block ("... for that scope first"), which never touches machine
	// substrate and reproduces the loop.
	if strings.Contains(err.Error(), "for that scope") {
		t.Fatalf("machine-substrate remedy must not point at the clusters-scope destroy that cannot clear it: %v", err)
	}
	// It must surface --skip-unreachable so a machine whose host substrate was
	// never provisioned or is powered off (e.g. a nested cluster on a host cluster
	// that never came up) does not fail closed at the infra-stage destroy.
	if !strings.Contains(err.Error(), "--skip-unreachable") {
		t.Fatalf("machine-substrate remedy must hint --skip-unreachable for never-provisioned/powered-off host substrate: %v", err)
	}
}
