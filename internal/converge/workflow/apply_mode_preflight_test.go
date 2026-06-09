package workflow

import (
	"strings"
	"testing"
)

// preflightObjects builds a real object set from seeded converge-safety records so
// the preflight is exercised through the same classification path apply uses.
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
	// missing: no record
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
	// All-missing -> create proceeds.
	missing := classifyTask("addon.demo.new", "clusterAddon", "demo")
	objs2, err := ClassifyApplyObjects([]ApplyTask{missing}, t.TempDir())
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	if err := EvaluateApplyModePreflight(ApplyModeCreate, objs2); err != nil {
		t.Fatalf("create on an all-missing set must proceed, got %v", err)
	}
}

func TestEvaluateApplyModePreflightContinueFailsDriftAndForeign(t *testing.T) {
	objs := preflightObjects(t, t.TempDir())
	err := EvaluateApplyModePreflight(ApplyModeContinue, objs)
	if err == nil {
		t.Fatal("continue must refuse drift/foreign")
	}
	for _, want := range []string{"addon.demo.drift", "addon.demo.foreign"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("continue error must name the offending object %q: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "addon.demo.missing") || strings.Contains(err.Error(), "addon.demo.match") {
		t.Fatalf("continue must not block missing/match objects: %v", err)
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

func TestEvaluateApplyModePreflightOverrideOnlyFailsForeign(t *testing.T) {
	objs := preflightObjects(t, t.TempDir())
	err := EvaluateApplyModePreflight(ApplyModeOverride, objs)
	if err == nil {
		t.Fatal("override must refuse foreign objects")
	}
	if !strings.Contains(err.Error(), "addon.demo.foreign") {
		t.Fatalf("override error must name the foreign object: %v", err)
	}
	// Override tolerates drift and match (rebuilds drift, skips match): the only
	// blocker is foreign, so an object set without foreign must proceed.
	if strings.Contains(err.Error(), "addon.demo.drift") {
		t.Fatalf("override must not block drift (it rebuilds it): %v", err)
	}
}
