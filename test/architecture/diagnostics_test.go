package architecture_test

import (
	"bytes"
	"go/ast"
	"go/token"
	"os"
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
		t.Errorf("%s sets a diagnostic code this test cannot resolve to a constant; use a literal or add a justified dynamicDiagnosticCodes entry", place)
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
// that prefix to every argument it receives.
type codeFlow struct {
	packages   map[string]*codePackage
	parameters map[*ast.Field]codeParameter
	flowing    map[codeParameter]string
	fields     map[string]string
	codes      diagnosticCodes
	seen       map[codePlace]bool
	active     map[*ast.Object]bool
}

type codePackage struct {
	sources   []*sourceFile
	functions map[string]*ast.FuncDecl
	methods   map[string][]*ast.FuncDecl
	values    map[string]ast.Expr
	structs   map[string]*ast.StructType
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
		packages: map[string]*codePackage{}, parameters: map[*ast.Field]codeParameter{},
		flowing: map[codeParameter]string{}, fields: map[string]string{"Code": ""},
	}
	for index := range sources {
		flow.index(&sources[index])
	}
	for {
		before := len(flow.flowing) + len(flow.fields)
		flow.codes = diagnosticCodes{emitted: map[string][]codePlace{}, dynamic: map[string]bool{}}
		flow.seen, flow.active = map[codePlace]bool{}, map[*ast.Object]bool{}
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
			values: map[string]ast.Expr{}, structs: map[string]*ast.StructType{}}
		f.packages[source.owner] = pkg
	}
	pkg.sources = append(pkg.sources, source)
	ast.Inspect(source.syntax, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.FuncDecl:
			if typed.Recv == nil {
				pkg.functions[typed.Name.Name] = typed
			} else {
				pkg.methods[typed.Name.Name] = append(pkg.methods[typed.Name.Name], typed)
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
						if position < len(value.Values) {
							pkg.values[name.Name] = value.Values[position]
						}
					}
				}
			}
		}
	}
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
// parameter.
func (f *codeFlow) visit(source *sourceFile) {
	for _, declaration := range source.syntax.Decls {
		switch declared := declaration.(type) {
		case *ast.FuncDecl:
			if declared.Body != nil {
				f.visitScope(codeScope{source: source, body: declared.Body})
			}
		case *ast.GenDecl:
			if declared.Tok == token.VAR {
				for _, spec := range declared.Specs {
					f.visitScope(codeScope{source: source, body: spec})
				}
			}
		}
	}
}

func (f *codeFlow) visitScope(scope codeScope) {
	ast.Inspect(scope.body, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.CompositeLit:
			f.visitLiteral(scope, typed)
		case *ast.AssignStmt:
			for position, target := range typed.Lhs {
				selector, ok := target.(*ast.SelectorExpr)
				if !ok || position >= len(typed.Rhs) {
					continue
				}
				if prefix, found := f.codeField(scope.source.owner, selector.Sel.Name); found {
					f.store(scope, typed.Rhs[position], prefix)
				}
			}
		case *ast.CallExpr:
			for _, callee := range f.callees(scope, typed.Fun) {
				for position, argument := range typed.Args {
					if prefix, flows := f.flowingParameter(callee, position); flows {
						f.store(scope, argument, prefix)
					}
				}
			}
		}
		return true
	})
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
		if imported := importPath(scope.source, typed.X); imported != "" {
			if pkg := f.packages[imported]; pkg != nil && pkg.values[typed.Sel.Name] != nil {
				f.store(codeScope{source: pkg.sources[0]}, pkg.values[typed.Sel.Name], prefix)
				return
			}
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

func (f *codeFlow) storeIdentifier(scope codeScope, identifier *ast.Ident, prefix string) {
	if identifier.Obj == nil {
		if value := f.packages[scope.source.owner].values[identifier.Name]; value != nil {
			f.store(scope, value, prefix)
			return
		}
		f.unresolved(scope, identifier)
		return
	}
	switch declaration := identifier.Obj.Decl.(type) {
	case *ast.Field:
		if parameter, found := f.parameters[declaration]; found {
			for position, name := range declaration.Names {
				if name.Obj == identifier.Obj {
					parameter.index += position
				}
			}
			f.flowing[parameter] = prefix
			return
		}
	case *ast.ValueSpec, *ast.AssignStmt:
		if scope.body == nil || f.active[identifier.Obj] {
			break
		}
		f.active[identifier.Obj] = true
		defer delete(f.active, identifier.Obj)
		assigned := false
		ast.Inspect(scope.body, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.AssignStmt:
				for position, target := range typed.Lhs {
					if name, ok := target.(*ast.Ident); ok && name.Obj == identifier.Obj && position < len(typed.Rhs) {
						assigned = true
						f.store(scope, typed.Rhs[position], prefix)
					}
				}
			case *ast.ValueSpec:
				for position, name := range typed.Names {
					if name.Obj == identifier.Obj && position < len(typed.Values) {
						assigned = true
						f.store(scope, typed.Values[position], prefix)
					}
				}
			}
			return true
		})
		if assigned {
			return
		}
	}
	f.unresolved(scope, identifier)
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

// callees resolves a called expression to the functions it may name: a
// package function, an imported function, a method of the same name in the
// calling package, or a function literal bound to a local or package variable.
func (f *codeFlow) callees(scope codeScope, expression ast.Expr) []*ast.FuncType {
	pkg := f.packages[scope.source.owner]
	switch typed := expression.(type) {
	case *ast.Ident:
		switch declaration := declarationOf(typed).(type) {
		case *ast.AssignStmt:
			for position, target := range declaration.Lhs {
				name, ok := target.(*ast.Ident)
				if ok && name.Obj == typed.Obj && position < len(declaration.Rhs) {
					if literal, ok := declaration.Rhs[position].(*ast.FuncLit); ok {
						return []*ast.FuncType{literal.Type}
					}
				}
			}
			return nil
		case *ast.ValueSpec:
			for position, name := range declaration.Names {
				if name.Obj == typed.Obj && position < len(declaration.Values) {
					if literal, ok := declaration.Values[position].(*ast.FuncLit); ok {
						return []*ast.FuncType{literal.Type}
					}
				}
			}
			return nil
		}
		if function := pkg.functions[typed.Name]; function != nil {
			return []*ast.FuncType{function.Type}
		}
		if literal, ok := pkg.values[typed.Name].(*ast.FuncLit); ok && typed.Obj == nil {
			return []*ast.FuncType{literal.Type}
		}
	case *ast.SelectorExpr:
		if imported := importPath(scope.source, typed.X); imported != "" {
			if other := f.packages[imported]; other != nil && other.functions[typed.Sel.Name] != nil {
				return []*ast.FuncType{other.functions[typed.Sel.Name].Type}
			}
			return nil
		}
		var found []*ast.FuncType
		for _, method := range pkg.methods[typed.Sel.Name] {
			found = append(found, method.Type)
		}
		for _, imported := range scope.source.imports {
			if other := f.packages[strings.TrimPrefix(imported.path, modulePath)]; other != nil {
				for _, method := range other.methods[typed.Sel.Name] {
					found = append(found, method.Type)
				}
			}
		}
		return found
	}
	return nil
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
