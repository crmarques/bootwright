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

func TestControllerDNSDependencyRegistryCoversEveryApplyTaskKind(t *testing.T) {
	live := map[string]bool{}
	for _, kind := range ApplyTaskKinds() {
		live[kind] = true
		class, ok := controllerDNSDependencyClasses[kind]
		if !ok {
			t.Errorf("apply task kind %q has no controller-DNS dependency classification; a new task could run before the live resolver proof", kind)
			continue
		}
		switch class {
		case controllerDNSBefore, controllerDNSBarrier, controllerDNSAfter, controllerDNSDynamic:
		default:
			t.Errorf("apply task kind %q has unknown controller-DNS dependency class %q", kind, class)
		}
	}
	for kind := range controllerDNSDependencyClasses {
		if !live[kind] {
			t.Errorf("controller-DNS dependency registry holds retired apply task kind %q", kind)
		}
	}
	if len(live) != len(controllerDNSDependencyClasses) {
		t.Fatalf("controller-DNS dependency registry has %d entries for %d live apply task kinds", len(controllerDNSDependencyClasses), len(live))
	}
	wantSpecial := map[string]controllerDNSDependencyClass{
		ApplyTaskKindProvider:                 controllerDNSBefore,
		ApplyTaskKindInfraComponentServices:   controllerDNSBefore,
		ApplyTaskKindControllerNameResolution: controllerDNSBarrier,
		ApplyTaskKindPlaybook:                 controllerDNSDynamic,
	}
	for kind, class := range controllerDNSDependencyClasses {
		want, special := wantSpecial[kind]
		if !special {
			want = controllerDNSAfter
		}
		if class != want {
			t.Errorf("apply task kind %q controller-DNS class = %q, want %q", kind, class, want)
		}
	}
}

func TestApplyTaskExecutionClassRegistryCoversEveryConstant(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "apply_tasks.go", nil, 0)
	if err != nil {
		t.Fatalf("parse apply_tasks.go: %v", err)
	}
	want := map[ApplyTaskExecutionClass]bool{ApplyTaskExecutionLiveProof: true}
	declared := map[ApplyTaskExecutionClass]string{}
	declaredCount := 0
	ast.Inspect(file, func(n ast.Node) bool {
		spec, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for i, name := range spec.Names {
			if !strings.HasPrefix(name.Name, "ApplyTaskExecution") {
				continue
			}
			if i >= len(spec.Values) {
				t.Errorf("%s has no explicit string value; every execution class must enter the closed graph validator", name.Name)
				continue
			}
			lit, ok := spec.Values[i].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				t.Errorf("%s is not an explicit string execution class", name.Name)
				continue
			}
			declaredCount++
			class := ApplyTaskExecutionClass(strings.Trim(lit.Value, `"`))
			if previous := declared[class]; previous != "" {
				t.Errorf("%s and %s both declare execution class %q; every class must have one canonical constant", previous, name.Name, class)
			}
			declared[class] = name.Name
			if !want[class] {
				t.Errorf("%s = %q has no closed task-kind and mutation-selector validation", name.Name, class)
			}
		}
		return true
	})
	if declaredCount == 0 {
		t.Fatal("found no ApplyTaskExecution constants to check; the guard would pass vacuously")
	}
	for class := range want {
		if declared[class] == "" {
			t.Errorf("registered apply task execution class %q has no constant", class)
		}
	}
	if declaredCount != len(want) || len(declared) != len(want) {
		t.Fatalf("declared %d apply task execution constants with %d distinct values, want %d closed classes", declaredCount, len(declared), len(want))
	}
}

func TestApplyTaskExecutionClassPinsControllerMutationDecision(t *testing.T) {
	cases := []struct {
		name           string
		kind           string
		class          ApplyTaskExecutionClass
		extraVarPairs  []string
		wantErrorPiece string
	}{
		{name: "mutating controller task", kind: ApplyTaskKindControllerNameResolution, extraVarPairs: []string{"bootwright_controller_name_resolution_mutation_selected=true"}},
		{name: "proof-only controller task", kind: ApplyTaskKindControllerNameResolution, class: ApplyTaskExecutionLiveProof, extraVarPairs: []string{"bootwright_controller_name_resolution_mutation_selected=false"}},
		{name: "mutating task cannot carry false", kind: ApplyTaskKindControllerNameResolution, extraVarPairs: []string{"bootwright_controller_name_resolution_mutation_selected=false"}, wantErrorPiece: "requires exactly"},
		{name: "proof task cannot carry true", kind: ApplyTaskKindControllerNameResolution, class: ApplyTaskExecutionLiveProof, extraVarPairs: []string{"bootwright_controller_name_resolution_mutation_selected=true"}, wantErrorPiece: "requires exactly"},
		{name: "decision is required", kind: ApplyTaskKindControllerNameResolution, wantErrorPiece: "requires exactly"},
		{name: "decision must be unique", kind: ApplyTaskKindControllerNameResolution, class: ApplyTaskExecutionLiveProof, extraVarPairs: []string{"bootwright_controller_name_resolution_mutation_selected=false", "bootwright_controller_name_resolution_mutation_selected=false"}, wantErrorPiece: "requires exactly"},
		{name: "live proof is kind-bound", kind: ApplyTaskKindClusterISO, class: ApplyTaskExecutionLiveProof, extraVarPairs: []string{"bootwright_controller_name_resolution_mutation_selected=false"}, wantErrorPiece: "unsupported task kind"},
		{name: "unknown class", kind: ApplyTaskKindControllerNameResolution, class: ApplyTaskExecutionClass("future"), extraVarPairs: []string{"bootwright_controller_name_resolution_mutation_selected=true"}, wantErrorPiece: "unknown execution class"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			graph := NewActivityGraph()
			err := graph.Add(Activity{
				ID: "task",
				Task: ApplyTask{
					Entry:          TaskLedgerEntry{ID: "task", Kind: tc.kind},
					ExecutionClass: tc.class,
					ExtraVarPairs:  tc.extraVarPairs,
				},
			})
			if tc.wantErrorPiece == "" {
				if err != nil {
					t.Fatalf("graph add: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErrorPiece) {
				t.Fatalf("graph add error = %v, want %q", err, tc.wantErrorPiece)
			}
		})
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
