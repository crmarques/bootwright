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
		Spec:     v1alpha1.EnvironmentSpec{Safety: v1alpha1.EnvironmentSafetySpec{DestroyProtection: v1alpha1.EnvironmentDestroyProtectionProtected}}},
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

	if err := CheckApplyOverrideDestroyProtection(v1alpha1.State{}, destructive, nil); err != nil {
		t.Fatalf("unprotected env must not block: %v", err)
	}
	if err := CheckApplyOverrideDestroyProtection(protectedEnvState(), nil, nil); err != nil {
		t.Fatalf("protected env with nothing destructive must proceed: %v", err)
	}
	if err := CheckApplyOverrideDestroyProtection(protectedEnvState(), reconfigure, nil); err != nil {
		t.Fatalf("protected env with only reconfigure-only drift must proceed: %v", err)
	}
	err := CheckApplyOverrideDestroyProtection(protectedEnvState(), destructive, nil)
	if err == nil {
		t.Fatal("protected env with destructive drift must fail closed")
	}
	for _, want := range []string{"StorageCluster/ceph", "nprd", "bootwright destroy --clusters ceph --authorize protected"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("gate error must contain %q: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "destroy --force` for that scope") {
		t.Fatalf("cluster-scope remedy must quote a scoped destroy, not the full-estate command: %v", err)
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

	err := CheckApplyOverrideDestroyProtection(protect(v1alpha1.KindStorageCluster), storage, nil)
	if err == nil {
		t.Fatal("protected StorageCluster kind must fail closed even on an allow-default env")
	}
	for _, want := range []string{"StorageCluster/ceph", "protectedKinds", "bootwright destroy --clusters ceph --authorize protected"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("granular gate error must contain %q: %v", want, err)
		}
	}
	if err := CheckApplyOverrideDestroyProtection(protect(v1alpha1.KindContainerCluster), storage, nil); err != nil {
		t.Fatalf("a StorageCluster rebuild must proceed when only ContainerCluster is protected: %v", err)
	}
}

func TestCheckApplyOverrideDestroyProtectionReinstalls(t *testing.T) {
	reinstalls := []string{"reinstall ContainerCluster/dc1-ocp (installed record matches desired inputs but the cluster does not report Available=True; to keep its data, repair the cluster to Available=True and re-run plain apply — --mode rebuild reinstalls it and wipes its node disks)"}
	protect := func(kinds ...string) v1alpha1.State {
		return v1alpha1.State{Environments: []v1alpha1.Environment{{
			Metadata: v1alpha1.Metadata{Name: "nprd"},
			Spec:     v1alpha1.EnvironmentSpec{Safety: v1alpha1.EnvironmentSafetySpec{ProtectedKinds: kinds}},
		}}}
	}

	err := CheckApplyOverrideDestroyProtection(protectedEnvState(), nil, reinstalls)
	if err == nil {
		t.Fatal("protected env with a cluster reinstall and no drift must fail closed")
	}
	for _, want := range []string{"reinstall ContainerCluster/dc1-ocp", "nprd", "destroy --authorize protected"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("protected-env reinstall refusal must contain %q: %v", want, err)
		}
	}
	err = CheckApplyOverrideDestroyProtection(protect(v1alpha1.KindContainerCluster), nil, reinstalls)
	if err == nil {
		t.Fatal("protected ContainerCluster kind must fail closed on a reinstall")
	}
	for _, want := range []string{"reinstall ContainerCluster/dc1-ocp", "protectedKinds", "destroy --authorize protected"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("protectedKinds reinstall refusal must contain %q: %v", want, err)
		}
	}
	if err := CheckApplyOverrideDestroyProtection(protect(v1alpha1.KindStorageCluster), nil, reinstalls); err != nil {
		t.Fatalf("a ContainerCluster reinstall must proceed when only StorageCluster is protected: %v", err)
	}
}

func managedRHSMStorageState(clusterName string) v1alpha1.State {
	return v1alpha1.State{
		Entitlements: []v1alpha1.Entitlement{{
			Metadata: v1alpha1.Metadata{Name: "rhcs"},
			Spec: v1alpha1.EntitlementSpec{
				Type: v1alpha1.EntitlementTypeRedHatCeph,
				RHSM: &v1alpha1.EntitlementRHSM{
					OrganizationRef:  v1alpha1.SecretRef{Name: "org"},
					ActivationKeyRef: v1alpha1.SecretRef{Name: "key"},
				},
			},
		}},
		StorageClusters: []v1alpha1.StorageCluster{{
			Metadata: v1alpha1.Metadata{Name: clusterName},
			Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
				Distribution:   v1alpha1.StorageCephDistributionRedHat,
				EntitlementRef: v1alpha1.LocalObjectReference{Name: "rhcs"},
			}},
		}},
	}
}

func TestCheckApplyOverrideDestroyProtectionManagedRHSMReimageRoutesToDestroy(t *testing.T) {
	machine := driftedObjects(t, workflow.ApplyTaskKindManagedMachineOS, "osinstall.ceph", "ceph")

	err := CheckApplyOverrideDestroyProtection(managedRHSMStorageState("ceph"), machine, nil)
	if err == nil {
		t.Fatal("in-place reimage of a managed-RHSM storage node must be routed to destroy, not reimaged in place")
	}
	for _, want := range []string{"managed-RHSM", "bootwright destroy --stage infra --clusters ceph --authorize protected", "RHSM"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("managed-RHSM reimage refusal must contain %q: %v", want, err)
		}
	}

	ossState := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Distribution: v1alpha1.StorageCephDistributionOSS,
		}},
	}}}
	if err := CheckApplyOverrideDestroyProtection(ossState, machine, nil); err != nil {
		t.Fatalf("OSS (non-managed-RHSM) storage node keeps in-place reimage: %v", err)
	}
}

func TestCheckApplyOverrideDestroyProtectionMachineSubstrateRemedy(t *testing.T) {
	machine := driftedObjects(t, workflow.ApplyTaskKindManagedMachineOS, "osinstall.ceph-nprd", "ceph-nprd")

	err := CheckApplyOverrideDestroyProtection(protectedEnvState(), machine, nil)
	if err == nil {
		t.Fatal("protected env with a drifted managed-OS machine must fail closed")
	}
	if want := "bootwright destroy --stage infra --clusters ceph-nprd --authorize protected"; !strings.Contains(err.Error(), want) {
		t.Fatalf("machine-substrate remedy must direct to %q: %v", want, err)
	}
	if strings.Contains(err.Error(), "for that scope") {
		t.Fatalf("machine-substrate remedy must not point at the clusters-scope destroy that cannot clear it: %v", err)
	}
	if !strings.Contains(err.Error(), "--authorize unreachable-nodes") {
		t.Fatalf("machine-substrate remedy must hint --authorize unreachable-nodes for never-provisioned/powered-off host substrate: %v", err)
	}
}
