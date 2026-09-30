package architecture_test

import (
	"bytes"
	"go/ast"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The code table under "Diagnostic taxonomy and order" in specs/cli/output.md is
// the registry of diagnostic codes. Every code production Go can emit is a row,
// and every row is a code production Go emits, so a new code is documented with
// its first emission and a retired one leaves the table with its last.
func TestDiagnosticCodesMatchOutputSpec(t *testing.T) {
	documented := documentedDiagnosticCodes(t)
	codes := emittedDiagnosticCodes(productionSources(t))
	for _, place := range codes.unresolved {
		t.Errorf("%s sets a diagnostic code, or passes on a function that sets one, which this test cannot follow to a constant; use a literal and a direct call, or add a justified dynamicDiagnosticCodes entry", place)
	}
	for code, places := range codes.emitted {
		if !documented[code] {
			t.Errorf("%s emits %s, which the specs/cli/output.md registry does not list", places[0], code)
		}
	}
	for code := range documented {
		if _, found := codes.emitted[code]; !found {
			t.Errorf("specs/cli/output.md lists %s, which no production code emits", code)
		}
	}
	for _, position := range sortedKeys(dynamicDiagnosticCodes) {
		if !codes.dynamic[position] {
			t.Errorf("dynamicDiagnosticCodes excuses %s, which no longer sets a diagnostic code; remove it", position)
		}
	}
	if len(documented) < 50 || len(codes.emitted) < 50 {
		t.Fatalf("read %d documented and %d emitted codes; the table or the emission shape has changed", len(documented), len(codes.emitted))
	}
}

func TestDiagnosticCodesAreFoundInPackageVariables(t *testing.T) {
	declaring := compositionSource(t, "internal/fixture", `package fixture
type Diagnostic struct{ Code string }
var refusal = Diagnostic{Code: "fixture.initializer"}
var refuse = func() Diagnostic { return Diagnostic{Code: "fixture.literal"} }
var emit = func(code string) Diagnostic { return Diagnostic{Code: code} }
func use() Diagnostic { return emit("fixture.argument") }
`)
	calling := compositionSource(t, "internal/fixture", "package fixture\nfunc elsewhere() Diagnostic { return emit(\"fixture.elsewhere\") }\n")
	calling.path = "internal/fixture/calling.go"
	codes := emittedDiagnosticCodes([]sourceFile{declaring, calling})
	if found := sortedKeys(codes.emitted); !slices.Equal(found, []string{"fixture.argument", "fixture.elsewhere", "fixture.initializer", "fixture.literal"}) || len(codes.unresolved) != 0 {
		t.Fatalf("emitted = %v, unresolved = %v", found, codes.unresolved)
	}
}

func TestDiagnosticCodesFollowPackageDeclarationsOfTheirFile(t *testing.T) {
	const diagnostic = "package fixture\ntype Diagnostic struct{ Code string }\n"
	for _, row := range []struct {
		name       string
		sources    map[string]string
		emitted    []string
		unresolved int
	}{
		{"a constant read in a function", map[string]string{
			"internal/fixture/fixture.go": diagnostic + `const code = "fixture.constant"
func refuse() Diagnostic { return Diagnostic{Code: code} }`,
		}, []string{"fixture.constant"}, 0},
		{"a variable read by a package-level initializer", map[string]string{
			"internal/fixture/fixture.go": diagnostic + `var code = "fixture.variable"
var refusal = Diagnostic{Code: code}`,
		}, []string{"fixture.variable"}, 0},
		{"an assignment in the reading scope, beside the declaration", map[string]string{
			"internal/fixture/fixture.go": diagnostic + `var code = "fixture.declared"
func refuse() Diagnostic { code = "fixture.assigned"; return Diagnostic{Code: code} }`,
		}, []string{"fixture.assigned", "fixture.declared"}, 0},
		{"a variable its file assigns in another function and a function literal", map[string]string{
			"internal/fixture/fixture.go": diagnostic + `var code = "fixture.initial"
func init() { code = "fixture.function" }
var configure = func() { code = "fixture.literal" }
func refuse() Diagnostic { return Diagnostic{Code: code} }`,
		}, []string{"fixture.function", "fixture.initial", "fixture.literal"}, 0},
		{"a variable another file of its package assigns", map[string]string{
			"internal/fixture/fixture.go": diagnostic + `var code = "fixture.initial"
func refuse() Diagnostic { return Diagnostic{Code: code} }`,
			"internal/fixture/other.go": `package fixture
func configure() { code = "fixture.elsewhere" }
func shadow(other string) { code := "fixture.local"; code = other; _ = code }`,
		}, []string{"fixture.elsewhere", "fixture.initial"}, 0},
		{"a variable whose address is taken", map[string]string{
			"internal/fixture/fixture.go": diagnostic + `var code = "fixture.initial"
func configure(target *string) {}
func init() { configure(&code) }
func refuse() Diagnostic { return Diagnostic{Code: code} }`,
		}, []string{"fixture.initial"}, 1},
		{"a variable written by a tuple, a compound assignment and a range clause", map[string]string{
			"internal/fixture/fixture.go": diagnostic + `var code = "fixture.initial"
func pair() (error, string) { return nil, "fixture.paired" }
func init() { _, code = pair() }
func extend() { code += ".suffix" }
func iterate() { for _, code = range []string{"fixture.ranged"} {} }
func refuse() Diagnostic { return Diagnostic{Code: code} }`,
		}, []string{"fixture.initial"}, 3},
		{"a scope with no body, reached through another package", map[string]string{
			"internal/other/fixture.go": "package other\nconst code = \"other.code\"\nvar Code = code\n",
			"internal/fixture/fixture.go": `package fixture
import "github.com/crmarques/bootwright/internal/other"
type Diagnostic struct{ Code string }
func refuse() Diagnostic { return Diagnostic{Code: other.Code} }`,
		}, []string{"other.code"}, 0},
		{"a declaration that gives no value", map[string]string{
			"internal/fixture/fixture.go": diagnostic + `var code string
func refuse() Diagnostic { return Diagnostic{Code: code} }`,
		}, nil, 1},
	} {
		t.Run(row.name, func(t *testing.T) {
			codes := fixtureCodes(t, row.sources)
			if found := sortedKeys(codes.emitted); !slices.Equal(found, row.emitted) || len(codes.unresolved) != row.unresolved {
				t.Fatalf("emitted = %v, unresolved = %v; want %v and %d unresolved", found, codes.unresolved, row.emitted, row.unresolved)
			}
		})
	}
}

// Each row is a path a code takes to a store, which the registry follows to
// the file giving the code, or reports unresolved in the file it cannot
// follow. A row lists each code with the file placing it, and the file of
// each unresolved place.
func TestDiagnosticCodesFollowEveryPathACodeTakes(t *testing.T) {
	const diagnostic = "package fixture\ntype Diagnostic struct{ Code string }\n"
	const other = `package fixture
import "github.com/crmarques/bootwright/internal/other"
`
	const refuse = "func refuse() Diagnostic { return Diagnostic{Code: code} }\n"
	for _, row := range []struct {
		name       string
		sources    map[string]string
		emitted    []string
		unresolved []string
	}{
		{"a constant another file of its package declares from a second constant", map[string]string{
			"internal/fixture/fixture.go": diagnostic + refuse,
			"internal/fixture/other.go":   "package fixture\nconst base = \"fixture.base\"\nconst code = base\n",
		}, []string{"fixture.base internal/fixture/other.go"}, nil},
		{"constants two build variants declare, read in one variant and in a file both compile", map[string]string{
			"internal/fixture/fixture.go":     diagnostic + refuse,
			"internal/fixture/probe_linux.go": "package fixture\nconst code = \"fixture.linux\"\nconst own = \"fixture.own\"\nfunc probe() Diagnostic { return Diagnostic{Code: own} }\n",
			"internal/fixture/probe_other.go": "//go:build !linux\n\npackage fixture\nconst code = \"fixture.other\"\nconst own = \"fixture.unread\"\n",
		}, []string{"fixture.linux internal/fixture/probe_linux.go", "fixture.other internal/fixture/probe_other.go", "fixture.own internal/fixture/probe_linux.go"}, nil},
		{"a variable the second file of another package declares", map[string]string{
			"internal/other/doc.go":       "package other\n",
			"internal/other/refusals.go":  "package other\nvar Refused = \"other.refused\"\n",
			"internal/fixture/fixture.go": other + "type Diagnostic struct{ Code string }\nfunc refuse() Diagnostic { return Diagnostic{Code: other.Refused} }\n",
		}, []string{"other.refused internal/other/refusals.go"}, nil},
		{"a variable read in another file of its package and assigned in a third", map[string]string{
			"internal/fixture/fixture.go": diagnostic + refuse,
			"internal/fixture/other.go":   "package fixture\nvar code = \"fixture.initial\"\n",
			"internal/fixture/third.go":   "package fixture\nfunc configure() { code = \"fixture.configured\" }\n",
		}, []string{"fixture.configured internal/fixture/third.go", "fixture.initial internal/fixture/other.go"}, nil},
		{"an exported variable a package importing it assigns", map[string]string{
			"internal/other/other.go": "package other\nvar Refused = \"other.initial\"\n",
			"internal/fixture/fixture.go": other + `type Diagnostic struct{ Code string }
func init() { other.Refused = "other.assigned" }
func refuse() Diagnostic { return Diagnostic{Code: other.Refused} }`,
		}, []string{"other.assigned internal/fixture/fixture.go", "other.initial internal/other/other.go"}, nil},
		{"a variable the reading scope writes, declared in another file and assigned in a third", map[string]string{
			"internal/fixture/fixture.go": diagnostic + "func refuse(local bool) Diagnostic { if local { code = \"fixture.local\" }; return Diagnostic{Code: code} }\n",
			"internal/fixture/other.go":   "package fixture\nvar code = \"fixture.initial\"\n",
			"internal/fixture/third.go":   "package fixture\nfunc configure() { code = \"fixture.configured\" }\n",
		}, []string{"fixture.configured internal/fixture/third.go", "fixture.initial internal/fixture/other.go", "fixture.local internal/fixture/fixture.go"}, nil},
		{"an exported variable the importing scope reading it also writes", map[string]string{
			"internal/other/other.go": "package other\nvar Refused = \"other.initial\"\n",
			"internal/fixture/fixture.go": other + `type Diagnostic struct{ Code string }
func refuse(local bool) Diagnostic { if local { other.Refused = "other.local" }; return Diagnostic{Code: other.Refused} }`,
		}, []string{"other.initial internal/other/other.go", "other.local internal/fixture/fixture.go"}, nil},
		{"a variable another file writes by a tuple, a range clause and a taken address", map[string]string{
			"internal/fixture/fixture.go": diagnostic + refuse,
			"internal/fixture/other.go": `package fixture
var code = "fixture.initial"
func pair() (error, string) { return nil, "fixture.paired" }
func configure(target *string) {}
func init() { _, code = pair(); configure(&code) }
func iterate() { for _, code = range []string{"fixture.ranged"} {} }`,
		}, []string{"fixture.initial internal/fixture/other.go"}, []string{"internal/fixture/other.go", "internal/fixture/other.go", "internal/fixture/other.go"}},
		{"a local variable written by a tuple, a compound assignment, a range clause and a taken address", map[string]string{
			"internal/fixture/fixture.go": diagnostic + `func pair() (error, string) { return nil, "fixture.paired" }
func configure(target *string) {}
func refuse() Diagnostic {
	code := "fixture.local"
	_, code = pair()
	code += ".suffix"
	for _, code = range []string{"fixture.ranged"} {}
	configure(&code)
	return Diagnostic{Code: code}
}`,
		}, []string{"fixture.local internal/fixture/fixture.go"}, []string{"internal/fixture/fixture.go", "internal/fixture/fixture.go", "internal/fixture/fixture.go", "internal/fixture/fixture.go"}},
		{"a local variable declared by a tuple and by a range clause", map[string]string{
			"internal/fixture/fixture.go": diagnostic + `func pair() (error, string) { return nil, "fixture.paired" }
func declared() Diagnostic { var _, code = pair(); return Diagnostic{Code: code} }
func ranged() Diagnostic {
	for _, code := range []string{"fixture.ranged"} { return Diagnostic{Code: code} }
	return Diagnostic{Code: "fixture.empty"}
}`,
		}, []string{"fixture.empty internal/fixture/fixture.go"}, []string{"internal/fixture/fixture.go", "internal/fixture/fixture.go"}},
		{"a code field written by a tuple, a compound assignment, a range clause and a taken address", map[string]string{
			"internal/fixture/fixture.go": diagnostic + `func pair() (error, string) { return nil, "fixture.paired" }
func configure(target *string) {}
func refuse() (refusal Diagnostic) {
	_, refusal.Code = pair()
	refusal.Code += ".suffix"
	for _, refusal.Code = range []string{"fixture.ranged"} {}
	configure(&refusal.Code)
	return refusal
}`,
		}, nil, []string{"internal/fixture/fixture.go", "internal/fixture/fixture.go", "internal/fixture/fixture.go", "internal/fixture/fixture.go"}},
		{"a parameter written by a plain assignment, a tuple, a range clause and a taken address", map[string]string{
			"internal/fixture/fixture.go": diagnostic + "func use() Diagnostic { return emit(\"fixture.argument\", true) }\n",
			"internal/fixture/other.go": `package fixture
func pair() (error, string) { return nil, "fixture.paired" }
func configure(target *string) {}
func emit(code string, paired bool) Diagnostic {
	if code == "" { code = "fixture.default" }
	if paired { _, code = pair() }
	for _, code = range []string{"fixture.ranged"} {}
	configure(&code)
	return Diagnostic{Code: code}
}`,
		}, []string{"fixture.argument internal/fixture/fixture.go", "fixture.default internal/fixture/other.go"}, []string{"internal/fixture/other.go", "internal/fixture/other.go", "internal/fixture/other.go"}},
		{"a parameter reassigned inside its function and its function literal, with no call passing it", map[string]string{
			"internal/fixture/fixture.go": diagnostic + `func refuse(code string, nested bool) Diagnostic {
	code = "fixture.reassigned"
	if nested { func() { code = "fixture.nested" }() }
	return Diagnostic{Code: code}
}`,
		}, []string{"fixture.nested internal/fixture/fixture.go", "fixture.reassigned internal/fixture/fixture.go"}, nil},
		{"a function variable another package declares", map[string]string{
			"internal/other/other.go": `package other
type Diagnostic struct{ Code string }
var Refuse = func(code string) Diagnostic { return Diagnostic{Code: code} }`,
			"internal/fixture/fixture.go": other + "func refuse() other.Diagnostic { return other.Refuse(\"fixture.imported\") }\n",
		}, []string{"fixture.imported internal/fixture/fixture.go"}, nil},
		{"a variable another package binds to a named function of its own", map[string]string{
			"internal/other/other.go": `package other
type Diagnostic struct{ Code string }
func build(code string) Diagnostic { return Diagnostic{Code: code} }
var Refuse = build`,
			"internal/fixture/fixture.go": other + "func refuse() other.Diagnostic { return other.Refuse(\"fixture.imported\") }\n",
		}, []string{"fixture.imported internal/fixture/fixture.go"}, nil},
		{"a package function variable bound to a named function or assigned one later", map[string]string{
			"internal/fixture/fixture.go": diagnostic + `func build(code string) Diagnostic { return Diagnostic{Code: code} }
func use() []Diagnostic { return []Diagnostic{refuse("fixture.bound"), reject("fixture.assigned")} }`,
			"internal/fixture/other.go": `package fixture
var refuse = build
var reject func(string) Diagnostic
func init() { reject = build }`,
		}, []string{"fixture.assigned internal/fixture/fixture.go", "fixture.bound internal/fixture/fixture.go"}, nil},
		{"a named function and a method value bound to local variables", map[string]string{
			"internal/fixture/fixture.go": diagnostic + `type refuser struct{}
func (refuser) refuse(code string) Diagnostic { return Diagnostic{Code: code} }
func build(code string) Diagnostic { return Diagnostic{Code: code} }
func use(r refuser) []Diagnostic {
	named, method := build, r.refuse
	return []Diagnostic{named("fixture.named"), method("fixture.method")}
}`,
		}, []string{"fixture.method internal/fixture/fixture.go", "fixture.named internal/fixture/fixture.go"}, nil},
		{"an exported method of a package the calling file does not import, called on a returned value and an embedded field, and passed on as a value", map[string]string{
			"internal/other/other.go": `package other
type Diagnostic struct{ Code string }
type Refuser struct{}
func (Refuser) Refuse(code string) Diagnostic { return Diagnostic{Code: code} }`,
			"internal/third/third.go": `package third
import "github.com/crmarques/bootwright/internal/other"
type Wrapper struct{ other.Refuser }
func Get() other.Refuser { return other.Refuser{} }`,
			"internal/fixture/fixture.go": `package fixture
import "github.com/crmarques/bootwright/internal/third"
func run(any) {}
func use(w third.Wrapper) { third.Get().Refuse("fixture.returned"); w.Refuse("fixture.embedded"); run(w.Refuse) }`,
		}, []string{"fixture.embedded internal/fixture/fixture.go", "fixture.returned internal/fixture/fixture.go"}, []string{"internal/fixture/fixture.go"}},
		{"an unexported method, which only its own package selects", map[string]string{
			"internal/other/other.go": `package other
type Diagnostic struct{ Code string }
type Refuser struct{}
func (Refuser) refuse(code string) Diagnostic { return Diagnostic{Code: code} }
func Use(r Refuser) Diagnostic { return r.refuse("other.own") }`,
			"internal/fixture/fixture.go": other + `type local struct{}
func (local) refuse(string) {}
func use(l local, r other.Refuser) other.Diagnostic { l.refuse("fixture.unrelated"); return other.Use(r) }`,
		}, []string{"other.own internal/other/other.go"}, nil},
		{"a local variable that shadows a package function, bound to another function", map[string]string{
			"internal/fixture/fixture.go": diagnostic + `func build(code string) Diagnostic { return Diagnostic{Code: code} }
func refuse(string) Diagnostic { return Diagnostic{} }
func use() Diagnostic { refuse := build; return refuse("fixture.shadowed") }`,
		}, []string{"fixture.shadowed internal/fixture/fixture.go"}, nil},
		{"a recursive closure declared before its literal, and a literal called where it is written", map[string]string{
			"internal/fixture/fixture.go": diagnostic + `func refuse(depth int) (found []Diagnostic) {
	var visit func(code string, depth int)
	visit = func(code string, depth int) {
		found = append(found, Diagnostic{Code: code})
		if depth > 0 { visit(code, depth-1) }
	}
	visit("fixture.visited", depth)
	return append(found, func(code string) Diagnostic { return Diagnostic{Code: code} }("fixture.invoked"))
}`,
		}, []string{"fixture.invoked internal/fixture/fixture.go", "fixture.visited internal/fixture/fixture.go"}, nil},
		{"a function passed to a function-typed parameter by name, as a method value, through a variable or as a literal", map[string]string{
			"internal/fixture/fixture.go": diagnostic + `type refuser struct{}
func (refuser) refuse(code string) Diagnostic { return Diagnostic{Code: code} }
func build(code string) Diagnostic { return Diagnostic{Code: code} }
func run(emit func(string) Diagnostic) Diagnostic { return emit("fixture.parameter") }
func use(r refuser) []Diagnostic {
	held := build
	return []Diagnostic{run(build), run(r.refuse), run(held), run(func(code string) Diagnostic { return Diagnostic{Code: code} })}
}`,
		}, nil, []string{"internal/fixture/fixture.go", "internal/fixture/fixture.go", "internal/fixture/fixture.go", "internal/fixture/fixture.go"}},
	} {
		t.Run(row.name, func(t *testing.T) {
			codes := fixtureCodes(t, row.sources)
			var emitted, unresolved []string
			for code, places := range codes.emitted {
				for _, place := range places {
					emitted = append(emitted, code+" "+place.source.path)
				}
			}
			for _, place := range codes.unresolved {
				unresolved = append(unresolved, place.source.path)
			}
			slices.Sort(emitted)
			slices.Sort(unresolved)
			if emitted = slices.Compact(emitted); !slices.Equal(emitted, row.emitted) || !slices.Equal(unresolved, row.unresolved) {
				t.Fatalf("emitted = %q, unresolved = %q; want %q and %q", emitted, unresolved, row.emitted, row.unresolved)
			}
		})
	}
}

func fixtureCodes(t *testing.T, files map[string]string) diagnosticCodes {
	t.Helper()
	var sources []sourceFile
	for _, name := range sortedKeys(files) {
		source := compositionSource(t, path.Dir(name), files[name])
		source.path = name
		sources = append(sources, source)
	}
	return emittedDiagnosticCodes(sources)
}

const modulePath = "github.com/crmarques/bootwright/"

// dynamicDiagnosticCodes are the code positions whose value is not a constant
// but can only carry a code already set elsewhere, keyed by file and callee.
var dynamicDiagnosticCodes = map[string]string{
	"internal/cli/output.go escapeDisplayLine": "escapes a diagnostic's existing code for human display",
}

func documentedDiagnosticCodes(t *testing.T) map[string]bool {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "specs", "cli", "output.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, section, found := strings.Cut(string(data), "\n## Diagnostic taxonomy and order\n")
	if !found {
		t.Fatal("specs/cli/output.md has no diagnostic taxonomy section")
	}
	section, _, _ = strings.Cut(section, "\n## ")
	codes := map[string]bool{}
	for _, line := range strings.Split(section, "\n") {
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		code, _, _ := strings.Cut(strings.TrimPrefix(line, "| `"), "`")
		if codes[code] {
			t.Errorf("specs/cli/output.md lists %s twice", code)
		}
		codes[code] = true
	}
	return codes
}

type diagnosticCodes struct {
	emitted    map[string][]codePlace
	unresolved []codePlace
	dynamic    map[string]bool
}

type codePlace struct {
	source   *sourceFile
	position token.Pos
}

// String reports the place as path:line. Each file is parsed on its own file
// set, whose base makes a position's offset one less than the position.
func (p codePlace) String() string {
	data, err := os.ReadFile(filepath.Join("..", "..", p.source.path))
	if err != nil || int(p.position) > len(data)+1 {
		return p.source.path
	}
	return p.source.path + ":" + strconv.Itoa(bytes.Count(data[:p.position-1], []byte("\n"))+1)
}

// codeFlow follows diagnostic codes from their constants to the places that
// store them. A code is stored in a Code field, which diagnostics.Diagnostic and
// api.Issue share, or in a package field, parameter or local variable proven to
// flow into one; a parameter that flows into a store with a literal prefix adds
// that prefix to every argument it receives. A parameter or variable holds the
// values written to it, each placed in the file that writes it.
type codeFlow struct {
	packages   map[string]*codePackage
	methods    map[string][]*ast.FuncDecl
	parameters map[*ast.Field]codeParameter
	globals    map[*ast.Object]codeVariable
	writes     map[codeVariable][]codeWrite
	flowing    map[codeParameter]string
	fields     map[string]string
	codes      diagnosticCodes
	seen       map[codePlace]bool
	active     map[codeVariable]bool
}

// codePackage indexes one package's declarations. Its methods are its
// unexported ones, which Go lets only its own code select; codeFlow.methods
// holds every package's exported ones.
type codePackage struct {
	sources   []*sourceFile
	functions map[string]*ast.FuncDecl
	methods   map[string][]*ast.FuncDecl
	values    map[string][]codeValue
	structs   map[string]*ast.StructType
}

// codeValue is one declaration of a package-level constant or variable: its
// object, the file declaring it and the value it gives, nil when it gives no
// single one. Each build variant of a package declares the name once.
type codeValue struct {
	object   *ast.Object
	source   *sourceFile
	value    ast.Expr
	variable bool
}

// codeVariable names a constant or variable, a package-level one by its
// package and name and a local one by its object. The zero value names none.
type codeVariable struct {
	object      *ast.Object
	owner, name string
}

// codeWrite is one write to a variable: the scope making it, the expression it
// writes through and the value it gives, nil when it gives no single value.
type codeWrite struct {
	scope  codeScope
	target ast.Expr
	value  ast.Expr
}

type codeParameter struct {
	function *ast.FuncType
	index    int
}

type codeScope struct {
	source *sourceFile
	body   ast.Node
}

func emittedDiagnosticCodes(sources []sourceFile) diagnosticCodes {
	flow := &codeFlow{
		packages: map[string]*codePackage{}, methods: map[string][]*ast.FuncDecl{}, parameters: map[*ast.Field]codeParameter{},
		globals: map[*ast.Object]codeVariable{}, writes: map[codeVariable][]codeWrite{},
		flowing: map[codeParameter]string{}, fields: map[string]string{"Code": ""},
	}
	for index := range sources {
		flow.index(&sources[index])
	}
	for _, owner := range sortedKeys(flow.packages) {
		for _, source := range flow.packages[owner].sources {
			for _, scope := range scopesOf(source) {
				flow.indexWrites(scope)
			}
		}
	}
	for {
		before := len(flow.flowing) + len(flow.fields)
		flow.codes = diagnosticCodes{emitted: map[string][]codePlace{}, dynamic: map[string]bool{}}
		flow.seen, flow.active = map[codePlace]bool{}, map[codeVariable]bool{}
		for _, owner := range sortedKeys(flow.packages) {
			for _, source := range flow.packages[owner].sources {
				flow.visit(source)
			}
		}
		if len(flow.flowing)+len(flow.fields) == before {
			return flow.codes
		}
	}
}

func (f *codeFlow) index(source *sourceFile) {
	pkg := f.packages[source.owner]
	if pkg == nil {
		pkg = &codePackage{functions: map[string]*ast.FuncDecl{}, methods: map[string][]*ast.FuncDecl{},
			values: map[string][]codeValue{}, structs: map[string]*ast.StructType{}}
		f.packages[source.owner] = pkg
	}
	pkg.sources = append(pkg.sources, source)
	ast.Inspect(source.syntax, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.FuncDecl:
			switch name := typed.Name.Name; {
			case typed.Recv == nil:
				pkg.functions[name] = typed
			case ast.IsExported(name):
				f.methods[name] = append(f.methods[name], typed)
			default:
				pkg.methods[name] = append(pkg.methods[name], typed)
			}
			f.indexParameters(typed.Type)
		case *ast.FuncLit:
			f.indexParameters(typed.Type)
		case *ast.TypeSpec:
			if structure, ok := typed.Type.(*ast.StructType); ok {
				pkg.structs[typed.Name.Name] = structure
			}
		}
		return true
	})
	for _, declaration := range source.syntax.Decls {
		if general, ok := declaration.(*ast.GenDecl); ok {
			for _, spec := range general.Specs {
				if value, ok := spec.(*ast.ValueSpec); ok {
					for position, name := range value.Names {
						if name.Name == "_" || name.Obj == nil {
							continue
						}
						declared := codeValue{object: name.Obj, source: source, variable: general.Tok == token.VAR}
						if len(value.Values) == len(value.Names) {
							declared.value = value.Values[position]
						}
						pkg.values[name.Name] = append(pkg.values[name.Name], declared)
						f.globals[name.Obj] = codeVariable{owner: source.owner, name: name.Name}
					}
				}
			}
		}
	}
}

// indexWrites records each write the scope makes to a constant or variable: an
// assignment, a declaration, a range clause or a taken address. A local
// declaration that gives no value writes nothing, because its zero value is no
// code; a package-level one is a write that gives no single value.
func (f *codeFlow) indexWrites(scope codeScope) {
	write := func(target, value ast.Expr) {
		if variable := f.variableOf(scope.source, target); variable != (codeVariable{}) {
			f.writes[variable] = append(f.writes[variable], codeWrite{scope: scope, target: target, value: value})
		}
	}
	ast.Inspect(scope.body, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.AssignStmt:
			for position, target := range typed.Lhs {
				if singleValue(typed) {
					write(target, typed.Rhs[position])
				} else {
					write(target, nil)
				}
			}
		case *ast.ValueSpec:
			for position, name := range typed.Names {
				if len(typed.Values) == len(typed.Names) {
					write(name, typed.Values[position])
				} else if _, global := f.globals[name.Obj]; global || len(typed.Values) > 0 {
					write(name, nil)
				}
			}
		case *ast.RangeStmt:
			for _, target := range []ast.Expr{typed.Key, typed.Value} {
				if target != nil {
					write(target, nil)
				}
			}
		case *ast.UnaryExpr:
			if typed.Op == token.AND {
				write(typed.X, nil)
			}
		}
		return true
	})
}

// singleValue reports whether an assignment gives each target one value of its
// own, which a tuple or compound assignment does not.
func singleValue(assignment *ast.AssignStmt) bool {
	return (assignment.Tok == token.ASSIGN || assignment.Tok == token.DEFINE) && len(assignment.Lhs) == len(assignment.Rhs)
}

// variableOf names the constant or variable an expression denotes: an
// identifier, or a selector naming one another package of this module
// declares.
func (f *codeFlow) variableOf(source *sourceFile, expression ast.Expr) codeVariable {
	switch typed := expression.(type) {
	case *ast.Ident:
		if typed.Obj != nil {
			if global, found := f.globals[typed.Obj]; found {
				return global
			}
			if typed.Obj.Kind == ast.Var || typed.Obj.Kind == ast.Con {
				return codeVariable{object: typed.Obj}
			}
		} else if _, found := f.packages[source.owner].values[typed.Name]; found {
			return codeVariable{owner: source.owner, name: typed.Name}
		}
	case *ast.SelectorExpr:
		imported := importPath(source, typed.X)
		if other := f.packages[imported]; imported != "" && other != nil {
			if _, found := other.values[typed.Sel.Name]; found {
				return codeVariable{owner: imported, name: typed.Sel.Name}
			}
		}
	}
	return codeVariable{}
}

func (f *codeFlow) indexParameters(function *ast.FuncType) {
	index := 0
	for _, field := range function.Params.List {
		f.parameters[field] = codeParameter{function: function, index: index}
		index += max(1, len(field.Names))
	}
}

// visit finds every code store in one file's function bodies and package-level
// variable initializers, function literals included: a code field set by a
// composite literal or an assignment, and an argument bound to a flowing
// parameter. A code field written by a tuple or compound assignment, a range
// clause or a taken address is unresolved, and so is a function with a flowing
// parameter that the scope passes on other than by calling it or binding it to
// a variable, because no call through that value is followed.
func (f *codeFlow) visit(source *sourceFile) {
	for _, scope := range scopesOf(source) {
		f.visitScope(scope)
	}
}

// scopesOf returns one file's function bodies and package-level variable
// specifications.
func scopesOf(source *sourceFile) []codeScope {
	var scopes []codeScope
	for _, declaration := range source.syntax.Decls {
		switch declared := declaration.(type) {
		case *ast.FuncDecl:
			if declared.Body != nil {
				scopes = append(scopes, codeScope{source: source, body: declared.Body})
			}
		case *ast.GenDecl:
			if declared.Tok == token.VAR {
				for _, spec := range declared.Specs {
					scopes = append(scopes, codeScope{source: source, body: spec})
				}
			}
		}
	}
	return scopes
}

func (f *codeFlow) visitScope(scope codeScope) {
	bound := map[ast.Node]bool{}
	ast.Inspect(scope.body, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.CompositeLit:
			f.visitLiteral(scope, typed)
			for _, element := range typed.Elts {
				if keyed, ok := element.(*ast.KeyValueExpr); ok {
					bound[keyed.Key] = true
				}
			}
		case *ast.AssignStmt:
			f.visitAssignment(scope, typed, bound)
		case *ast.ValueSpec:
			for position, name := range typed.Names {
				bound[name] = true
				if len(typed.Values) == len(typed.Names) {
					bound[functionExpression(typed.Values[position])] = true
				}
			}
		case *ast.Field:
			for _, name := range typed.Names {
				bound[name] = true
			}
		case *ast.RangeStmt:
			for _, target := range []ast.Expr{typed.Key, typed.Value} {
				if _, found := f.codeTarget(scope, target); found {
					f.unresolved(scope, target)
				}
			}
		case *ast.UnaryExpr:
			if _, found := f.codeTarget(scope, typed.X); found && typed.Op == token.AND {
				f.unresolved(scope, typed.X)
			}
		case *ast.CallExpr:
			bound[functionExpression(typed.Fun)] = true
			for _, callee := range f.functionsOf(scope, typed.Fun) {
				for position, argument := range typed.Args {
					if prefix, flows := f.flowingParameter(callee, position); flows {
						f.store(scope, argument, prefix)
					}
				}
			}
		case *ast.SelectorExpr:
			bound[typed.Sel] = true
			f.escape(scope, typed, bound)
		case *ast.Ident:
			f.escape(scope, typed, bound)
		case *ast.FuncLit:
			f.escape(scope, typed, bound)
		}
		return true
	})
}

// visitAssignment stores what an assignment writes to a code field and marks
// each target, and each value bound to a variable, as no escaping function.
func (f *codeFlow) visitAssignment(scope codeScope, assignment *ast.AssignStmt, bound map[ast.Node]bool) {
	for position, target := range assignment.Lhs {
		bound[target] = true
		blank, _ := target.(*ast.Ident)
		if singleValue(assignment) && (blank != nil && blank.Name == "_" || f.variableOf(scope.source, target) != (codeVariable{})) {
			bound[functionExpression(assignment.Rhs[position])] = true
		}
		prefix, found := f.codeTarget(scope, target)
		switch {
		case !found:
		case singleValue(assignment):
			f.store(scope, assignment.Rhs[position], prefix)
		default:
			f.unresolved(scope, target)
		}
	}
}

// codeTarget returns the prefix of the code field an expression writes
// through, and whether it names one.
func (f *codeFlow) codeTarget(scope codeScope, target ast.Expr) (string, bool) {
	selector, ok := target.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	return f.codeField(scope.source.owner, selector.Sel.Name)
}

// escape reports a function with a flowing parameter that the expression
// passes on as a value: anything but a call of it or a value bound to a
// variable, whose calls functionsOf follows.
func (f *codeFlow) escape(scope codeScope, expression ast.Expr, bound map[ast.Node]bool) {
	if bound[expression] {
		return
	}
	for _, function := range f.functionsOf(scope, expression) {
		for index := range function.Params.NumFields() {
			if _, flows := f.flowingParameter(function, index); flows {
				f.unresolved(scope, expression)
				return
			}
		}
	}
}

// functionExpression returns the function a called or bound expression names,
// without its parentheses and type arguments.
func functionExpression(expression ast.Expr) ast.Expr {
	for {
		switch typed := expression.(type) {
		case *ast.ParenExpr:
			expression = typed.X
		case *ast.IndexExpr:
			expression = typed.X
		case *ast.IndexListExpr:
			expression = typed.X
		default:
			return expression
		}
	}
}

func (f *codeFlow) visitLiteral(scope codeScope, literal *ast.CompositeLit) {
	var names []string
	if structure := f.structOf(scope.source, literal.Type); structure != nil {
		for _, field := range structure.Fields.List {
			if len(field.Names) == 0 {
				names = append(names, "")
			}
			for _, name := range field.Names {
				names = append(names, name.Name)
			}
		}
	}
	for position, element := range literal.Elts {
		if keyed, ok := element.(*ast.KeyValueExpr); ok {
			if key, ok := keyed.Key.(*ast.Ident); ok {
				if prefix, found := f.codeField(scope.source.owner, key.Name); found {
					f.store(scope, keyed.Value, prefix)
				}
			}
		} else if position < len(names) {
			if prefix, found := f.codeField(scope.source.owner, names[position]); found {
				f.store(scope, element, prefix)
			}
		}
	}
}

func (f *codeFlow) codeField(owner, name string) (string, bool) {
	if prefix, found := f.fields[name]; found {
		return prefix, true
	}
	prefix, found := f.fields[owner+"."+name]
	return prefix, found
}

// store records the codes one expression can hold, or marks the parameter,
// field or variable it reads as flowing so that its sources are followed.
func (f *codeFlow) store(scope codeScope, expression ast.Expr, prefix string) {
	switch typed := expression.(type) {
	case *ast.BasicLit:
		value, err := strconv.Unquote(typed.Value)
		if typed.Kind != token.STRING || err != nil {
			f.unresolved(scope, expression)
			return
		}
		f.codes.emitted[prefix+value] = append(f.codes.emitted[prefix+value], codePlace{scope.source, typed.Pos()})
	case *ast.ParenExpr:
		f.store(scope, typed.X, prefix)
	case *ast.BinaryExpr:
		left, ok := typed.X.(*ast.BasicLit)
		if !ok || typed.Op != token.ADD || left.Kind != token.STRING {
			f.unresolved(scope, expression)
			return
		}
		value, _ := strconv.Unquote(left.Value)
		f.store(scope, typed.Y, prefix+value)
	case *ast.Ident:
		f.storeIdentifier(scope, typed, prefix)
	case *ast.SelectorExpr:
		if f.variableOf(scope.source, typed) != (codeVariable{}) {
			f.storeVariable(scope, typed, prefix)
			return
		}
		if known, found := f.codeField(scope.source.owner, typed.Sel.Name); found && known != prefix {
			f.unresolved(scope, expression)
			return
		}
		f.fields[scope.source.owner+"."+typed.Sel.Name] = prefix
	default:
		f.unresolved(scope, expression)
	}
}

// storeIdentifier stores what an identifier reads. A parameter holds every
// argument its function receives and every value its function writes to it.
func (f *codeFlow) storeIdentifier(scope codeScope, identifier *ast.Ident, prefix string) {
	if declaration, ok := declarationOf(identifier).(*ast.Field); ok {
		if parameter, found := f.parameters[declaration]; found {
			for position, name := range declaration.Names {
				if name.Obj == identifier.Obj {
					parameter.index += position
				}
			}
			f.flowing[parameter] = prefix
			f.storeWrites(codeVariable{object: identifier.Obj}, prefix)
			return
		}
	}
	f.storeVariable(scope, identifier, prefix)
}

// storeWrites stores each value written to a variable in the scope writing
// it, and reports each write that gives no single value.
func (f *codeFlow) storeWrites(variable codeVariable, prefix string) {
	if f.active[variable] {
		return
	}
	f.active[variable] = true
	defer delete(f.active, variable)
	for _, write := range f.writes[variable] {
		if write.value == nil {
			f.unresolved(write.scope, write.target)
		} else {
			f.store(write.scope, write.value, prefix)
		}
	}
}

// storeVariable stores what the constant or variable an expression reads can
// hold, each value in the scope that gives it: the value of each package-level
// constant declaration the read can denote, or every value written to a
// variable anywhere, the reading scope's own writes and a package-level one's
// declarations included. A write that gives no single value is unresolved,
// and so is a read of a variable nothing writes.
func (f *codeFlow) storeVariable(scope codeScope, read ast.Expr, prefix string) {
	variable := f.variableOf(scope.source, read)
	if variable == (codeVariable{}) || f.active[variable] {
		f.unresolved(scope, read)
		return
	}
	f.active[variable] = true
	defer delete(f.active, variable)
	if variable.object == nil {
		varying := false
		for _, declared := range f.declarationsRead(read, variable) {
			switch {
			case declared.variable:
				varying = true
			case declared.value == nil:
				f.unresolved(scope, read)
			default:
				f.store(codeScope{source: declared.source}, declared.value, prefix)
			}
		}
		if !varying {
			return
		}
	}
	writes := f.writes[variable]
	if len(writes) == 0 {
		f.unresolved(scope, read)
		return
	}
	for _, write := range writes {
		if write.value == nil {
			f.unresolved(write.scope, write.target)
		} else {
			f.store(write.scope, write.value, prefix)
		}
	}
}

// declarationsRead returns the declarations of a package-level constant or
// variable that a read can denote: the one in the read's own file, which the
// parser resolves the read to, or else every build variant's.
func (f *codeFlow) declarationsRead(read ast.Expr, variable codeVariable) []codeValue {
	declarations := f.packages[variable.owner].values[variable.name]
	if identifier, ok := read.(*ast.Ident); ok && identifier.Obj != nil {
		for _, declared := range declarations {
			if declared.object == identifier.Obj {
				return []codeValue{declared}
			}
		}
	}
	return declarations
}

func (f *codeFlow) unresolved(scope codeScope, expression ast.Expr) {
	if call, ok := expression.(*ast.CallExpr); ok {
		if name, ok := call.Fun.(*ast.Ident); ok && dynamicDiagnosticCodes[scope.source.path+" "+name.Name] != "" {
			f.codes.dynamic[scope.source.path+" "+name.Name] = true
			return
		}
	}
	place := codePlace{scope.source, expression.Pos()}
	if !f.seen[place] {
		f.seen[place] = true
		f.codes.unresolved = append(f.codes.unresolved, place)
	}
}

// functionsOf resolves an expression to the functions it may denote: a
// package or imported function, a method of that name that the call can
// select (an unexported one of the calling package, or an exported one of any
// package, since without types a value's package is unknown), a function
// literal, or a variable holding one of these, followed through every value
// written to it. A function a parameter receives is not followed; escape
// reports each one with a flowing parameter where it is passed on.
func (f *codeFlow) functionsOf(scope codeScope, expression ast.Expr) []*ast.FuncType {
	pkg := f.packages[scope.source.owner]
	switch typed := functionExpression(expression).(type) {
	case *ast.FuncLit:
		return []*ast.FuncType{typed.Type}
	case *ast.Ident:
		if typed.Obj == nil || typed.Obj.Kind == ast.Fun {
			if function := pkg.functions[typed.Name]; function != nil {
				return []*ast.FuncType{function.Type}
			}
		}
	case *ast.SelectorExpr:
		imported := importPath(scope.source, typed.X)
		if imported == "" {
			methods := pkg.methods[typed.Sel.Name]
			if ast.IsExported(typed.Sel.Name) {
				methods = f.methods[typed.Sel.Name]
			}
			var found []*ast.FuncType
			for _, method := range methods {
				found = append(found, method.Type)
			}
			return found
		}
		if other := f.packages[imported]; other != nil && other.functions[typed.Sel.Name] != nil {
			return []*ast.FuncType{other.functions[typed.Sel.Name].Type}
		}
	}
	variable := f.variableOf(scope.source, functionExpression(expression))
	if variable == (codeVariable{}) || f.active[variable] {
		return nil
	}
	f.active[variable] = true
	defer delete(f.active, variable)
	var found []*ast.FuncType
	for _, write := range f.writes[variable] {
		if write.value != nil {
			found = append(found, f.functionsOf(write.scope, write.value)...)
		}
	}
	return found
}

func (f *codeFlow) flowingParameter(function *ast.FuncType, position int) (string, bool) {
	prefix, flows := f.flowing[codeParameter{function: function, index: position}]
	return prefix, flows
}

func (f *codeFlow) structOf(source *sourceFile, expression ast.Expr) *ast.StructType {
	switch typed := expression.(type) {
	case *ast.Ident:
		return f.packages[source.owner].structs[typed.Name]
	case *ast.SelectorExpr:
		if other := f.packages[importPath(source, typed.X)]; other != nil {
			return other.structs[typed.Sel.Name]
		}
	}
	return nil
}

func declarationOf(identifier *ast.Ident) any {
	if identifier.Obj == nil {
		return nil
	}
	return identifier.Obj.Decl
}

// importPath returns the module-relative directory an identifier imports, or
// "" when it is not an import of this module.
func importPath(source *sourceFile, expression ast.Expr) string {
	identifier, ok := expression.(*ast.Ident)
	if !ok || identifier.Obj != nil {
		return ""
	}
	for _, imported := range source.imports {
		if imported.alias == identifier.Name {
			return strings.TrimPrefix(imported.path, modulePath)
		}
	}
	return ""
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
