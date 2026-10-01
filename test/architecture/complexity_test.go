package architecture_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"testing"
)

// functionLineLimit is the length past which a production function stops
// explaining itself through its own structure. It is not a style rule: a body
// this long has phases a reader must hold in their head, and the fix is to name
// them.
const functionLineLimit = 100

// longFunctionsAwaitingSplit are the bodies that already exceed the limit. The
// list is empty and only shrinks: a new entry means a function grew past it
// instead of being split.
func longFunctionsAwaitingSplit() []string {
	return []string{}
}

func TestProductionFunctionsStayWithinTheLineLimit(t *testing.T) {
	known := longFunctionsAwaitingSplit()
	var seen []string
	for _, source := range productionSources(t) {
		fileSet := token.NewFileSet()
		syntax, err := parser.ParseFile(fileSet, filepath.Join("..", "..", source.path), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range syntax.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			lines := fileSet.Position(function.Body.End()).Line - fileSet.Position(function.Body.Pos()).Line + 1
			if lines <= functionLineLimit {
				continue
			}
			name := source.path + "." + function.Name.Name
			seen = append(seen, name)
			if !slices.Contains(known, name) {
				t.Errorf("%s is %d lines; split it into named phases or add it to longFunctionsAwaitingSplit with its reason", name, lines)
			}
		}
	}
	for index, name := range known {
		if slices.Contains(known[:index], name) {
			t.Errorf("%s is listed twice in longFunctionsAwaitingSplit; remove the copy", name)
		} else if !slices.Contains(seen, name) {
			t.Errorf("%s is no longer over the limit; remove it from longFunctionsAwaitingSplit", name)
		}
	}
}
