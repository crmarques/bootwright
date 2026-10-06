package prerequisites

import (
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// CheckIDs is what a presenter labels, so it names exactly the identities this
// package reports: every check it builds, settles or announces, and every
// setup receipt action.
func TestEveryEmittedCheckIDIsListed(t *testing.T) {
	emitted := emittedCheckIDs(t)
	listed := CheckIDs()
	for _, id := range slices.Sorted(maps.Keys(emitted)) {
		if !slices.Contains(listed, id) {
			t.Errorf("%s reports the check %s, which CheckIDs does not list", emitted[id], id)
		}
	}
	for _, id := range listed {
		if _, found := emitted[id]; !found {
			t.Errorf("CheckIDs lists %s, which this package never reports", id)
		}
	}
}

func emittedCheckIDs(t *testing.T) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	emitted := map[string]string{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		files := token.NewFileSet()
		syntax, err := parser.ParseFile(files, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		record := func(expression ast.Expr) {
			literal, ok := expression.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return
			}
			id, err := strconv.Unquote(literal.Value)
			if err != nil {
				t.Fatal(err)
			}
			if _, found := emitted[id]; !found {
				emitted[id] = files.Position(literal.Pos()).String()
			}
		}
		ast.Inspect(syntax, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.CompositeLit:
				if element := elementType(typed.Type); element != "" {
					for _, value := range typed.Elts {
						if inner, ok := value.(*ast.CompositeLit); ok && inner.Type == nil {
							recordIdentity(element, inner, record)
						}
					}
				}
				recordIdentity(typeName(typed.Type), typed, record)
			case *ast.CallExpr:
				if function, ok := typed.Fun.(*ast.Ident); ok && len(typed.Args) != 0 {
					switch function.Name {
					case "readiness", "unverified", "start":
						record(typed.Args[0])
					}
				}
			}
			return true
		})
	}
	if len(emitted) == 0 {
		t.Fatal("found no reported check identity; the emission shape has changed")
	}
	return emitted
}

// recordIdentity reads a Check's identity, positional or keyed, and a
// SetupAction's keyed one.
func recordIdentity(kind string, literal *ast.CompositeLit, record func(ast.Expr)) {
	if kind != "Check" && kind != "SetupAction" {
		return
	}
	for position, element := range literal.Elts {
		if keyed, ok := element.(*ast.KeyValueExpr); ok {
			if key, ok := keyed.Key.(*ast.Ident); ok && key.Name == "ID" {
				record(keyed.Value)
			}
		} else if kind == "Check" && position == 0 {
			record(element)
		}
	}
}

func typeName(expression ast.Expr) string {
	if identifier, ok := expression.(*ast.Ident); ok {
		return identifier.Name
	}
	return ""
}

func elementType(expression ast.Expr) string {
	if array, ok := expression.(*ast.ArrayType); ok {
		return typeName(array.Elt)
	}
	return ""
}
