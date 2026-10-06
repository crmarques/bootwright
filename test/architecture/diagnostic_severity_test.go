package architecture_test

import (
	"go/ast"
	"go/token"
	"strconv"
	"testing"
)

// specs/cli/output.md defines a diagnostic's severity as error or warning, the
// two values human output presents as [FAIL] and [WARN]. Every literal
// production Go gives a Severity field, in a keyed composite literal or an
// assignment, is one of them.
func TestDiagnosticSeveritiesAreErrorOrWarning(t *testing.T) {
	read := 0
	check := func(source sourceFile, value ast.Expr) {
		literal, ok := value.(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return
		}
		read++
		severity, err := strconv.Unquote(literal.Value)
		if err != nil {
			t.Fatal(err)
		}
		if severity != "error" && severity != "warning" {
			t.Errorf("%s gives a diagnostic the severity %q; use error or warning", codePlace{source: &source, position: literal.Pos()}, severity)
		}
	}
	for _, source := range productionSources(t) {
		ast.Inspect(source.syntax, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.KeyValueExpr:
				if key, ok := typed.Key.(*ast.Ident); ok && key.Name == "Severity" {
					check(source, typed.Value)
				}
			case *ast.AssignStmt:
				for index, target := range typed.Lhs {
					if selector, ok := target.(*ast.SelectorExpr); ok && selector.Sel.Name == "Severity" && index < len(typed.Rhs) {
						check(source, typed.Rhs[index])
					}
				}
			}
			return true
		})
	}
	if read < 20 {
		t.Fatalf("read %d severity literals; the diagnostic construction shape has changed", read)
	}
}
