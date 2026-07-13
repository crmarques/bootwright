package workflow

import (
	"strings"
	"testing"
)

func TestOverrideReconfigureOnlyKindsMatchPublishedContract(t *testing.T) {
	want := map[string]bool{
		ApplyTaskKindProvider:               true,
		ApplyTaskKindInfraComponentServices: true,
		ApplyTaskKindNodeConfigApply:        true,
		ApplyTaskKindHostVirtctl:            true,
		ApplyTaskKindClusterAddon:           true,
		ApplyTaskKindProvisioningPlaybook:   true,
		"storageAttachmentApply":            true,
	}
	if len(overrideReconfigureOnlyKinds) != len(want) {
		t.Fatalf("overrideReconfigureOnlyKinds has %d kinds, published contract has %d; update specs/state-model.md --override taxonomy", len(overrideReconfigureOnlyKinds), len(want))
	}
	for kind := range want {
		if !overrideReconfigureOnlyKinds[kind] {
			t.Errorf("published reconfigure-only kind %q missing from overrideReconfigureOnlyKinds", kind)
		}
	}
	for kind := range overrideReconfigureOnlyKinds {
		if !want[kind] {
			t.Errorf("overrideReconfigureOnlyKinds has unpublished kind %q; add it to the specs/state-model.md --override taxonomy", kind)
		}
	}
}

func preflightObjects(t *testing.T, runsDir string) []ObjectClassification {
	t.Helper()
	match := classifyTask("addon.demo.match", "clusterAddon", "demo")
	drift := classifyTask("addon.demo.drift", "clusterAddon", "demo")
	foreign := classifyTask("addon.demo.foreign", "clusterAddon", "demo")
	missing := classifyTask("addon.demo.missing", "clusterAddon", "demo")
	matchHash, err := ApplyTaskDesiredHash(match)
	if err != nil {
		t.Fatalf("desired hash: %v", err)
	}
	saveStateCheckRecord(t, runsDir, match, matchHash, ConvergeSafetyOwner)
	saveStateCheckRecord(t, runsDir, drift, "sha256:stale", ConvergeSafetyOwner)
	saveStateCheckRecord(t, runsDir, foreign, "sha256:stale", "someone-else")
	objs, err := ClassifyApplyObjects([]ApplyTask{match, drift, foreign, missing}, runsDir)
	if err != nil {
		t.Fatalf("ClassifyApplyObjects: %v", err)
	}
	return objs
}

func TestEvaluateApplyModePreflightCreateGreenfieldOnly(t *testing.T) {
	objs := preflightObjects(t, t.TempDir())
	err := EvaluateApplyModePreflight(ApplyModeCreate, objs)
	if err == nil {
		t.Fatal("create must refuse when objects already exist")
	}
	if !strings.Contains(err.Error(), "--expect-new") {
		t.Fatalf("create error must name --expect-new: %v", err)
	}
	missing := classifyTask("addon.demo.new", "clusterAddon", "demo")
	objs2, err := ClassifyApplyObjects([]ApplyTask{missing}, t.TempDir())
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	if err := EvaluateApplyModePreflight(ApplyModeCreate, objs2); err != nil {
		t.Fatalf("create on an all-missing set must proceed, got %v", err)
	}
}

func TestEvaluateApplyModePreflightContinueFailsStructuralDriftAndForeign(t *testing.T) {
	runsDir := t.TempDir()
	addonDrift := classifyTask("addon.demo.drift", ApplyTaskKindClusterAddon, "demo")
	foreign := classifyTask("addon.demo.foreign", ApplyTaskKindClusterAddon, "demo")
	osDrift := classifyTask("os.demo", ApplyTaskKindManagedMachineOS, "demo")
	saveStateCheckRecord(t, runsDir, addonDrift, "sha256:stale", ConvergeSafetyOwner)
	saveStateCheckRecord(t, runsDir, foreign, "sha256:stale", "someone-else")
	saveStateCheckRecord(t, runsDir, osDrift, "sha256:stale", ConvergeSafetyOwner)
	objs, err := ClassifyApplyObjects([]ApplyTask{addonDrift, foreign, osDrift}, runsDir)
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	err = EvaluateApplyModePreflight(ApplyModeContinue, objs)
	if err == nil {
		t.Fatal("continue must refuse foreign and destructive structural drift")
	}
	for _, want := range []string{"os.demo", "addon.demo.foreign"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("continue error must name the offending object %q: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "addon.demo.drift") {
		t.Fatalf("continue must reconcile a reconfigure-only drift in place, not refuse it: %v", err)
	}
}

func TestEvaluateApplyModePreflightContinueReconcilesReconfigureOnlyDrift(t *testing.T) {
	runsDir := t.TempDir()
	addonDrift := classifyTask("addon.demo.drift", ApplyTaskKindClusterAddon, "demo")
	nodeCfgDrift := classifyTask("nodeconfig.demo", ApplyTaskKindNodeConfigApply, "demo")
	saveStateCheckRecord(t, runsDir, addonDrift, "sha256:stale", ConvergeSafetyOwner)
	saveStateCheckRecord(t, runsDir, nodeCfgDrift, "sha256:stale", ConvergeSafetyOwner)
	objs, err := ClassifyApplyObjects([]ApplyTask{addonDrift, nodeCfgDrift}, runsDir)
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	if err := EvaluateApplyModePreflight(ApplyModeContinue, objs); err != nil {
		t.Fatalf("continue over reconfigure-only drift must reconcile in place, got %v", err)
	}
}

func TestEvaluateApplyModePreflightContinueAllMatchProceeds(t *testing.T) {
	runsDir := t.TempDir()
	a := classifyTask("addon.demo.a", "clusterAddon", "demo")
	b := classifyTask("addon.demo.b", "clusterAddon", "demo")
	for _, task := range []ApplyTask{a, b} {
		h, err := ApplyTaskDesiredHash(task)
		if err != nil {
			t.Fatalf("desired hash: %v", err)
		}
		saveStateCheckRecord(t, runsDir, task, h, ConvergeSafetyOwner)
	}
	objs, err := ClassifyApplyObjects([]ApplyTask{a, b}, runsDir)
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	if err := EvaluateApplyModePreflight(ApplyModeContinue, objs); err != nil {
		t.Fatalf("continue over an all-match set must proceed (and no-op), got %v", err)
	}
}

func TestOverrideDestructiveDriftedObjects(t *testing.T) {
	runsDir := t.TempDir()
	infra := classifyTask("infra-component.bastion", ApplyTaskKindInfraComponentServices, "")
	provider := classifyTask("provider.bastion", ApplyTaskKindProvider, "")
	storage := classifyTask("storage.ceph", ApplyTaskKindStorageCluster, "ceph")
	matchedStorage := classifyTask("storage.ceph2", ApplyTaskKindStorageCluster, "ceph2")
	missingCluster := classifyTask("iso.ocp", ApplyTaskKindClusterISO, "ocp")

	saveStateCheckRecord(t, runsDir, infra, "sha256:stale", ConvergeSafetyOwner)
	saveStateCheckRecord(t, runsDir, provider, "sha256:stale", ConvergeSafetyOwner)
	saveStateCheckRecord(t, runsDir, storage, "sha256:stale", ConvergeSafetyOwner)
	matchedHash, err := ApplyTaskDesiredHash(matchedStorage)
	if err != nil {
		t.Fatalf("desired hash: %v", err)
	}
	saveStateCheckRecord(t, runsDir, matchedStorage, matchedHash, ConvergeSafetyOwner)

	objs, err := ClassifyApplyObjects([]ApplyTask{infra, provider, storage, matchedStorage, missingCluster}, runsDir)
	if err != nil {
		t.Fatalf("ClassifyApplyObjects: %v", err)
	}
	got := OverrideDestructiveDriftedObjects(objs)
	if len(got) != 1 || got[0] != "StorageCluster/ceph" {
		t.Fatalf("destructive drift = %v, want only [StorageCluster/ceph]: reconfigure-only drift, matched, and missing must all be exempt", got)
	}
}

func TestOverrideDestructiveMachineSubstrate(t *testing.T) {
	runsDir := t.TempDir()
	managedOS := classifyTask("osinstall.ceph", ApplyTaskKindManagedMachineOS, "ceph")
	storage := classifyTask("storage.ceph", ApplyTaskKindStorageCluster, "ceph")
	saveStateCheckRecord(t, runsDir, managedOS, "sha256:stale", ConvergeSafetyOwner)
	saveStateCheckRecord(t, runsDir, storage, "sha256:stale", ConvergeSafetyOwner)

	objs, err := ClassifyApplyObjects([]ApplyTask{managedOS, storage}, runsDir)
	if err != nil {
		t.Fatalf("ClassifyApplyObjects: %v", err)
	}
	labels, clusters := OverrideDestructiveMachineSubstrate(objs)
	if len(labels) != 1 || labels[0] != "osinstall.ceph" {
		t.Fatalf("machine-substrate labels = %v, want only the managed-OS install (StorageCluster is a cluster rebuild)", labels)
	}
	if len(clusters) != 1 || clusters[0] != "ceph" {
		t.Fatalf("machine-substrate clusters = %v, want [ceph]", clusters)
	}
}

func TestEvaluateApplyModePreflightOverrideOnlyFailsForeign(t *testing.T) {
	objs := preflightObjects(t, t.TempDir())
	err := EvaluateApplyModePreflight(ApplyModeOverride, objs)
	if err == nil {
		t.Fatal("override must refuse foreign objects")
	}
	if !strings.Contains(err.Error(), "addon.demo.foreign") {
		t.Fatalf("override error must name the foreign object: %v", err)
	}
	if strings.Contains(err.Error(), "addon.demo.drift") {
		t.Fatalf("override must not block drift (it rebuilds it): %v", err)
	}
}
