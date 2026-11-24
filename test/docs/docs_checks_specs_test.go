package docs_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"slices"
	"strconv"
	"testing"
)

// specPathLiteral matches a string literal that names the specs directory or a
// path under it, relative to the repository or to a test's package directory:
// "specs", "specs/", "../../specs/api.md", a revision path ":specs/a.md", a
// "restore:specs/a.md#anchor" name, a "specs/%s.md" template. A sentence that
// merely mentions a page, such as a failure message, holds a space and does
// not match.
var specPathLiteral = regexp.MustCompile(`^([A-Za-z0-9_.+-]*[:=])?(\.\.?/)*specs(/[A-Za-z0-9_./*?%#-]*)?$`)

// packageDeclaration is one top-level function, method, constant or variable of
// a package: whether its own source names a path under specs/, and the other
// top-level names its source mentions.
type packageDeclaration struct {
	namesSpecPath bool
	mentions      map[string]bool
}

// packageDeclarations collects the top-level declarations of one package's test
// files by name; declarations sharing a name, as methods and build-tagged twins
// do, merge.
func packageDeclarations(files []*ast.File) map[string]*packageDeclaration {
	declarations := map[string]*packageDeclaration{}
	add := func(name string, root ast.Node) {
		declaration := declarations[name]
		if declaration == nil {
			declaration = &packageDeclaration{mentions: map[string]bool{}}
			declarations[name] = declaration
		}
		local := map[string]bool{}
		defined := func(expressions ...ast.Expr) {
			for _, expression := range expressions {
				if identifier, ok := expression.(*ast.Ident); ok {
					local[identifier.Name] = true
				}
			}
		}
		ast.Inspect(root, func(node ast.Node) bool {
			switch node := node.(type) {
			case *ast.AssignStmt:
				if node.Tok == token.DEFINE {
					defined(node.Lhs...)
				}
			case *ast.RangeStmt:
				if node.Tok == token.DEFINE {
					defined(node.Key, node.Value)
				}
			case *ast.FuncType:
				for _, list := range []*ast.FieldList{node.Params, node.Results} {
					if list == nil {
						continue
					}
					for _, field := range list.List {
						for _, identifier := range field.Names {
							local[identifier.Name] = true
						}
					}
				}
			case *ast.ValueSpec:
				if node != root {
					for _, identifier := range node.Names {
						local[identifier.Name] = true
					}
				}
			}
			return true
		})
		ast.Inspect(root, func(node ast.Node) bool {
			switch node := node.(type) {
			case *ast.BasicLit:
				if value, err := strconv.Unquote(node.Value); err == nil && specPathLiteral.MatchString(value) {
					declaration.namesSpecPath = true
				}
			case *ast.Ident:
				if !local[node.Name] {
					declaration.mentions[node.Name] = true
				}
			}
			return true
		})
	}
	for _, file := range files {
		for _, declaration := range file.Decls {
			switch declaration := declaration.(type) {
			case *ast.FuncDecl:
				add(declaration.Name.Name, declaration)
			case *ast.GenDecl:
				for _, spec := range declaration.Specs {
					if value, ok := spec.(*ast.ValueSpec); ok {
						for _, name := range value.Names {
							add(name.Name, value)
						}
					}
				}
			}
		}
	}
	return declarations
}

// specReaders names the tests of one package whose source, or the source of a
// package-level constant, variable or function they mention directly or
// through others, names a path under specs/: the tests that hold spec text
// whatever they are called.
func specReaders(files []*ast.File) []string {
	declarations := packageDeclarations(files)
	reads := map[string]bool{}
	for name, declaration := range declarations {
		reads[name] = declaration.namesSpecPath
	}
	for changed := true; changed; {
		changed = false
		for name, declaration := range declarations {
			if reads[name] {
				continue
			}
			for mention := range declaration.mentions {
				if mention != name && reads[mention] {
					reads[name], changed = true, true
					break
				}
			}
		}
	}
	var found []string
	for _, file := range files {
		for _, name := range declaredTests(file) {
			if reads[name] && !slices.Contains(found, name) {
				found = append(found, name)
			}
		}
	}
	return found
}

// specFixtureTests are the tests that name a path under specs/ without opening
// a page of the repository: each builds a temporary repository or an in-memory
// tree holding a specs/ file as its fixture, or matches such a path in a name
// it was handed. The key is the package directory and the test name; the value
// says why. An excuse for a test that names no such path, or that the list
// holds, fails TestDocsChecksListIsComplete.
var specFixtureTests = map[string]string{
	"internal/workspace/contextfs TestAnInterruptedPublicationLeavesAUsableStoreAndItsRetryConverges": "matches the specs/ prefix of a ledger citation and opens no page",
	"test/architecture TestSourceMapRefusesAnUnmappedFile":                                            "file names of an in-memory tree",
	"test/docs TestIgnoredGuidancePathsFailOnceNothingOutsideThemCitesThem":                           "pages of an in-memory tree",
	"test/docs TestWorkingTreeWalkersFollowWhatEachTestNames":                                         "source of an in-memory module",
	"test/docs TestWorkingTreeWalkersMatchListingsNotNames":                                           "source of an in-memory module",
	"test/tooling TestCheckCommitsHoldsATextOnlySpecCommitToItsLiterals":                              "a temporary repository",
	"test/tooling TestQuickTestComparesWithTheNearerMain":                                             "a temporary repository",
	"test/tooling TestQuickTestFocusTestsChangesWithoutTheirDependents":                               "a temporary repository",
	"test/tooling TestQuickTestListsWhatItWouldTest":                                                  "a temporary repository",
	"test/tooling TestQuickTestSelectsChangedPackagesAndNamesWhatTheBuildOmits":                       "a temporary repository",
	"test/tooling TestQuickTestSelectsEveryPackageWhoseDirectoryHoldsAChangedFile":                    "a temporary repository",
	"test/tooling TestQuickTestSelectsThePackagesOfAPathGitQuotes":                                    "a temporary repository",
	"test/tooling TestQuickTestSelectsThePackagesWhoseTestsAloneImportAChange":                        "a temporary repository",
	"test/tooling TestQuickTestSelectsThePackagesWhoseTestsReadAChangedFile":                          "a temporary repository",
	"test/tooling TestQuickTestTestsThePackageAMovedFileLeft":                                         "a temporary repository",
}

func TestDocsChecksListRefusesAnUnlistedSpecReader(t *testing.T) {
	declared := map[string][]string{"a": {"TestReadsAPage", "TestListed", "TestDocsNamed", "TestFixture", "TestStale", "TestListedFixture", "TestApart", "TestApartStale"}}
	readers := map[string][]string{"a": {"TestReadsAPage", "TestListed", "TestDocsNamed", "TestFixture", "TestListedFixture", "TestApart"}}
	list, err := parseDocsCheckList("a TestListed\na TestDocsNamed\na TestListedFixture\n")
	if err != nil {
		t.Fatal(err)
	}
	outside := map[string]string{"a TestApart": "walks", "a TestApartStale": "walks"}
	fixtures := map[string]string{"a TestFixture": "a temporary repository", "a TestStale": "a temporary repository", "a TestListedFixture": "a temporary repository"}
	want := []string{
		"a TestReadsAPage reads a page under specs/ but testdata/docs-checks.txt does not list it; add the line",
		"specFixtureTests excuses a TestListedFixture, which testdata/docs-checks.txt lists; remove the excuse",
		"specFixtureTests excuses a TestStale, which names no path under specs/; remove it",
		"specReadersOutsideDocsCheck excuses a TestApartStale, which names no path under specs/; remove it",
	}
	if got := docsCheckViolations(list, declared, readers, fixtures, outside); !slices.Equal(got, want) {
		t.Fatalf("violations = %q, want %q", got, want)
	}
}

func TestDocsSpecPathLiteralsMatchEachSpelling(t *testing.T) {
	for literal, want := range map[string]bool{
		"specs": true, "specs/": true, "../../specs/api.md": true, "specs/*.md": true, ":specs/milestones/m2.md": true,
		"build:specs/a.md": true, "restore:specs/a.md#anchor": true, "specs/%s.md": true, "root=specs/api": true,
		"specs/a.md lists a row": false, "myspecs/a.md": false, "see specs/a.md": false, "": false,
	} {
		if got := specPathLiteral.MatchString(literal); got != want {
			t.Errorf("specPathLiteral matches %q = %v, want %v", literal, got, want)
		}
	}
}

func TestDocsSpecReadersFollowAPathThroughConstantsAndHelpers(t *testing.T) {
	const source = `package fixture

import "testing"

const page = "../../specs/api.md"
const sentence = "specs/api.md lists a row"

var table = filepath.Join("..", "..", "specs", "cli", "stops.md")

func read(path string) { _ = path }
func load()            { read(page) }
func glob()            { _ = trackedGlob("specs/*.md") }
func prose()           { _ = sentence }
func local() {
	page := "elsewhere"
	_ = page
}

func TestNamesAPage(t *testing.T)      { read(page) }
func TestNamesATable(t *testing.T)     { _ = table }
func TestCallsAHelper(t *testing.T)    { load() }
func TestGlobsPages(t *testing.T)      { glob() }
func TestMentionsOnlyProse(t *testing.T) { prose() }
func TestShadowsAPage(t *testing.T)    { local() }
func TestShadowsInline(t *testing.T)   { page := 1; _ = page }
func TestReadsNothing(t *testing.T)    {}
func helper(t *testing.T)              { read(page) }
`
	syntax, err := parser.ParseFile(token.NewFileSet(), "fixture_test.go", source, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"TestNamesAPage", "TestNamesATable", "TestCallsAHelper", "TestGlobsPages"}
	if got := specReaders([]*ast.File{syntax}); !slices.Equal(got, want) {
		t.Fatalf("readers = %q, want %q", got, want)
	}
}

// specReadersOutsideDocsCheck are the tests that read a page under specs/ and
// stay out of the list. The key is the package directory and the test name; the
// value says why. An excuse for a test that names no such path, or that the
// list holds, fails TestDocsChecksListIsComplete.
var specReadersOutsideDocsCheck = map[string]string{
	"cmd/bootwright TestEveryCephMatrixFixtureHasATestedRow":   "would join the working-tree walkers that specs/architecture.md names",
	"test/architecture TestCompositionRootBuildsNoDiagnostics": "would join the working-tree walkers that specs/architecture.md names",
}
