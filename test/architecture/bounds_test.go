package architecture_test

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// documentedBound ties one bound a spec states to the Go code that enforces
// it. The phrase is the spec text around the value, with %s where the value
// appears, and it must occur exactly once. The constant names a package-level
// constant, or "name()" for the deadline that function passes to
// context.WithTimeout when the code has not named it.
type documentedBound struct {
	spec, phrase, source, constant string
}

func documentedBounds() []documentedBound {
	const (
		contexts     = "specs/contexts.md"
		store        = "internal/workspace/contextfs/store.go"
		bundles      = "internal/workspace/contextfs/controller_bundles_linux_amd64.go"
		operations   = "internal/reconciliation/operationstore/records.go"
		lifecycleRun = "internal/reconciliation/ansiblerunner/process_linux_amd64.go"
		setupRun     = "internal/controller/ansiblelocal/runner_linux_amd64.go"
	)
	return []documentedBound{
		{contexts, "| Active or reserved context names | %s |", store, "maxContexts"},
		{contexts, "The %s-name bound applies", store, "maxContexts"},
		{contexts, "| Revisions per context | %s |", store, "maxRevisions"},
		{contexts, "| Retained [controller bundle namespaces](contexts/controller-record.md#bounds) | %s |", bundles, "maxControllerBundles"},
		{"specs/contexts/controller-record.md", "There are at most %s retained bundle", bundles, "maxControllerBundles"},
		{contexts, "| Lifecycle operations one context retains | %s |", operations, "MaxOperations"},
		{contexts, "| One lifecycle adapter invocation | %s |", lifecycleRun, "invocationTimeout"},
		{contexts, "| One controller Ansible run: setup, its recovery or a controller-stage client installation | %s |", setupRun, "runProcess()"},
	}
}

func TestDocumentedBoundsMatchCode(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, bound := range documentedBounds() {
		t.Run(bound.constant+" in "+filepath.Base(bound.spec), func(t *testing.T) {
			spec, err := os.ReadFile(filepath.Join(root, bound.spec))
			if err != nil {
				t.Fatal(err)
			}
			value, duration := boundValue(t, boundExpression(t, filepath.Join(root, bound.source), bound.constant))
			phrase := fmt.Sprintf(bound.phrase, renderBound(t, value, duration))
			if count := strings.Count(string(spec), phrase); count != 1 {
				t.Errorf("%s states %q %d times, want once: %s in %s changed, or the spec did", bound.spec, phrase, count, bound.constant, bound.source)
			}
		})
	}
}

// boundExpression finds the expression that defines a bound: a package-level
// constant's value, or the one deadline a function passes to
// context.WithTimeout.
func boundExpression(t *testing.T, source, name string) ast.Expr {
	t.Helper()
	syntax, err := parser.ParseFile(token.NewFileSet(), source, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var found []ast.Expr
	function, deadline := strings.CutSuffix(name, "()")
	for _, declaration := range syntax.Decls {
		switch declared := declaration.(type) {
		case *ast.GenDecl:
			if deadline || declared.Tok != token.CONST {
				continue
			}
			for _, spec := range declared.Specs {
				value := spec.(*ast.ValueSpec)
				for index, identifier := range value.Names {
					if identifier.Name == name && index < len(value.Values) {
						found = append(found, value.Values[index])
					}
				}
			}
		case *ast.FuncDecl:
			if !deadline || declared.Name.Name != function || declared.Body == nil {
				continue
			}
			ast.Inspect(declared.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if ok && isSelector(call.Fun, "context", "WithTimeout") && len(call.Args) == 2 {
					found = append(found, call.Args[1])
				}
				return true
			})
		}
	}
	if len(found) != 1 {
		t.Fatalf("%s defines %s %d times, want once", source, name, len(found))
	}
	return found[0]
}

func isSelector(expression ast.Expr, pkg, name string) bool {
	selector, ok := expression.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != name {
		return false
	}
	identifier, ok := selector.X.(*ast.Ident)
	return ok && identifier.Name == pkg
}

// boundValue folds a constant expression of literals, time units and
// arithmetic, and reports whether it is a duration.
func boundValue(t *testing.T, expression ast.Expr) (constant.Value, bool) {
	t.Helper()
	units := map[string]time.Duration{
		"Nanosecond": time.Nanosecond, "Microsecond": time.Microsecond, "Millisecond": time.Millisecond,
		"Second": time.Second, "Minute": time.Minute, "Hour": time.Hour,
	}
	switch node := expression.(type) {
	case *ast.BasicLit:
		return constant.MakeFromLiteral(node.Value, node.Kind, 0), false
	case *ast.ParenExpr:
		return boundValue(t, node.X)
	case *ast.SelectorExpr:
		if unit, ok := units[node.Sel.Name]; ok && isSelector(node, "time", node.Sel.Name) {
			return constant.MakeInt64(int64(unit)), true
		}
	case *ast.BinaryExpr:
		x, xDuration := boundValue(t, node.X)
		y, yDuration := boundValue(t, node.Y)
		if node.Op == token.SHL || node.Op == token.SHR {
			shift, ok := constant.Uint64Val(y)
			if !ok {
				t.Fatalf("shift %s is not a constant count", y)
			}
			return constant.Shift(x, node.Op, uint(shift)), xDuration
		}
		return constant.BinaryOp(x, node.Op, y), xDuration || yDuration
	}
	t.Fatalf("bound expression %T is not a supported constant expression", expression)
	return nil, false
}

// renderBound writes a value the way the specs state it: a count in decimal,
// and a duration in the largest whole unit.
func renderBound(t *testing.T, value constant.Value, duration bool) string {
	t.Helper()
	number, exact := constant.Int64Val(value)
	if !exact {
		t.Fatalf("bound %s is not an integer", value)
	}
	if !duration {
		return fmt.Sprint(number)
	}
	for _, unit := range []struct {
		size time.Duration
		name string
	}{{time.Hour, "hour"}, {time.Minute, "minute"}, {time.Second, "second"}} {
		if count := time.Duration(number) / unit.size; count > 0 && time.Duration(number)%unit.size == 0 {
			if count == 1 {
				return "1 " + unit.name
			}
			return fmt.Sprint(int64(count)) + " " + unit.name + "s"
		}
	}
	t.Fatalf("duration %v is not a whole number of seconds", time.Duration(number))
	return ""
}
