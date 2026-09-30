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

// workingTreeListings name, by import path, the standard library functions
// that list a directory; the io/fs ones list the filesystem they are given.
// workingTreeMethods name the methods that list one, such as
// (*os.File).ReadDir, whose receiver a syntax walk cannot type. A check that
// reaches one sees the untracked and ignored files of the working tree that a
// CI checkout lacks.
var (
	workingTreeListings = map[string][]string{
		"io/fs": {"Glob", "ReadDir", "WalkDir"}, "io/ioutil": {"ReadDir"}, "os": {"ReadDir"},
		"path/filepath": {"Glob", "Walk", "WalkDir"},
	}
	workingTreeMethods = []string{"Glob", "ReadDir", "Readdir", "Readdirnames"}
)

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
// bodies of its functions and the values given to its package variables,
// directly or through an element, field or pointer, by name, and the bodies of
// its methods, by method name. qualifiers are the names under which its
// external tests import the package they test, all gathered before any
// assignment through one is read. listings are the references that list a
// directory.
type packageReferences struct {
	named, methods map[string][]ast.Node
	qualifiers     map[string]bool
	listings       map[ast.Node]bool
}

func directoryReferences(files map[string]*ast.File, tested string) packageReferences {
	references := packageReferences{
		named: map[string][]ast.Node{}, methods: map[string][]ast.Node{}, qualifiers: map[string]bool{}, listings: map[ast.Node]bool{},
	}
	variables, memory := map[string]bool{}, memoryVariables(files)
	for name, file := range files {
		if !strings.HasSuffix(name, "_test.go") {
			references.qualifiers[file.Name.Name] = true
		}
		for _, imported := range file.Imports {
			if importPath, _ := strconv.Unquote(imported.Path.Value); importPath == tested && imported.Name != nil {
				references.qualifiers[imported.Name.Name] = true
			}
		}
		fileListings(file, memory[file.Name.Name], references.listings)
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
	for _, file := range files {
		ast.Inspect(file, func(node ast.Node) bool {
			if assignment, ok := node.(*ast.AssignStmt); ok && assignment.Tok == token.ASSIGN {
				for _, target := range assignment.Lhs {
					for _, variable := range assignedVariables(target, references.qualifiers) {
						if variables[variable] {
							references.named[variable] = append(references.named[variable], valueNodes(assignment.Rhs)...)
						}
					}
				}
			}
			return true
		})
	}
	return references
}

// assignedVariables returns the names of the package variables an assignment
// target may write: the variable it names, the one whose element or field it
// writes or through which it writes a pointed value, and the one it names
// through the tested package's qualifier.
func assignedVariables(target ast.Expr, qualifiers map[string]bool) []string {
	var names []string
	for {
		switch expression := ast.Unparen(target).(type) {
		case *ast.Ident:
			return append(names, expression.Name)
		case *ast.IndexExpr:
			target = expression.X
		case *ast.StarExpr:
			target = expression.X
		case *ast.SelectorExpr:
			if qualifier, ok := expression.X.(*ast.Ident); ok && qualifiers[qualifier.Name] {
				names = append(names, expression.Sel.Name)
			}
			target = expression.X
		default:
			return names
		}
	}
}

// fileImports resolves a name of one file to the packages it may be a member
// of: through an import's qualifier, or through a dot import.
type fileImports struct {
	qualified map[string]string
	dotted    []string
}

func importsOf(file *ast.File) fileImports {
	imports := fileImports{qualified: map[string]string{}}
	for _, imported := range file.Imports {
		importPath, _ := strconv.Unquote(imported.Path.Value)
		switch {
		case imported.Name == nil:
			imports.qualified[path.Base(importPath)] = importPath
		case imported.Name.Name == ".":
			imports.dotted = append(imports.dotted, importPath)
		default:
			imports.qualified[imported.Name.Name] = importPath
		}
	}
	return imports
}

func (imports fileImports) member(expression ast.Expr) ([]string, string) {
	switch expression := ast.Unparen(expression).(type) {
	case *ast.Ident:
		if expression.Obj == nil {
			return imports.dotted, expression.Name
		}
	case *ast.SelectorExpr:
		if qualifier, ok := expression.X.(*ast.Ident); ok && qualifier.Obj == nil && imports.qualified[qualifier.Name] != "" {
			return []string{imports.qualified[qualifier.Name]}, expression.Sel.Name
		}
	}
	return nil, ""
}

func (imports fileImports) lists(expression ast.Expr) bool {
	packages, name := imports.member(expression)
	return slices.ContainsFunc(packages, func(importPath string) bool { return slices.Contains(workingTreeListings[importPath], name) })
}

func (imports fileImports) mapFS(expression ast.Expr) bool {
	packages, name := imports.member(expression)
	return name == "MapFS" && slices.Contains(packages, "testing/fstest")
}

// mapFSValue reports whether a value is an fstest.MapFS literal or conversion.
func (imports fileImports) mapFSValue(expression ast.Expr) bool {
	switch expression := ast.Unparen(expression).(type) {
	case *ast.CompositeLit:
		return imports.mapFS(expression.Type)
	case *ast.CallExpr:
		return len(expression.Args) == 1 && imports.mapFS(expression.Fun)
	}
	return false
}

// declaresMapFS reports whether a declaration of the named variable, a
// parameter, a var specification or a short variable declaration, gives it
// the type fstest.MapFS, so it holds an in-memory filesystem whatever is
// later assigned to it.
func (imports fileImports) declaresMapFS(declaration any, variable string) bool {
	switch declaration := declaration.(type) {
	case *ast.Field:
		return imports.mapFS(declaration.Type)
	case *ast.ValueSpec:
		if declaration.Type != nil {
			return imports.mapFS(declaration.Type)
		}
		index := slices.IndexFunc(declaration.Names, func(name *ast.Ident) bool { return name.Name == variable })
		return index >= 0 && len(declaration.Values) == len(declaration.Names) && imports.mapFSValue(declaration.Values[index])
	case *ast.AssignStmt:
		index := slices.IndexFunc(declaration.Lhs, func(target ast.Expr) bool {
			name, ok := target.(*ast.Ident)
			return ok && name.Name == variable
		})
		return declaration.Tok == token.DEFINE && index >= 0 && len(declaration.Lhs) == len(declaration.Rhs) && imports.mapFSValue(declaration.Rhs[index])
	}
	return false
}

// memoryVariables returns, by package name, the package variables whose
// declaration, in any file of the directory, gives them the type fstest.MapFS;
// every other file of the package reads one as an unresolved name.
func memoryVariables(files map[string]*ast.File) map[string]map[string]bool {
	memory := map[string]map[string]bool{}
	for _, file := range files {
		imports := importsOf(file)
		for _, declaration := range file.Decls {
			if declaration, ok := declaration.(*ast.GenDecl); ok && declaration.Tok == token.VAR {
				for _, spec := range declaration.Specs {
					for _, variable := range spec.(*ast.ValueSpec).Names {
						if imports.declaresMapFS(spec, variable.Name) {
							if memory[file.Name.Name] == nil {
								memory[file.Name.Name] = map[string]bool{}
							}
							memory[file.Name.Name][variable.Name] = true
						}
					}
				}
			}
		}
	}
	return memory
}

// fileListings adds to listings each reference in one file to a function or
// method that lists a directory: a function through its import's qualifier or
// a dot import, or a method on any receiver but an in-memory filesystem, a
// value the syntax declares an fstest.MapFS; memory names the package
// variables other files declare one. A call of an io/fs function on an
// in-memory filesystem lists nothing either. The identifier that keys an
// element of a composite literal, a field name or a map key, which a function
// cannot be, is no reference.
func fileListings(file *ast.File, memory map[string]bool, listings map[ast.Node]bool) {
	imports := importsOf(file)
	inMemory := func(expression ast.Expr) bool {
		variable, ok := ast.Unparen(expression).(*ast.Ident)
		switch {
		case !ok:
			return imports.mapFSValue(expression)
		case variable.Obj == nil:
			return memory[variable.Name]
		}
		return variable.Obj.Kind == ast.Var && imports.declaresMapFS(variable.Obj.Decl, variable.Name)
	}
	inMemoryCalls := map[ast.Node]bool{}
	var visit func(ast.Node) bool
	visit = func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.CallExpr:
			if len(node.Args) > 0 && inMemory(node.Args[0]) {
				inMemoryCalls[ast.Unparen(node.Fun)] = true
			}
		case *ast.KeyValueExpr:
			if _, ok := node.Key.(*ast.Ident); ok {
				ast.Inspect(node.Value, visit)
				return false
			}
		case *ast.SelectorExpr:
			if packages, _ := imports.member(node); packages != nil {
				if imports.lists(node) && !inMemoryCalls[node] {
					listings[node] = true
				}
				return false
			}
			if slices.Contains(workingTreeMethods, node.Sel.Name) && !inMemory(node.X) {
				listings[node] = true
			}
			ast.Inspect(node.X, visit)
			return false
		case *ast.Ident:
			if imports.lists(node) && !inMemoryCalls[node] {
				listings[node] = true
			}
		}
		return true
	}
	ast.Inspect(file, visit)
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
				found = found || references.listings[node] || walks(node.X, seen) || follow(references.methods[node.Sel.Name], seen) ||
					qualified && references.qualifiers[qualifier.Name] && follow(references.named[node.Sel.Name], seen)
				return false
			case *ast.Ident:
				found = found || references.listings[node] || follow(references.named[node.Name], seen)
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
import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"example.com/other"
)
func TestDocsWalks(t *testing.T) { sources(t) }
func TestDocsVisits(t *testing.T) { visit(entries) }
func TestDocsReadsDirectly(t *testing.T) { _, _ = os.ReadDir(".") }
func TestDocsListsAnOpenDirectory(t *testing.T) { directory, _ := os.Open("."); _, _ = directory.Readdirnames(-1) }
func TestDocsCallsAMethod(t *testing.T) { probeLister{}.list(".") }
func TestDocsCallsAPointedVariable(t *testing.T) { (*pointed)(".") }
func TestDocsCallsAVariable(t *testing.T) { listProbe(".") }
func TestDocsCallsAnAssignedVariable(t *testing.T) { assigned(".") }
func TestDocsCallsAnAssignedElement(t *testing.T) { listers["probe"](".") }
func TestDocsCallsAnAssignedField(t *testing.T) { hooks.probe(".") }
func TestDocsCallsAVariableItsExternalTestsAssign(t *testing.T) { Hook() }
func TestDocsCallsAnElementItsExternalTestsAssign(t *testing.T) { Listers["probe"]() }
func TestDocsCallsAFieldItsExternalTestsAssign(t *testing.T) { Hooks.Probe() }
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
var listers = map[string]func(string){}
var hooks struct{ probe func(string) }
var pointed = new(func(string))
func init() {
	assigned = func(root string) { _, _ = filepath.Glob(root) }
	listers["probe"] = func(root string) { _, _ = os.ReadDir(root) }
	hooks.probe = func(root string) { _ = filepath.WalkDir(root, nil) }
	*pointed = func(root string) { _, _ = os.ReadDir(root) }
}
func sources(t *testing.T) { _ = filepath.WalkDir(".", nil) }
func visit(list func() []string) {}
func entries() []string { matches, _ := filepath.Glob("*"); return matches }
func loop() { loop() }
func tracked() { _ = exec.Command("git", "ls-files").Run(); _ = filepath.Join("a", "b") }
`)},
		"fixture/fixture.go": {Data: []byte(`package fixture
import (
	"os"
	"path/filepath"
	"testing"
)
var Hook = func() {}
var Listers = map[string]func(){}
var Hooks struct{ Probe func() }
func Sources() { _ = filepath.Walk(".", nil) }
func unreached() { _, _ = os.ReadDir(".") }
func TestDocsInSources(t *testing.T) { _, _ = os.ReadDir(".") }
`)},
		"fixture/external_test.go": {Data: []byte(`package fixture_test
import (
	"os"
	"testing"

	"example.com/fixture"
)
func init() {
	fixture.Hook = func() { _, _ = os.ReadDir(".") }
	fixture.Listers["probe"] = func() { _, _ = os.ReadDir(".") }
	fixture.Hooks.Probe = func() { _, _ = os.ReadDir(".") }
}
func TestDocsCallsTheTestedPackage(t *testing.T) { fixture.Sources() }
`)},
		"fixture/aliased_test.go": {Data: []byte(`package fixture_test
import (
	"testing"

	tested "example.com/fixture"
)
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
	want := []string{
		"TestDocsCallsAFieldItsExternalTestsAssign", "TestDocsCallsAMethod", "TestDocsCallsAPointedVariable", "TestDocsCallsAVariable",
		"TestDocsCallsAVariableItsExternalTestsAssign", "TestDocsCallsAnAssignedElement", "TestDocsCallsAnAssignedField",
		"TestDocsCallsAnAssignedVariable", "TestDocsCallsAnElementItsExternalTestsAssign", "TestDocsCallsItsSources",
		"TestDocsCallsTheAliasedPackage", "TestDocsCallsTheTestedPackage", "TestDocsListsAnOpenDirectory",
		"TestDocsReadsDirectly", "TestDocsVisits", "TestDocsWalks",
	}
	assertFixtureWalkers(t, packages["fixture"], want)
}

// assertFixtureWalkers holds the fixture's walking docs checks to want. Go
// ranges over the files in a new order each time, and no order may decide the
// verdict.
func assertFixtureWalkers(t *testing.T, files map[string]*ast.File, want []string) {
	t.Helper()
	for range 32 {
		if found := workingTreeWalkers(files, "example.com/fixture", regexp.MustCompile("^TestDocs")); !slices.Equal(found, want) {
			t.Fatalf("walkers = %v, want %v", found, want)
		}
	}
}

func TestWorkingTreeWalkersMatchListingsNotNames(t *testing.T) {
	module := fstest.MapFS{
		"fixture/listings_test.go": {Data: []byte(`package fixture
import (
	"go/ast"
	"io/fs"
	"os"
	"testing"
	"testing/fstest"
)
func TestDocsWalksSyntax(t *testing.T) { ast.Walk(nil, &ast.File{}) }
func TestDocsWalksInMemory(t *testing.T) {
	module := fstest.MapFS{"a": {}}
	_ = fs.WalkDir(module, ".", nil)
	_, _ = fs.ReadDir(fstest.MapFS{}, ".")
	_, _ = module.Glob("*")
	memory(module)
}
func TestDocsWalksAnotherFilesMemory(t *testing.T) {
	_ = fs.WalkDir(shared, ".", nil)
	_, _ = typed.ReadDir(".")
}
func TestDocsWalksTheModule(t *testing.T) { _ = fs.WalkDir(os.DirFS("."), ".", nil) }
func TestDocsWalksAGivenFilesystem(t *testing.T) { given(fstest.MapFS{}) }
func memory(module fstest.MapFS) {
	var copied fstest.MapFS = module
	var literal = fstest.MapFS{}
	_, _ = fs.Glob(module, "*")
	_, _ = copied.ReadDir(".")
	_, _ = literal.ReadDir(".")
	_, _ = fs.ReadDir(fstest.MapFS(nil), ".")
}
func given(module fs.FS) { _ = fs.WalkDir(module, ".", nil) }
`)},
		"fixture/dotted_test.go": {Data: []byte(`package fixture
import (
	. "path/filepath"
	"testing"
)
type options struct{ Glob string }
func TestDocsListsThroughADotImport(t *testing.T) { _, _ = Glob("*") }
func TestDocsJoinsThroughADotImport(t *testing.T) { _ = Join("a", "b") }
func TestDocsKeysAFieldThroughADotImport(t *testing.T) { _ = options{Glob: "*"} }
`)},
		"fixture/dotsyntax_test.go": {Data: []byte(`package fixture
import (
	. "go/ast"
	"testing"
)
func TestDocsWalksSyntaxThroughADotImport(t *testing.T) { Walk(nil, &File{}) }
`)},
		"fixture/memory_test.go": {Data: []byte(`package fixture
import memoryfs "testing/fstest"
var shared = memoryfs.MapFS{}
var typed memoryfs.MapFS
`)},
		"fixture/external_test.go": {Data: []byte(`package fixture_test
import "os"
var shared = os.DirFS(".")
`)},
		"fixture/external_walk_test.go": {Data: []byte(`package fixture_test
import (
	"io/fs"
	"testing"
)
func TestDocsWalksItsOwnPackagesVariable(t *testing.T) { _ = fs.WalkDir(shared, ".", nil) }
`)},
	}
	packages := directoryGoFiles(t, module, sortedKeys(module), []string{"fixture"})
	assertFixtureWalkers(t, packages["fixture"], []string{
		"TestDocsListsThroughADotImport", "TestDocsWalksAGivenFilesystem", "TestDocsWalksItsOwnPackagesVariable", "TestDocsWalksTheModule",
	})
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
