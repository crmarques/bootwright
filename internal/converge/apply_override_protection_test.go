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
		HashSchema:   workflow.ConvergeHashSchema,
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

	if err := CheckApplyOverrideDestroyProtection(v1alpha1.State{}, destructive); err != nil {
		t.Fatalf("unprotected env must not block: %v", err)
	}
	if err := CheckApplyOverrideDestroyProtection(protectedEnvState(), nil); err != nil {
		t.Fatalf("protected env with nothing destructive must proceed: %v", err)
	}
	if err := CheckApplyOverrideDestroyProtection(protectedEnvState(), reconfigure); err != nil {
		t.Fatalf("protected env with only reconfigure-only drift must proceed: %v", err)
	}
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

func TestCheckApplyOverrideDestroyProtectionGranularKinds(t *testing.T) {
	storage := driftedObjects(t, workflow.ApplyTaskKindStorageCluster, "storage.ceph", "ceph")
	protect := func(kinds ...string) v1alpha1.State {
		return v1alpha1.State{Environments: []v1alpha1.Environment{{
			Metadata: v1alpha1.Metadata{Name: "nprd"},
			Spec:     v1alpha1.EnvironmentSpec{Safety: v1alpha1.EnvironmentSafetySpec{ProtectedKinds: kinds}},
		}}}
	}

	err := CheckApplyOverrideDestroyProtection(protect(v1alpha1.KindStorageCluster), storage)
	if err == nil {
		t.Fatal("protected StorageCluster kind must fail closed even on an allow-default env")
	}
	for _, want := range []string{"StorageCluster/ceph", "protectedKinds", "destroy --override"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("granular gate error must contain %q: %v", want, err)
		}
	}
	if err := CheckApplyOverrideDestroyProtection(protect(v1alpha1.KindContainerCluster), storage); err != nil {
		t.Fatalf("a StorageCluster rebuild must proceed when only ContainerCluster is protected: %v", err)
	}
}

func TestCheckApplyOverrideDestroyProtectionMachineSubstrateRemedy(t *testing.T) {
	machine := driftedObjects(t, workflow.ApplyTaskKindManagedMachineOS, "osinstall.ceph-nprd", "ceph-nprd")

	err := CheckApplyOverrideDestroyProtection(protectedEnvState(), machine)
	if err == nil {
		t.Fatal("protected env with a drifted managed-OS machine must fail closed")
	}
	if want := "bootwright destroy --stage infra --clusters ceph-nprd --override"; !strings.Contains(err.Error(), want) {
		t.Fatalf("machine-substrate remedy must direct to %q: %v", want, err)
	}
	if strings.Contains(err.Error(), "for that scope") {
		t.Fatalf("machine-substrate remedy must not point at the clusters-scope destroy that cannot clear it: %v", err)
	}
	if !strings.Contains(err.Error(), "--skip-unreachable") {
		t.Fatalf("machine-substrate remedy must hint --skip-unreachable for never-provisioned/powered-off host substrate: %v", err)
	}
}
