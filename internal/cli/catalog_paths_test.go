package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The catalog is the only place a command path is matched. Another file that
// compares a value with a command path, as a switch case or with == or !=,
// keeps a second list of commands that no entry states.
var pathMatchExemptFiles = map[string]string{
	"catalog.go":      "the catalog states every path",
	"confirmation.go": "the confirmation actions are the application layer's words, which a consumer sends and which happen to repeat a command path",
}

func TestNoCommandPathIsMatchedOutsideTheCatalog(t *testing.T) {
	paths := map[string]bool{}
	for _, spec := range commandCatalog() {
		paths[spec.path] = true
		paths["bootwright "+spec.path] = true
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") || pathMatchExemptFiles[file] != "" {
			continue
		}
		checked++
		fileSet := token.NewFileSet()
		parsed, err := parser.ParseFile(fileSet, file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		report := func(position token.Pos, expression ast.Expr) {
			literal, ok := expression.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return
			}
			if value, err := strconv.Unquote(literal.Value); err == nil && paths[value] {
				t.Errorf("%s matches the command path %s; state the fact on the catalog entry", fileSet.Position(position), literal.Value)
			}
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			switch node := node.(type) {
			case *ast.CaseClause:
				for _, expression := range node.List {
					report(expression.Pos(), expression)
				}
			case *ast.BinaryExpr:
				if node.Op == token.EQL || node.Op == token.NEQ {
					report(node.X.Pos(), node.X)
					report(node.Y.Pos(), node.Y)
				}
			}
			return true
		})
	}
	if checked < 50 {
		t.Fatalf("checked %d files; the package has changed shape", checked)
	}
	for file, reason := range pathMatchExemptFiles {
		if reason == "" {
			t.Errorf("%s is exempt without a reason", file)
		}
	}
}
