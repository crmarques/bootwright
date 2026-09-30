package architecture_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
)

// workingTreeListings name the calls that list a directory, on any receiver: a
// package function such as os.ReadDir or filepath.WalkDir, or a method such as
// (*os.File).ReadDir. A check that reaches one sees the untracked and ignored
// files of the working tree that a CI checkout lacks.
var workingTreeListings = map[string]bool{
	"Glob": true, "ReadDir": true, "Readdir": true, "Readdirnames": true, "Walk": true, "WalkDir": true,
}

// walkingDocsChecks is where specs/architecture.md names the docs checks that
// walk the working tree; the backticked tests between it and walkingDocsEnd.
const walkingDocsChecks, walkingDocsEnd = "The docs checks that list production Go source, ", " walk the working tree"

// docsCheckRun returns the -run pattern of the Makefile's docs-check target
// and the directories of the packages it tests.
func docsCheckRun(t *testing.T) (*regexp.Regexp, []string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	recipe := regexp.MustCompile(`(?m)^docs-check:\n\t\$\(GO\) test -count=1 -run '([^']+)'((?: \./\S+)+)$`).FindSubmatch(data)
	if recipe == nil {
		t.Fatal("the Makefile docs-check target no longer runs one go test -run pattern over named packages")
	}
	pattern, err := regexp.Compile(string(recipe[1]))
	if err != nil {
		t.Fatal(err)
	}
	var directories []string
	for _, name := range strings.Fields(string(recipe[2])) {
		directories = append(directories, strings.TrimPrefix(name, "./"))
	}
	return pattern, directories
}

// directoryGoFiles parses each named Go file, sources and tests alike, that
// sits directly in one of the directories, by directory and then file name.
func directoryGoFiles(t *testing.T, module fs.FS, names, directories []string) map[string]map[string]*ast.File {
	t.Helper()
	packages := map[string]map[string]*ast.File{}
	for _, name := range names {
		if !strings.HasSuffix(name, ".go") || !slices.Contains(directories, path.Dir(name)) {
			continue
		}
		data, err := fs.ReadFile(module, name)
		if err != nil {
			t.Fatal(err)
		}
		syntax, err := parser.ParseFile(token.NewFileSet(), name, data, 0)
		if err != nil {
			t.Fatal(err)
		}
		if packages[path.Dir(name)] == nil {
			packages[path.Dir(name)] = map[string]*ast.File{}
		}
		packages[path.Dir(name)][path.Base(name)] = syntax
	}
	return packages
}

// packageReferences maps what a name can run in one directory's packages: the
// bodies of its functions and the values given to its package variables, by
// name, and the bodies of its methods, by method name. qualifiers are the names
// under which its external tests import the package they test.
type packageReferences struct {
	named, methods map[string][]ast.Node
	qualifiers     map[string]bool
}

func directoryReferences(files map[string]*ast.File, tested string) packageReferences {
	references := packageReferences{named: map[string][]ast.Node{}, methods: map[string][]ast.Node{}, qualifiers: map[string]bool{}}
	variables := map[string]bool{}
	for _, file := range files {
		for _, imported := range file.Imports {
			if importPath, _ := strconv.Unquote(imported.Path.Value); importPath == tested && imported.Name != nil {
				references.qualifiers[imported.Name.Name] = true
			}
		}
		for _, declaration := range file.Decls {
			switch declaration := declaration.(type) {
			case *ast.FuncDecl:
				if declaration.Body == nil || declaration.Name.Name == "_" {
					continue
				}
				if declaration.Recv != nil {
					references.methods[declaration.Name.Name] = append(references.methods[declaration.Name.Name], declaration.Body)
				} else {
					references.named[declaration.Name.Name] = append(references.named[declaration.Name.Name], declaration.Body)
				}
			case *ast.GenDecl:
				for _, spec := range declaration.Specs {
					if value, ok := spec.(*ast.ValueSpec); ok && declaration.Tok == token.VAR {
						for _, variable := range value.Names {
							if variable.Name == "_" {
								continue
							}
							variables[variable.Name] = true
							references.named[variable.Name] = append(references.named[variable.Name], valueNodes(value.Values)...)
						}
					}
				}
			}
		}
	}
	for name, file := range files {
		if !strings.HasSuffix(name, "_test.go") {
			references.qualifiers[file.Name.Name] = true
		}
		ast.Inspect(file, func(node ast.Node) bool {
			if assignment, ok := node.(*ast.AssignStmt); ok && assignment.Tok == token.ASSIGN {
				for _, target := range assignment.Lhs {
					if variable, ok := target.(*ast.Ident); ok && variables[variable.Name] {
						references.named[variable.Name] = append(references.named[variable.Name], valueNodes(assignment.Rhs)...)
					}
				}
			}
			return true
		})
	}
	return references
}

func valueNodes(values []ast.Expr) []ast.Node {
	nodes := make([]ast.Node, 0, len(values))
	for _, value := range values {
		nodes = append(nodes, value)
	}
	return nodes
}

// workingTreeWalkers returns the tests of one directory's packages that the
// pattern selects and that list a directory of the working tree, in their own
// body or, at any depth, through a function, method or package variable those
// packages declare, in their sources or their tests. tested is the import path
// of the directory's package.
func workingTreeWalkers(files map[string]*ast.File, tested string, pattern *regexp.Regexp) []string {
	references := directoryReferences(files, tested)
	var walks func(node ast.Node, seen map[ast.Node]bool) bool
	follow := func(nodes []ast.Node, seen map[ast.Node]bool) bool {
		for _, node := range nodes {
			if !seen[node] {
				seen[node] = true
				if walks(node, seen) {
					return true
				}
			}
		}
		return false
	}
	walks = func(node ast.Node, seen map[ast.Node]bool) bool {
		found := false
		ast.Inspect(node, func(node ast.Node) bool {
			switch node := node.(type) {
			case *ast.SelectorExpr:
				qualifier, qualified := node.X.(*ast.Ident)
				found = found || workingTreeListings[node.Sel.Name] || walks(node.X, seen) || follow(references.methods[node.Sel.Name], seen) ||
					qualified && references.qualifiers[qualifier.Name] && follow(references.named[node.Sel.Name], seen)
				return false
			case *ast.Ident:
				found = found || follow(references.named[node.Name], seen)
			}
			return !found
		})
		return found
	}
	walking := map[string]bool{}
	for _, name := range sortedKeys(files) {
		if !strings.HasSuffix(name, "_test.go") {
			continue
		}
		for _, declaration := range files[name].Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if ok && function.Recv == nil && function.Body != nil && strings.HasPrefix(function.Name.Name, "Test") &&
				pattern.MatchString(function.Name.Name) && walks(function.Body, map[ast.Node]bool{function.Body: true}) {
				walking[function.Name.Name] = true
			}
		}
	}
	return sortedKeys(walking)
}

func TestWorkingTreeWalkersFollowWhatEachTestNames(t *testing.T) {
	module := fstest.MapFS{
		"fixture/fixture_test.go": {Data: []byte(`package fixture
func TestDocsWalks(t *testing.T) { sources(t) }
func TestDocsVisits(t *testing.T) { visit(entries) }
func TestDocsReadsDirectly(t *testing.T) { _, _ = os.ReadDir(".") }
func TestDocsListsAnOpenDirectory(t *testing.T) { directory, _ := os.Open("."); _, _ = directory.Readdirnames(-1) }
func TestDocsCallsAMethod(t *testing.T) { probeLister{}.list(".") }
func TestDocsCallsAVariable(t *testing.T) { listProbe(".") }
func TestDocsCallsAnAssignedVariable(t *testing.T) { assigned(".") }
func TestDocsCallsItsSources(t *testing.T) { Sources() }
func TestDocsLoops(t *testing.T) { loop(); tracked() }
func TestDocsReadsTracked(t *testing.T) { tracked(); _ = other.sources; _ = other.unreached }
func TestDocsDiscards(t *testing.T) { _ = t }
func TestOtherWalks(t *testing.T) { sources(t) }
var _ = probeLister{}
type probeLister struct{}
func (probeLister) list(root string) { _, _ = os.ReadDir(root) }
var listProbe = func(root string) { _, _ = os.ReadDir(root) }
var assigned func(string)
func init() { assigned = func(root string) { _, _ = filepath.Glob(root) } }
func sources(t *testing.T) { _ = filepath.WalkDir(".", nil) }
func visit(list func() []string) {}
func entries() []string { matches, _ := filepath.Glob("*"); return matches }
func loop() { loop() }
func tracked() { _ = exec.Command("git", "ls-files").Run(); _ = filepath.Join("a", "b") }
`)},
		"fixture/fixture.go": {Data: []byte(`package fixture
func Sources() { _ = filepath.Walk(".", nil) }
func unreached() { _, _ = os.ReadDir(".") }
func TestDocsInSources(t *testing.T) { _, _ = os.ReadDir(".") }
`)},
		"fixture/external_test.go": {Data: []byte(`package fixture_test
import "example.com/fixture"
func TestDocsCallsTheTestedPackage(t *testing.T) { fixture.Sources() }
`)},
		"fixture/aliased_test.go": {Data: []byte(`package fixture_test
import tested "example.com/fixture"
func TestDocsCallsTheAliasedPackage(t *testing.T) { tested.Sources() }
`)},
		"fixture/nested/nested_test.go": {Data: []byte(`package nested
func TestDocsInANestedDirectory(t *testing.T) { _, _ = os.ReadDir(".") }
`)},
	}
	packages := directoryGoFiles(t, module, sortedKeys(module), []string{"fixture"})
	if found := sortedKeys(packages); !slices.Equal(found, []string{"fixture"}) {
		t.Fatalf("parsed directories = %v, want only fixture", found)
	}
	found := workingTreeWalkers(packages["fixture"], "example.com/fixture", regexp.MustCompile("^TestDocs"))
	want := []string{
		"TestDocsCallsAMethod", "TestDocsCallsAVariable", "TestDocsCallsAnAssignedVariable", "TestDocsCallsItsSources",
		"TestDocsCallsTheAliasedPackage", "TestDocsCallsTheTestedPackage", "TestDocsListsAnOpenDirectory",
		"TestDocsReadsDirectly", "TestDocsVisits", "TestDocsWalks",
	}
	if !slices.Equal(found, want) {
		t.Fatalf("walkers = %v, want %v", found, want)
	}
}

// A docs check that walks the working tree reads untracked and ignored files,
// so a local docs-check can fail where CI passes. specs/architecture.md names
// each such check, and names no other, beside the rule that every other docs
// check lists only the files Git tracks.
func TestDocsNameEveryCheckThatWalksTheWorkingTree(t *testing.T) {
	root := filepath.Join("..", "..")
	pattern, directories := docsCheckRun(t)
	packages := directoryGoFiles(t, os.DirFS(root), repositoryFiles(t), directories)
	walking := map[string]bool{}
	for _, directory := range sortedKeys(packages) {
		for _, name := range workingTreeWalkers(packages[directory], modulePath+directory, pattern) {
			walking[name] = true
		}
	}
	data, err := os.ReadFile(filepath.Join(root, "specs", "architecture.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(strings.Fields(string(data)), " ")
	_, clause, found := strings.Cut(text, walkingDocsChecks)
	clause, _, ended := strings.Cut(clause, walkingDocsEnd)
	if !found || !ended || strings.Count(text, walkingDocsChecks) != 1 {
		t.Fatalf("specs/architecture.md states %q once, followed by the checks it names and %q", walkingDocsChecks, walkingDocsEnd)
	}
	named := map[string]bool{}
	for _, match := range citedGoTest.FindAllStringSubmatch(clause, -1) {
		named[match[1]] = true
	}
	for _, name := range sortedKeys(walking) {
		if !named[name] {
			t.Errorf("the docs check %s walks the working tree, which specs/architecture.md does not name among the docs checks that list production Go source", name)
		}
	}
	for _, name := range sortedKeys(named) {
		if !walking[name] {
			t.Errorf("specs/architecture.md names %s among the docs checks that walk the working tree, which is no docs check that does; remove it", name)
		}
	}
	if len(walking) == 0 {
		t.Fatalf("found no docs check that walks the working tree in %v; the listing or the Makefile recipe changed", directories)
	}
}
