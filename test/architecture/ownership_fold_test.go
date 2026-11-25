package architecture_test

import (
	"go/ast"
	"go/build"
	"go/importer"
	"go/types"
	"io/fs"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
)

const (
	lifecyclePackage = "internal/reconciliation/lifecycle"
	foldPackage      = "internal/reconciliation"
	foldAdapter      = "lineage.go"
)

// lineageFoldAllowed lists the production files outside package
// reconciliation that may call reconciliation.Lineage or Additive, which fold
// bare plans without the records a take-back is read from.
var lineageFoldAllowed = []string{}

// TestOwnershipIsFoldedOnlyByTheFold forbids a reader from finding what a
// lineage holds without reconciliation.Fold: outside the lifecycle file that
// hands the lineage to the fold, no production code selects a lineage member's
// plan, whether a range, an index or a closure reaches it, and outside package
// reconciliation no production code folds bare plans with Lineage or Additive.
// The fold answers holder, incarnation, state and life, so a step a removal
// took back and an apply re-created resolves in one place.
func TestOwnershipIsFoldedOnlyByTheFold(t *testing.T) {
	tree, err := newSourceTree(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, found := range ownershipFoldViolations(t, tree) {
		t.Error(found)
	}
}

// TestOwnershipFoldGuardFindsPlantedViolations proves each rule against a
// module that plants a ranged, an indexed and a closure selection of a member's
// plan, a Lineage and an Additive call through an alias and from another
// package; the adapter file, a test file, another type's plan field and the
// fold's own package are not reported.
func TestOwnershipFoldGuardFindsPlantedViolations(t *testing.T) {
	module := fstest.MapFS{
		"go.mod": {Data: []byte("module example.com/fixture\n\ngo 1.26\n")},
		"internal/reconciliation/plan.go": {Data: []byte(`package reconciliation

type Plan struct{ Blocks []string }

func Lineage(plans ...Plan) (Plan, error) { return Plan{}, nil }

func Additive(fresh Plan, owned ...Plan) (Plan, error) { return Lineage(owned...) }
`)},
		"internal/reconciliation/lifecycle/lineage.go": {Data: []byte(`package lifecycle

import "example.com/fixture/internal/reconciliation"

type lineageMember struct{ plan reconciliation.Plan }

type lineage []lineageMember

func (l lineage) held() []string { return l[0].plan.Blocks }
`)},
		"internal/reconciliation/lifecycle/readers.go": {Data: []byte(`package lifecycle

import (
	"slices"

	folding "example.com/fixture/internal/reconciliation"
)

type other struct{ plan folding.Plan }

func ranged(chain lineage) {
	for _, member := range chain {
		_ = member.plan
	}
}

func indexed(chain lineage) {
	for index := range chain {
		_ = chain[index].plan.Blocks
	}
}

func found(chain lineage) int {
	return slices.IndexFunc(chain, func(member lineageMember) bool { return len(member.plan.Blocks) != 0 })
}

func unrelated(value other) folding.Plan { return value.plan }

func merged(chain lineage) (folding.Plan, error) { return folding.Lineage(chain.plans()...) }

func (l lineage) plans() []folding.Plan { return nil }

func backward(chain lineage) {
	for _, member := range slices.Backward(chain) {
		_ = member.plan
	}
}
`)},
		"internal/reconciliation/lifecycle/readers_test.go": {Data: []byte(`package lifecycle

func tested(chain lineage) { _ = chain[0].plan }
`)},
		"internal/reconciliation/operationstore/store.go": {Data: []byte(`package operationstore

import "example.com/fixture/internal/reconciliation"

func admitted(plans ...reconciliation.Plan) (reconciliation.Plan, error) {
	return reconciliation.Additive(plans[0], plans...)
}
`)},
	}
	want := []string{
		"internal/reconciliation/lifecycle/readers.go:13: selects a lineage member's plan",
		"internal/reconciliation/lifecycle/readers.go:19: selects a lineage member's plan",
		"internal/reconciliation/lifecycle/readers.go:24: selects a lineage member's plan",
		"internal/reconciliation/lifecycle/readers.go:29: calls reconciliation.Lineage",
		"internal/reconciliation/lifecycle/readers.go:35: selects a lineage member's plan",
		"internal/reconciliation/operationstore/store.go:6: calls reconciliation.Additive",
	}
	var got []string
	for _, found := range ownershipFoldViolations(t, module) {
		got = append(got, strings.SplitN(found, ";", 2)[0])
	}
	if !slices.Equal(got, want) {
		t.Fatalf("the guard found\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func ownershipFoldViolations(t *testing.T, module fs.FS) []string {
	t.Helper()
	modulePath := moduleDeclaration(t, module)
	platform := build.Default
	platform.GOOS, platform.GOARCH, platform.CgoEnabled = "linux", "amd64", false
	loader := newRequestLoader(t, module, modulePath, productionGoFiles(t, module), platform)
	loader.standard = importer.ForCompiler(loader.fset, "source", nil)
	var found []string
	if _, present := loader.sources[lifecyclePackage]; present {
		checked := loader.check(lifecyclePackage)
		for expression, selection := range checked.info.Selections {
			position := loader.fset.Position(expression.Sel.Pos())
			if path.Base(position.Filename) == foldAdapter || !selectsMemberPlan(selection) {
				continue
			}
			found = append(found, position.Filename+":"+strconv.Itoa(position.Line)+": selects a lineage member's plan; ask the lineage's ownership fold instead")
		}
	}
	for dir, files := range loader.sources {
		if dir == foldPackage {
			continue
		}
		for _, file := range files {
			name := loader.fset.Position(file.Pos()).Filename
			if slices.Contains(lineageFoldAllowed, name) {
				continue
			}
			for _, call := range planFolds(file, modulePath+"/"+foldPackage) {
				position := loader.fset.Position(call.Pos())
				found = append(found, name+":"+strconv.Itoa(position.Line)+": calls reconciliation."+call.Sel.Name+"; fold the lineage's records with LineageOf or AdditiveOver instead")
			}
		}
	}
	slices.Sort(found)
	return found
}

func selectsMemberPlan(selection *types.Selection) bool {
	if selection.Kind() != types.FieldVal || selection.Obj().Name() != "plan" {
		return false
	}
	receiver := selection.Recv()
	if pointer, ok := receiver.Underlying().(*types.Pointer); ok {
		receiver = pointer.Elem()
	}
	named, ok := types.Unalias(receiver).(*types.Named)
	return ok && named.Obj().Name() == "lineageMember"
}

func planFolds(file *ast.File, foldImport string) []*ast.SelectorExpr {
	var names []string
	for _, spec := range file.Imports {
		imported, _ := strconv.Unquote(spec.Path.Value)
		if imported != foldImport {
			continue
		}
		name := path.Base(imported)
		if spec.Name != nil {
			name = spec.Name.Name
		}
		names = append(names, name)
	}
	var calls []*ast.SelectorExpr
	ast.Inspect(file, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "Lineage" && selector.Sel.Name != "Additive" {
			return true
		}
		if qualifier, ok := selector.X.(*ast.Ident); ok && slices.Contains(names, qualifier.Name) {
			calls = append(calls, selector)
		}
		return true
	})
	return calls
}
