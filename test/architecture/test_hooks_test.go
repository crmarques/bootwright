package architecture_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
)

const (
	adapterRunnerPackage = "internal/reconciliation/ansiblerunner"
	adapterRunnerImport  = "github.com/crmarques/bootwright/" + adapterRunnerPackage
	adapterRunnerHook    = "Isolated"
)

// testHookViolations names each production source that references the
// adapter runner's test constructor, which exists for tests only: a qualified
// use from another package, or a use inside the package other than its
// declaration.
func testHookViolations(sources []sourceFile) []string {
	var violations []string
	for _, source := range sources {
		aliases := map[string]bool{}
		for _, imported := range source.imports {
			if imported.path == adapterRunnerImport {
				aliases[imported.alias] = true
			}
		}
		if aliases["."] {
			violations = append(violations, fmt.Sprintf("%s dot-imports %s, which hides a use of %s", source.path, adapterRunnerPackage, adapterRunnerHook))
		}
		declaration := map[*ast.Ident]bool{}
		ast.Inspect(source.syntax, func(node ast.Node) bool {
			switch node := node.(type) {
			case *ast.FuncDecl:
				declaration[node.Name] = true
			case *ast.SelectorExpr:
				if qualifier, ok := node.X.(*ast.Ident); ok && aliases[qualifier.Name] && node.Sel.Name == adapterRunnerHook {
					violations = append(violations, fmt.Sprintf("%s references %s.%s", source.path, qualifier.Name, adapterRunnerHook))
				}
			case *ast.Ident:
				if source.owner == adapterRunnerPackage && node.Name == adapterRunnerHook && !declaration[node] {
					violations = append(violations, fmt.Sprintf("%s references %s", source.path, adapterRunnerHook))
				}
			}
			return true
		})
	}
	return violations
}

func TestProductionNeverReferencesTheAdapterRunnerTestHook(t *testing.T) {
	sources := productionSources(t)
	if !slices.ContainsFunc(sources, func(source sourceFile) bool { return source.owner == adapterRunnerPackage }) {
		t.Fatalf("%s is not found among the production sources", adapterRunnerPackage)
	}
	for _, violation := range testHookViolations(sources) {
		t.Error(violation)
	}
}

func TestTestHookViolationsFindEveryReference(t *testing.T) {
	parse := func(owner, content string) sourceFile {
		syntax, err := parser.ParseFile(token.NewFileSet(), "fixture.go", content, 0)
		if err != nil {
			t.Fatal(err)
		}
		source := sourceFile{path: owner + "/fixture.go", owner: owner, syntax: syntax}
		for _, imp := range syntax.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			alias := filepath.Base(path)
			if imp.Name != nil {
				alias = imp.Name.Name
			}
			source.imports = append(source.imports, packageImport{alias: alias, path: path})
		}
		return source
	}
	const qualified = "package fixture\nimport runner \"" + adapterRunnerImport + "\"\nvar _ = runner.Isolated\n"
	const other = "package fixture\nimport \"" + adapterRunnerImport + "\"\nvar _ = ansiblerunner.New\n"
	const inside = "package ansiblerunner\nfunc Isolated() {}\nfunc use() { Isolated() }\n"
	const dotted = "package fixture\nimport . \"" + adapterRunnerImport + "\"\nvar _ = Isolated\n"
	got := testHookViolations([]sourceFile{
		parse("cmd/fixture", qualified), parse("cmd/clean", other), parse(adapterRunnerPackage, inside), parse("cmd/dotted", dotted),
	})
	want := []string{
		"cmd/fixture/fixture.go references runner.Isolated",
		adapterRunnerPackage + "/fixture.go references Isolated",
		"cmd/dotted/fixture.go dot-imports " + adapterRunnerPackage + ", which hides a use of Isolated",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("violations = %q, want %q", got, want)
	}
}
