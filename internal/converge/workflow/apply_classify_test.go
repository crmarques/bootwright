package workflow

import "testing"

func classifyTask(id, kind, cluster string) ApplyTask {
	return ApplyTask{Entry: TaskLedgerEntry{ID: id, Kind: kind, Label: id, Cluster: cluster, ClusterKind: "container"}}
}

func TestClassifyApplyObjectsGroupsContainerInstall(t *testing.T) {
	runsDir := t.TempDir()
	iso := classifyTask("iso.demo", ApplyTaskKindClusterISO, "demo")
	boot := classifyTask("boot.demo", ApplyTaskKindNodeBoot, "demo")
	wait := classifyTask("wait.demo", ApplyTaskKindInstallWait, "demo")
	for _, task := range []ApplyTask{iso, boot} {
		h, err := ApplyTaskDesiredHash(task)
		if err != nil {
			t.Fatalf("desired hash: %v", err)
		}
		saveStateCheckRecord(t, runsDir, task, h, ConvergeSafetyOwner)
	}

	objs, err := ClassifyApplyObjects([]ApplyTask{iso, boot, wait}, runsDir)
	if err != nil {
		t.Fatalf("ClassifyApplyObjects: %v", err)
	}
	if len(objs) != 1 {
		t.Fatalf("three container install tasks must collapse to one object, got %d: %+v", len(objs), objs)
	}
	o := objs[0]
	if o.ObjectKey != "ContainerCluster/demo" {
		t.Fatalf("unexpected object key %q", o.ObjectKey)
	}
	if !o.Recorded() {
		t.Fatal("a partially applied object must be Recorded (--mode create must refuse it)")
	}
	if o.HasDrift() || o.HasForeign() {
		t.Fatalf("partial-but-matching object must not be drift/foreign, got class %q", o.Class)
	}
	if o.Class != ConvergeSafetyMissing {
		t.Fatalf("a partially applied object should display as incomplete (missing), got %q", o.Class)
	}
}

func TestClassifyApplyObjectsIndependentDriftAndForeign(t *testing.T) {
	runsDir := t.TempDir()
	match := classifyTask("addon.demo.a", "clusterAddon", "demo")
	drift := classifyTask("addon.demo.b", "clusterAddon", "demo")
	foreign := classifyTask("addon.demo.c", "clusterAddon", "demo")
	matchHash, err := ApplyTaskDesiredHash(match)
	if err != nil {
		t.Fatalf("desired hash: %v", err)
	}
	saveStateCheckRecord(t, runsDir, match, matchHash, ConvergeSafetyOwner)
	saveStateCheckRecord(t, runsDir, drift, "sha256:stale", ConvergeSafetyOwner)
	saveStateCheckRecord(t, runsDir, foreign, "sha256:stale", "someone-else")

	objs, err := ClassifyApplyObjects([]ApplyTask{match, drift, foreign}, runsDir)
	if err != nil {
		t.Fatalf("ClassifyApplyObjects: %v", err)
	}
	if len(objs) != 3 {
		t.Fatalf("distinct addon tasks are distinct objects, got %d", len(objs))
	}
	byKey := map[string]ObjectClassification{}
	for _, o := range objs {
		byKey[o.ObjectKey] = o
	}
	if o := byKey["clusterAddon/addon.demo.a"]; o.HasDrift() || o.HasForeign() || o.Class != ConvergeSafetyMatch {
		t.Fatalf("a should be match, got %+v", o)
	}
	if o := byKey["clusterAddon/addon.demo.b"]; !o.HasDrift() || o.HasForeign() {
		t.Fatalf("b should be drift only, got %+v", o)
	}
	if o := byKey["clusterAddon/addon.demo.c"]; !o.HasForeign() {
		t.Fatalf("c should be foreign, got %+v", o)
	}
}
