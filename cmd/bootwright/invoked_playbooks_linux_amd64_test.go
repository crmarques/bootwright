package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/ansible"
	"github.com/crmarques/bootwright/internal/storage/ceph"
)

// invokedPackage is one production package's syntax: its string constants and
// every function, so an operation passed down a call chain resolves to the
// literals its callers name.
type invokedPackage struct {
	constants map[string]string
	functions []*ast.FuncDecl
}

// invokedPairs reads every lifecycle.Invocation and lifecycle.RunRequest the
// production tree builds and answers each implementation/operation key it can
// send to the runner. A literal whose implementation is no package constant
// (a managed service's definition) is left to the capability contract suite,
// which records what each offered capability actually runs.
func invokedPairs(t *testing.T) map[string]string {
	t.Helper()
	packages := map[string]*invokedPackage{}
	fileset := token.NewFileSet()
	err := filepath.WalkDir(filepath.Join("..", "..", "internal"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		file, err := parser.ParseFile(fileset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		dir := filepath.Dir(path)
		if packages[dir] == nil {
			packages[dir] = &invokedPackage{constants: map[string]string{}}
		}
		for _, declaration := range file.Decls {
			switch declaration := declaration.(type) {
			case *ast.FuncDecl:
				packages[dir].functions = append(packages[dir].functions, declaration)
			case *ast.GenDecl:
				for _, spec := range declaration.Specs {
					value, ok := spec.(*ast.ValueSpec)
					if !ok || declaration.Tok != token.CONST || len(value.Names) != len(value.Values) {
						continue
					}
					for i, name := range value.Names {
						if text, ok := stringLiteral(value.Values[i]); ok {
							packages[dir].constants[name.Name] = text
						}
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	pairs := map[string]string{}
	for dir, pkg := range packages {
		for _, function := range pkg.functions {
			ast.Inspect(function, func(node ast.Node) bool {
				literal, ok := node.(*ast.CompositeLit)
				if !ok || !runnerRequestType(literal.Type) {
					return true
				}
				fields := map[string]ast.Expr{}
				for _, element := range literal.Elts {
					if pair, ok := element.(*ast.KeyValueExpr); ok {
						if key, ok := pair.Key.(*ast.Ident); ok {
							fields[key.Name] = pair.Value
						}
					}
				}
				name, ok := fields["Implementation"].(*ast.Ident)
				implementation, constant := "", false
				if ok {
					implementation, constant = pkg.constants[name.Name]
				}
				if !constant {
					return true
				}
				at := fileset.Position(literal.Pos()).String()
				operations := pkg.operations(function, fields["Operation"], map[string]bool{})
				if len(operations) == 0 {
					t.Errorf("%s: no operation of %s is derivable from %s", at, implementation, dir)
				}
				for _, operation := range operations {
					pairs[implementation+"/"+operation] = at
				}
				return true
			})
		}
	}
	return pairs
}

func runnerRequestType(expression ast.Expr) bool {
	selector, ok := expression.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	qualifier, ok := selector.X.(*ast.Ident)
	return ok && qualifier.Name == "lifecycle" && (selector.Sel.Name == "Invocation" || selector.Sel.Name == "RunRequest")
}

func stringLiteral(expression ast.Expr) (string, bool) {
	literal, ok := expression.(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return "", false
	}
	text, err := strconv.Unquote(literal.Value)
	return text, err == nil
}

// operations resolves an operation expression to every string it can hold: a
// literal, a package constant, a literal assigned to a local, or, for a
// parameter, whatever each caller in the package passes in its place.
func (p *invokedPackage) operations(function *ast.FuncDecl, expression ast.Expr, seen map[string]bool) []string {
	if text, ok := stringLiteral(expression); ok {
		return []string{text}
	}
	name, ok := expression.(*ast.Ident)
	if !ok {
		return nil
	}
	if text, ok := p.constants[name.Name]; ok {
		return []string{text}
	}
	key := function.Name.Name + "." + name.Name
	if seen[key] {
		return nil
	}
	seen[key] = true
	var found []string
	ast.Inspect(function.Body, func(node ast.Node) bool {
		assignment, ok := node.(*ast.AssignStmt)
		if !ok || len(assignment.Lhs) != len(assignment.Rhs) {
			return true
		}
		for i, target := range assignment.Lhs {
			if assigned, ok := target.(*ast.Ident); ok && assigned.Name == name.Name {
				if text, ok := stringLiteral(assignment.Rhs[i]); ok {
					found = append(found, text)
				}
			}
		}
		return true
	})
	position, arity := -1, 0
	for _, field := range function.Type.Params.List {
		for _, parameter := range field.Names {
			if parameter.Name == name.Name {
				position = arity
			}
			arity++
		}
	}
	if position >= 0 {
		for _, caller := range p.functions {
			ast.Inspect(caller, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok || len(call.Args) != arity || calledName(call.Fun) != function.Name.Name {
					return true
				}
				found = append(found, p.operations(caller, call.Args[position], seen)...)
				return true
			})
		}
	}
	slices.Sort(found)
	return slices.Compact(found)
}

func calledName(expression ast.Expr) string {
	switch expression := expression.(type) {
	case *ast.Ident:
		return expression.Name
	case *ast.SelectorExpr:
		return expression.Sel.Name
	}
	return ""
}

// Every implementation and operation the production tree can hand the runner
// is bound to a playbook the embedded collection carries. The pairs come from
// the code that builds each request, so a call no capability contract row
// exercises (a gated Ceph row) still cannot reach a runner that refuses it as
// unrecognized.
func TestEveryInvokedOperationIsBoundToAnEmbeddedPlaybook(t *testing.T) {
	pairs := invokedPairs(t)
	for _, required := range []string{ceph.Implementation + "/apply"} {
		if pairs[required] == "" {
			t.Errorf("the derivation misses %s, which the production tree runs", required)
		}
	}
	bindings := operationPlaybook()
	assets := ansible.Assets()
	for _, key := range slices.Sorted(maps.Keys(pairs)) {
		playbook, bound := bindings[key]
		if !bound {
			t.Errorf("%s runs %s, which no playbook is bound to", pairs[key], key)
			continue
		}
		if _, ok := assets[playbookRoot+playbook]; !ok {
			t.Errorf("%s binds %q, which the embedded collection does not carry", key, playbook)
		}
	}
}
