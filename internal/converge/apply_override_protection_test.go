package converge

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/remedy"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func assertProtectedLayerRemedy(t *testing.T, err error, roles ...remedy.TargetRole) *ApplyOverrideDestroyProtectionError {
	t.Helper()
	var typed *ApplyOverrideDestroyProtectionError
	if !errors.As(err, &typed) {
		t.Fatalf("error %T does not retain typed protection evidence: %v", err, err)
	}
	var remedial remedy.Error
	if !errors.As(err, &remedial) {
		t.Fatalf("error %T does not carry a typed remedy: %v", err, err)
	}
	request := remedial.Remedy()
	if request.Action != remedy.ActionDestroyProtectedLayersThenRebuildSameSelection {
		t.Fatalf("action = %q, want protected-layer teardown and same-selection rebuild", request.Action)
	}
	if len(request.Targets) != len(roles) {
		t.Fatalf("targets = %#v, want roles %v", request.Targets, roles)
	}
	for i, role := range roles {
		if request.Targets[i].Role != role {
			t.Fatalf("target %d role = %q, want %q", i, request.Targets[i].Role, role)
		}
	}
	for _, forbidden := range []string{"bootwright apply", "bootwright destroy"} {
		if strings.Contains(err.Error(), forbidden) {
			t.Fatalf("backend protection error embeds executable argv %q: %v", forbidden, err)
		}
	}
	return typed
}

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
	typed := assertProtectedLayerRemedy(t, err, remedy.TargetRoleClusterLayer)
	for _, want := range []string{"StorageCluster/ceph", "nprd", "explicit destroy boundary"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("gate error must contain %q: %v", want, err)
		}
	}
	if len(typed.ClusterLayer) != 1 || typed.ClusterLayer[0] != "ceph" {
		t.Fatalf("cluster-layer evidence = %v, want ceph", typed.ClusterLayer)
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
	assertProtectedLayerRemedy(t, err, remedy.TargetRoleClusterLayer)
	for _, want := range []string{"StorageCluster/ceph", "protectedKinds", "explicit destroy boundary"} {
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
	assertProtectedLayerRemedy(t, err, remedy.TargetRoleClusterLayer)
	for _, want := range []string{"reinstall ContainerCluster/dc1-ocp", "nprd", "explicit destroy boundary"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("protected-env reinstall refusal must contain %q: %v", want, err)
		}
	}
	err = CheckApplyOverrideDestroyProtection(protect(v1alpha1.KindContainerCluster), nil, reinstalls)
	if err == nil {
		t.Fatal("protected ContainerCluster kind must fail closed on a reinstall")
	}
	assertProtectedLayerRemedy(t, err, remedy.TargetRoleClusterLayer)
	for _, want := range []string{"reinstall ContainerCluster/dc1-ocp", "protectedKinds", "explicit destroy boundary"} {
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
	typed := assertProtectedLayerRemedy(t, err, remedy.TargetRoleMachineLayer)
	for _, want := range []string{"managed-RHSM", "explicit destroy boundary", "RHSM"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("managed-RHSM reimage refusal must contain %q: %v", want, err)
		}
	}
	if len(typed.ManagedRHSMClusters) != 1 || typed.ManagedRHSMClusters[0] != "ceph" {
		t.Fatalf("managed-RHSM evidence = %v, want ceph", typed.ManagedRHSMClusters)
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

func TestCheckApplyOverrideDestroyProtectionManagedRHSMMixedWithProtectedClusterLayer(t *testing.T) {
	state := managedRHSMStorageState("ceph")
	state.Environments = []v1alpha1.Environment{{
		Metadata: v1alpha1.Metadata{Name: "nprd"},
		Spec: v1alpha1.EnvironmentSpec{Safety: v1alpha1.EnvironmentSafetySpec{
			ProtectedKinds: []string{v1alpha1.KindStorageCluster},
		}},
	}}
	machine := driftedObjects(t, workflow.ApplyTaskKindManagedMachineOS, "osinstall.ceph", "ceph")
	storage := driftedObjects(t, workflow.ApplyTaskKindStorageCluster, "storage.ceph", "ceph")

	err := CheckApplyOverrideDestroyProtection(state, append(machine, storage...), nil)
	if err == nil {
		t.Fatal("managed-RHSM machine work mixed with a protected cluster rebuild must fail closed on both layers")
	}
	typed := assertProtectedLayerRemedy(t, err, remedy.TargetRoleMachineLayer, remedy.TargetRoleClusterLayer)
	if len(typed.ClusterLayer) != 1 || typed.ClusterLayer[0] != "ceph" {
		t.Fatalf("cluster-layer evidence = %v, want ceph", typed.ClusterLayer)
	}
	for _, want := range []string{"managed-RHSM", "protected cluster-layer work ceph", "explicit destroy boundary"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("mixed managed-RHSM refusal must contain %q: %v", want, err)
		}
	}
}

func TestCheckApplyOverrideDestroyProtectionMachineSubstrateRemedy(t *testing.T) {
	machine := driftedObjects(t, workflow.ApplyTaskKindManagedMachineOS, "osinstall.ceph-nprd", "ceph-nprd")

	err := CheckApplyOverrideDestroyProtection(protectedEnvState(), machine, nil)
	if err == nil {
		t.Fatal("protected env with a drifted managed-OS machine must fail closed")
	}
	typed := assertProtectedLayerRemedy(t, err, remedy.TargetRoleMachineLayer)
	if len(typed.MachineLayer) != 1 || typed.MachineLayer[0] != "osinstall.ceph-nprd" {
		t.Fatalf("machine-layer evidence = %v, want selected managed-OS object", typed.MachineLayer)
	}
}

func TestApplyProtectionSourceNeverEmbedsStateChangeArgv(t *testing.T) {
	for _, name := range []string{"apply.go", "apply_override_remedy.go"} {
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"bootwright apply", "bootwright destroy"} {
			if strings.Contains(string(source), forbidden) {
				t.Fatalf("%s embeds state-changing argv %q; return typed layer evidence and let internal/cli render the resolved selection", name, forbidden)
			}
		}
	}
}
