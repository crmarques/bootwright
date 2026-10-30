package cli

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// cliResults is every type a command may encode as its JSON result.
var cliResults = []reflect.Type{
	reflect.TypeFor[addOnInfoDocument](),
	reflect.TypeFor[addOnsPresentation](),
	reflect.TypeFor[controllerPresentation](),
	reflect.TypeFor[rebindPresentation](),
	reflect.TypeFor[uninstallPresentation](),
	reflect.TypeFor[contextPresentation](),
	reflect.TypeFor[contextListPresentation](),
	reflect.TypeFor[contextAdmissionPresentation](),
	reflect.TypeFor[contextDeletionPresentation](),
	reflect.TypeFor[secretMutationPresentation](),
	reflect.TypeFor[mediaMutationPresentation](),
	reflect.TypeFor[encryptionMutationPresentation](),
	reflect.TypeFor[setupDownloadPresentation](),
	reflect.TypeFor[exportResult](),
	reflect.TypeFor[helpPresentation](),
	reflect.TypeFor[versionPresentation](),
	reflect.TypeFor[schemaPresentation](),
	reflect.TypeFor[planPresentation](),
	reflect.TypeFor[operationPresentation](),

	reflect.TypeFor[validationResult](),
	reflect.TypeFor[storagePreflightResult](),
	reflect.TypeFor[effectiveResult](),
	reflect.TypeFor[renderPresentation](),
	reflect.TypeFor[statusResult](),
	reflect.TypeFor[diffResult](),
	reflect.TypeFor[secretCheckResult](),
	reflect.TypeFor[secretListResult](),
	reflect.TypeFor[encryptionStatusResult](),
	reflect.TypeFor[trustResult](),
	reflect.TypeFor[setupPlanResult](),
	reflect.TypeFor[machineListPresentation](),
	reflect.TypeFor[machinePowerPresentation](),
	reflect.TypeFor[mediaListPresentation](),
	reflect.TypeFor[clusterListPresentation](),
	reflect.TypeFor[clusterInfoPresentation](),
	reflect.TypeFor[machineInfoPresentation](),
	reflect.TypeFor[secretInfoResult](),
}

// sharedResultLeaves are the only types from outside this package a result may
// hold. The raw effective state is the encoder's own canonical bytes, and the
// diagnostic object is the one the output contract defines for envelope
// diagnostics and validate advisories alike.
var sharedResultLeaves = []reflect.Type{
	reflect.TypeFor[json.RawMessage](),
	reflect.TypeFor[diagnostics.Diagnostic](),
	reflect.TypeFor[diagnostics.SourceLocation](),
	reflect.TypeFor[diagnostics.ObjectIdentity](),
}

// A JSON result is built only from types this package declares and tags in
// the documented shape, so a domain type's field names, omissions and nested
// shape can never become the public contract by being encoded directly. The
// envelope admits only the result marker's implementers, and each one is
// walked to its leaves.
func TestEveryJSONResultIsCLIOwned(t *testing.T) {
	want := make([]string, 0, len(cliResults))
	for _, result := range cliResults {
		want = append(want, result.Name())
	}
	slices.Sort(want)
	if got := markedResultTypes(t); !slices.Equal(got, want) {
		t.Fatalf("result marker receivers = %v, want the table %v", got, want)
	}
	own := reflect.TypeFor[commandEnvelope]().PkgPath()
	for _, result := range cliResults {
		checkResultType(t, result.Name(), result, own, map[reflect.Type]bool{})
	}
}

// markedResultTypes names each receiver of the result marker method declared
// in this package's production sources.
func markedResultTypes(t *testing.T) []string {
	t.Helper()
	sources, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	files := token.NewFileSet()
	var receivers []string
	for _, source := range sources {
		if strings.HasSuffix(source, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(files, source, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv == nil || function.Name.Name != "documentedResult" {
				continue
			}
			receiver := function.Recv.List[0].Type
			if pointer, ok := receiver.(*ast.StarExpr); ok {
				receiver = pointer.X
			}
			name, ok := receiver.(*ast.Ident)
			if !ok {
				t.Fatalf("%s: the result marker has an unnamed receiver", files.Position(function.Pos()))
			}
			receivers = append(receivers, name.Name)
		}
	}
	slices.Sort(receivers)
	return receivers
}

func checkResultType(t *testing.T, path string, typ reflect.Type, own string, seen map[reflect.Type]bool) {
	t.Helper()
	if slices.Contains(sharedResultLeaves, typ) {
		return
	}
	if typ.Name() != "" && typ.PkgPath() != "" && typ.PkgPath() != own {
		t.Errorf("%s is %s, a type declared outside internal/cli", path, typ)
		return
	}
	if seen[typ] {
		return
	}
	seen[typ] = true
	switch typ.Kind() {
	case reflect.Interface:
		t.Errorf("%s is the interface %s, whose encoded shape no result declares", path, typ)
	case reflect.Pointer, reflect.Slice, reflect.Array:
		checkResultType(t, path+"[]", typ.Elem(), own, seen)
	case reflect.Map:
		checkResultType(t, path+"{key}", typ.Key(), own, seen)
		checkResultType(t, path+"{}", typ.Elem(), own, seen)
	case reflect.Struct:
		for index := range typ.NumField() {
			field := typ.Field(index)
			if _, tagged := field.Tag.Lookup("json"); field.IsExported() && !tagged {
				t.Errorf("%s.%s has no json tag", path, field.Name)
			}
			checkResultType(t, path+"."+field.Name, field.Type, own, seen)
		}
	}
}

// A presentation group is never a JSON result member (specs/cli/output.md,
// Multi-machine presentation): a block that ran groups lists the documented
// block members only.
func TestAnOperationBlockCarriesNoGroupsMember(t *testing.T) {
	result := &lifecycle.OperationResult{Verb: "apply", Blocks: []lifecycle.BlockResult{{ID: "media", Description: "serve media", Stage: "infrastructure-services", State: "done", Outcome: "changed", Attempts: 1,
		Groups: []lifecycle.GroupResult{{BlockID: "media", GroupID: "pull-image", Description: "acquire the pinned server image", Status: "done", Machines: 1}}}}}
	data, err := json.Marshal(displayOperation(result).Blocks)
	if err != nil {
		t.Fatal(err)
	}
	var blocks []map[string]json.RawMessage
	if err := json.Unmarshal(data, &blocks); err != nil || len(blocks) != 1 {
		t.Fatalf("blocks = %s (%v)", data, err)
	}
	members := slices.Sorted(maps.Keys(blocks[0]))
	if want := []string{"attempts", "description", "id", "outcome", "stage", "state"}; !slices.Equal(members, want) {
		t.Fatalf("an operation block carries %v, want %v", members, want)
	}
}
