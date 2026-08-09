package workflow

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/converge/remedy"
)

func TestApplyTaskKindsRegistryCoversEveryConstant(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "apply_tasks.go", nil, 0)
	if err != nil {
		t.Fatalf("parse apply_tasks.go: %v", err)
	}
	registered := map[string]bool{}
	for _, kind := range ApplyTaskKinds() {
		registered[kind] = true
	}
	declared := 0
	ast.Inspect(file, func(n ast.Node) bool {
		spec, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for i, name := range spec.Names {
			if !strings.HasPrefix(name.Name, "ApplyTaskKind") || i >= len(spec.Values) {
				continue
			}
			lit, ok := spec.Values[i].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				continue
			}
			declared++
			value := strings.Trim(lit.Value, `"`)
			if !registered[value] {
				t.Errorf("%s = %q is not in ApplyTaskKinds(); every mutating apply task kind must be registered so the authorization guards cover it", name.Name, value)
			}
		}
		return true
	})
	if declared == 0 {
		t.Fatal("found no ApplyTaskKind constants to check; the guard would pass vacuously")
	}
	if declared != len(registered) {
		t.Errorf("ApplyTaskKinds() has %d entries but apply_tasks.go declares %d ApplyTaskKind constants", len(registered), declared)
	}
}

func TestEveryApplyTaskKindHasAnOverrideClassification(t *testing.T) {
	for _, kind := range ApplyTaskKinds() {
		drifted := ObjectClassification{
			Kind:   kind,
			Label:  kind + "/demo",
			counts: map[ConvergeSafetyClassification]int{ConvergeSafetyDrift: 1},
		}
		destructive := isOverrideDestructive(drifted)
		if destructive == ApplyTaskKindIsReconfigureOnly(kind) {
			t.Errorf("apply task kind %q must be exactly one of reconfigure-only or destructive under --mode rebuild", kind)
		}
		if !destructive {
			continue
		}
		if consequence := structuralRebuildConsequence(drifted); strings.TrimSpace(consequence) == "" {
			t.Errorf("destructive apply task kind %q has no refusal consequence text; a refusal must name what it would do", kind)
		}
	}
}

func TestEveryDestructiveApplyTaskKindMapsToExactlyOneProtectedLayer(t *testing.T) {
	live := map[string]bool{}
	for _, kind := range ApplyTaskKinds() {
		live[kind] = true
		drifted := ObjectClassification{
			Kind:   kind,
			Label:  kind + "/demo",
			counts: map[ConvergeSafetyClassification]int{ConvergeSafetyDrift: 1},
		}
		if !isOverrideDestructive(drifted) {
			if _, registered := overrideDestructiveLayerRole(kind); registered {
				t.Errorf("reconfigure-only apply task kind %q is registered as a destructive protected layer", kind)
			}
			continue
		}
		role, registered := overrideDestructiveLayerRole(kind)
		if !registered {
			t.Errorf("destructive apply task kind %q has no explicit protected-layer role; classify it as machine-layer or cluster-layer before it can ship", kind)
			continue
		}
		if role != remedy.TargetRoleMachineLayer && role != remedy.TargetRoleClusterLayer {
			t.Errorf("destructive apply task kind %q has unsupported protected-layer role %q", kind, role)
		}
		machines, _ := OverrideDestructiveMachineSubstrate([]ObjectClassification{drifted})
		clusters := OverrideDestructiveClusterScope([]ObjectClassification{drifted})
		if (len(machines) > 0) == (len(clusters) > 0) {
			t.Errorf("destructive apply task kind %q maps to machine=%v cluster=%v; protected rebuild remedies require exactly one fixed destroy layer", kind, machines, clusters)
		}
	}
	for kind := range overrideDestructiveLayerRoles {
		if !live[kind] {
			t.Errorf("protected-layer registry holds retired apply task kind %q", kind)
		}
	}
}

func TestOverrideReconfigureOnlyAllowlistHoldsOnlyLiveTaskKinds(t *testing.T) {
	live := map[string]bool{}
	for _, kind := range ApplyTaskKinds() {
		live[kind] = true
	}
	for kind := range overrideReconfigureOnlyKinds {
		if !live[kind] {
			t.Errorf("overrideReconfigureOnlyKinds holds retired kind %q: an allowlist member that is not a live ApplyTaskKind is unreachable and makes the published taxonomy claim a safety the code cannot deliver", kind)
		}
	}
}

func TestWrittenConvergeSafetyRecordClassifiesAsOwnedMatch(t *testing.T) {
	runsDir := t.TempDir()
	task := classifyTask("addon.demo.roundtrip", ApplyTaskKindClusterAddon, "demo")
	if err := MarkApplyTaskConvergeSafety(runsDir, "ctx", "run", task, ConvergeSafetyStatusCreated, time.Now()); err != nil {
		t.Fatalf("MarkApplyTaskConvergeSafety: %v", err)
	}
	record, found, err := LoadConvergeSafetyRecord(runsDir, applyTaskSafetyResourceID(task))
	if err != nil || !found {
		t.Fatalf("LoadConvergeSafetyRecord: found=%v err=%v", found, err)
	}
	hash, err := ApplyTaskDesiredHash(task)
	if err != nil {
		t.Fatalf("ApplyTaskDesiredHash: %v", err)
	}
	if got := ClassifyConvergeSafety(record, hash, ConvergeSafetyOwner); got != ConvergeSafetyMatch {
		t.Fatalf("a record this writer just wrote classifies as %q, want %q; the owner field the writer stamps and the one the classifier reads have diverged", got, ConvergeSafetyMatch)
	}
	if got := ClassifyConvergeSafety(record, "sha256:other", ConvergeSafetyOwner); got != ConvergeSafetyDrift {
		t.Fatalf("a changed desired hash classifies as %q, want %q", got, ConvergeSafetyDrift)
	}
	if got := ClassifyConvergeSafety(record, hash, "someone-else"); got != ConvergeSafetyForeign {
		t.Fatalf("a record owned by another manager classifies as %q, want %q; the foreign refusal must stay reachable", got, ConvergeSafetyForeign)
	}
}
