package architecture_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// docsChecksList is the list make docs-check selects its tests from: one
// package directory and test name per line.
const docsChecksList = "testdata/docs-checks.txt"

// docsCheckPrefixes and docsCheckSuffixes name the tests that must be listed
// wherever they live: a new docs gate is named TestDocs and joins the list, and
// the gates the Makefile's old pattern selected keep their names, so none falls
// out of the selection by a name that pattern did not match. A name matches when
// it begins with a prefix, or begins with Test and ends with a suffix.
var (
	docsCheckPrefixes = []string{
		"TestDocs", "TestDiagnosticCodesMatchOutputSpec", "TestDocumentedBoundsMatchCode", "TestCommandCatalogMatchesSpec",
		"TestTransitionTablesMatchSpec", "TestUnclaimedObjectTableMatchesTheEngine", "TestCephMatrix", "TestCephProfile",
	}
	docsCheckSuffixes = []string{"RefusalTableMatchesUnsupported", "ObservableFieldTableMatchesProjector"}
)

// namedForADocsCheck says whether the convention requires the list to hold name.
func namedForADocsCheck(name string) bool {
	for _, prefix := range docsCheckPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	for _, suffix := range docsCheckSuffixes {
		if strings.HasPrefix(name, "Test") && strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

// docsCheckList reads the list by package directory, refusing a line that is
// not a directory and a test name, and a test listed twice.
func docsCheckList(t *testing.T) map[string][]string {
	t.Helper()
	data, err := os.ReadFile(docsChecksList)
	if err != nil {
		t.Fatal(err)
	}
	list, err := parseDocsCheckList(string(data))
	if err != nil {
		t.Fatalf("%s: %v", docsChecksList, err)
	}
	return list
}

func parseDocsCheckList(text string) (map[string][]string, error) {
	list := map[string][]string{}
	for number, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || !strings.HasPrefix(fields[1], "Test") || path.Clean(fields[0]) != fields[0] {
			return nil, fmt.Errorf("line %d is not a package directory and a test name", number+1)
		}
		if slices.Contains(list[fields[0]], fields[1]) {
			return nil, fmt.Errorf("line %d lists %s %s twice", number+1, fields[0], fields[1])
		}
		list[fields[0]] = append(list[fields[0]], fields[1])
	}
	return list, nil
}

// declaredTests names the top-level functions of a file that take one *testing.T
// and are named Test.
func declaredTests(syntax *ast.File) []string {
	var found []string
	for _, declaration := range syntax.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv != nil || !strings.HasPrefix(function.Name.Name, "Test") || function.Type.Params.NumFields() != 1 {
			continue
		}
		if star, ok := function.Type.Params.List[0].Type.(*ast.StarExpr); ok {
			if selector, ok := star.X.(*ast.SelectorExpr); ok && selector.Sel.Name == "T" {
				found = append(found, function.Name.Name)
			}
		}
	}
	return found
}

// testFunctions names the Test functions each package directory declares in
// its tracked _test.go files.
func testFunctions(t *testing.T, root string, files []string) map[string][]string {
	t.Helper()
	found := map[string][]string{}
	for _, name := range files {
		if !strings.HasSuffix(name, "_test.go") {
			continue
		}
		syntax, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, filepath.FromSlash(name)), nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		found[path.Dir(name)] = append(found[path.Dir(name)], declaredTests(syntax)...)
	}
	return found
}

// docsCheckViolations compares the list with the tests that exist: a listed
// test that is not declared, and a test the convention names that the list leaves out.
func docsCheckViolations(list, declared map[string][]string) []string {
	var violations []string
	for _, directory := range sortedKeys(list) {
		for _, name := range list[directory] {
			if !slices.Contains(declared[directory], name) {
				violations = append(violations, fmt.Sprintf("%s lists %s %s, which no tracked test file declares; remove the line", docsChecksList, directory, name))
			}
		}
	}
	for _, directory := range sortedKeys(declared) {
		for _, name := range declared[directory] {
			if namedForADocsCheck(name) && !slices.Contains(list[directory], name) {
				violations = append(violations, fmt.Sprintf("%s %s is named for a docs check but %s does not list it; add the line", directory, name, docsChecksList))
			}
		}
	}
	return violations
}

// TestDocsChecksListIsComplete holds the list make docs-check selects from to
// the tests that exist, so a renamed or removed gate fails here instead of
// quietly leaving the selection.
func TestDocsChecksListIsComplete(t *testing.T) {
	for _, violation := range docsCheckViolations(docsCheckList(t), testFunctions(t, filepath.Join("..", ".."), repositoryFiles(t))) {
		t.Error(violation)
	}
}

func TestDocsChecksListRefusesAMissingAndAnUnlistedTest(t *testing.T) {
	declared := map[string][]string{
		"a": {"TestDocsKept", "TestDocsNew", "TestOther", "TestXRefusalTableMatchesUnsupported", "TestYObservableFieldTableMatchesProjector",
			"TestCephMatrixNew", "TestCephProfileNew", "TestDiagnosticCodesMatchOutputSpec", "TestDocumentedBoundsMatchCode",
			"TestCommandCatalogMatchesSpec", "TestTransitionTablesMatchSpec", "TestUnclaimedObjectTableMatchesTheEngine"},
		"b": {"TestDocsElsewhere"},
	}
	list, err := parseDocsCheckList("a TestDocsKept\na TestGone\nb TestDocsElsewhere\n")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"testdata/docs-checks.txt lists a TestGone, which no tracked test file declares; remove the line",
	}
	for _, name := range []string{
		"TestDocsNew", "TestXRefusalTableMatchesUnsupported", "TestYObservableFieldTableMatchesProjector", "TestCephMatrixNew", "TestCephProfileNew",
		"TestDiagnosticCodesMatchOutputSpec", "TestDocumentedBoundsMatchCode", "TestCommandCatalogMatchesSpec", "TestTransitionTablesMatchSpec",
		"TestUnclaimedObjectTableMatchesTheEngine",
	} {
		want = append(want, "a "+name+" is named for a docs check but testdata/docs-checks.txt does not list it; add the line")
	}
	if got := docsCheckViolations(list, declared); !slices.Equal(got, want) {
		t.Fatalf("violations = %q, want %q", got, want)
	}
	for _, bad := range []string{"a\n", "a TestX extra\n", "./a TestX\n", "a notatest\n", "a TestX\na TestX\n"} {
		if _, err := parseDocsCheckList(bad); err == nil {
			t.Errorf("parseDocsCheckList accepted %q", bad)
		}
	}
}

func TestDocsCheckConventionIsNotBroaderThanTheNames(t *testing.T) {
	for _, name := range []string{"TestOther", "TestRefusalTable", "RefusalTableMatchesUnsupported", "TestCeph", "TestDiagnostic"} {
		if namedForADocsCheck(name) {
			t.Errorf("%s is not named for a docs check", name)
		}
	}
}

// TestDocsCheckRecipeReadsTheList holds the Makefile to the list: the recipe
// selects its tests and packages from the file and names none itself.
func TestDocsCheckRecipeReadsTheList(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	if problem := docsCheckRecipeProblem(string(data)); problem != "" {
		t.Error(problem)
	}
}

func TestDocsCheckRecipeRefusesAHardCodedSelection(t *testing.T) {
	const good = "DOCS_CHECKS := test/architecture/testdata/docs-checks.txt\ndocs-check:\n\t$(GO) test -run '^($(shell awk '{print $$2}' $(DOCS_CHECKS)))$$' ./a\n\nfocus:\n\ttrue\n"
	if problem := docsCheckRecipeProblem(good); problem != "" {
		t.Fatalf("a recipe reading the list was refused: %s", problem)
	}
	for _, bad := range []string{
		"DOCS_CHECKS := x\ndocs-check:\n\t$(GO) test -run '^(TestDocs)' ./a\n",
		"DOCS_CHECKS := x\ndocs-check:\n\t$(GO) test -run '^($(shell awk '{print $$2}' $(DOCS_CHECKS)))$$|TestCephProfile' ./a\n",
		"DOCS_CHECKS := x\nfocus:\n\ttrue\n",
	} {
		if docsCheckRecipeProblem(bad) == "" {
			t.Errorf("a recipe was accepted: %q", bad)
		}
	}
}

// docsCheckRecipeProblem reports why the docs-check recipe does not read the
// list, or "" when it does: it must expand the list variable and name no test.
func docsCheckRecipeProblem(makefile string) string {
	var recipe []string
	inRecipe := false
	for _, line := range strings.Split(makefile, "\n") {
		switch {
		case strings.HasPrefix(line, "docs-check:"):
			inRecipe = true
		case inRecipe && strings.HasPrefix(line, "\t"):
			recipe = append(recipe, line)
		case inRecipe:
			inRecipe = false
		}
	}
	if len(recipe) == 0 {
		return "Makefile has no docs-check recipe"
	}
	text := strings.Join(recipe, "\n")
	if !strings.Contains(text, "$(DOCS_CHECKS)") || !strings.Contains(makefile, "DOCS_CHECKS := test/architecture/"+docsChecksList) {
		return "the Makefile docs-check recipe must read the list test/architecture/" + docsChecksList + " through $(DOCS_CHECKS)"
	}
	if strings.Contains(text, "Test") {
		return "the Makefile docs-check recipe names a test; the list owns the selection"
	}
	return ""
}
